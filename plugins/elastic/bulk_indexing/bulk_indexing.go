package bulk_indexing

import (
	"fmt"
	"infini.sh/framework/core/config"
	"infini.sh/framework/core/rate"
	"infini.sh/framework/core/rotate"
	"infini.sh/framework/lib/bytebufferpool"
	"runtime"
	"sync"
	"time"

	log "github.com/cihub/seelog"
	"infini.sh/framework/core/elastic"
	"infini.sh/framework/core/errors"
	"infini.sh/framework/core/global"
	"infini.sh/framework/core/pipeline"
	"infini.sh/framework/core/queue"
	"infini.sh/framework/core/stats"
	"infini.sh/framework/core/util"
)

//#操作合并任务
//写入到本地一个队列，hash 散列
//【内存、磁盘、Kafka 三种持久化选项】以分片为单位合并数据，写本地磁盘队列，一个分片一个队列
//读取分片数据，发送到所在节点上

//#bulk 发送任务
//以节点为单位，然后以主分片为单位进行流量合并发送
//一个节点一个 go 协程，用于发送数据

//#将写模式改成拉模式，由各个分片主动去拉数据
//各个分片的数据线本地压缩好，变成固定大小的包
//由各个节点所在的 agent，压缩传输过去之后，本地快速重建
//调用目标节点所在的 agent 服务，rpc 远程写磁盘数据，然后目标服务器本地读取磁盘队列。

//读各个分片的数据，写 es

//处理 bulk 格式的数据索引。
type BulkIndexingProcessor struct {
	bufferPool           *bytebufferpool.Pool
	config               *Config
	runningConfigs       map[string]*queue.Config
	wg                   sync.WaitGroup
	inFlightQueueConfigs sync.Map
	detectorRunning      bool
	id                   string
}

type Config struct {
	NumOfWorkers         int `config:"worker_size"`
	IdleTimeoutInSecond  int `config:"idle_timeout_in_seconds"`
	MaxConnectionPerHost int `config:"max_connection_per_node"`

	Queues map[string]interface{} `config:"queues,omitempty"`

	Consumer queue.ConsumerConfig `config:"consumer"`

	MaxWorkers int `config:"max_worker_size"`

	DetectActiveQueue  bool `config:"detect_active_queue"`
	DetectIntervalInMs int  `config:"detect_interval"`

	ValidateRequest   bool `config:"valid_request"`
	SkipEmptyQueue    bool `config:"skip_empty_queue"`
	SkipOnMissingInfo bool `config:"skip_info_missing"`

	RotateConfig rotate.RotateConfig `config:"rotate"`

	BulkConfig elastic.BulkProcessorConfig `config:"bulk"`

	Elasticsearch string `config:"elasticsearch,omitempty"`

	WaitingAfter []string `config:"waiting_after"`
}

func init() {
	pipeline.RegisterProcessorPlugin("bulk_indexing", New)
}

func New(c *config.Config) (pipeline.Processor, error) {
	cfg := Config{
		NumOfWorkers:         1,
		MaxWorkers:           10,
		MaxConnectionPerHost: 1,
		IdleTimeoutInSecond:  5,
		DetectIntervalInMs:   10000,
		Queues:               map[string]interface{}{},

		Consumer: queue.ConsumerConfig{
			Group:            "group-001",
			Name:             "consumer-001",
			FetchMinBytes:    1,
			FetchMaxMessages: 100,
			FetchMaxWaitMs:   10000,
		},

		DetectActiveQueue: true,
		ValidateRequest:   false,
		SkipEmptyQueue:    true,
		SkipOnMissingInfo: false,
		RotateConfig:      rotate.DefaultConfig,
		BulkConfig:        elastic.DefaultBulkProcessorConfig,
	}

	if err := c.Unpack(&cfg); err != nil {
		log.Error(err)
		return nil, fmt.Errorf("failed to unpack the configuration of flow_runner processor: %s", err)
	}

	runner := BulkIndexingProcessor{
		id:                   util.GetUUID(),
		config:               &cfg,
		runningConfigs:       map[string]*queue.Config{},
		inFlightQueueConfigs: sync.Map{},
	}

	bulkSizeInByte := runner.config.BulkConfig.GetBulkSizeInBytes()
	estimatedBulkSizeInByte := bulkSizeInByte + (bulkSizeInByte / 3)
	runner.bufferPool = bytebufferpool.NewPool(uint64(estimatedBulkSizeInByte), uint64(bulkSizeInByte*2))

	runner.wg = sync.WaitGroup{}

	return &runner, nil
}

