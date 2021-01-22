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

package elastic

import (
	"encoding/base64"
	"fmt"
	"github.com/bkaradzic/go-lz4"
	log "github.com/cihub/seelog"
	"infini.sh/framework/core/elastic"
	"infini.sh/framework/core/errors"
	"infini.sh/framework/core/orm"
	"infini.sh/framework/core/util"
)

type ElasticStore struct {
	Client elastic.API
	Config StoreConfig
}

type StoreConfig struct {
	Enabled      bool   `config:"enabled"`
	IndexName  string `config:"index_name"`
}

func (store ElasticStore) Open() error {
	o:=Blob{}
	err:=orm.RegisterSchemaWithIndexName(o,store.Config.IndexName)
	if err!=nil{
		panic(err)
	}
	if store.Config.IndexName==""{
		store.Config.IndexName=orm.GetIndexName(o)
	}
	return nil
}

func (store ElasticStore) Close() error {
	return nil
}

func (store ElasticStore) GetCompressedValue(bucket string, key []byte) ([]byte, error) {

	data, err := store.GetValue(bucket, key)
	if err != nil {
		return nil, err
	}
	data, err = lz4.Decode(nil, data)
	if err != nil {
		log.Error("Failed to decode:", err)
		return nil, err
	}
	return data, nil
}

func (store ElasticStore) GetValue(bucket string, key []byte) ([]byte, error) {
	response, err := store.Client.Get(store.Config.IndexName,"_doc", getKey(bucket, string(key)))
	if err != nil {
		return nil, err
	}
	if response.Found {
		content := response.Source["content"]
		if content != nil {
			uDec, err := base64.URLEncoding.DecodeString(content.(string))

			if err != nil {
				return nil, err
			}
			return uDec, nil
		}
	}
	return nil, errors.New("not found")
}

func (store ElasticStore) AddValueCompress(bucket string, key []byte, value []byte) error {
	value, err := lz4.Encode(nil, value)
	if err != nil {
		log.Error("Failed to encode:", err)
		return err
	}
	return store.AddValue(bucket, key, value)
}

func getKey(bucket, key string) string {
	return util.MD5digest(fmt.Sprintf("%s_%s", bucket, key))
}

func (store ElasticStore) AddValue(bucket string, key []byte, value []byte) error {
	file := Blob{}
	file.Content = base64.URLEncoding.EncodeToString(value)
	_, err := store.Client.Index(store.Config.IndexName, "_doc", getKey(bucket, string(key)), file)
	return err
}

func (store ElasticStore) DeleteKey(bucket string, key []byte) error {
	_, err := store.Client.Delete(store.Config.IndexName, "_doc", getKey(bucket, string(key)))
	return err
}

func (store ElasticStore) DeleteBucket(bucket string) error {
	panic(errors.New("not implemented yet"))
}
