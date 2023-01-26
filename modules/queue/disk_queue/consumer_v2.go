/* Copyright © INFINI LTD. All rights reserved.
 * Web: https://infinilabs.com
 * Email: hello#infini.ltd */

package queue

import (
	"bufio"
	"encoding/binary"
	log "github.com/cihub/seelog"
	"infini.sh/framework/core/errors"
	"infini.sh/framework/core/global"
	"infini.sh/framework/core/queue"
	"infini.sh/framework/core/util"
	"infini.sh/framework/core/util/zstd"
	"io"
	"os"
	"strings"
	"sync"
	"time"
)

type Consumer struct {
	ID string
	diskQueue *DiskBasedQueue

	mCfg *DiskQueueConfig
	cCfg *queue.ConsumerConfig

	fileName            string
	maxBytesPerFileRead int64

	reader              *bufio.Reader
	readFile            *os.File

	queue   string
	segment int64
	readPos int64

	fileLock sync.RWMutex
}

func (c *Consumer) getFileSize()(int64)  {
	var err error
	readFile, err:= os.OpenFile(c.fileName, os.O_RDONLY, 0600)
	if err != nil {
		log.Error(c.diskQueue.writeSegmentNum,",",err)
		return -1
	}
	defer readFile.Close()
	var stat os.FileInfo
	stat, err = readFile.Stat()
	if err!=nil{
		log.Error(err)
		return -1
	}
	return stat.Size()
}

func (d *DiskBasedQueue) AcquireConsumer(consumer *queue.ConsumerConfig, segment,readPos int64) (queue.ConsumerAPI,error){
	output:=Consumer{
		ID:util.ToString(util.GetIncrementID("consumer")),
		mCfg: d.cfg,
		diskQueue: d,
		cCfg: consumer,
		queue:d.name,
	}

	if global.Env().IsDebug{
		log.Debugf("acquire consumer:%v, %v, %v, %v-%v",output.ID,d.name,consumer.Key(), segment,readPos)
	}
	err:=output.ResetOffset(segment,readPos)
	return &output, err
}