func (processor *BulkIndexingProcessor) Name() string {
	return "bulk_indexing"
}

func (processor *BulkIndexingProcessor) Process(c *pipeline.Context) error {

	defer func() {
		if !global.Env().IsDebug {
			if r := recover(); r != nil {
				var v string
				switch r.(type) {
				case error:
					v = r.(error).Error()
				case runtime.Error:
					v = r.(runtime.Error).Error()
				case string:
					v = r.(string)
				}
				log.Error("error in bulk indexing processor,", v)
			}
		}
		log.Trace("exit bulk indexing processor")
	}()

	//handle updates
	if processor.config.DetectActiveQueue {
		log.Tracef("detector running [%v]", processor.detectorRunning)
		if !processor.detectorRunning {
			processor.detectorRunning = true
			processor.wg.Add(1)
			go func(c *pipeline.Context) {
				log.Tracef("init detector for active queue [%v] ", processor.id)
				defer func() {
					if !global.Env().IsDebug {
						if r := recover(); r != nil {
							var v string
							switch r.(type) {
							case error:
								v = r.(error).Error()
							case runtime.Error:
								v = r.(runtime.Error).Error()
							case string:
								v = r.(string)
							}
							log.Error("error in bulk indexing processor,", v)
						}
					}
					processor.detectorRunning = false
					log.Debug("exit detector for active queue")
					processor.wg.Done()
				}()

				for {
					if c.IsCanceled() {
						return
					}

					log.Tracef("inflight queues: %v", util.MapLength(&processor.inFlightQueueConfigs))

					if global.Env().IsDebug {
						processor.inFlightQueueConfigs.Range(func(key, value interface{}) bool {
							log.Tracef("inflight queue:%v", key)
							return true
						})
					}

					cfgs := queue.GetQueuesFilterByLabel(processor.config.Queues)
					for _, v := range cfgs {
						if c.IsCanceled() {
							return
						}
						//if have depth and not in in flight
						if queue.HasLag(v) {
							_, ok := processor.inFlightQueueConfigs.Load(v.Id)
							if !ok {
								log.Tracef("detecting new queue: %v", v.Name)
								processor.HandleQueueConfig(v, c)
							}
						}
					}
					if processor.config.DetectIntervalInMs > 0 {
						time.Sleep(time.Millisecond * time.Duration(processor.config.DetectIntervalInMs))
					}
				}
			}(c)
		}
	} else {
		cfgs := queue.GetQueuesFilterByLabel(processor.config.Queues)
		log.Debugf("filter queue by:%v, num of queues:%v", processor.config.Queues, len(cfgs))
		for _, v := range cfgs {
			log.Tracef("checking queue: %v", v)
			processor.HandleQueueConfig(v, c)
		}
	}

	processor.wg.Wait()

	return nil
}

