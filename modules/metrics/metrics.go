package metrics

import (
	"context"
	"fmt"
	log "github.com/cihub/seelog"
	. "infini.sh/framework/core/config"
	"infini.sh/framework/core/env"
	"infini.sh/framework/core/event"
	"infini.sh/framework/core/global"
	"infini.sh/framework/core/task"
	"infini.sh/framework/core/util"
	"infini.sh/framework/modules/metrics/elastic"
	"infini.sh/framework/modules/metrics/host/cpu"
	"infini.sh/framework/modules/metrics/host/disk"
	"infini.sh/framework/modules/metrics/host/memory"
	"infini.sh/framework/modules/metrics/host/network"
	agent2 "infini.sh/framework/modules/metrics/instance"
)

type MetricConfig struct {
	Enabled bool  `config:"enabled"`
	MajorIPPattern string `config:"major_ip_pattern"`
	Queue          string `config:"queue"`

	InstanceConfig *Config `config:"instance"`

	NetworkConfig        *Config `config:"network"`
	DiskConfig           *Config `config:"disk"`
	CPUConfig            *Config `config:"cpu"`
	MemoryConfig         *Config `config:"memory"`
	ElasticsearchConfig  *Config `config:"elasticsearch"`

	Tags   []string          `config:"tags"`
	Labels map[string]string `config:"labels"`

}

type MetricsModule struct {
	config *MetricConfig
	taskIDs 	  []string
}

func (module *MetricsModule) Name() string {
	return "metrics"
}

func (module *MetricsModule) Setup() {

	module.config = &MetricConfig{Enabled: true}

	exits, err := env.ParseConfig("metrics", module.config)
	if !exits{
		module.config.Enabled=false
		return
	}
	if err != nil {
		panic(err)
	}

	if !module.config.Enabled{
		return
	}


	_, publicIP, _, _ := util.GetPublishNetworkDeviceInfo(module.config.MajorIPPattern)
	meta := event.AgentMeta{
		AgentID:   global.Env().SystemConfig.NodeConfig.ID,
		MajorIP:   publicIP,
		Hostname:  util.GetHostName(),
		IP:        util.GetLocalIPs(),
		QueueName: util.StringDefault(module.config.Queue, "metrics"),
		Labels:    module.config.Labels,
		Tags:      module.config.Tags}

	event.RegisterMeta(&meta)

	tail:=fmt.Sprintf("ip: %v,host: %v", meta.MajorIP, meta.Hostname)
	if len(meta.Labels)>0{
		tail=tail+",labels: "+util.JoinMapString(meta.Labels, "->")
	}
	if len(meta.Tags)>0{
		tail=tail+",tags: "+util.JoinArray(meta.Tags, ",")
	}

	log.Info(tail)

	module.CollectAgentMetric()
	module.CollectHostMetric()
	module.CollectESMetric()
}

func (module *MetricsModule) CollectESMetric(){
	if module.config.ElasticsearchConfig!=nil{
		//elasticsearch
		es, err := elastic.New(module.config.ElasticsearchConfig)
		if err != nil {
			panic(err)
		}
		if es.Enabled{
			taskId := util.GetUUID()
			module.taskIDs = append(module.taskIDs, taskId)
			var task1 = task.ScheduleTask{
				ID: taskId,
				Description: "monitoring for elasticsearch clusters",
				Type:        "interval",
				Interval:    es.Interval,
				Task: func(ctx context.Context) {
					log.Debug("collecting elasticsearch metrics")
					es.Collect()
				},
			}
			task.RegisterScheduleTask(task1)
		}
	}
}

func (module *MetricsModule) CollectAgentMetric() {
	if module.config.InstanceConfig != nil{
		agentM, err := agent2.New(module.config.InstanceConfig)
		if err != nil {
			panic(err)
		}
		if agentM.Enabled{
			taskId := util.GetUUID()
			module.taskIDs = append(module.taskIDs, taskId)
			var task1 = task.ScheduleTask{
				ID: taskId,
				Description: "fetch instance metrics",
				Type:        "interval",
				Interval:    "10s",
				Task: func(ctx context.Context) {
					log.Debug("collecting instance metrics")
					agentM.Collect()
				},
			}
			task.RegisterScheduleTask(task1)
		}
	}
}

