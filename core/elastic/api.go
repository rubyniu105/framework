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
	"context"
	"infini.sh/framework/core/util"
	"infini.sh/framework/lib/fasthttp"
)

type API interface {
	ScrollAPI
	MappingAPI
	TemplateAPI
	ReplicationAPI

	InitDefaultTemplate(templateName, indexPrefix string)

	GetMajorVersion() int

	ClusterHealth() (*ClusterHealth, error)
	ClusterHealthSpecEndpoint(endPoint string) (*ClusterHealth, error)

	GetClusterState() (*ClusterState, error)

	GetClusterStats(node string) (*ClusterStats, error)
	GetClusterStatsSpecEndpoint(node string, endPoint string) (*ClusterStats, error)

	GetNodesStats(nodeID, host string, level string) *NodesStats

	GetIndicesStats() *IndicesStats

	GetVersion() Version

	CreateIndex(name string, settings map[string]interface{}) error

	Index(indexName, docType string, id interface{}, data interface{}, refresh string) (*InsertResponse, error)

	Update(indexName, docType string, id interface{}, data interface{}, refresh string) (*InsertResponse, error)

	Bulk(data []byte) (*util.Result, error)

	Get(indexName, docType, id string) (*GetResponse, error)
	Delete(indexName, docType, id string, refresh ...string) (*DeleteResponse, error)
	Count(ctx context.Context, indexName string, body []byte) (*CountResponse, error)
	Search(indexName string, query *SearchRequest) (*SearchResponse, error)

	QueryDSL(ctx context.Context, indexName string, queryArgs *[]util.KV, queryDSL []byte) (*SearchResponse, error)

	SearchWithRawQueryDSL(indexName string, queryDSL []byte) (*SearchResponse, error)

	GetIndexSettings(indexNames string) (*util.MapStr, error)
	UpdateIndexSettings(indexName string, settings map[string]interface{}) error

	IndexExists(indexName string) (bool, error)

	DeleteIndex(name string) error

	Refresh(name string) (err error)

	GetNodes() (*map[string]NodesInfo, error)

	GetNodeInfo(nodeID string) (*NodesInfo, error)

	GetIndices(pattern string) (*map[string]IndexInfo, error)

	GetPrimaryShards() (*map[string]map[int]ShardInfo, error)
	GetAliases() (*map[string]AliasInfo, error)
	GetAliasesDetail() (*map[string]AliasDetailInfo, error)
	GetAliasesAndIndices() (*AliasAndIndicesResponse, error)

	SearchTasksByIds(ids []string) (*SearchResponse, error)
	Reindex(body []byte) (*ReindexResponse, error)
	DeleteByQuery(indexName string, body []byte) (*DeleteByQueryResponse, error)
	UpdateByQuery(indexName string, body []byte) (*UpdateByQueryResponse, error)

	GetIndexStats(indexName string) (*util.MapStr, error)
	GetStats() (*Stats, error)
	Forcemerge(indexName string, maxCount int) error
	SetSearchTemplate(templateID string, body []byte) error
	DeleteSearchTemplate(templateID string) error
	RenderTemplate(body map[string]interface{}) ([]byte, error)
	SearchTemplate(body map[string]interface{}) ([]byte, error)
	Alias(body []byte) error
	FieldCaps(target string) ([]byte, error)
	CatShards() ([]CatShardResponse, error)
	CatShardsSpecEndpoint(endPoint string) ([]CatShardResponse, error)
	CatNodes(colStr string) ([]CatNodeResponse, error)

	GetIndexRoutingTable(index string) (map[string][]IndexShardRouting, error)
	GetClusterSettings() (map[string]interface{}, error)
	UpdateClusterSettings(body []byte) error
	GetIndex(indexName string) (map[string]interface{}, error)
	Exists(target string) (bool, error)
	GetILMPolicy(target string) (map[string]interface{}, error)
	PutILMPolicy(target string, policyConfig []byte) error
	DeleteILMPolicy(target string) error
	GetRemoteInfo()([]byte, error)
}

type TemplateAPI interface {
	TemplateExists(templateName string) (bool, error)
	PutTemplate(templateName string, template []byte) ([]byte, error)
	GetTemplate(templateName string) (map[string]interface{}, error)
}

type MappingAPI interface {
	GetMapping(copyAllIndexes bool, indexNames string) (string, int, *util.MapStr, error)
	UpdateMapping(indexName string, docType string, mappings []byte) ([]byte, error)
}

type ScrollAPI interface {
	NewScroll(indexNames string, scrollTime string, docBufferCount int, query *SearchRequest, slicedId, maxSlicedCount int) ([]byte, error)
	NextScroll(ctx *APIContext, scrollTime string, scrollId string) ([]byte, error)
	ClearScroll(scrollId string) error
}

type ReplicationAPI interface {
	StartReplication(followIndex string, body []byte) error
	StopReplication(indexName string, body []byte) error
	PauseReplication(followIndex string, body []byte) error
	ResumeReplication(followIndex string, body []byte) error
	GetReplicationStatus(followIndex string) ([]byte, error)
	GetReplicationFollowerStats(followIndex string) ([]byte, error)
	CreateAutoFollowReplication(autoFollowPatternName string, body []byte) error
	GetAutoFollowStats(autoFollowPatternName string)([]byte, error)
	DeleteAutoFollowReplication(autoFollowPatternName string, body []byte) error
}

type APIContext struct {
	context.Context `json:"-"`
	Client          *fasthttp.Client
	Request         *fasthttp.Request
	Response        *fasthttp.Response
}

type ScrollResponseAPI interface {
	GetScrollId() string
	SetScrollId(id string)
	GetHitsTotal() int64
	GetShardResponse() ShardResponse
	GetDocs() []IndexDocument
}