func (processor *BulkIndexingProcessor) HandleQueueConfig(v *queue.Config, c *pipeline.Context) {

	if processor.config.SkipEmptyQueue {
		if !queue.HasLag(v) {
			if global.Env().IsDebug {
				log.Tracef("skip empty queue:[%v]", v.Name)
			}
			return
		}
	}

	elasticsearch, ok := v.Labels["elasticsearch"]
	if !ok {
		if processor.config.Elasticsearch == "" {
			log.Errorf("label [elasticsearch] was not found in: %v", v)
			return
		} else {
			elasticsearch = processor.config.Elasticsearch
		}
	}

	meta := elastic.GetMetadata(util.ToString(elasticsearch))
	if meta == nil {
		log.Debugf("metadata for [%v] is nil", elasticsearch)
		return
	}

	level, ok := v.Labels["level"]

	if level == "node" {
		nodeID, ok := v.Labels["node_id"]
		if ok {
			nodeInfo := meta.GetNodeInfo(util.ToString(nodeID))
			if nodeInfo != nil {
				host := nodeInfo.GetHttpPublishHost()
				processor.wg.Add(1)
				go processor.NewBulkWorker("bulk_indexing_"+host, c, processor.config.BulkConfig.GetBulkSizeInBytes(), v, host)
				return
			} else {
				log.Debugf("node info not found: %v", nodeID)
			}
		} else {
			log.Debugf("node_id not found: %v", v)
		}
	} else if level == "shard" || level == "partition" {
		index, ok := v.Labels["index"]
		if ok {
			routingTable, err := meta.GetIndexRoutingTable(util.ToString(index))
			if err != nil {
				if rate.GetRateLimiter("error", err.Error(), 1, 1, time.Second*3).Allow() {
					log.Warn(err)
				}
				return
			}
			shard, ok := v.Labels["shard"]
			if ok {
				shards, ok := routingTable[util.ToString(shard)]
				if ok {
					for _, x := range shards {
						if x.Primary {
							//each primary shard has a goroutine, or run by one goroutine
							if x.Node != "" {
								nodeInfo := meta.GetNodeInfo(x.Node)
								if nodeInfo != nil {
									nodeHost := nodeInfo.GetHttpPublishHost()
									processor.wg.Add(1)
									go processor.NewBulkWorker("bulk_indexing_"+nodeHost, c, processor.config.BulkConfig.GetBulkSizeInBytes(), v, nodeHost)
									return
								} else {
									log.Debugf("nodeInfo not found: %v", v)
								}
							} else {
								log.Debugf("nodeID not found: %v", v)
							}
							if processor.config.SkipOnMissingInfo {
								return
							}
						}
					}
				} else {
					log.Debugf("routing table not found: %v", v)
				}
			} else {
				log.Debugf("shard not found: %v", v)
			}
		} else {
			log.Debugf("index not found: %v", v)
		}
		if processor.config.SkipOnMissingInfo {
			return
		}
	}

	host := meta.GetActiveHost()
	log.Debugf("random choose node [%v] to consume queue [%v]", host, v.Id)
	processor.wg.Add(1)
	go processor.NewBulkWorker("bulk_indexing_"+host, c, processor.config.BulkConfig.GetBulkSizeInBytes(), v, host)
}