func (module *MetricsModule) CollectHostMetric()  {
	var netM *network.Metric
	var diskM *disk.Metric
	var cpuM *cpu.Metric
	var memoryM *memory.Metric
	var err error

	if module.config.NetworkConfig != nil{
		if netM == nil {
			netM, err = network.New(module.config.NetworkConfig)
			if err != nil {
				panic(err)
			}
		}
		taskId := util.GetUUID()
		module.taskIDs = append(module.taskIDs, taskId)
		var netTask = task.ScheduleTask{
			ID: taskId,
			Description: "fetch network metrics",
			Type:        "interval",
			Interval:    "10s",
			Task: func(ctx context.Context) {
				log.Debug("collecting network metrics")
				netM.Collect()
			},
		}
		task.RegisterScheduleTask(netTask)
	}

	if module.config.DiskConfig != nil {
		if diskM == nil {
			diskM, err = disk.New(module.config.DiskConfig)
			if err != nil {
				panic(err)
			}
		}
		taskId := util.GetUUID()
		module.taskIDs = append(module.taskIDs, taskId)
		var diskTask = task.ScheduleTask{
			ID: taskId,
			Description: "fetch disk metrics",
			Type:        "interval",
			Interval:    "10s",
			Task: func(ctx context.Context) {
				log.Debug("collecting disk metrics")
				diskM.Collect()
			},
		}
		task.RegisterScheduleTask(diskTask)
	}

	if module.config.CPUConfig != nil {
		if cpuM == nil {
			cpuM, err = cpu.New(module.config.CPUConfig)
			if err != nil {
				panic(err)
			}
		}
		taskId := util.GetUUID()
		module.taskIDs = append(module.taskIDs, taskId)
		var cpuTask = task.ScheduleTask{
			ID: taskId,
			Description: "fetch cpu metrics",
			Type:        "interval",
			Interval:    "10s",
			Task: func(ctx context.Context) {
				log.Debug("collecting cpu metrics")
				cpuM.Collect()
			},
		}
		task.RegisterScheduleTask(cpuTask)
	}

	if module.config.MemoryConfig != nil {
		if memoryM == nil {
			memoryM, err = memory.New(module.config.MemoryConfig)
			if err != nil {
				panic(err)
			}
		}
		taskId := util.GetUUID()
		module.taskIDs = append(module.taskIDs, taskId)
		var memTask = task.ScheduleTask{
			ID: taskId,
			Description: "fetch memory metrics",
			Type:        "interval",
			Interval:    "10s",
			Task: func(ctx context.Context) {
				log.Debug("collecting memory metrics")
				memoryM.Collect()
			},
		}
		task.RegisterScheduleTask(memTask)
	}
}

func (module *MetricsModule) Start() error {

	NotifyOnConfigSectionChange("metrics", func(pCfg,cCfg *Config) {

		if cCfg == nil {
			return
		}

		newCfg := &MetricConfig{}
		err := cCfg.Unpack(newCfg)
		if err != nil || newCfg == nil {
			log.Error(err)
			return
		}

		for _, taskId := range module.taskIDs {
			task.StopTask(taskId)
			task.DeleteTask(taskId)
		}
		module.taskIDs = nil

		module.config = newCfg
		_, publicIP, _, _ := util.GetPublishNetworkDeviceInfo(module.config.MajorIPPattern)
		meta := event.AgentMeta{
			AgentID:   global.Env().SystemConfig.NodeConfig.ID,
			MajorIP:   publicIP,
			Hostname:  util.GetHostName(),
			IP:        util.GetLocalIPs(),
			QueueName: util.StringDefault(module.config.Queue, "metrics"),
			Labels:    module.config.Labels,
			Tags:      module.config.Tags}

		event.RegisterMeta(&meta)

		tail:=fmt.Sprintf("ip: %v,host: %v", meta.MajorIP, meta.Hostname)
		if len(meta.Labels)>0{
			tail=tail+",labels: "+util.JoinMapString(meta.Labels, "->")
		}
		if len(meta.Tags)>0{
			tail=tail+",tags: "+util.JoinArray(meta.Tags, ",")
		}

		log.Info(tail)

		module.CollectAgentMetric()
		module.CollectHostMetric()
		module.CollectESMetric()
	})

	return nil
}

func (module *MetricsModule) Stop() error {

	//TODO cancel or stop background jobs

	return nil
}
