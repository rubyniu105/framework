/* Copyright © INFINI Ltd. All rights reserved.
 * web: https://infinilabs.com
 * mail: hello#infini.ltd */

package api

import (
	"fmt"
	httprouter "infini.sh/framework/core/api/router"
	"infini.sh/framework/core/elastic"
	"infini.sh/framework/core/event"
	"infini.sh/framework/core/orm"
	"infini.sh/framework/core/util"
	"infini.sh/framework/modules/elastic/common"
	"net/http"
	log "src/github.com/cihub/seelog"
	"strings"
	"time"
)

func (h *APIHandler) SearchNodeMetadata(w http.ResponseWriter, req *http.Request, ps httprouter.Params) {
	var (
		name        = h.GetParameterOrDefault(req, "keyword", "")
		queryDSL    = `{"query":{"bool":{"should":[%s]}}, "size": %d, "from": %d, "sort": [
    {
      "timestamp": {
        "order": "desc"
      }
    }
  ], "collapse": {"field": "node_id"}}`
		size        = h.GetIntOrDefault(req, "size", 20)
		from        = h.GetIntOrDefault(req, "from", 0)
		mustBuilder = &strings.Builder{}
	)

	if name != "" {
		mustBuilder.WriteString(fmt.Sprintf(`{"prefix":{"name.text": "%s"}}`, name))
		mustBuilder.WriteString(fmt.Sprintf(`,{"query_string":{"query": "%s"}}`, name))
		mustBuilder.WriteString(fmt.Sprintf(`,{"query_string":{"query": "%s*"}}`, name))
	}

	if size <= 0 {
		size = 20
	}

	if from < 0 {
		from = 0
	}

	q := orm.Query{}
	queryDSL = fmt.Sprintf(queryDSL, mustBuilder.String(), size, from)
	q.RawQuery = []byte(queryDSL)

	err, res := orm.Search(&elastic.NodeMetadata{}, &q)
	if err != nil {
		h.WriteError(w, err.Error(), http.StatusInternalServerError)
		return
	}

	response := elastic.SearchResponse{}
	util.FromJSONBytes(res.Raw, &response)

	var nodeIDs []interface{}

	for _, hit := range response.Hits.Hits {
		nodeIDs = append(nodeIDs, hit.Source["node_id"])
	}

	if len(nodeIDs) == 0 {
		h.WriteJSON(w, util.MapStr{
			"hits": util.MapStr{
				"total": util.MapStr{
					"value":    0,
					"relation": "eq",
				},
				"hits": []interface{}{},
			},
		}, 200)
		return
	}
	q1 := orm.Query{WildcardIndex: true}
	query := util.MapStr{
			"sort": []util.MapStr{
				{
					"timestamp": util.MapStr{
						"order": "desc",
					},
				},
			},
			"collapse": util.MapStr{
			"field": "metadata.labels.node_id",
		},
			"query": util.MapStr{
			"bool": util.MapStr{
			"must": []util.MapStr{
				{
					"term": util.MapStr{
						"metadata.category": util.MapStr{
							"value": "elasticsearch",
						},
					},
				},
				{
					"term": util.MapStr{
						"metadata.name": util.MapStr{
							"value": "node_stats",
						},
					},
				},
				{
					"terms": util.MapStr{
						"metadata.labels.node_id": nodeIDs,
					},
				},
			},
		},
		},
	}
	q1.RawQuery = util.MustToJSONBytes(query)

	err, results := orm.Search(&event.Event{}, &q1)
	if err != nil {
		log.Error(err)
		h.WriteError(w, err.Error(), http.StatusInternalServerError)
		return
	}
	statusMap := map[string]interface{}{}
	for _, v := range results.Result {
		result, ok := v.(map[string]interface{})
		if ok {
			nodeID, ok := util.GetMapValueByKeys([]string{"metadata", "labels", "node_id"}, result)
			if ok {
				source := map[string]interface{}{}
				timestamp, ok := result["timestamp"].(string)
				if ok {
					lastTime, err := time.Parse("2006-01-02T15:04:05.99999Z07:00", timestamp)

					if err != nil {
						log.Error( result["timestamp"], err)
						source["status"] = "online"
					}else{
						if time.Now().Sub(lastTime).Seconds() > 15 {
							source["status"] = "offline"
						}else{
							source["status"] = "online"
						}
					}
				}
				uptime, ok := util.GetMapValueByKeys([]string{"payload", "elasticsearch", "node_stats", "jvm", "uptime_in_millis"}, result)
				if ok {
					source["uptime"] = uptime
				}

				storeAvailableInBytes, ok := util.GetMapValueByKeys([]string{"payload", "elasticsearch", "node_stats", "fs", "total", "available_in_bytes"}, result)
				if ok {
					source["available_store_bytes"] = storeAvailableInBytes
				}

				storeTotalInBytes, ok := util.GetMapValueByKeys([]string{"payload", "elasticsearch", "node_stats", "fs", "total", "total_in_bytes"}, result)
				if ok {
					source["max_store_bytes"] = storeTotalInBytes
				}
				fs, ok := util.GetMapValueByKeys([]string{"payload", "elasticsearch", "node_stats", "fs"}, result)
				if ok {
					source["fs"] = fs
				}

				jvm, ok := util.GetMapValueByKeys([]string{"payload", "elasticsearch", "node_stats", "jvm"}, result)
				if ok {
					source["jvm"] = jvm
				}

				statusMap[util.ToString(nodeID)] = source
			}
		}
	}
	statusMetric, err := getNodeOnlineStatusOfRecentDay(nodeIDs, 14)
	if err != nil {
		log.Error(err)
		h.WriteError(w, err.Error(), http.StatusInternalServerError)
		return
	}
	bucketSize, min, max, err := h.getMetricRangeAndBucketSize(req, 60, (15))
	if err != nil {
		panic(err)
		return
	}
	// 索引速率
	indexMetric:=newMetricItem("indexing", 1, OperationGroupKey)
	indexMetric.AddAxi("indexing rate","group1",common.PositionLeft,"num","0,0","0,0.[00]",5,true)
	nodeMetricItems := []GroupMetricItem{}
	nodeMetricItems=append(nodeMetricItems, GroupMetricItem{
		Key: "indexing",
		Field: "payload.elasticsearch.node_stats.indices.indexing.index_total",
		ID: util.GetUUID(),
		IsDerivative: true,
		MetricItem: indexMetric,
		FormatType: "num",
		Units: "Indexing/s",
	})
	queryMetric:=newMetricItem("search", 2, OperationGroupKey)
	queryMetric.AddAxi("query rate","group1",common.PositionLeft,"num","0,0","0,0.[00]",5,true)
	nodeMetricItems=append(nodeMetricItems, GroupMetricItem{
		Key: "search",
		Field: "payload.elasticsearch.node_stats.indices.search.query_total",
		ID: util.GetUUID(),
		IsDerivative: true,
		MetricItem: queryMetric,
		FormatType: "num",
		Units: "Search/s",
	})

	aggs:=map[string]interface{}{}
	query=map[string]interface{}{}
	query["query"]=util.MapStr{
		"bool": util.MapStr{
			"must":  []util.MapStr{
				{
					"term": util.MapStr{
						"metadata.category": util.MapStr{
							"value": "elasticsearch",
						},
					},
				},
				{
					"term": util.MapStr{
						"metadata.name": util.MapStr{
							"value": "node_stats",
						},
					},
				},
				{
					"terms": util.MapStr{
						"metadata.labels.node_id": nodeIDs,
					},
				},
			},
			"filter": []util.MapStr{
				{
					"range": util.MapStr{
						"timestamp": util.MapStr{
							"gte": min,
							"lte": max,
						},
					},
				},
			},
		},
	}

	for _,metricItem:=range nodeMetricItems{
		aggs[metricItem.ID]=util.MapStr{
			"max":util.MapStr{
				"field": metricItem.Field,
			},
		}
		if metricItem.IsDerivative{
			aggs[metricItem.ID+"_deriv"]=util.MapStr{
				"derivative":util.MapStr{
					"buckets_path": metricItem.ID,
				},
			}
		}
	}

	query["size"]=0
	query["aggs"]= util.MapStr{
		"group_by_level": util.MapStr{
			"terms": util.MapStr{
				"field": "metadata.labels.node_id",
				"size":  100,
			},
			"aggs": util.MapStr{
				"dates": util.MapStr{
					"date_histogram":util.MapStr{
						"field": "timestamp",
						"fixed_interval": fmt.Sprintf("%ds", bucketSize),
					},
					"aggs":aggs,
				},
			},
		},
	}
	metrics := h.getMetrics(query, nodeMetricItems, bucketSize)
	indexMetrics := map[string]util.MapStr{}
	for key, item := range metrics {
		for _, line := range item.Lines {
			if _, ok := indexMetrics[line.Metric.Label]; !ok{
				indexMetrics[line.Metric.Label] = util.MapStr{
				}
			}
			indexMetrics[line.Metric.Label][key] = line.Data
		}
	}
	for i, hit := range response.Hits.Hits {
		result := util.MapStr{}

		source := hit.Source
		nodeID := source["node_id"].(string)

		result["metadata"] = source
		result["summary"] = statusMap[nodeID]
		result["metrics"] = util.MapStr{
			"status": util.MapStr{
				"metric": util.MapStr{
					"label": "Recent Node Status",
					"units": "day",
				},
				"data": statusMetric[nodeID],
			},
			"indexing": util.MapStr{
				"metric": util.MapStr{
					"label": "Indexing",
					"units": "s",
				},
				"data": indexMetrics[nodeID]["indexing"],
			},
			"search": util.MapStr{
				"metric": util.MapStr{
					"label": "Search",
					"units": "s",
				},
				"data": indexMetrics[nodeID]["search"],
			},
		}
		response.Hits.Hits[i].Source = result
	}

	h.WriteJSON(w, response, http.StatusOK)
}

