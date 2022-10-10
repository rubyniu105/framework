/* Copyright © INFINI LTD. All rights reserved.
 * Web: https://infinilabs.com
 * Email: hello#infini.ltd */

package nutsdb

import (
	"github.com/xujiajun/nutsdb"
	"github.com/bkaradzic/go-lz4"
	log "github.com/cihub/seelog"
	"sync"
)

type NutsdbFilter struct {
	Options nutsdb.Options
}

var v = []byte("true")
var l sync.RWMutex
var handler *nutsdb.DB

func (filter *NutsdbFilter) Open() error {
	l.Lock()
	defer l.Unlock()

	var err error
	h, err := nutsdb.Open(filter.Options)
	if err != nil {
		panic(err)
	}
	handler=h
	return nil
}

func (filter *NutsdbFilter) Close() error {
	if handler!=nil{
		handler.Close()
	}
	return nil
}

func (filter *NutsdbFilter) Exists(bucket string, key []byte) bool {

	var entry *nutsdb.Entry
	if err := handler.View(
		func(tx *nutsdb.Tx) error {
			if e, err := tx.Get(bucket, key); err != nil {
				return err
			} else {
				entry=e
			}
			return nil
		}); err != nil {
	}

	if entry!=nil{
		return true
	}
		return false
}

func (filter *NutsdbFilter) Add(bucket string, key []byte) error {
	err := handler.Update(
		func(tx *nutsdb.Tx) error {
			val := []byte("0")
			if err := tx.Put(bucket, key, val, 0); err != nil {
				return err
			}
			return nil
		})

	return err
}

func (filter *NutsdbFilter) Delete(bucket string, key []byte) error {
	return handler.Update(
		func(tx *nutsdb.Tx) error {
			if err := tx.Delete(bucket, key); err != nil {
				return err
			}
			return nil
		})
}

func (filter *NutsdbFilter) CheckThenAdd(bucket string, key []byte) (b bool, err error) {
	l.Lock()
	defer l.Unlock()
	b = filter.Exists(bucket, key)
	if !b {
		err = filter.Add(bucket, key)
	}
	return b, err
}



//for kv implementation
func (f *NutsdbFilter) GetValue(bucket string, key []byte) ([]byte, error) {
	var entry *nutsdb.Entry
	if err := handler.View(
		func(tx *nutsdb.Tx) error {
			if e, err := tx.Get(bucket, key); err != nil {
				return err
			} else {
				entry=e
			}
			return nil
		}); err != nil {
	}

	if entry!=nil{
		return entry.Value,nil
	}
	return nil,nil
}

func (f *NutsdbFilter) GetCompressedValue(bucket string, key []byte) ([]byte, error) {
	d,err:=f.GetValue(bucket,key)
	if err!=nil{
		return d, err
	}
	data, err := lz4.Decode(nil, d)
	if err != nil {
		log.Error("Failed to decode:", err)
		return nil, err
	}
	return data,err
}

func (f *NutsdbFilter) AddValueCompress(bucket string, key []byte, value []byte) error {
	value, err := lz4.Encode(nil, value)
	if err != nil {
		log.Error("Failed to encode:", err)
		return err
	}
	return f.AddValue(bucket, key, value)
}

func (f *NutsdbFilter) AddValue(bucket string, key []byte, value []byte) error {
	err := handler.Update(
		func(tx *nutsdb.Tx) error {
			if err := tx.Put(bucket, key, value, 0); err != nil {
				return err
			}
			return nil
		})

	return err
}

func (f *NutsdbFilter) ExistsKey(bucket string, key []byte) (bool, error) {
	ok:= f.Exists(bucket,key)
	return ok,nil
}

func (f *NutsdbFilter) DeleteKey(bucket string, key []byte) error {
	return f.Delete(bucket,key)
}

func (f *NutsdbFilter) IterateBucketKeys(bucket string, rangeFunc func (key, value []byte) error) error {
	err := handler.View(
		func(tx *nutsdb.Tx) error {
			entries, err := tx.GetAll(bucket)
			if err != nil {
				return err
			}

			for _, entry := range entries {
				err = rangeFunc(entry.Key, entry.Value)
				if err != nil {
					return err
				}
			}
			return nil
		})
	return err
}

func (f *NutsdbFilter) Merge() error {
	return handler.Merge()
}