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

package modules

import (
	"infini.sh/framework/core/module"
	"infini.sh/framework/modules/api"
	"infini.sh/framework/modules/elastic"
	"infini.sh/framework/modules/pipeline"
	queue2 "infini.sh/framework/modules/queue/disk_queue"
	kafka_queue "infini.sh/framework/modules/queue/kafka_queue"
	"infini.sh/framework/modules/queue/mem_queue"
	"infini.sh/framework/modules/redis"
	"infini.sh/framework/modules/stats"
	"infini.sh/framework/modules/task"
	"infini.sh/framework/modules/ui"
)

// RegisterSystemModule is where modules are registered
func Register() {
	module.RegisterSystemModule(&stats.SimpleStatsModule{})
	//module.RegisterSystemModule(nsq.NSQModule{})
	module.RegisterSystemModule(&elastic.ElasticModule{})
	module.RegisterSystemModule(&queue2.DiskQueue{})
	module.RegisterSystemModule(&mem_queue.MemoryQueue{})
	module.RegisterSystemModule(&kafka_queue.KafkaQueue{})
	module.RegisterSystemModule(&redis.RedisModule{})
	module.RegisterSystemModule(&api.APIModule{})
	module.RegisterSystemModule(&ui.UIModule{})
	module.RegisterSystemModule(&pipeline.PipeModule{})
	//module.RegisterSystemModule(cluster.ClusterModule{})
	module.RegisterSystemModule(&task.TaskModule{})
}
