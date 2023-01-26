package bulk_indexing

import (
	"fmt"
	"infini.sh/framework/core/config"
	"infini.sh/framework/core/rate"
	"infini.sh/framework/core/rotate"
	"infini.sh/framework/core/task/ants"
	"runtime"
	"github.com/OneOfOne/xxhash"
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
	config               *Config
	runningConfigs       map[string]*queue.QueueConfig
	wg                   sync.WaitGroup
	inFlightQueueConfigs sync.Map
	detectorRunning      bool
	id                   string
	sync.RWMutex
	pool *ants.Pool
}

type Config struct {
	NumOfSlices int   `config:"num_of_slices"`
	Slices      []int `config:"slices"`

	enabledSlice map[int]int

	IdleTimeoutInSecond  int `config:"idle_timeout_in_seconds"`
	MaxConnectionPerHost int `config:"max_connection_per_node"`

	QueueLabels map[string]interface{} `config:"queues,omitempty"`

	Selector queue.QueueSelector `config:"queue_selector"`

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
		NumOfSlices:          1,
		MaxWorkers:           10,
		MaxConnectionPerHost: 1,
		IdleTimeoutInSecond:  5,
		DetectIntervalInMs:   5000,

		Selector: queue.QueueSelector{
			Labels: map[string]interface{}{},
		},

		Consumer: queue.ConsumerConfig{
			Group:            "group-001",
			Name:             "consumer-001",
			FetchMinBytes:    1,
			FetchMaxBytes:    10 * 1024 * 1024,
			FetchMaxMessages: 500,
			EOFRetryDelayInMs: 500,
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

	if len(cfg.QueueLabels) > 0 {
		for k, v := range cfg.QueueLabels {
			cfg.Selector.Labels[k] = v
		}
	}

	if cfg.NumOfSlices <= 0 {
		cfg.NumOfSlices = 1
	}

	if len(cfg.Slices) > 0 {
		cfg.enabledSlice = map[int]int{}
		for _, v := range cfg.Slices {
			cfg.enabledSlice[v] = v
		}
	}

	runner := BulkIndexingProcessor{
		id:                   util.GetUUID(),
		config:               &cfg,
		runningConfigs:       map[string]*queue.QueueConfig{},
		inFlightQueueConfigs: sync.Map{},
	}

	runner.wg = sync.WaitGroup{}

	if runner.config.MaxWorkers<10{
		runner.config.MaxWorkers=10
	}

	pool,err:=ants.NewPoolWithTag("bulk_indexing",runner.config.MaxWorkers)
	if err!=nil{
		panic(err)
	}

	runner.pool=pool
	global.RegisterShutdownCallback(func() {
		runner.pool.Release()
	})

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

					cfgs := queue.GetConfigBySelector(&processor.config.Selector)

					log.Tracef("get %v queues",len(cfgs))

					for _, v := range cfgs {
						if c.IsCanceled() {
							return
						}
						//if have depth and not in in flight
						if !processor.config.SkipEmptyQueue ||queue.HasLag(v) {
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
		cfgs := queue.GetConfigBySelector(&processor.config.Selector)
		log.Debugf("filter queue by:%v, num of queues:%v", processor.config.Selector.ToString(), len(cfgs))
		for _, v := range cfgs {
			log.Tracef("checking queue: %v", v)
			processor.HandleQueueConfig(v, c)
		}
	}

	processor.wg.Wait()

	return nil
}

func (processor *BulkIndexingProcessor) HandleQueueConfig(v *queue.QueueConfig, c *pipeline.Context) {
	if processor.config.SkipEmptyQueue &&!queue.HasLag(v) {
			if global.Env().IsDebug {
				log.Tracef("skip empty queue:[%v]", v.Name)
			}
			return
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
				processor.NewBulkWorker("bulk_indexing_"+host, c, processor.config.BulkConfig.GetBulkSizeInBytes(), v, host)
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
									processor.NewBulkWorker("bulk_indexing_"+nodeHost, c, processor.config.BulkConfig.GetBulkSizeInBytes(), v, nodeHost)
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
	log.Tracef("random choose node [%v] to consume queue [%v]", host, v.Id)
	processor.NewBulkWorker("bulk_indexing_"+host, c, processor.config.BulkConfig.GetBulkSizeInBytes(), v, host)
}

func (processor *BulkIndexingProcessor) NewBulkWorker(tag string, ctx *pipeline.Context, bulkSizeInByte int, qConfig *queue.QueueConfig, host string) {

	//check slice
	for sliceID := 0; sliceID < processor.config.NumOfSlices; sliceID++ {
		if global.Env().IsDebug{
			log.Tracef("checking slice_id: %v", sliceID)
		}
		if len(processor.config.enabledSlice) > 0 {
			_, ok := processor.config.enabledSlice[sliceID]
			if !ok {
				log.Tracef("skipping slice_id: %v", sliceID)
				continue
			}
		}

		//queue-slice
		key := fmt.Sprintf("%v-%v", qConfig.Id, sliceID)

		if processor.config.MaxWorkers > 0 && util.MapLength(&processor.inFlightQueueConfigs) > processor.config.MaxWorkers {
			log.Tracef("reached max num of workers, skip init [%v], slice_id:%v", qConfig.Name, sliceID)
			return
		}

		processor.Lock()
		_, exists := processor.inFlightQueueConfigs.Load(key)
		if exists {
			processor.Unlock()
			log.Tracef("[%v], queue [%v], slice_id:%v has more then one consumer, key:%v, %v", tag, qConfig.Id, sliceID, key, processor.inFlightQueueConfigs)
			continue
		} else {
			var workerID = util.GetUUID()
			log.Tracef("starting worker:[%v], queue:[%v], slice_id:%v, host:[%v]", workerID, qConfig.Name, sliceID, host)
			processor.wg.Add(1)
			//err:=processor.pool.Submit(func() {
				go processor.NewSlicedBulkWorker(key, workerID, sliceID, processor.config.NumOfSlices, tag, ctx, bulkSizeInByte, qConfig, host)
			//})
			processor.Unlock()
			//if err!=nil{
			//	panic(err)
			//}
		}
	}
}

var xxHashPool= sync.Pool{
	New: func() interface{} {
		return xxhash.New32()
	},
}

func (processor *BulkIndexingProcessor) NewSlicedBulkWorker(key, workerID string, sliceID, maxSlices int, tag string, ctx *pipeline.Context, bulkSizeInByte int, qConfig *queue.QueueConfig, host string) {

	if global.Env().IsDebug {
		log.Tracef("new slice bulk worker: %v, %v, %v", key, workerID, sliceID)
	}

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
				log.Errorf("error in bulk indexing processor, %v, queue:%v, slice_id:%v", v, qConfig.Id, sliceID)
			}
		}
		processor.wg.Done()
		log.Tracef("exit bulk indexing processor, queue:%v, slice_id:%v", qConfig.Id, sliceID)
	}()

	mainBuf := elastic.AcquireBulkBuffer()
	mainBuf.Queue = qConfig.Id
	defer elastic.ReturnBulkBuffer(mainBuf)

	var bulkProcessor elastic.BulkProcessor
	var esClusterID string
	var meta *elastic.ElasticsearchMetadata
	var initOffset string
	var offset string
	var groupName = processor.config.Consumer.Group
	if maxSlices > 1 {
		groupName=fmt.Sprintf("%v-%v", processor.config.Consumer.Group, sliceID)
	}

	var consumerConfig = queue.GetOrInitConsumerConfig(qConfig.Id, groupName, processor.config.Consumer.Name)
	var skipFinalDocsProcess bool

	xxHash := xxHashPool.Get().(*xxhash.XXHash32)
	defer xxHashPool.Put(xxHash)

	//TODO check lag
	//if !queue.HasLag(qConfig) {
	//	return
	//}

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
				log.Errorf("worker[%v], queue:[%v], slice:[%v], offset:[%v]->[%v],%v", workerID, qConfig.Id, sliceID, initOffset, offset, v)
				ctx.Failed()
				skipFinalDocsProcess = true
			}
		}

		processor.inFlightQueueConfigs.Delete(key)

		if skipFinalDocsProcess {
			return
		}

		//cleanup buffer before exit worker
		continueNext, err := processor.submitBulkRequest(ctx,tag, esClusterID, meta, host, bulkProcessor, mainBuf)
		mainBuf.ResetData()
		if continueNext {
			if offset != "" && initOffset != offset {
				ok, err := queue.CommitOffset(qConfig, consumerConfig, offset)
				if !ok || err != nil {
					panic(err)
				}
			}
		} else {
			if global.Env().IsDebug {
				log.Errorf("queue:[%v], slice_id:%v, offset [%v]-[%v], err:%v", qConfig.Id, sliceID, initOffset, offset, err)
			}
			panic(errors.Errorf("queue:[%v], slice_id:%v, offset [%v]-[%v], err:%v", qConfig.Id, sliceID, initOffset, offset, err))
		}
		log.Debugf("exit worker[%v], queue:[%v], slice_id:%v", workerID, qConfig.Id, sliceID)
	}()

	processor.inFlightQueueConfigs.Store(key, workerID)

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

	if host==""{
		host=meta.GetActivePreferredSeedHost()
	}

	if global.Env().IsDebug{
		log.Trace("get host:",host,",is dead:",elastic.IsHostDead(host),",is available:",elastic.IsHostAvailable(host))
	}

	if elastic.IsHostDead(host)||!elastic.IsHostAvailable(host) {
		host = meta.GetActiveHost()
	}

	if global.Env().IsDebug{
		log.Trace("get final host:",host)
	}

	bulkProcessor = elastic.BulkProcessor{
		Config: processor.config.BulkConfig,
	}

	if bulkProcessor.Config.DeadletterRequestsQueue == "" {
		bulkProcessor.Config.DeadletterRequestsQueue = fmt.Sprintf("%v-bulk-dead_letter-items", esClusterID)
	}

	var lastCommit time.Time = time.Now()
	initOffset, _ = queue.GetOffset(qConfig, consumerConfig)

	if global.Env().IsDebug{
		log.Debugf("get init offset: %v for consumer:%v,%v", initOffset, consumerConfig.Group, consumerConfig.Name)
	}
	offset = initOffset

	consumerInstance,err:=queue.AcquireConsumer(qConfig,consumerConfig,offset)
	if err!=nil||consumerInstance==nil{
		panic(err)
	}
	defer consumerInstance.Close()

	ctx1 := &queue.Context{}

