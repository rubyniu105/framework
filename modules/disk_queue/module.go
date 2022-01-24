/* ©INFINI, All Rights Reserved.
 * mail: contact#infini.ltd */

package queue

import (
	log "github.com/cihub/seelog"
	"infini.sh/framework/core/api"
	"infini.sh/framework/core/config"
	"infini.sh/framework/core/env"
	"infini.sh/framework/core/errors"
	"infini.sh/framework/core/global"
	"infini.sh/framework/core/kv"
	"infini.sh/framework/core/queue"
	"infini.sh/framework/core/s3"
	"infini.sh/framework/core/util"
	"os"
	"path"
	"strings"
	"sync"
	"time"
)

type DiskQueue struct {
	cfg *DiskQueueConfig
	initLocker sync.Mutex
	api.Handler
	queues sync.Map
}

func (module *DiskQueue) Name() string {
	return "disk_queue"
}

type RetentionConfig struct{
		MaxNumOfLocalFiles int64  `config:"max_num_of_local_files"`
		//DeleteAfterSaveToS3 bool `config:"delete_after_save_to_s3"`
		//MaxAge int  	   `config:"max_age"`
}

//#  disk.max_used_bytes:  100GB #trigger warning message
//#  disk.warning_free_bytes:  20GB #trigger warning message
//#  disk.reserved_free_bytes: 10GB #enter readonly mode, no writes allowed
type DiskQueueConfig struct {


	MinMsgSize       int32   `config:"min_msg_size"`
	MaxMsgSize       int32   `config:"max_msg_size"`
	MaxBytesPerFile  int64 `config:"max_bytes_per_file"`
	SyncEveryRecords int64 `config:"sync_every_records"`
	SyncTimeoutInMS  int   `config:"sync_timeout_in_ms"`
	ReadChanBuffer   int   `config:"read_chan_buffer_size"`
	WriteChanBuffer   int   `config:"write_chan_buffer_size"`

	MaxUsedBytes   uint64   `config:"max_used_bytes"`
	WarningFreeBytes   uint64   `config:"warning_free_bytes"`
	ReservedFreeBytes   uint64   `config:"reserved_free_bytes"`


	UploadToS3   bool   `config:"upload_to_s3"`

	Retention RetentionConfig `config:"retention"`

	S3 struct{
		Async   bool   `config:"async"`
		Server   string   `config:"server"`
		Location   string   `config:"location"`
		Bucket   string   `config:"bucket"`
	}`config:"s3"`

}

const queueS3LastFileNum ="last_success_file_for_queue"

func GetLastS3UploadFileNum(queueID string)int64  {
	b,err:=kv.GetValue(queueS3LastFileNum,util.UnsafeStringToBytes(queueID))
	if err!=nil{
		panic(err)
	}
	if b==nil||len(b)==0{
		return -1
	}
	//log.Errorf("bytes to int64: %v",b)
	return util.BytesToInt64(b)
}

var s3uploaderLocker sync.RWMutex
func (module *DiskQueue)uploadToS3(queueID string,fileNum  int64){
	//TODO move to channel, async
	s3uploaderLocker.Lock()
	defer s3uploaderLocker.Unlock()

	//send s3 upload signal
	if module.cfg.UploadToS3{

		//skip uploaded file
		lastFileNum:=GetLastS3UploadFileNum(queueID)
		log.Errorf("lastupload:%v, fileNum:%v",lastFileNum, fileNum)
		if fileNum<lastFileNum{
			//skip old queue file, no need to upload
			return
		}

		if module.cfg.S3.Server!=""&&module.cfg.S3.Bucket!=""{
			fileName:= GetFileName(queueID,fileNum)
			toFile:=util.TrimLeftStr(fileName,global.Env().GetDataDir())
			var success=false
			var err error
			if module.cfg.S3.Async{
				err:=s3.AsyncUpload(fileName,module.cfg.S3.Server,module.cfg.S3.Location,module.cfg.S3.Bucket,toFile)
				if err==nil{
					success=true
				}
			}else{
				var ok bool
				ok,err=s3.SyncUpload(fileName,module.cfg.S3.Server,module.cfg.S3.Location,module.cfg.S3.Bucket,toFile)
				if err==nil&&ok{
					success=true
				}
			}
			//update last mark
			if success{
				err=kv.AddValue(queueS3LastFileNum,util.UnsafeStringToBytes(queueID),util.Int64ToBytes(fileNum))
				if err!=nil{
					panic(err)
				}
				log.Debugf("queue [%v][%v] uploaded to s3",queueID,fileNum)
			}else{
				log.Debugf("failed to upload queue [%v][%v] to s3, %v",queueID,fileNum,err)
			}
		}else{
			log.Errorf("invalid s3 config:%v",module.cfg.S3)
		}
	}
}

