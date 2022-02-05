/*
Copyright Medcl (m AT medcl.net)

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

package elastic

import (
	"crypto/tls"
	"fmt"
	log "github.com/cihub/seelog"
	"infini.sh/framework/core/errors"
	"infini.sh/framework/core/global"
	"infini.sh/framework/core/rate"
	"infini.sh/framework/core/stats"
	"infini.sh/framework/core/util"
	"infini.sh/framework/lib/fasthttp"
	uri "net/url"
	"strings"
	"sync"
	"time"
)

var apis = sync.Map{}
var cfgs = sync.Map{}
var metas = sync.Map{}
var hosts = sync.Map{}

func RegisterInstance(elastic string, cfg ElasticsearchConfig, handler API) {
	apis.Store(elastic, handler)
	cfgs.Store(elastic, &cfg)
}

func GetOrInitHost(host string) *NodeAvailable {
	v := NodeAvailable{Host: host, available: true}
	v1, loaded := hosts.LoadOrStore(host, &v)
	if loaded {
		return v1.(*NodeAvailable)
	}
	return &v
}

func RemoveInstance(elastic string) {
	cfgs.Delete(elastic)
	apis.Delete(elastic)
	metas.Delete(elastic)
}

func GetConfig(k string) *ElasticsearchConfig {
	if k == "" {
		panic(fmt.Errorf("elasticsearch config undefined"))
	}
	v, ok := cfgs.Load(k)
	if !ok {
		panic(fmt.Sprintf("elasticsearch config [%v] was not found", k))
	}
	return v.(*ElasticsearchConfig)
}

var versions = map[string]int{}
var versionLock = sync.RWMutex{}

func (c *ElasticsearchConfig) ParseMajorVersion() int {
	if c.Version != "" {
		vs := strings.Split(c.Version, ".")
		n, err := util.ToInt(vs[0])
		if err != nil {
			panic(err)
		}
		return n
	}
	return -1
}

func (meta *ElasticsearchMetadata) GetMajorVersion() int {

	versionLock.RLock()
	esMajorVersion, ok := versions[meta.Config.ID]
	versionLock.RUnlock()

	if !ok {
		versionLock.Lock()
		defer versionLock.Unlock()

		v:=meta.Config.ParseMajorVersion()
		if v>0{
			versions[meta.Config.ID] = v
			return v
		}

		esMajorVersion = GetClient(meta.Config.ID).GetMajorVersion()
		if esMajorVersion>0{
			versions[meta.Config.ID] = esMajorVersion
		}
	}

	return esMajorVersion
}

func GetOrInitMetadata(cfg *ElasticsearchConfig) *ElasticsearchMetadata {
	v := GetMetadata(cfg.ID)
	if v == nil {
		v = &ElasticsearchMetadata{Config: cfg}
		v.Init(false)
		SetMetadata(cfg.ID, v)
	}
	return v
}

func GetMetadata(k string) *ElasticsearchMetadata {
	if k == "" {
		panic(fmt.Errorf("elasticsearch metata undefined"))
	}

	v, ok := metas.Load(k)
	if !ok {
		log.Debug(fmt.Sprintf("elasticsearch metadata [%v] was not found", k))
		return nil
	}
	x, ok := v.(*ElasticsearchMetadata)
	return x
}

func GetClient(k string) API {
	if k == "" {
		panic(fmt.Errorf("elasticsearch config undefined"))
	}

	v, ok := apis.Load(k)
	if ok {
		f, ok := v.(API)
		if ok {
			return f
		}
	}

	panic(fmt.Sprintf("elasticsearch client [%v] was not found", k))
}

//最后返回的为判断是否继续 walk
func WalkMetadata(walkFunc func(key, value interface{}) bool) {
	metas.Range(walkFunc)
}

func WalkConfigs(walkFunc func(key, value interface{}) bool) {
	cfgs.Range(walkFunc)
}

func WalkHosts(walkFunc func(key, value interface{}) bool) {
	hosts.Range(walkFunc)
}

func SetMetadata(k string, v *ElasticsearchMetadata) {
	metas.Store(k, v)
}

func IsHostAvailable(host string) bool {
	info, ok := hosts.Load(host)
	if ok {
		return info.(*NodeAvailable).IsAvailable()
	}
	log.Debugf("no available info for host [%v]", host)
	return true
}

//ip:port
func (meta *ElasticsearchMetadata) GetSeedHosts() []string {

	if len(meta.seedHosts) > 0 {
		return meta.seedHosts
	}

	hosts := []string{}
	if len(meta.Config.Hosts) > 0 {
		for _, h := range meta.Config.Hosts {
			hosts = append(hosts, h)
		}
	}
	if len(meta.Config.Host) > 0 {
		hosts = append(hosts, meta.Config.Host)
	}

	if meta.Config.Endpoint != "" {
		i, err := uri.Parse(meta.Config.Endpoint)
		if err != nil {
			panic(err)
		}
		hosts = append(hosts, i.Host)
	}
	if len(meta.Config.Endpoints) > 0 {
		for _, h := range meta.Config.Endpoints {
			i, err := uri.Parse(h)
			if err != nil {
				panic(err)
			}
			hosts = append(hosts, i.Host)
		}
	}
	if len(hosts) == 0 {
		panic(errors.Errorf("no valid endpoint for [%v]", meta.Config.Name))
	}
	meta.seedHosts = hosts
	return meta.seedHosts
}

func (node *NodesInfo) GetHttpPublishHost() string {
	if util.ContainStr(node.Http.PublishAddress,"/"){
		if global.Env().IsDebug{
			log.Tracef("node's public address contains `/`,try to remove prefix")
		}
		arr:=strings.Split(node.Http.PublishAddress,"/")
		if len(arr)==2{
			return arr[1]
		}
	}
	return node.Http.PublishAddress
}


var clients = map[string]*fasthttp.Client{}
var clientLock sync.RWMutex


func (metadata *ElasticsearchMetadata) GetActivePreferredHost(host string) *fasthttp.Client {

	//get available host
	available := IsHostAvailable(host)

	if !available {
		if metadata.IsAvailable() {
			newEndpoint := metadata.GetActiveHost()
			log.Warnf("[%v] is not available, try: [%v]", host, newEndpoint)
			host = newEndpoint
		} else {
			time.Sleep(1 * time.Second)
		}
	}

	clientLock.RLock()
	client, ok := clients[host]
	clientLock.RUnlock()

	//TODO configureable
	if !ok{
		clientLock.Lock()
		defer clientLock.Unlock()

		client = &fasthttp.Client{
			MaxConnsPerHost: 5000,
			MaxConnDuration:               0,
			MaxIdleConnDuration:           0,
			ReadTimeout:                   0,
			WriteTimeout:                  0,
			DisableHeaderNamesNormalizing: true,
			DisablePathNormalizing:        true,
			MaxConnWaitTimeout:            0,
			TLSConfig:                     &tls.Config{InsecureSkipVerify: true},
		}

		if metadata.Config.TrafficControl != nil &&metadata.Config.TrafficControl.MaxConnectionPerNode>0 {
			client.MaxConnsPerHost = metadata.Config.TrafficControl.MaxConnectionPerNode
		}

		clients[host] = client

	}

	return client
}


func (metadata *ElasticsearchMetadata) LastSuccess()time.Time{
	return metadata.lastSuccess
}

func (metadata *ElasticsearchMetadata) CheckNodeTrafficThrottle(node string,req , dataSize ,maxWaitInMS int){
	if metadata.Config.TrafficControl != nil {

		if metadata.Config.TrafficControl.MaxWaitTimeInMs <= 0 {
			metadata.Config.TrafficControl.MaxWaitTimeInMs = 10 * 1000
		}

		if maxWaitInMS>0 {
			metadata.Config.TrafficControl.MaxWaitTimeInMs = maxWaitInMS
		}

		maxTime := time.Duration(metadata.Config.TrafficControl.MaxWaitTimeInMs) * time.Millisecond
		startTime := time.Now()
	RetryRateLimit:

		if time.Now().Sub(startTime) < maxTime {

			if metadata.Config.TrafficControl.MaxQpsPerNode > 0 && req>0 {
				if !rate.GetRateLimiterPerSecond(metadata.Config.ID, "req-max_qps", int(metadata.Config.TrafficControl.MaxQpsPerNode)).Allow() {
					stats.Increment(metadata.Config.ID, "req-max_qps_throttled")
					if global.Env().IsDebug {
						log.Debugf("request qps throttle on node [%v]", node)
					}
					time.Sleep(10 * time.Millisecond)
					goto RetryRateLimit
				}
			}

			if metadata.Config.TrafficControl.MaxBytesPerNode > 0 &&dataSize>0{
				if !rate.GetRateLimiterPerSecond(metadata.Config.ID, "req-max_bps",
					int(metadata.Config.TrafficControl.MaxBytesPerNode)).AllowN(time.Now(), dataSize) {
					stats.Increment(metadata.Config.ID, "req-max_bps_throttled")
					if global.Env().IsDebug {
						log.Debugf("request traffic throttle on node [%v]", node)
					}
					time.Sleep(10 * time.Millisecond)
					goto RetryRateLimit
				}
			}

		} else {
			log.Warn("reached max traffic control time, throttle exit")
		}
	}
}

//func (metadata *ElasticsearchMetadata) GetIndexSetting(index string) (string,*IndexInfo, error) {
//	if metadata.Indices==nil{
//		return index,nil,errors.Errorf("index [%v] setting not found,", index)
//	}
//
//	indexSettings, ok := (*metadata.Indices)[index]
//
//	if !ok {
//		if global.Env().IsDebug {
//			log.Tracef("index [%v] was not found in index settings,", index)
//		}
//
//		if metadata.Aliases!=nil{
//			alias, ok := (*metadata.Aliases)[index]
//			if ok {
//				if global.Env().IsDebug {
//					log.Tracef("found index [%v] in alias settings,", index)
//				}
//				newIndex := alias.WriteIndex
//				if alias.WriteIndex == "" {
//					if len(alias.Index) == 1 {
//						newIndex = alias.Index[0]
//						if global.Env().IsDebug {
//							log.Trace("found index [%v] in alias settings, no write_index, but only have one index, will use it,", index)
//						}
//					} else {
//						log.Warnf("writer_index [%v] was not found in alias [%v] settings,", index, alias)
//						return index,nil,errors.Error("writer_index was not found in alias settings,", index, ",", alias)
//					}
//				}
//				indexSettings, ok = (*metadata.Indices)[newIndex]
//				if ok {
//					if global.Env().IsDebug {
//						log.Trace("index was found in index settings, ", index, "=>", newIndex, ",", indexSettings)
//					}
//					index = newIndex
//					return index,&indexSettings,nil
//
//				} else {
//					if global.Env().IsDebug {
//						log.Tracef("writer_index [%v] was not found in index settings,", index)
//					}
//				}
//			} else {
//				if global.Env().IsDebug {
//					log.Tracef("index [%v] was not found in alias settings,", index)
//				}
//			}
//		}
//
//		return index,nil,errors.Errorf("index [%v] setting not found,", index)
//	}
//
//	return index,&indexSettings,nil
//}


func (metadata *ElasticsearchMetadata) GetIndexRoutingTable(index string) (map[string][]IndexShardRouting,error) {
	if metadata.ClusterState!=nil{
		if metadata.ClusterState.RoutingTable!=nil{
			table,ok:=metadata.ClusterState.RoutingTable.Indices[index]
			if !ok{
				//check alias
				if global.Env().IsDebug {
					log.Tracef("index [%v] was not found in index settings,", index)
				}
				if metadata.Aliases!=nil{
					alias, ok := (*metadata.Aliases)[index]
					if ok {
						if global.Env().IsDebug {
							log.Tracef("found index [%v] in alias settings,", index)
						}
						newIndex := alias.WriteIndex
						if alias.WriteIndex == "" {
							if len(alias.Index) == 1 {
								newIndex = alias.Index[0]
								if global.Env().IsDebug {
									log.Trace("found index [%v] in alias settings, no write_index, but only have one index, will use it,", index)
								}
							} else {
								//log.Warnf("writer_index [%v] was not found in alias [%v] settings,", index, alias)
								return nil,errors.Error("routing table not found and writer_index was not found in alias settings,", index, ",", alias)
							}
						}
						//try again with real index name
						return metadata.GetIndexRoutingTable(newIndex)
					} else {
						if global.Env().IsDebug {
							log.Tracef("index [%v] was not found in alias settings,", index)
						}
					}
				}
			}
			return table.Shards,nil
		}
	}
	return nil, errors.Errorf("routing table for index [%v] was not found",index)
}

func (metadata *ElasticsearchMetadata) GetIndexPrimaryShardRoutingTable(index string,shard int)(*IndexShardRouting,error)  {
	routingTable, err := metadata.GetIndexRoutingTable(index)
	if err != nil {
		return nil,err
	}
	shards,ok:=routingTable[util.ToString(shard)]
	if ok{
		for _,x:=range shards{
			if x.Primary{
				return &x,nil
			}
		}
	}
	return nil,errors.New("not found")
}
