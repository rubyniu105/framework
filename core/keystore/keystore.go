/* Copyright © INFINI Ltd. All rights reserved.
 * Web: https://infinilabs.com
 * Email: hello#infini.ltd */

package keystore

import (
	"fmt"
	log "github.com/cihub/seelog"
	"github.com/fsnotify/fsnotify"
	"infini.sh/framework/core/global"
	"infini.sh/framework/core/util"
	"infini.sh/framework/lib/go-ucfg"
	"infini.sh/framework/lib/go-ucfg/parse"
	"infini.sh/framework/lib/keystore"
	"os"
	"path/filepath"
	"strings"
	"sync"
)

var (
	defaultKeystore keystore.Keystore
	keystoreOnce sync.Once
)

func GetOrInitKeystore() (keystore.Keystore, error) {
	var err error
	keystoreOnce.Do(func() {
		defaultKeystore, err = initKeystore()
	})
	return defaultKeystore, err
}

func GetWriteableKeystore()(keystore.WritableKeystore, error) {
	ks, err := GetOrInitKeystore()
	if err != nil {
		return nil, err
	}
	return keystore.AsWritableKeystore(ks)
}

func getKeystorePath() string{
	ksPath := GetKeystoreBasePath()
	return filepath.Join(ksPath, ".keystore")
}
func initKeystore() (keystore.Keystore, error){
	keystorePath := getKeystorePath()
	if !util.FileExists(keystorePath){
		err := os.Mkdir(keystorePath, 0750)
		if err != nil {
			return nil, err
		}
	}
	storePath := filepath.Join(keystorePath, "ks")
	keyPath := filepath.Join(keystorePath, "key")
	var password *keystore.SecureString
	if !util.FileExists(keyPath){
		randStr := util.GenerateRandomString(32)
		password = keystore.NewSecureString([]byte(randStr))
		_, err := util.FilePutContent(keyPath, randStr)
		if err != nil {
			return nil, fmt.Errorf("save keystore password to file error: %w", err)
		}
	}else{
		passwordBytes, err := util.FileGetContent(keyPath)
		if err != nil {
			return nil, fmt.Errorf("read keystore password file error: %w", err)
		}
		password = keystore.NewSecureString(passwordBytes)
	}

	return keystore.NewFileKeystoreWithPassword(storePath, password)
}

const PathEnvKey = "KEYSTORE_PATH"
func GetKeystoreBasePath() string {
	ksPath, exists := os.LookupEnv(PathEnvKey)
	if exists {
		return ksPath
	}
	return global.Env().GetDataDir()
}

func GetValue(key string) ([]byte, error) {
	ks, err := GetOrInitKeystore()
	if err != nil {
		return nil, err
	}
	secStr, err := ks.Retrieve(key)
	if err != nil {
		return nil, err
	}
	return secStr.Get()
}

func SetValue(key string, value []byte) error {
	ksw, err := GetWriteableKeystore()
	if err != nil {
		return err
	}
	err = ksw.Store(key, value)
	if err != nil {
		return err
	}
	return ksw.Save()
}

func GetVariableResolver() (ucfg.Option, error){
	ks, err := GetOrInitKeystore()
	if err != nil {
		return nil, err
	}
	return ucfg.Resolve(func(keyName string) (string, parse.Config, error) {
		if strings.HasPrefix(keyName, "keystore."){
			v, pc, err := keystore.ResolverWrap(ks)(keyName[9:])
			if err == ucfg.ErrMissing {
				return "", parse.NoopConfig, nil
			}
			return v, pc, err
		}
		return "", parse.NoopConfig, ucfg.ErrMissing
	}), nil
}

var watcher *fsnotify.Watcher
func Watch(){
	var err error
	watcher, err = fsnotify.NewWatcher()
	if err != nil {
		log.Error(err)
	}
	keystorePath := getKeystorePath()
	keystoreFile := filepath.Join(keystorePath, "ks")
	err = watcher.Add(keystoreFile)
	if err != nil {
		log.Error(err)
	}
	for {
		select {
		case event, ok := <-watcher.Events:
			if !ok {
				return
			}
			if event.Has(fsnotify.Create) {
				defaultKeystore, err = initKeystore()
				if err != nil {
					log.Error("init keystore error: ", err)
				}
			}
		case err, ok := <-watcher.Errors:
			if !ok {
				return
			}
			log.Error(err)
		}
	}
}

func CloseWatch(){
	if watcher != nil {
		watcher.Close()
	}
}