func (processor *BulkIndexingProcessor) NewBulkWorker(tag string, ctx *pipeline.Context, bulkSizeInByte int, qConfig *queue.Config, host string) {

	defer func() {
		if !global.Env().IsDebug {
			if r := recover(); r != nil {
				var v string
				switch r.(type) {
				case error:
					v = r.(error).Error()
				case runtime.Error:
					v = r.(runtime.Error).Error()
				case string:
					v = r.(string)
				}
				log.Error("error in bulk indexing processor,", v)
			}
		}
		processor.wg.Done()
		log.Trace("exit bulk indexing processor")
	}()

	if !queue.HasLag(qConfig) {
		return
	}

	key := fmt.Sprintf("%v", qConfig.Id)

	if processor.config.MaxWorkers > 0 && util.MapLength(&processor.inFlightQueueConfigs) > processor.config.MaxWorkers {
		log.Debugf("reached max num of workers, skip init [%v]", qConfig.Name)
		time.Sleep(500 * time.Millisecond)
		return
	}

	var workerID = util.GetUUID()
	_, exists := processor.inFlightQueueConfigs.Load(key)
	if exists {
		log.Errorf("[%v], queue [%v] has more then one consumer", tag, qConfig.Id)
		return
	}

	processor.inFlightQueueConfigs.Store(key, workerID)
	log.Debugf("starting worker:[%v], queue:[%v], host:[%v]", workerID, qConfig.Name, host)

	mainBuf := elastic.AcquireBulkBuffer()
	mainBuf.Queue = qConfig.Id
	defer elastic.ReturnBulkBuffer(mainBuf)

	var bulkProcessor elastic.BulkProcessor
	var esClusterID string
	var meta *elastic.ElasticsearchMetadata
	var initOffset string
	var offset string
	var consumer = queue.GetOrInitConsumerConfig(qConfig.Id, processor.config.Consumer.Group, processor.config.Consumer.Name)
	var skipFinalDocsProcess bool

	defer func() {
		if !global.Env().IsDebug {
			if r := recover(); r != nil {
				var v string
				switch r.(type) {
				case error:
					v = r.(error).Error()
				case runtime.Error:
					v = r.(runtime.Error).Error()
				case string:
					v = r.(string)
				}
				log.Errorf("error in bulk_indexing worker[%v],queue:[%v], offset:[%v]->[%v],%v", workerID, qConfig.Id, initOffset, offset, v)
				ctx.Failed()
				skipFinalDocsProcess = true
			}
		}

		processor.inFlightQueueConfigs.Delete(key)

		if skipFinalDocsProcess {
			return
		}

		//cleanup buffer before exit worker
		continueNext := processor.submitBulkRequest(tag, esClusterID, meta, host, bulkProcessor, mainBuf)
		mainBuf.Reset()
		if continueNext {
			if offset != "" && initOffset != offset {
				ok, err := queue.CommitOffset(qConfig, consumer, offset)
				if !ok || err != nil {
					panic(err)
				}
			}
		} else {
			if global.Env().IsDebug {
				log.Errorf("error between queue:[%v] offset [%v]-[%v]", qConfig.Id, initOffset, offset)
			}
			panic(errors.Errorf("error between queue:[%v] offset [%v]-[%v]", qConfig.Id, initOffset, offset))
		}
		log.Debugf("exit worker[%v], queue:[%v]", workerID, qConfig.Id)
	}()

	idleDuration := time.Duration(processor.config.IdleTimeoutInSecond) * time.Second
	elasticsearch, ok := qConfig.Labels["elasticsearch"]
	if !ok {
		if processor.config.Elasticsearch == "" {
			log.Errorf("label [elasticsearch] was not found in: %v", qConfig)
			return
		} else {
			elasticsearch = processor.config.Elasticsearch
		}
	}
	esClusterID = util.ToString(elasticsearch)
	meta = elastic.GetMetadata(esClusterID)
	if meta == nil {
		panic(errors.Errorf("cluster metadata [%v] not ready", esClusterID))
	}

	if elastic.IsHostDead(host) {
		host = meta.GetActiveHost()
	}

	bulkProcessor = elastic.BulkProcessor{
		Config: processor.config.BulkConfig,
	}

	if bulkProcessor.Config.DeadletterRequestsQueue == "" {
		bulkProcessor.Config.DeadletterRequestsQueue = fmt.Sprintf("%v-bulk-dead_letter-items", esClusterID)
	}

	var lastCommit time.Time = time.Now()

READ_DOCS:

	initOffset, _ = queue.GetOffset(qConfig, consumer)
	offset = initOffset
	for {
		if ctx.IsCanceled() {
			goto CLEAN_BUFFER
		}

		//TODO add config to enable check or not
		if !elastic.IsHostAvailable(host) {
			if elastic.IsHostDead(host) {
				host1 := host
				host = meta.GetActiveHost()
				if rate.GetRateLimiter("host_dead", host, 1, 1, time.Second*3).Allow() {
					log.Infof("host [%v] is dead, use: [%v]", host1, host)
				}
			} else {
				if global.Env().IsDebug {
					log.Debugf("host [%v] is not available", host)
				}
				time.Sleep(time.Second * 1)
			}

			goto READ_DOCS
		}

		if len(processor.config.WaitingAfter) > 0 {
			for _, v := range processor.config.WaitingAfter {
				qCfg := queue.GetOrInitConfig(v)
				hasLag := queue.HasLag(qCfg)

				if global.Env().IsDebug {
					log.Debugf("check queue lag: [%v] for [%v], %v", qCfg.Name, qConfig.Name, hasLag)
				}

				if hasLag {
					log.Tracef("%v has pending messages to consume, cleanup it first", v)
					time.Sleep(5 * time.Second)
					goto READ_DOCS
				}
			}
		}

		//each message is complete bulk message, must be end with \n
		if global.Env().IsDebug {
			log.Debugf("worker:[%v] start consume queue:[%v] offset:%v", workerID, qConfig.Id, offset)
		}

		//runnintCtx:=pipeline.AcquireContext()
		log.Tracef("star to consume queue:%v", qConfig.Name)
		ctx1, messages, timeout, err := queue.Consume(qConfig, consumer.Name, offset, processor.config.Consumer.FetchMaxMessages, time.Millisecond*time.Duration(processor.config.Consumer.FetchMaxWaitMs))
		log.Tracef("get %v messages from queue:%v", len(messages), qConfig.Name)

		//log.Errorf("max fetch messages:%v, fetched:%v",processor.config.Consumer.FetchMaxMessages,len(messages))

		if global.Env().IsDebug {
			log.Debugf("[%v] consume message:%v,ctx:%v,timeout:%v,err:%v", consumer.Name, len(messages), ctx1, timeout, err)
		}

		if timeout {
			log.Tracef("timeout on queue:[%v]", qConfig.Name)
			ctx.Failed()
			goto CLEAN_BUFFER
		}

		if err != nil {
			log.Tracef("error on queue:[%v]", qConfig.Name)
			if err.Error() == "EOF" || err.Error() == "unexpected EOF" {
				if len(messages) > 0 {
					goto HANDLE_MESSAGE
				}
				return
			}
			panic(err)
		}

	HANDLE_MESSAGE:

		//update temp offset, not committed, continued reading
		if ctx1 == nil {
			goto CLEAN_BUFFER
		}
		if len(messages) > 0 {
			for _, pop := range messages {

				if processor.config.ValidateRequest {
					elastic.ValidateBulkRequest("write_pop", string(pop.Data))
				}

				mainBuf.WriteMessageID(pop.Offset)
				mainBuf.WriteByteBuffer(pop.Data)

				if global.Env().IsDebug {
					log.Tracef("message count: %v, size: %v", mainBuf.GetMessageCount(), util.ByteSize(uint64(mainBuf.GetMessageSize())))
				}
				msgSize := mainBuf.GetMessageSize()
				msgCount := mainBuf.GetMessageCount()

				if msgSize > (bulkSizeInByte) || (processor.config.BulkConfig.BulkMaxDocsCount > 0 && msgCount > processor.config.BulkConfig.BulkMaxDocsCount) {
					if global.Env().IsDebug {
						log.Tracef("consuming [%v], hit buffer limit, size:%v, count:%v, submit now", qConfig.Name, msgSize, msgCount)
					}

					//submit request
					continueRequest := processor.submitBulkRequest(tag, esClusterID, meta, host, bulkProcessor, mainBuf)
					//reset buffer
					mainBuf.Reset()
					if !continueRequest {
						log.Errorf("error between queue:[%v] offset [%v]-[%v]", qConfig.Id, initOffset, offset)
						panic(errors.Errorf("error between queue:[%v] offset [%v]-[%v]", qConfig.Id, initOffset, offset))
					} else {
						if pop.NextOffset != "" && pop.NextOffset != initOffset {
							ok, err := queue.CommitOffset(qConfig, consumer, pop.NextOffset)
							if !ok || err != nil {
								panic(err)
							}
						}
						offset = pop.NextOffset
					}
				}
			}
		}
		offset = ctx1.NextOffset

		if time.Since(lastCommit) > idleDuration && mainBuf.GetMessageSize() > 0 {
			if global.Env().IsDebug {
				log.Trace("hit idle timeout, ", idleDuration.String())
			}
			goto CLEAN_BUFFER
		}
	}

CLEAN_BUFFER:

	lastCommit = time.Now()
	// check bulk result, if ok, then commit offset, or retry non-200 requests, or save failure offset
	continueNext := processor.submitBulkRequest(tag, esClusterID, meta, host, bulkProcessor, mainBuf)
	//reset buffer
	mainBuf.Reset()
	if continueNext {
		if offset != "" && offset != initOffset {
			ok, err := queue.CommitOffset(qConfig, consumer, offset)
			if !ok || err != nil {
				panic(err)
			}
		}
	} else {
		//logging failure offset boundry
		log.Errorf("error between offset [%v]-[%v]", initOffset, offset)
		panic(errors.Errorf("error between offset [%v]-[%v]", initOffset, offset))
	}

	if offset == "" || ctx.IsCanceled() || ctx.IsFailed() {
		log.Tracef("invalid offset or canceled, return on queue:[%v]", qConfig.Name)
		return
	}

	log.Tracef("goto READ_DOCS, return on queue:[%v]", qConfig.Name)

	if !ctx.IsCanceled() {
		goto READ_DOCS
	}

}

func (processor *BulkIndexingProcessor) submitBulkRequest(tag, esClusterID string, meta *elastic.ElasticsearchMetadata, host string, bulkProcessor elastic.BulkProcessor, mainBuf *elastic.BulkBuffer) bool {

	if mainBuf == nil || meta == nil {
		return true
	}

	count := mainBuf.GetMessageCount()
	size := mainBuf.GetMessageSize()

	if mainBuf.GetMessageCount() > 0 {

		log.Trace(meta.Config.Name, ", starting submit bulk request")
		start := time.Now()
		contrinueRequest := bulkProcessor.Bulk(tag, meta, host, mainBuf)
		stats.Timing("elasticsearch."+esClusterID+".bulk", "elapsed_ms", time.Since(start).Milliseconds())
		log.Debug(meta.Config.Name, ", ", host, ", count:", count, ", size:", util.ByteSize(uint64(size)), ", elapsed:", time.Since(start))
		return contrinueRequest
	}

	return true
}