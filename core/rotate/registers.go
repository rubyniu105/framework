/* ©INFINI, All Rights Reserved.
 * mail: contact#infini.ltd */

package rotate

import (
	log "github.com/cihub/seelog"
	"infini.sh/framework/core/global"
	"sync"
)

var fileHandlers = map[string]*RotateWriter{}
var lock sync.RWMutex
var callbackRegistered bool

type RotateConfig struct {
	Compress     bool `config:"compress_after_rotate"`
	MaxFileAge   int  `config:"max_file_age"`
	MaxFileCount int  `config:"max_file_count"`
	MaxFileSize  int  `config:"max_file_size_in_mb"`
}

var DefaultConfig = RotateConfig{
	Compress:     true,
	MaxFileAge:   0,
	MaxFileCount: 10,
	MaxFileSize:  1024,
}

func GetFileHandler(path string, config RotateConfig) *RotateWriter {
	lock.Lock()
	if !callbackRegistered{
		global.RegisterShutdownCallback(func() {
			Close()
		})
		callbackRegistered=true
	}
	v, ok := fileHandlers[path]
	if !ok {
		v = &RotateWriter{
			Filename:         path,
			Compress:         config.Compress,
			MaxFileAge:       config.MaxFileAge,
			MaxRotationCount: config.MaxFileCount,
			MaxFileSize:      config.MaxFileSize,
		}
		fileHandlers[path] = v
	}
	lock.Unlock()
	return v
}

func Close() {
	for k, v := range fileHandlers {
		log.Trace("closing rotate writer: ", k)
		err := v.Close()
		if err != nil {
			log.Error(err)
		}
	}
}