func getNodeOnlineStatusOfRecentDay(nodeIDs []interface{}, days int)(map[string][]interface{}, error){
	q := orm.Query{
		WildcardIndex: true,
	}
	query := util.MapStr{
		"aggs": util.MapStr{
			"group_by_node_id": util.MapStr{
				"terms": util.MapStr{
					"field": "metadata.labels.node_id",
						"size": 100,
				},
				"aggs": util.MapStr{
					"uptime_histogram": util.MapStr{
						"date_histogram": util.MapStr{
							"field": "timestamp",
								"interval": "1d",
						},
						"aggs": util.MapStr{
							"min_uptime": util.MapStr{
								"min": util.MapStr{
									"field": "payload.elasticsearch.node_stats.jvm.uptime_in_millis",
								},
							},
						},
					},
				},
			},
		},
		"sort": []util.MapStr{
			{
				"timestamp": util.MapStr{
					"order": "desc",
				},
			},
		},
		"size": 0,
		"query": util.MapStr{
			"bool": util.MapStr{
				"filter": []util.MapStr{
					{
						"range": util.MapStr{
							"timestamp": util.MapStr{
								"gte": fmt.Sprintf("now-%dd", days),
								"lte": "now",
							},
						},
					},
				},
				"must": []util.MapStr{
					{
						"term": util.MapStr{
							"metadata.name": util.MapStr{
								"value": "node_stats",
							},
						},
					},
					{
						"terms": util.MapStr{
							"metadata.labels.node_id": nodeIDs,
						},
					},
				},
			},
		},
	}
	q.RawQuery = util.MustToJSONBytes(query)

	err, res := orm.Search(&event.Event{}, &q)
	if err != nil {
		return nil, err
	}

	response := elastic.SearchResponse{}
	util.FromJSONBytes(res.Raw, &response)
	recentStatus := map[string][]interface{}{}
	for _, bk := range response.Aggregations["group_by_node_id"].Buckets {
		nodeKey := bk["key"].(string)
		recentStatus[nodeKey] = []interface{}{}
		if histogramAgg, ok := bk["uptime_histogram"].(map[string]interface{}); ok {
			if bks, ok := histogramAgg["buckets"].([]interface{}); ok {
				for _, bkItem := range  bks {
					if bkVal, ok := bkItem.(map[string]interface{}); ok {
						if minUptime, ok := util.GetMapValueByKeys([]string{"min_uptime", "value"}, bkVal); ok {
							//mark node status as offline when uptime less than 10m
							if minUptime.(float64) < 600000 {
								recentStatus[nodeKey] = append(recentStatus[nodeKey], []interface{}{bkVal["key"], "offline"})
							}
							recentStatus[nodeKey] = append(recentStatus[nodeKey], []interface{}{bkVal["key"], "online"})
						}
					}
				}
			}
		}
	}
	return recentStatus, nil
}