READ_DOCS:

	consumerInstance.ResetOffset(queue.ConvertOffset(offset))

	for {
		if ctx.IsCanceled() {
			goto CLEAN_BUFFER
		}

		//TODO add config to enable check or not, panic or skip
		if !elastic.IsHostAvailable(host) {
			if elastic.IsHostDead(host) {
				host1 := host
				host = meta.GetActiveHost()
				if rate.GetRateLimiter("host_dead", host, 1, 1, time.Second*3).Allow() {
					if host!=host1{
						log.Infof("host [%v] is dead, choose: [%v]", host1, host)
					}else{
						log.Infof("host [%v] is dead, no other host available", host1)
					}
				}
				//if no new host found, skip processing
				if host1==host{
					log.Debugf("host [%v] is dead, but still use [%v], skip and clean_buffer", host1,host)
					goto CLEAN_BUFFER
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
			log.Tracef("worker:[%v] start consume queue:[%v][%v] offset:%v", workerID, qConfig.Id, sliceID, offset)
		}

		if global.Env().IsDebug{
			log.Tracef("star to consume queue:%v, slice:%v， offset:%v", qConfig.Name, sliceID, offset)
		}

		messages, timeout, err := consumerInstance.FetchMessages(ctx1,consumerConfig.FetchMaxMessages)

		if global.Env().IsDebug {
			log.Tracef("[%v][%v] consume message:%v,ctx:%v,timeout:%v,err:%v", consumerConfig.Name,sliceID, len(messages), ctx1.String(), timeout, err)
		}

		//TODO 不能重复处理，也需要处理 offset 的妥善持久化，避免重复数据，也要避免拿不到数据迟迟不退出。

		if err != nil {
			if err.Error() == "EOF" || err.Error() == "unexpected EOF" {
				if len(messages) > 0 || mainBuf.GetMessageCount() > 0 {
					goto HANDLE_MESSAGE
				}

				log.Tracef("error on consume queue:[%v], slice_id:%v, no data fetched, offset: %v", qConfig.Name, sliceID, ctx1)
				ctx.Failed()
				goto CLEAN_BUFFER
				return
			}
			log.Errorf("error on queue:[%v], slice_id:%v, %v", qConfig.Name, sliceID, err)
			log.Flush()
			panic(err)
		}

	HANDLE_MESSAGE:
		//update temp offset, not committed, continued reading
		if ctx1 == nil {
			goto CLEAN_BUFFER
		}

		if len(messages) > 0 {
			for msgOffset, pop := range messages {

				if processor.config.ValidateRequest {
					elastic.ValidateBulkRequest("write_pop", string(pop.Data))
				}

				if maxSlices > 1 {
					var totalOps, sliceOps int
					var collectMeta = false
					elastic.WalkBulkRequests(pop.Data,  func(eachLine []byte) (skipNextLine bool) {
						return false
					}, func(metaBytes []byte, actionStr, index, typeName, id,routing string,offset int) (err error) {
						totalOps++

						var partitionID int
						var msgID=id
						if id!=""{
							//check hash
							xxHash.Reset()
							xxHash.WriteString(id)
							partitionID = int(xxHash.Sum32()) % maxSlices
						}else{
							partitionID=msgOffset % maxSlices
							msgID=fmt.Sprintf("%v",msgOffset)
						}

						if global.Env().IsDebug{
							log.Debug(sliceID,",",id,",",partitionID,",",partitionID == sliceID)
						}

						if partitionID == sliceID {
							sliceOps++
							mainBuf.WriteNewByteBufferLine("meta1",metaBytes)
							mainBuf.WriteMessageID(msgID)
							collectMeta = true
						}else{
							collectMeta = false
						}
						return nil
					}, func(payloadBytes []byte, actionStr, index, typeName, id,routing string) {
						if collectMeta {
							mainBuf.WriteNewByteBufferLine("payload1",payloadBytes)
							collectMeta = false
						}
					})
				} else {
					mainBuf.WriteMessageID(pop.Offset)
					mainBuf.WriteByteBuffer(pop.Data)
				}

				if global.Env().IsDebug {
					log.Tracef("message count: %v, size: %v", mainBuf.GetMessageCount(), util.ByteSize(uint64(mainBuf.GetMessageSize())))
				}

				msgSize := mainBuf.GetMessageSize()
				msgCount := mainBuf.GetMessageCount()

				if (bulkSizeInByte>0 && msgSize > (bulkSizeInByte)) || (processor.config.BulkConfig.BulkMaxDocsCount > 0 && msgCount > processor.config.BulkConfig.BulkMaxDocsCount) {
					if global.Env().IsDebug {
						log.Tracef("consuming [%v], slice_id:%v, hit buffer limit, size:%v, count:%v, submit now", qConfig.Name, sliceID, msgSize, msgCount)
					}

					//submit request
					continueRequest, err := processor.submitBulkRequest(ctx,tag, esClusterID, meta, host, bulkProcessor, mainBuf)
					//reset buffer
					mainBuf.ResetData()
					if !continueRequest {
						//TODO handle 429 gracefully
						if !util.ContainStr(err.Error(),"code 429"){
							//skipFinalDocsProcess=true
							//return
							panic(errors.Errorf("error between queue:[%v], slice_id:%v, offset [%v]-[%v], host:%v, err:%v", qConfig.Id, sliceID, initOffset, offset, host,err))
						}
					} else {
						if pop.NextOffset != "" && pop.NextOffset != initOffset {
							ok, err := queue.CommitOffset(qConfig, consumerConfig, pop.NextOffset)
							if !ok || err != nil {
								panic(err)
							}
							initOffset=pop.NextOffset
						}
						offset = pop.NextOffset
					}
				}
			}
		}
		offset = ctx1.NextOffset.String() //TODO

		if time.Since(lastCommit) > idleDuration && mainBuf.GetMessageSize() > 0 {
			if global.Env().IsDebug {
				log.Trace("hit idle timeout, ", idleDuration.String())
			}
			goto CLEAN_BUFFER
		}
		if len(messages)==0 && mainBuf.GetMessageSize() == 0 {
			log.Trace("no message found in queue: "+qConfig.Id)
			return
		}
	}

CLEAN_BUFFER:

	lastCommit = time.Now()
	// check bulk result, if ok, then commit offset, or retry non-200 requests, or save failure offset
	continueNext, err := processor.submitBulkRequest(ctx,tag, esClusterID, meta, host, bulkProcessor, mainBuf)
	//reset buffer
	mainBuf.ResetData()

	if continueNext {
		if offset != "" && offset != initOffset {
			ok, err := queue.CommitOffset(qConfig, consumerConfig, offset)
			if !ok || err != nil {
				panic(err)
			}
			initOffset=offset
		}
	} else {
		//logging failure offset boundry
		//TODO handle 429 gracefully
		if !util.ContainStr(err.Error(),"429") {
			//skipFinalDocsProcess=true
			//return
			panic(errors.Errorf("queue:%v, slice_id:%v, error between offset [%v]-[%v], err:%v", qConfig.Name, sliceID, initOffset, offset, err))
		}
	}

	if offset == "" || ctx.IsCanceled() || ctx.IsFailed() {
		log.Tracef("invalid offset or canceled, return on queue:[%v], slice_id:%v", qConfig.Name, sliceID)
		return
	}

	log.Tracef("goto READ_DOCS, return on queue:[%v], slice_id:%v", qConfig.Name, sliceID)

	if !ctx.IsCanceled() {
		goto READ_DOCS
	}
}

func (processor *BulkIndexingProcessor) submitBulkRequest(ctx *pipeline.Context,tag, esClusterID string, meta *elastic.ElasticsearchMetadata, host string, bulkProcessor elastic.BulkProcessor, mainBuf *elastic.BulkBuffer) (bool, error) {

	if mainBuf == nil || meta == nil {
		return true, errors.New("invalid buffer or meta")
	}

	count := mainBuf.GetMessageCount()
	size := mainBuf.GetMessageSize()

	if count>0&&size==0||count==0&&size>0{
		panic(errors.Errorf("invalid bulk message, count: %v, size:%v, msg: %v",mainBuf.GetMessageCount(),mainBuf.GetMessageSize(),string(mainBuf.GetMessageBytes())))
	}

	if count > 0 &&size>0{

		log.Trace(meta.Config.Name, ", starting submit bulk request")
		start := time.Now()
		contrinueRequest,statsMap, err := bulkProcessor.Bulk(ctx.Context,tag, meta, host, mainBuf)

		if global.Env().IsDebug{
			stats.Timing("elasticsearch."+esClusterID+".bulk", "elapsed_ms", time.Since(start).Milliseconds())
		}

		log.Debug(meta.Config.Name, ", ", host,", stats:",statsMap, ", count:", count, ", size:", util.ByteSize(uint64(size)), ", elapsed:", time.Since(start))

		return contrinueRequest, err
	}

	return true, nil
}