func (d *Consumer) FetchMessages(ctx *queue.Context,numOfMessages int) (messages []queue.Message, isTimeout bool, err error){
	var msgSize int32
	var messageOffset=0
	var totalMessageSize int = 0

	//initOffset := queue.AcquireOffset(d.segment, d.readPos) //fmt.Sprintf("%v,%v", d.segment, d.readPos)
	//ctx.InitOffset = initOffset

	ctx.UpdateInitOffset(d.segment,d.readPos)
	ctx.NextOffset = ctx.InitOffset

	messages = []queue.Message{}

READ_MSG:
	//read message size
	err = binary.Read(d.reader, binary.BigEndian, &msgSize)
	if err != nil {
		errMsg:=err.Error()
		if util.ContainStr(errMsg,"EOF")||util.ContainStr(errMsg,"file already closed"){
			//current have changes, reload file with new position
			if d.getFileSize()>d.readPos{
				log.Debug("current file have changes, reload:",d.queue,",",d.getFileSize()," > ",d.readPos)
				if d.cCfg.EOFRetryDelayInMs>0{
					time.Sleep(time.Duration(d.cCfg.EOFRetryDelayInMs)*time.Millisecond)
				}
				ctx.UpdateNextOffset(d.segment,d.readPos)//.NextOffset = fmt.Sprintf("%v,%v", d.segment, d.readPos)
				err=d.ResetOffset(d.segment,d.readPos)
				if err!=nil{
					if strings.Contains(err.Error(),"not found"){
						return messages, false, nil
					}
					panic(err)
				}
				goto READ_MSG
			}
			nextFile,exists:= SmartGetFileName(d.mCfg,d.queue,d.segment+1)
			if exists||util.FileExists(nextFile){
				log.Trace("EOF, continue read:",nextFile)
				Notify(d.queue, ReadComplete,d.segment)
				ctx.UpdateNextOffset(d.segment,d.readPos)//.NextOffset = fmt.Sprintf("%v,%v", d.segment, d.readPos)
				err=d.ResetOffset(d.segment+1,0)
				if err!=nil{
					if strings.Contains(err.Error(),"not found"){
						return messages, false, nil
					}
					panic(err)
				}
				goto READ_MSG
			}else{
				log.Tracef("EOF, but next file [%v] not exists, pause and waiting for new data, messages:%v, newFile:%v", nextFile, len(messages), d.segment < d.diskQueue.writeSegmentNum)

				if d.diskQueue==nil{
					panic("queue can't be nil")
				}

				if d.segment < d.diskQueue.writeSegmentNum {
					oldPart := d.segment
					Notify(d.queue, ReadComplete, d.segment)
					log.Debugf("EOF, but current read segment_id [%v] is less than current write segment_id [%v], increase ++", oldPart, d.segment)
					ctx.UpdateNextOffset(d.segment,d.readPos)//.NextOffset = fmt.Sprintf("%v,%v", d.segment, d.readPos)
					err=d.ResetOffset(d.segment + 1,0)
					if err!=nil{
						if strings.Contains(err.Error(),"not found"){
							return messages, false, nil
						}
						panic(err)
					}

					ctx.UpdateNextOffset(d.segment,d.readPos)//.NextOffset = fmt.Sprintf("%v,%v", d.segment, d.readPos)
					return messages, false, err
				}

				if len(messages) == 0 {
					if global.Env().IsDebug {
						log.Tracef("no message found in queue: %v, sleep 1s", d.queue)
					}
					if d.cCfg.EOFRetryDelayInMs>0{
						time.Sleep(time.Duration(d.cCfg.EOFRetryDelayInMs)*time.Millisecond)
					}
				}
			}
			//No error for EOF error
			err=nil
		}else{
			log.Error("[%v] err:%v,msgSizeDataRead:%v,maxPerFileRead:%v,msg:%v",d.fileName,err,msgSize,d.maxBytesPerFileRead,len(messages))
		}
		return messages,false,err
	}

	if int32(msgSize) < d.mCfg.MinMsgSize || int32(msgSize) > d.mCfg.MaxMsgSize {

		//current have changes, reload file with new position
		if d.getFileSize()>d.maxBytesPerFileRead{
			d.ResetOffset(d.segment,d.readPos)
			return messages, false, err
		}

		err=errors.Errorf("queue:%v,offset:%v,%v, invalid message size: %v, should between: %v TO %v",d.queue,d.segment,d.readPos,msgSize,d.mCfg.MinMsgSize,d.mCfg.MaxMsgSize)
		return messages,false,err
	}

	//read message
	readBuf := make([]byte, msgSize)
	_, err = io.ReadFull(d.reader, readBuf)
	if err != nil {
		if util.ContainStr(err.Error(),"EOF") {
			err=nil
		}else{
			log.Error(err)
		}
		return messages,false,err
	}

	totalBytes := int(4 + msgSize)
	nextReadPos := d.readPos + int64(totalBytes)
	previousPos:=d.readPos
	d.readPos=nextReadPos

	if d.mCfg.Compress.Message.Enabled{
		newData,err:= zstd.ZSTDDecompress(nil,readBuf)
		if err!=nil{
			log.Error(err)
			ctx.UpdateNextOffset(d.segment,nextReadPos)//.NextOffset=fmt.Sprintf("%v,%v",d.segment,nextReadPos)
			return messages,false,err
		}
		readBuf=newData
	}

	message := queue.Message{
		Data:       readBuf,
		Size:       totalBytes,
		Offset:     queue.Itoa64(d.segment)+","+queue.Itoa64(previousPos),//fmt.Sprintf("%v,%v", d.segment, previousPos),
		NextOffset: queue.Itoa64(d.segment)+","+queue.Itoa64(nextReadPos),//fmt.Sprintf("%v,%v", d.segment, nextReadPos),
	}

	ctx.UpdateNextOffset(d.segment,nextReadPos)//.NextOffset = fmt.Sprintf("%v,%v", d.segment, nextReadPos)

	messages = append(messages, message)
	totalMessageSize += message.Size

	if len(messages) >= d.cCfg.FetchMaxMessages {
		log.Tracef("queue:%v, consumer:%v, total messages count(%v)>=max message count(%v)", d.queue, d.cCfg.Name, len(messages), d.cCfg.FetchMaxMessages)
		return messages, false, err
	}

	if totalMessageSize > d.cCfg.FetchMaxBytes && d.cCfg.FetchMaxBytes > 0 {
		log.Tracef("queue:%v, consumer:%v, total messages size(%v)>=max message size(%v)", d.queue, d.cCfg.Name, util.ByteSize(uint64(totalMessageSize)), util.ByteSize(uint64(d.cCfg.FetchMaxBytes)))
		return messages, false, err
	}

	if nextReadPos >= d.maxBytesPerFileRead {
		nextFile,exists := SmartGetFileName(d.mCfg,d.queue, d.segment+1)
		if exists||util.FileExists(nextFile) {
			//current have changes, reload file with new position
			if d.getFileSize()>d.readPos{
				if global.Env().IsDebug{
					log.Debug("current file have changes, reload:",d.queue,",",d.getFileSize()," > ",d.readPos)
				}
				ctx.UpdateNextOffset(d.segment,d.readPos)//.NextOffset = fmt.Sprintf("%v,%v", d.segment, d.readPos)
				err=d.ResetOffset(d.segment,d.readPos)
				if err!=nil{
					if strings.Contains(err.Error(),"not found"){
						return messages, false, nil
					}
					panic(err)
				}

				if d.cCfg.EOFRetryDelayInMs>0{
					time.Sleep(time.Duration(d.cCfg.EOFRetryDelayInMs)*time.Millisecond)
				}

				goto READ_MSG
			}

			log.Trace("EOF, continue read:", nextFile)

			Notify(d.queue, ReadComplete, d.segment)
			ctx.UpdateNextOffset(d.segment,d.readPos)//.NextOffset = fmt.Sprintf("%v,%v", d.segment, d.readPos)
			err=d.ResetOffset(d.segment+1,0)
			if err!=nil{
				if strings.Contains(err.Error(),"not found"){
					return messages, false, nil
				}
				panic(err)
			}
			goto READ_MSG
		}
		return messages, false, err
	}

	messageOffset++
	goto READ_MSG
}