func (module *DiskQueue) Init(name string) error {
	module.initLocker.Lock()
	defer module.initLocker.Unlock()

	_,ok:= module.queues.Load(name)
	if ok{
		return nil
	}

	log.Debugf("init queue: %s", name)

	dataPath := GetDataPath(name)

	if !util.FileExists(dataPath){
		os.MkdirAll(dataPath, 0755)
	}

	tempQueue := NewDiskQueueByConfig(name,dataPath,module.cfg)

	module.queues.Store(name,&tempQueue)

	return nil
}

func GetDataPath(queueID string)string  {
	return path.Join(global.Env().GetDataDir(), "queue", strings.ToLower(queueID))
}

func getQueueConfigPath() string {
	os.MkdirAll(path.Join(global.Env().GetDataDir(),"queue"),0755)
	return path.Join(global.Env().GetDataDir(),"queue","configs")
}



func (module *DiskQueue) Setup(config *config.Config) {

	module.cfg = &DiskQueueConfig{
		UploadToS3:       false,
		Retention: 		  RetentionConfig{ MaxNumOfLocalFiles: 10},
		MinMsgSize:       1,
		MaxMsgSize:       104857600, //100MB
		MaxBytesPerFile:  200 * 1024 * 1024, //200MB
		SyncEveryRecords: 1000,
		SyncTimeoutInMS:  1000,
		ReadChanBuffer:   0,
		WriteChanBuffer:   0,
		WarningFreeBytes: 10 * 1024 * 1024 * 1024,
		ReservedFreeBytes: 5 * 1024 * 1024 * 1024,
	}

	ok,err:=env.ParseConfig("disk_queue", module.cfg)
	if ok&&err!=nil{
		panic(err)
	}

	module.queues=sync.Map{}

	//load configs from static config
	configs := []queue.Config{}
	ok, err = env.ParseConfig("queue", &configs)
	if ok && err != nil {
		panic(err)
	}

	for _,v:=range configs{
		v.Source="file"
		if v.Id==""{
			v.Id=v.Name
		}
		queue.RegisterConfig(v.Name,&v)
	}

	//load configs from local metadata
	if util.FileExists(getQueueConfigPath()){
		data,err:=util.FileGetContent(getQueueConfigPath())
		if err!=nil{
			panic(err)
		}

		cfgs:=map[string]*queue.Config{}
		err=util.FromJSONBytes(data,&cfgs)
		if err!=nil{
			panic(err)
		}

		for _,v:=range cfgs{
			if v.Id==""{
				v.Id=v.Name
			}
			queue.RegisterConfig(v.Name,v)
		}
	}


	//load queue information from directory

	//load configs from remote elasticsearch


	//register queue listener
	queue.RegisterQueueConfigChangeListener(func() {
		persistQueueMetadata()
	})


	RegisterEventListener(func(event Event) error {

		log.Trace("received event: ",event)
		switch event.Type {
		case WriteComplete:

			//TODO, convert to signal, move to async

			//upload old file to s3
			module.uploadToS3(event.Queue,event.FileNum)

			//check capacity

			//delete old unused files
			module.deleteUnusedFiles(event.Queue,event.FileNum)

			break
		case ReadComplete:

			//delete old unused files
			module.deleteUnusedFiles(event.Queue,event.FileNum)

			break;

		}

		return nil
	})

	////register consumer listener
	//queue.RegisterConsumerConfigChangeListener(func(queueID string,configs map[string]*queue.ConsumerConfig) {
	//	persistConsumerMetadata(queueID,configs)
	//})

	queue.Register("disk", module)
	queue.RegisterDefaultHandler(module)
}

func (module *DiskQueue) Push(k string, v []byte) error {
	module.Init(k)
	q,ok:=module.queues.Load(k)
	if ok{
		return (*q.(*BackendQueue)).Put(v)
	}
	return errors.Errorf("queue [%v] not found",k)
}

func (module *DiskQueue) ReadChan(k string) <-chan []byte{
	module.Init(k)
	q,ok:=module.queues.Load(k)
	if ok{
		return (*q.(*BackendQueue)).ReadChan()
	}
	panic(errors.Errorf("queue [%v] not found",k))
}

func (module *DiskQueue) Pop(k string, timeoutDuration time.Duration) (data []byte,timeout bool) {
	err:= module.Init(k)
	if err!=nil{
		panic(err)
	}

	if timeoutDuration > 0 {
		to := time.NewTimer(timeoutDuration)
		for {
			to.Reset(timeoutDuration)
			select {
			case b := <-module.ReadChan(k):
				return b,false
			case <-to.C:
				return nil,true
			}
		}
	} else {
		b := <-module.ReadChan(k)
		return b,false
	}
}

