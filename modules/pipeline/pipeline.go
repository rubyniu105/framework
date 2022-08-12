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

package pipeline

import (
	log "github.com/cihub/seelog"
	"infini.sh/framework/core/api"
	"infini.sh/framework/core/api/router"
	"infini.sh/framework/core/config"
	"infini.sh/framework/core/env"
	"infini.sh/framework/core/errors"
	"infini.sh/framework/core/global"
	"infini.sh/framework/core/pipeline"
	"infini.sh/framework/core/progress"
	"infini.sh/framework/core/rate"
	"infini.sh/framework/core/util"
	"net/http"
	"runtime"
	"sync"
	"time"
)

type PipeModule struct {
	api.Handler
	pipelines map[string]*pipeline.Processors
	configs   map[string]*PipelineConfigV2
	contexts  map[string]*pipeline.Context
	started   bool
	//runners map[string]*PipeRunner
	wg sync.WaitGroup
}

func (module PipeModule) Name() string {
	return "Pipeline"
}

var moduleCfg = struct {
	APIEnabled bool `config:"api_enabled"`
}{
	APIEnabled: true,
}

func (module *PipeModule) Setup(cfg *config.Config) {

	cfg.Unpack(&moduleCfg)

	if global.Env().IsDebug {
		log.Debug("pipeline framework config: ", moduleCfg)
	}

	module.pipelines = map[string]*pipeline.Processors{}
	module.contexts = map[string]*pipeline.Context{}
	module.configs = map[string]*PipelineConfigV2{}

	pipeline.RegisterProcessorPlugin("dag", pipeline.NewDAGProcessor)
	pipeline.RegisterProcessorPlugin("echo", NewEchoProcessor)

	if moduleCfg.APIEnabled {
		handler := API{}
		handler.Init()
		api.HandleAPIMethod(api.GET, "/pipeline/tasks/", module.getPipelines)
		api.HandleAPIMethod(api.POST, "/pipeline/task/:id/_start", module.startTask)
		api.HandleAPIMethod(api.POST, "/pipeline/task/:id/_stop", module.stopTask)
	}

}

type PipelineConfigV2 struct {
	Name           string `config:"name" json:"name,omitempty"`
	AutoStart      bool   `config:"auto_start" json:"auto_start"`
	KeepRunning    bool   `config:"keep_running" json:"keep_running"`
	RetryDelayInMs int    `config:"retry_delay_in_ms" json:"retry_delay_in_ms"`
	//Processors     []map[string]interface{} `config:"processor" json:"processor,omitempty"`
	Processors []*config.Config `config:"processor" json:"processor,omitempty"`
}

func (module *PipeModule) startTask(w http.ResponseWriter, req *http.Request, ps httprouter.Params) {
	id := ps.ByName("id")
	ctx, ok := module.contexts[id]
	if ok {
		if ctx.IsPause() {
			ctx.Resume()
		}

		if ctx.IsExit() {
			ctx.Resume()
		}

		if ctx.GetRunningState() != pipeline.STARTED {
			ctx.Starting()
		}
		module.WriteAckOKJSON(w)
	} else {
		module.WriteAckJSON(w, false, 404, util.MapStr{
			"error": "task not found",
		})
	}
}

func (module *PipeModule) stopTask(w http.ResponseWriter, req *http.Request, ps httprouter.Params) {
	id := ps.ByName("id")
	ctx, ok := module.contexts[id]
	if ok {
		if ctx.GetRunningState() == pipeline.STARTED || ctx.GetRunningState() == pipeline.STARTING {
			ctx.CancelTask()
			ctx.Exit()
		}
		module.WriteAckOKJSON(w)
	} else {
		module.WriteAckJSON(w, false, 404, util.MapStr{
			"error": "task not found",
		})
	}
}

func (module *PipeModule) getPipelines(w http.ResponseWriter, req *http.Request, ps httprouter.Params) {
	obj := util.MapStr{}
	for k, _ := range module.pipelines {
		obj[k] = util.MapStr{
			"state":      module.contexts[k].GetRunningState(),
			"start_time": module.contexts[k].GetStartTime(),
			"end_time":   module.contexts[k].GetEndTime(),
		}
	}
	module.WriteJSON(w, obj, 200)
}

