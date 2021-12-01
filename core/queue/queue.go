/*
Copyright 2016 Medcl (m AT medcl.net)

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

   http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package queue

import (
	log "github.com/cihub/seelog"
	"github.com/emirpasic/gods/sets/hashset"
	"infini.sh/framework/core/errors"
	"infini.sh/framework/core/stats"
	"infini.sh/framework/core/util"
	"sync"
	"time"
)

type QueueAPI interface {
	Push(string, []byte) error
	Pop(string, time.Duration) (data []byte, timeout bool)
	Close(string) error
	Depth(string) int64
	GetQueues() []string
}

var defaultHandler QueueAPI
var handlers map[string]QueueAPI = map[string]QueueAPI{}

type Config struct {
	Source   string                 `config:"source" json:"source"`
	Id       string                 `config:"id" json:"id"`   //uuid for each queue
	Name     string                 `config:"name" json:"name"` //unique name of each queue
	Codec    string                 `config:"codec" json:"codec"`
	Type     string                 `config:"type" json:"type"`
	Metadata map[string]interface{} `config:"metadata" json:"metadata"`
}

func getHandler(name string) QueueAPI {
	handler, ok := handlers[name]
	if ok {
		return handler
	}
	return defaultHandler
}

func Push(k string, v []byte) error {
	var err error = nil
	if k == "" {
		panic(errors.New("queue name can't be nil"))
	}
	handler := getHandler(k)
	if handler != nil {
		err = handler.Push(k, v)
		if err == nil {
			stats.Increment("queue."+k, "push")
			return nil
		}
		stats.Increment("queue."+k, "push_error")
		return err
	}
	panic(errors.New("handler is not registered"))
}

var pauseMsg = errors.New("queue was paused to read")

var configs = map[string]*Config{}
var idConfigs = map[string]*Config{}
var cfgLock = sync.RWMutex{}

func RegisterConfig(name string, cfg *Config) (bool,*Config, error) {
	cfgLock.Lock()
	defer cfgLock.Unlock()

	v, ok := configs[name]
	if ok {
		return true,v, errors.New("config exists")
	} else {
		//init empty id
		if cfg.Id == "" {
			cfg.Id = util.GetUUID()
		}
		idConfigs[cfg.Id] = cfg
		configs[name] = cfg
		return false,cfg, nil
	}
}

func IsConfigExists(name string) bool {
	cfgLock.Lock()
	defer cfgLock.Unlock()
	_, ok := configs[name]
	return ok
}

func GetConfig(name string) (*Config, bool) {
	cfgLock.Lock()
	defer cfgLock.Unlock()
	v, ok := configs[name]
	return v, ok
}

func GetConfigByUUID(id string) (*Config, bool) {
	cfgLock.Lock()
	defer cfgLock.Unlock()
	v, ok := idConfigs[id]
	return v, ok
}

func GetAllConfigs() map[string]*Config {
	cfgLock.Lock()
	defer cfgLock.Unlock()
	return configs
}

func Pop(k string) ([]byte, error) {
	if k == "" {
		panic(errors.New("queue name can't be nil"))
	}

	handler := getHandler(k)
	if handler != nil {
		if pausedReadQueue.Contains(k) {
			return nil, pauseMsg
		}

		o, timeout := handler.Pop(k, -1)
		if !timeout {
			stats.Increment("queue."+k, "pop")
			return o, nil
		}
		stats.Increment("queue."+k, "pop_timeout")
		return o, errors.New("timeout")
	}
	panic(errors.New("handler is not registered"))
}

func PopTimeout(k string, timeoutInSeconds time.Duration) (data []byte, timeout bool, err error) {
	if k == "" {
		panic(errors.New("queue name can't be nil"))
	}

	if timeoutInSeconds < 1 {
		timeoutInSeconds = 5
	}

	handler := getHandler(k)

	if handler != nil {

		if pausedReadQueue.Contains(k) {
			return nil, false, pauseMsg
		}

		o, timeout := handler.Pop(k, timeoutInSeconds)
		if !timeout {
			stats.Increment("queue."+k, "pop")
		}
		stats.Increment("queue."+k, "pop_timeout")
		return o, timeout, nil
	}
	panic(errors.New("handler is not registered"))
}

func Close(k string) error {
	handler := getHandler(k)
	if handler != nil {
		o := handler.Close(k)
		stats.Increment("queue."+k, "close")
		return o
	}
	stats.Increment("queue."+k, "close_error")
	panic(errors.New("handler is not closed"))
}

func Depth(k string) int64 {
	if k == "" {
		panic(errors.New("queue name can't be nil"))
	}

	handler := getHandler(k)
	if handler != nil {
		o := handler.Depth(k)
		stats.Increment("queue."+k, "call_depth")
		return o
	}
	panic(errors.New("handler is not registered"))
}

func GetQueues() map[string][]string {
	results := map[string][]string{}
	for q, handler := range adapters {
		result := []string{}
		if handler != nil {
			o := handler.GetQueues()
			stats.Increment("queue."+q, "get_queues")
			result = append(result, o...)
			results[q] = result
		}
	}
	return results
}

var pausedReadQueue = hashset.New()
var pauseChan map[string]chan bool = map[string]chan bool{}
var pauseCount = map[string]int{}
var pauseLock sync.Mutex

func PauseRead(k string) {
	pauseLock.Lock()
	defer pauseLock.Unlock()
	pauseCount[k] = 0
	pauseChan[k] = make(chan bool)
	pausedReadQueue.Add(k)
}

func ResumeRead(k string) {
	pauseLock.Lock()
	defer pauseLock.Unlock()
	pausedReadQueue.Remove(k)
	size := pauseCount[k]
	for i := 0; i < size; i++ {
		pauseChan[k] <- true
	}
	log.Debugf("queue: %s was resumed, signal: %v", k, size)
}

var adapters map[string]QueueAPI = map[string]QueueAPI{}

func RegisterDefaultHandler(h QueueAPI) {
	defaultHandler = h
}

func IniQueue(name string, typeOfAdaptor string) {
	if name == "" {
		panic(errors.New("queue name can't be nil"))
	}

	handler, ok := adapters[typeOfAdaptor]
	if !ok {
		panic(errors.Errorf("queue adaptor [%v] not found", typeOfAdaptor))
	}
	handlers[name] = handler
}

func Register(name string, h QueueAPI) {
	_, ok := adapters[name]
	if ok {
		panic(errors.Errorf("queue handler with same name: %v already exists", name))
	}

	adapters[name] = h
	log.Debug("register queue handler: ", name)
}