func ConvertOffset(offsetStr string) (int64,int64) {
	data:=strings.Split(offsetStr,",")
	if len(data)!=2{
		panic(errors.Errorf("invalid offset: %v",offsetStr))
	}
	var part,offset int64
	part,_=util.ToInt64(data[0])
	offset,_=util.ToInt64(data[1])
	return part,offset
}

func (module *DiskQueue) Consume(queueName,consumer,offsetStr string,count int, timeDuration time.Duration) (ctx *queue.Context,messages []queue.Message,timeout bool,err error) {

	module.Init(queueName)
	q,ok:=module.queues.Load(queueName)
	if ok{
		part,offset:=ConvertOffset(offsetStr)
		q1:=(*q.(*BackendQueue))
		ctx,messages,timeout,err:=q1.Consume(consumer,part,offset,count, timeDuration)
		return ctx,messages,timeout,err
	}

	panic(errors.Errorf("queue [%v] not found",queueName))
}

func (module *DiskQueue) Close(k string) error {
	q,ok:=module.queues.Load(k)
	if ok{
		return (*q.(*BackendQueue)).Close()
	}
	panic(errors.Errorf("queue [%v] not found",k))
}

func (module *DiskQueue) LatestOffset(k string) string {
	module.Init(k)
	q,ok:=module.queues.Load(k)
	if ok{
		return (*q.(*BackendQueue)).LatestOffset()
	}

	panic(errors.Errorf("queue [%v] not found",k))
}

func (module *DiskQueue) Depth(k string) int64 {
	module.Init(k)
	q,ok:=module.queues.Load(k)
	if ok{
		return (*q.(*BackendQueue)).Depth()
	}
	panic(errors.Errorf("queue [%v] not found",k))
}

func (module *DiskQueue) GetQueues() []string {
	result := []string{}

	module.queues.Range(func(key, value interface{}) bool {
		result = append(result, key.(string))
		return true
	})
	return result
}

func (module *DiskQueue) Start() error {

	//load configs from local file
	cfgs:=queue.GetAllConfigs()

	if cfgs != nil && len(cfgs) > 0 {
		for _, v := range cfgs {
			if v.Id==""{
				v.Id=v.Name
			}
			queue.IniQueue(v, v.Type)
		}
	}

	module.RegisterAPI()

	//trigger s3 uploading
	//from lastUpload to current WrtieFile
	for _, v := range cfgs {
		last:=GetLastS3UploadFileNum(v.Id)
		offsetStr:=queue.LatestOffset(v)
		part,_:=ConvertOffset(offsetStr)
		log.Tracef("check offset %v/%v/%v,%v, last upload:%v",v.Name,v.Id,offsetStr,part,last)
		if part>last{
			for x:=last;x<part;x++{
				if x>=0{
					log.Tracef("upload %v/%v",v.Id,x)
					module.uploadToS3(v.Id,x)
				}
			}
		}
	}
	return nil
}

func (module *DiskQueue) Stop() error {

	module.queues.Range(func(key, value interface{}) bool {
		q,ok:=module.queues.Load(key)
		if ok{
			err := (*q.(*BackendQueue)).Close()
			if err != nil {
				log.Error(err)
			}
		}
		return true
	})

	persistQueueMetadata()

	return nil
}

func (module *DiskQueue) deleteUnusedFiles(queueID string,fileNum  int64) {
	//check last uploaded mark
	b,err:=kv.GetValue(queueS3LastFileNum,util.UnsafeStringToBytes(queueID))
	if err!=nil{
		panic(err)
	}
	lastFileNum:=util.BytesToInt64(b)
	//check consumers offset
	consumers,part,_:=queue.GetEarlierOffsetByQueueID(queueID)
	fileStartToDelete:=fileNum-module.cfg.Retention.MaxNumOfLocalFiles
	//no consumers or consumer/s3 already ahead of this file
		if consumers==0||(fileStartToDelete<part&&fileStartToDelete<lastFileNum){
			log.Trace("start to delete:",fileStartToDelete,",consumers:",consumers,",part:",part)
			for x:=fileStartToDelete;x>=0;x--{
				file:=GetFileName(queueID,x)
				if util.FileExists(file){
					log.Debug("delete queue file:",file)
					err:=os.Remove(file)
					if err!=nil{
						panic(err)
					}
				}else{
					//skip
					break
				}
			}
		}
	}




var persistentLocker sync.RWMutex
func persistQueueMetadata()  {
	persistentLocker.Lock()
	defer persistentLocker.Unlock()

	//persist configs to local store
	bytes:=queue.GetAllConfigBytes()
	_,err:=util.FilePutContentWithByte(getQueueConfigPath(),bytes)
	if err!=nil{
		panic(err)
	}
}