func (module *PipeModule) Start() error {
	if module.started {
		return errors.New("pipeline framework already started, please stop it first.")
	}

	//TODO, each pipeline could be initialized
	var pipelines []PipelineConfigV2
	ok, err := env.ParseConfig("pipeline", &pipelines)
	if ok && err != nil {
		panic(err)
	}
	if ok {
		for _, v := range pipelines {

			processor, err := pipeline.NewPipeline(v.Processors)
			if err != nil {
				log.Error(v.Name, err)
				continue
			}
			ctx := pipeline.AcquireContext()

			if v.RetryDelayInMs <= 0 {
				v.RetryDelayInMs = 1000
			}

			module.configs[v.Name] = &v
			module.pipelines[v.Name] = processor
			module.contexts[v.Name] = ctx

			go func(cfg PipelineConfigV2, p *pipeline.Processors, ctx *pipeline.Context) {
				defer func() {
					if !global.Env().IsDebug {
						if r := recover(); r != nil {
							var err string
							switch r.(type) {
							case error:
								err = r.(error).Error()
							case runtime.Error:
								err = r.(runtime.Error).Error()
							case string:
								err = r.(string)
							}
							log.Errorf("error on pipeline: %v, retry delay: %vms", cfg.Name, err)
						}
					}
				}()

				if !cfg.AutoStart {
					ctx.Stopped()
				} else {
					ctx.Starting()
				}

				log.Debug("processing pipeline_v2:", cfg.Name)

				for {
					state := ctx.GetRunningState()

					log.Tracef("%v, state:%v", cfg.Name, state)

					switch state {
					case pipeline.STARTING:
					RESTART:
						if global.Env().IsDebug {
							log.Debugf("pipeline [%v] start running", cfg.Name)
						}
						ctx.Started()
						err = p.Process(ctx)
						if cfg.KeepRunning && !ctx.IsExit() {
							if ctx.GetRunningState() != pipeline.STOPPED && ctx.GetRunningState() != pipeline.STOPPING {
								log.Tracef("pipeline [%v] end running, restart again, retry in [%v]ms", cfg.Name, cfg.RetryDelayInMs)
								if cfg.RetryDelayInMs > 0 {
									time.Sleep(time.Duration(cfg.RetryDelayInMs) * time.Millisecond)
								}
								goto RESTART
							}
						}

						if err != nil {
							ctx.Failed()
							log.Errorf("error on pipeline:%v, %v", cfg.Name, err)
							break
						} else {
							ctx.Stopped()
						}

						log.Debugf("pipeline [%v] end running", cfg.Name)
						ctx.Finished()
						break
					case pipeline.FAILED:
						log.Debugf("pipeline [%v] failed", cfg.Name)
						if !cfg.KeepRunning {
							ctx.Pause()
						}
						break
					case pipeline.STOPPING:
						ctx.CancelTask()
						ctx.Pause()
						break
					case pipeline.STOPPED:
						log.Debugf("pipeline [%v] stopped", cfg.Name)
						ctx.Pause()
						break
					case pipeline.FINISHED:
						log.Debugf("pipeline [%v] finished", cfg.Name)
						ctx.Pause()
						break
					}
					time.Sleep(1 * time.Second)
				}

			}(v, processor, ctx)

		}
	}

	module.started = true
	return nil
}

func (module *PipeModule) Stop() error {

	if module.started {

		total := len(module.contexts)

		if total <= 0 {
			return nil
		}

		log.Debug("shutting down pipeline framework")
		start := time.Now()

	CLOSING:

		for k, v := range module.contexts {
			if v.GetRunningState() == pipeline.FINISHED || v.GetRunningState() == pipeline.STARTED || v.GetRunningState() == pipeline.STARTED {
				progress.RegisterBar("pipeline", "shutdown", 1)

				if global.Env().IsDebug {
					if rate.GetRateLimiterPerSecond("pipeline","shutdown"+k+string(v.GetRunningState()),1).Allow(){
						log.Trace("start shutting down pipeline:", k,",state:",v.GetRunningState())
					}
				}

				v.CancelTask()
				v.Exit()

				if global.Env().IsDebug {
					if rate.GetRateLimiterPerSecond("pipeline","shutdown"+k+string(v.GetRunningState()),1).Allow() {
						log.Trace("finished shutting down pipeline:", k)
					}
				}
			}
		}

		progress.Start()

		for k, v := range module.contexts {
			if v.GetRunningState() == pipeline.STARTED || v.GetRunningState() == pipeline.STARTING || v.GetRunningState() == pipeline.STOPPING {
				if time.Now().Sub(start).Minutes() > 5 {
					log.Error("pipeline framework failure to stop tasks, quiting")
					return errors.New("pipeline framework failure to stop tasks, quiting")
				}
				if rate.GetRateLimiterPerSecond("pipeline","shutdown"+k+string(v.GetRunningState()),1).Allow(){
					log.Trace("pipeline still running:", k,",state:",v.GetRunningState(),", closing")
				}
				goto CLOSING
			}
		}
		module.started = false
		progress.Stop()
	} else {
		log.Error("pipeline framework is not started")
	}

	return nil
}