func (d *Consumer) Close() error {
	d.fileLock.Lock()
	d.fileLock.Unlock()
	d.diskQueue.DeleteSegmentConsumerInReading(d.ID)
	if d.readFile!=nil{
		 err:=d.readFile.Close()
		 if err!=nil&&!util.ContainStr(err.Error(),"already"){
			 log.Error(err)
			 panic(err)
		 }
		 d.readFile=nil
		return err
	}
	return nil
}

func (d *Consumer) ResetOffset(segment,readPos int64)error {

	if global.Env().IsDebug{
		log.Debugf("reset offset: %v,%v, file: %v",segment,readPos,d.fileName)
	}

	if segment>d.diskQueue.writeSegmentNum{
		log.Errorf("reading segment [%v] is greater than writing segment [%v]",segment,d.diskQueue.writeSegmentNum)
		return io.EOF
	}

	d.fileLock.Lock()
	d.fileLock.Unlock()
	if d.segment!=segment{
		if global.Env().IsDebug{
			log.Debugf("start to switch segment, previous:%v,%v, now: %v,%v",d.segment,d.readPos,segment,readPos)
		}
		//potential file handler leak
		if d.readFile!=nil {
			d.readFile.Close()
		}
	}


	d.segment= segment
	d.readPos= readPos
	d.maxBytesPerFileRead=0

	d.diskQueue.UpdateSegmentConsumerInReading(d.ID,d.segment)

	fileName,exists := SmartGetFileName(d.mCfg,d.queue, segment)
	if !exists{
		if !util.FileExists(fileName) {
			return errors.New(fileName+" not found")
		}
	}

	var err error
	readFile, err:= os.OpenFile(fileName, os.O_RDONLY, 0600)
	if err != nil {
		log.Error(err)
		return err
	}
	d.readFile=readFile
	var maxBytesPerFileRead int64= d.mCfg.MaxBytesPerFile
	var stat os.FileInfo
	stat, err = readFile.Stat()
	if err!=nil{
		log.Error(err)
		return err
	}
	maxBytesPerFileRead = stat.Size()

	if d.readPos > 0 {
		_, err = readFile.Seek(d.readPos, 0)
		if err != nil {
			log.Error(err)
			return err
		}
	}

	d.maxBytesPerFileRead=maxBytesPerFileRead
	if d.reader!=nil{
		d.reader.Reset(d.readFile)
	}else{
		d.reader= bufio.NewReader(d.readFile)
	}
	d.fileName=fileName
	return nil
}