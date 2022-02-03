package api

import (
	"fmt"
	log "github.com/cihub/seelog"
	"infini.sh/framework/core/elastic"
	"infini.sh/framework/core/util"
	"infini.sh/framework/modules/elastic/common"
	"sort"
	"time"
)

func (h *APIHandler) getIndexMetrics(clusterID string, bucketSize int, min, max int64, indexName string, top int) map[string]*common.MetricItem{
	bucketSizeStr:=fmt.Sprintf("%vs",bucketSize)
	indexNames, err := h.getTopIndexName(clusterID, top, 15)
	if err != nil {
		log.Error(err)
	}

	var must = []util.MapStr{
		{
			"term":util.MapStr{
				"metadata.labels.cluster_id":util.MapStr{
					"value": clusterID,
				},
			},
		},
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
					"value": "index_stats",
				},
			},
		},
	}
	if indexName != "" {
		top = 1
		must = append(must, util.MapStr{
			"term": util.MapStr{
				"metadata.labels.index_name": util.MapStr{
					"value": indexName,
				},
			},
		})
	}else{
		if len(indexNames) > 0 {
			must = append(must, util.MapStr{
				"terms": util.MapStr{
					"metadata.labels.index_name": indexNames,
				},
			})
		}
	}

	query:=map[string]interface{}{}
	query["query"]=util.MapStr{
		"bool": util.MapStr{
			"must": must,
			"must_not": []util.MapStr{
				{
					"term": util.MapStr{
						"metadata.labels.index_name": util.MapStr{
							"value": "_all",
						},
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
	//索引存储大小
	indexStorageMetric := newMetricItem("index_storage", 1, StorageGroupKey)
	indexStorageMetric.AddAxi("Index storage","group1",common.PositionLeft,"bytes","0.[0]","0.[0]",5,true)

	indexMetricItems := []GroupMetricItem{
		{
			Key: "index_storage",
			Field: "payload.elasticsearch.index_stats.total.store.size_in_bytes",
			ID: util.GetUUID(),
			IsDerivative: false,
			MetricItem: indexStorageMetric,
			FormatType: "bytes",
			Units: "",
		},
	}
	// segment 数量
	segmentCountMetric:=newMetricItem("segment_count", 15, StorageGroupKey)
	segmentCountMetric.AddAxi("segment count","group1",common.PositionLeft,"num","0,0","0,0.[00]",5,true)
	indexMetricItems=append(indexMetricItems, GroupMetricItem{
		Key: "segment_count",
		Field: "payload.elasticsearch.index_stats.total.segments.count",
		ID: util.GetUUID(),
		IsDerivative: false,
		MetricItem: segmentCountMetric,
		FormatType: "num",
		Units: "",
	})
	//索引文档个数
	docCountMetric := newMetricItem("doc_count", 2, DocumentGroupKey)
	docCountMetric.AddAxi("Doc count","group1",common.PositionLeft,"num","0.[0]","0.[0]",5,true)

	indexMetricItems = append(indexMetricItems, GroupMetricItem{
		Key: "doc_count",
		Field: "payload.elasticsearch.index_stats.total.docs.count",
		ID: util.GetUUID(),
		IsDerivative: false,
		MetricItem: docCountMetric,
		FormatType: "num",
		Units: "",
	})
	// docs 删除数量
	docsDeletedMetric:=newMetricItem("docs_deleted", 17, DocumentGroupKey)
	docsDeletedMetric.AddAxi("docs deleted","group1",common.PositionLeft,"num","0,0","0,0.[00]",5,true)
	indexMetricItems=append(indexMetricItems, GroupMetricItem{
		Key: "docs_deleted",
		Field: "payload.elasticsearch.index_stats.total.docs.deleted",
		ID: util.GetUUID(),
		IsDerivative: false,
		MetricItem: docsDeletedMetric,
		FormatType: "num",
		Units: "",
	})
	//查询次数
	queryTimesMetric := newMetricItem("query_times", 2, OperationGroupKey)
	queryTimesMetric.AddAxi("Query times","group1",common.PositionLeft,"num","0.[0]","0.[0]",5,true)

	indexMetricItems = append(indexMetricItems, GroupMetricItem{
		Key: "query_times",
		Field: "payload.elasticsearch.index_stats.total.search.query_total",
		ID: util.GetUUID(),
		IsDerivative: true,
		MetricItem: queryTimesMetric,
		FormatType: "num",
		Units: "",
	})

	//Fetch次数
	fetchTimesMetric := newMetricItem("fetch_times", 3, OperationGroupKey)
	fetchTimesMetric.AddAxi("Fetch times","group1",common.PositionLeft,"num","0.[0]","0.[0]",5,true)
	indexMetricItems = append(indexMetricItems, GroupMetricItem{
		Key: "fetch_times",
		Field: "payload.elasticsearch.index_stats.total.search.fetch_total",
		ID: util.GetUUID(),
		IsDerivative: true,
		MetricItem: fetchTimesMetric,
		FormatType: "num",
		Units: "",
	})
	//scroll 次数
	scrollTimesMetric := newMetricItem("scroll_times", 4, OperationGroupKey)
	scrollTimesMetric.AddAxi("scroll times","group1",common.PositionLeft,"num","0.[0]","0.[0]",5,true)
	indexMetricItems = append(indexMetricItems, GroupMetricItem{
		Key: "scroll_times",
		Field: "payload.elasticsearch.index_stats.total.search.scroll_total",
		ID: util.GetUUID(),
		IsDerivative: true,
		MetricItem: scrollTimesMetric,
		FormatType: "num",
		Units: "",
	})
	//Merge次数
	mergeTimesMetric := newMetricItem("merge_times", 7, OperationGroupKey)
	mergeTimesMetric.AddAxi("Merge times","group1",common.PositionLeft,"num","0.[0]","0.[0]",5,true)
	indexMetricItems = append(indexMetricItems, GroupMetricItem{
		Key: "merge_times",
		Field: "payload.elasticsearch.index_stats.total.merges.total",
		ID: util.GetUUID(),
		IsDerivative: true,
		MetricItem: mergeTimesMetric,
		FormatType: "num",
		Units: "",
	})
	//Refresh次数
	refreshTimesMetric := newMetricItem("refresh_times", 5, OperationGroupKey)
	refreshTimesMetric.AddAxi("Refresh times","group1",common.PositionLeft,"num","0.[0]","0.[0]",5,true)
	indexMetricItems = append(indexMetricItems, GroupMetricItem{
		Key: "refresh_times",
		Field: "payload.elasticsearch.index_stats.total.refresh.total",
		ID: util.GetUUID(),
		IsDerivative: true,
		MetricItem: refreshTimesMetric,
		FormatType: "num",
		Units: "",
	})
	//flush 次数
	flushTimesMetric := newMetricItem("flush_times", 6, OperationGroupKey)
	flushTimesMetric.AddAxi("flush times","group1",common.PositionLeft,"num","0.[0]","0.[0]",5,true)
	indexMetricItems = append(indexMetricItems, GroupMetricItem{
		Key: "flush_times",
		Field: "payload.elasticsearch.index_stats.total.flush.total",
		ID: util.GetUUID(),
		IsDerivative: true,
		MetricItem: flushTimesMetric,
		FormatType: "num",
		Units: "",
	})

	//写入速率
	indexingRateMetric := newMetricItem("indexing_rate", 1, OperationGroupKey)
	indexingRateMetric.AddAxi("Indexing rate","group1",common.PositionLeft,"num","0.[0]","0.[0]",5,true)
	indexMetricItems = append(indexMetricItems, GroupMetricItem{
		Key: "indexing_rate",
		Field: "payload.elasticsearch.index_stats.total.indexing.index_total",
		ID: util.GetUUID(),
		IsDerivative: true,
		MetricItem: indexingRateMetric,
		FormatType: "num",
		Units: "",
	})
	//写入时延
	indexingLatencyMetric := newMetricItem("indexing_latency", 1, LatencyGroupKey)
	indexingLatencyMetric.AddAxi("Indexing latency","group1",common.PositionLeft,"num","0.[0]","0.[0]",5,true)
	indexMetricItems = append(indexMetricItems, GroupMetricItem{
		Key: "indexing_latency",
		Field: "payload.elasticsearch.index_stats.total.indexing.index_time_in_millis",
		Field2: "payload.elasticsearch.index_stats.total.indexing.index_total",
		Calc: func(value, value2 float64) float64 {
			return value/value2
		},
		ID: util.GetUUID(),
		IsDerivative: true,
		MetricItem: indexingLatencyMetric,
		FormatType: "num",
		Units: "ms",
	})

	//查询时延
	queryLatencyMetric := newMetricItem("query_latency", 2, LatencyGroupKey)
	queryLatencyMetric.AddAxi("Query latency","group1",common.PositionLeft,"num","0.[0]","0.[0]",5,true)
	indexMetricItems = append(indexMetricItems, GroupMetricItem{
		Key: "query_latency",
		Field: "payload.elasticsearch.index_stats.total.search.query_time_in_millis",
		Field2: "payload.elasticsearch.index_stats.total.search.query_total",
		Calc: func(value, value2 float64) float64 {
			return value/value2
		},
		ID: util.GetUUID(),
		IsDerivative: true,
		MetricItem: queryLatencyMetric,
		FormatType: "num",
		Units: "ms",
	})
	//fetch时延
	fetchLatencyMetric := newMetricItem("fetch_latency", 3, LatencyGroupKey)
	fetchLatencyMetric.AddAxi("Fetch latency","group1",common.PositionLeft,"num","0.[0]","0.[0]",5,true)
	indexMetricItems = append(indexMetricItems, GroupMetricItem{
		Key: "fetch_latency",
		Field: "payload.elasticsearch.index_stats.total.search.fetch_time_in_millis",
		Field2: "payload.elasticsearch.index_stats.total.search.fetch_total",
		Calc: func(value, value2 float64) float64 {
			return value/value2
		},
		ID: util.GetUUID(),
		IsDerivative: true,
		MetricItem: fetchLatencyMetric,
		FormatType: "num",
		Units: "ms",
	})

	//merge时延
	mergeLatencyMetric := newMetricItem("merge_latency", 7, LatencyGroupKey)
	mergeLatencyMetric.AddAxi("Merge latency","group1",common.PositionLeft,"num","0.[0]","0.[0]",5,true)
	indexMetricItems = append(indexMetricItems, GroupMetricItem{
		Key: "merge_latency",
		Field: "payload.elasticsearch.index_stats.total.merges.total_time_in_millis",
		Field2: "payload.elasticsearch.index_stats.total.merges.total",
		Calc: func(value, value2 float64) float64 {
			return value/value2
		},
		ID: util.GetUUID(),
		IsDerivative: true,
		MetricItem: mergeLatencyMetric,
		FormatType: "num",
		Units: "ms",
	})
	//refresh时延
	refreshLatencyMetric := newMetricItem("refresh_latency", 5, LatencyGroupKey)
	refreshLatencyMetric.AddAxi("Refresh latency","group1",common.PositionLeft,"num","0.[0]","0.[0]",5,true)
	indexMetricItems = append(indexMetricItems, GroupMetricItem{
		Key: "refresh_latency",
		Field: "payload.elasticsearch.index_stats.total.refresh.total_time_in_millis",
		Field2: "payload.elasticsearch.index_stats.total.refresh.total",
		Calc: func(value, value2 float64) float64 {
			return value/value2
		},
		ID: util.GetUUID(),
		IsDerivative: true,
		MetricItem: refreshLatencyMetric,
		FormatType: "num",
		Units: "ms",
	})
	//scroll时延
	scrollLatencyMetric := newMetricItem("scroll_latency", 4, LatencyGroupKey)
	scrollLatencyMetric.AddAxi("Scroll Latency","group1",common.PositionLeft,"num","0.[0]","0.[0]",5,true)
	indexMetricItems = append(indexMetricItems, GroupMetricItem{
		Key: "scroll_latency",
		Field: "payload.elasticsearch.index_stats.total.search.scroll_time_in_millis",
		Field2: "payload.elasticsearch.index_stats.total.search.scroll_total",
		Calc: func(value, value2 float64) float64 {
			return value/value2
		},
		ID: util.GetUUID(),
		IsDerivative: true,
		MetricItem: scrollLatencyMetric,
		FormatType: "num",
		Units: "ms",
	})
	//flush 时延
	flushLatencyMetric := newMetricItem("flush_latency", 6, LatencyGroupKey)
	flushLatencyMetric.AddAxi("Flush latency","group1",common.PositionLeft,"num","0.[0]","0.[0]",5,true)
	indexMetricItems = append(indexMetricItems, GroupMetricItem{
		Key: "flush_latency",
		Field: "payload.elasticsearch.index_stats.total.flush.total_time_in_millis",
		Field2: "payload.elasticsearch.index_stats.total.flush.total",
		Calc: func(value, value2 float64) float64 {
			return value/value2
		},
		ID: util.GetUUID(),
		IsDerivative: true,
		MetricItem: flushLatencyMetric,
		FormatType: "num",
		Units: "ms",
	})
	//queryCache
	queryCacheMetric := newMetricItem("query_cache", 1, CacheGroupKey)
	queryCacheMetric.AddAxi("Query cache","group1",common.PositionLeft,"bytes","0.[0]","0.[0]",5,true)
	indexMetricItems = append(indexMetricItems, GroupMetricItem{
		Key: "query_cache",
		Field: "payload.elasticsearch.index_stats.total.query_cache.memory_size_in_bytes",
		ID: util.GetUUID(),
		IsDerivative: false,
		MetricItem: queryCacheMetric,
		FormatType: "bytes",
		Units: "",
	})
	//requestCache
	requestCacheMetric := newMetricItem("request_cache", 2, CacheGroupKey)
	requestCacheMetric.AddAxi("request cache","group1",common.PositionLeft,"bytes","0.[0]","0.[0]",5,true)
	indexMetricItems = append(indexMetricItems, GroupMetricItem{
		Key: "request_cache",
		Field: "payload.elasticsearch.index_stats.total.request_cache.memory_size_in_bytes",
		ID: util.GetUUID(),
		IsDerivative: false,
		MetricItem: requestCacheMetric,
		FormatType: "bytes",
		Units: "",
	})
	// Request Cache Hit
	requestCacheHitMetric:=newMetricItem("request_cache_hit", 6, CacheGroupKey)
	requestCacheHitMetric.AddAxi("request cache hit","group1",common.PositionLeft,"num","0,0","0,0.[00]",5,true)
	indexMetricItems=append(indexMetricItems, GroupMetricItem{
		Key: "request_cache_hit",
		Field: "payload.elasticsearch.index_stats.total.request_cache.hit_count",
		ID: util.GetUUID(),
		IsDerivative: true,
		MetricItem: requestCacheHitMetric,
		FormatType: "num",
		Units: "hits",
	})
	// Request Cache Miss
	requestCacheMissMetric:=newMetricItem("request_cache_miss", 8, CacheGroupKey)
	requestCacheMissMetric.AddAxi("request cache miss","group1",common.PositionLeft,"num","0,0","0,0.[00]",5,true)
	indexMetricItems=append(indexMetricItems, GroupMetricItem{
		Key: "request_cache_miss",
		Field: "payload.elasticsearch.index_stats.total.request_cache.miss_count",
		ID: util.GetUUID(),
		IsDerivative: true,
		MetricItem: requestCacheMissMetric,
		FormatType: "num",
		Units: "misses",
	})
	// Query Cache Count
	queryCacheCountMetric:=newMetricItem("query_cache_count", 4, CacheGroupKey)
	queryCacheCountMetric.AddAxi("query cache miss","group1",common.PositionLeft,"num","0,0","0,0.[00]",5,true)
	indexMetricItems=append(indexMetricItems, GroupMetricItem{
		Key: "query_cache_count",
		Field: "payload.elasticsearch.index_stats.total.query_cache.cache_count",
		ID: util.GetUUID(),
		IsDerivative: true,
		MetricItem: queryCacheCountMetric,
		FormatType: "num",
		Units: "",
	})
	// Query Cache Miss
	queryCacheHitMetric:=newMetricItem("query_cache_hit", 5, CacheGroupKey)
	queryCacheHitMetric.AddAxi("query cache hit","group1",common.PositionLeft,"num","0,0","0,0.[00]",5,true)
	indexMetricItems=append(indexMetricItems, GroupMetricItem{
		Key: "query_cache_hit",
		Field: "payload.elasticsearch.index_stats.total.query_cache.hit_count",
		ID: util.GetUUID(),
		IsDerivative: true,
		MetricItem: queryCacheHitMetric,
		FormatType: "num",
		Units: "hits",
	})

	//// Query Cache evictions
	//queryCacheEvictionsMetric:=newMetricItem("query_cache_evictions", 11, CacheGroupKey)
	//queryCacheEvictionsMetric.AddAxi("query cache evictions","group1",common.PositionLeft,"num","0,0","0,0.[00]",5,true)
	//indexMetricItems=append(indexMetricItems, GroupMetricItem{
	//	Key: "query_cache_evictions",
	//	Field: "payload.elasticsearch.index_stats.total.query_cache.evictions",
	//	ID: util.GetUUID(),
	//	IsDerivative: true,
	//	MetricItem: queryCacheEvictionsMetric,
	//	FormatType: "num",
	//	Units: "evictions",
	//})

	// Query Cache Miss
	queryCacheMissMetric:=newMetricItem("query_cache_miss", 7, CacheGroupKey)
	queryCacheMissMetric.AddAxi("query cache miss","group1",common.PositionLeft,"num","0,0","0,0.[00]",5,true)
	indexMetricItems=append(indexMetricItems, GroupMetricItem{
		Key: "query_cache_miss",
		Field: "payload.elasticsearch.index_stats.total.query_cache.miss_count",
		ID: util.GetUUID(),
		IsDerivative: true,
		MetricItem: queryCacheMissMetric,
		FormatType: "num",
		Units: "misses",
	})
	// Fielddata内存占用大小
	fieldDataCacheMetric:=newMetricItem("fielddata_cache", 3, CacheGroupKey)
	fieldDataCacheMetric.AddAxi("FieldData Cache","group1",common.PositionLeft,"bytes","0,0","0,0.[00]",5,true)
	indexMetricItems=append(indexMetricItems, GroupMetricItem{
		Key: "fielddata_cache",
		Field: "payload.elasticsearch.index_stats.total.fielddata.memory_size_in_bytes",
		ID: util.GetUUID(),
		IsDerivative: false,
		MetricItem: fieldDataCacheMetric,
		FormatType: "bytes",
		Units: "",
	})
	//segment memory
	segmentMemoryMetric := newMetricItem("segment_memory", 13, MemoryGroupKey)
	segmentMemoryMetric.AddAxi("Segment memory","group1",common.PositionLeft,"bytes","0.[0]","0.[0]",5,true)
	indexMetricItems = append(indexMetricItems, GroupMetricItem{
		Key: "segment_memory",
		Field: "payload.elasticsearch.index_stats.total.segments.memory_in_bytes",
		ID: util.GetUUID(),
		IsDerivative: false,
		MetricItem: segmentMemoryMetric,
		FormatType: "bytes",
		Units: "",
	})

	//segment doc values memory
	docValuesMemoryMetric := newMetricItem("segment_doc_values_memory", 13, MemoryGroupKey)
	docValuesMemoryMetric.AddAxi("Segment Doc values Memory","group1",common.PositionLeft,"bytes","0.[0]","0.[0]",5,true)
	indexMetricItems = append(indexMetricItems, GroupMetricItem{
		Key: "segment_doc_values_memory",
		Field: "payload.elasticsearch.index_stats.total.segments.doc_values_memory_in_bytes",
		ID: util.GetUUID(),
		IsDerivative: false,
		MetricItem: docValuesMemoryMetric,
		FormatType: "bytes",
		Units: "",
	})

	//segment terms memory
	termsMemoryMetric := newMetricItem("segment_terms_memory", 13, MemoryGroupKey)
	termsMemoryMetric.AddAxi("Segment Terms Memory","group1",common.PositionLeft,"bytes","0.[0]","0.[0]",5,true)
	indexMetricItems = append(indexMetricItems, GroupMetricItem{
		Key: "segment_terms_memory",
		Field: "payload.elasticsearch.index_stats.total.segments.terms_memory_in_bytes",
		ID: util.GetUUID(),
		IsDerivative: false,
		MetricItem: termsMemoryMetric,
		FormatType: "bytes",
		Units: "",
	})

	//segment fields memory
	fieldsMemoryMetric := newMetricItem("segment_fields_memory", 13, MemoryGroupKey)
	fieldsMemoryMetric.AddAxi("Segment Fields Memory","group1",common.PositionLeft,"bytes","0.[0]","0.[0]",5,true)
	indexMetricItems = append(indexMetricItems, GroupMetricItem{
		Key: "segment_fields_memory",
		Field: "payload.elasticsearch.index_stats.total.segments.stored_fields_memory_in_bytes",
		ID: util.GetUUID(),
		IsDerivative: false,
		MetricItem: fieldsMemoryMetric,
		FormatType: "bytes",
		Units: "",
	})
	// segment index writer memory
	segmentIndexWriterMemoryMetric:=newMetricItem("segment_index_writer_memory", 16, MemoryGroupKey)
	segmentIndexWriterMemoryMetric.AddAxi("segment doc values memory","group1",common.PositionLeft,"bytes","0,0","0,0.[00]",5,true)
	indexMetricItems=append(indexMetricItems, GroupMetricItem{
		Key: "segment_index_writer_memory",
		Field: "payload.elasticsearch.index_stats.total.segments.index_writer_memory_in_bytes",
		ID: util.GetUUID(),
		IsDerivative: false,
		MetricItem: segmentIndexWriterMemoryMetric,
		FormatType: "bytes",
		Units: "",
	})
	// segment term vectors memory
	segmentTermVectorsMemoryMetric:=newMetricItem("segment_term_vectors_memory", 16, MemoryGroupKey)
	segmentTermVectorsMemoryMetric.AddAxi("segment term vectors memory","group1",common.PositionLeft,"bytes","0,0","0,0.[00]",5,true)
	indexMetricItems=append(indexMetricItems, GroupMetricItem{
		Key: "segment_term_vectors_memory",
		Field: "payload.elasticsearch.index_stats.total.segments.term_vectors_memory_in_bytes",
		ID: util.GetUUID(),
		IsDerivative: false,
		MetricItem: segmentTermVectorsMemoryMetric,
		FormatType: "bytes",
		Units: "",
	})

	aggs:=map[string]interface{}{}

	for _,metricItem:=range indexMetricItems {
		aggs[metricItem.ID]=util.MapStr{
			"max":util.MapStr{
				"field": metricItem.Field,
			},
		}

		if metricItem.Field2 != ""{
			aggs[metricItem.ID + "_field2"]=util.MapStr{
				"max":util.MapStr{
					"field": metricItem.Field2,
				},
			}
		}

		if metricItem.IsDerivative{
			aggs[metricItem.ID+"_deriv"]=util.MapStr{
				"derivative":util.MapStr{
					"buckets_path": metricItem.ID,
				},
			}
			if metricItem.Field2 != "" {
				aggs[metricItem.ID + "_deriv_field2"]=util.MapStr{
					"derivative":util.MapStr{
						"buckets_path": metricItem.ID + "_field2",
					},
				}
			}
		}
	}

	query["size"]=0
	query["aggs"]= util.MapStr{
		"group_by_level": util.MapStr{
			"terms": util.MapStr{
				"field": "metadata.labels.index_name",
				"size":  top,
				"order": util.MapStr{
					"max_store": "desc",
				},
			},
			"aggs": util.MapStr{
				"dates": util.MapStr{
					"date_histogram":util.MapStr{
						"field": "timestamp",
						"fixed_interval": bucketSizeStr,
					},
					"aggs":aggs,
				},
				"max_store": util.MapStr{
					"max": util.MapStr{
						"field": "payload.elasticsearch.index_stats.total.store.size_in_bytes",
					},
				},
			},
		},
	}
	return h.getMetrics(query, indexMetricItems, bucketSize)

}

func (h *APIHandler) getTopIndexName(clusterID string, top int, lastMinutes int) ([]string, error){
	var (
		now = time.Now()
		max = now.UnixNano()/1e6
		min = now.Add(-time.Duration(lastMinutes) * time.Minute).UnixNano()/1e6
	)
	query := util.MapStr{
		"size": 0,
		"query": util.MapStr{
			"bool": util.MapStr{
				"must_not": []util.MapStr{
					{
						"term": util.MapStr{
							"metadata.labels.index_name": util.MapStr{
								"value": "_all",
							},
						},
					},
				},
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
								"value": "index_stats",
							},
						},
					},
					{
						"term": util.MapStr{
							"metadata.labels.cluster_id": util.MapStr{
								"value": clusterID,
							},
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
		},
		"aggs": util.MapStr{
			"group_by_index": util.MapStr{
				"terms": util.MapStr{
					"field": "metadata.labels.index_name",
					"size":  10000,
				},
				"aggs": util.MapStr{
					"max_qps": util.MapStr{
						"max_bucket": util.MapStr{
							"buckets_path": "dates>search_qps",
						},
					},
					"max_qps_bucket_sort": util.MapStr{
						"bucket_sort": util.MapStr{
							"sort": []util.MapStr{
								{"max_qps": util.MapStr{"order": "desc"}}},
							"size": top,
						},
					},
					"dates": util.MapStr{
						"date_histogram": util.MapStr{
							"field":    "timestamp",
							"interval": "60s",
						},
						"aggs": util.MapStr{
							"search_query_total": util.MapStr{
								"max": util.MapStr{
									"field": "payload.elasticsearch.index_stats.total.search.query_total",
								},
							},
							"search_qps": util.MapStr{
								"derivative": util.MapStr{
									"buckets_path": "search_query_total",
								},
							},
						},
					},
				},
			},
			"group_by_index1": util.MapStr{
				"terms": util.MapStr{
					"field": "metadata.labels.index_name",
					"size":  10000,
				},
				"aggs": util.MapStr{
					"max_qps": util.MapStr{
						"max_bucket": util.MapStr{
							"buckets_path": "dates>index_qps",
						},
					},
					"max_qps_bucket_sort": util.MapStr{
						"bucket_sort": util.MapStr{
							"sort": []util.MapStr{
								{"max_qps": util.MapStr{"order": "desc"}},
							},
							"size": top,
						},
					},
					"dates": util.MapStr{
						"date_histogram": util.MapStr{
							"field":    "timestamp",
							"interval": "60s",
						},
						"aggs": util.MapStr{
							"index_total": util.MapStr{
								"max": util.MapStr{
									"field": "payload.elasticsearch.index_stats.total.indexing.index_total",
								},
							},
							"index_qps": util.MapStr{
								"derivative": util.MapStr{
									"buckets_path": "index_total",
								},
							},
						},
					},
				},
			},
		},
	}
	response,err:=elastic.GetClient(h.Config.Elasticsearch).SearchWithRawQueryDSL(getAllMetricsIndex(),util.MustToJSONBytes(query))
	if err!=nil{
		log.Error(err)
		return nil, err
	}
	var maxQpsKVS = map[string] float64{}
	for _, agg := range response.Aggregations {
		for _, bk := range agg.Buckets {
			key := bk["key"].(string)
			if maxQps, ok := bk["max_qps"].(map[string]interface{}); ok {
				val := maxQps["value"].(float64)
				if _, ok = maxQpsKVS[key] ; ok {
					maxQpsKVS[key] = maxQpsKVS[key] + val
				}else{
					maxQpsKVS[key] = val
				}
			}
		}
	}
	var (
		qpsValues TopTermOrder
	)
	for k, v := range maxQpsKVS {
		qpsValues = append(qpsValues, TopTerm{
			Key:   k,
			Value: v,
		})
	}
	sort.Sort(qpsValues)
	var length = top
	if top > len(qpsValues) {
		length = len(qpsValues)
	}
	indexNames := []string{}
	for i := 0; i <length; i++ {
		indexNames = append(indexNames, qpsValues[i].Key)
	}
	return indexNames, nil
}

type TopTerm struct {
	Key   string
	Value float64
}
type TopTermOrder []TopTerm
func (t TopTermOrder) Len() int{
	return len(t)
}
func (t TopTermOrder) Less(i, j int) bool{
	return t[i].Value > t[j].Value //desc
}
func (t TopTermOrder) Swap(i, j int){
	t[i], t[j] = t[j], t[i]
}
