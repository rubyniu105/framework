package api

import (
	"fmt"
	"infini.sh/framework/core/util"
	"infini.sh/framework/modules/elastic/common"
)

func (h *APIHandler) getIndexMetrics(clusterID string, bucketSize int, min, max int64, indexName string, top int) map[string]*common.MetricItem{
	bucketSizeStr:=fmt.Sprintf("%vs",bucketSize)

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
	queryTimesMetric := newMetricItem("query_times", 3, OperationGroupKey)
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
	fetchTimesMetric := newMetricItem("fetch_times", 4, OperationGroupKey)
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
	scrollTimesMetric := newMetricItem("scroll_times", 6, OperationGroupKey)
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
	mergeTimesMetric := newMetricItem("merge_times", 5, OperationGroupKey)
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
	refreshTimesMetric := newMetricItem("refresh_times", 6, OperationGroupKey)
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
	indexingRateMetric := newMetricItem("indexing_rate", 7, OperationGroupKey)
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
	indexingLatencyMetric := newMetricItem("indexing_latency", 8, LatencyGroupKey)
	indexingLatencyMetric.AddAxi("Indexing latency","group1",common.PositionLeft,"num","0.[0]","0.[0]",5,true)
	indexMetricItems = append(indexMetricItems, GroupMetricItem{
		Key: "indexing_latency",
		Field: "payload.elasticsearch.index_stats.total.indexing.index_time_in_millis",
		ID: util.GetUUID(),
		IsDerivative: true,
		MetricItem: indexingLatencyMetric,
		FormatType: "num",
		Units: "ms",
	})

	//查询时延
	queryLatencyMetric := newMetricItem("query_latency", 9, LatencyGroupKey)
	queryLatencyMetric.AddAxi("Query latency","group1",common.PositionLeft,"num","0.[0]","0.[0]",5,true)
	indexMetricItems = append(indexMetricItems, GroupMetricItem{
		Key: "query_latency",
		Field: "payload.elasticsearch.index_stats.total.search.query_time_in_millis",
		ID: util.GetUUID(),
		IsDerivative: true,
		MetricItem: queryLatencyMetric,
		FormatType: "num",
		Units: "ms",
	})
	//fetch时延
	fetchLatencyMetric := newMetricItem("fetch_latency", 9, LatencyGroupKey)
	fetchLatencyMetric.AddAxi("Fetch latency","group1",common.PositionLeft,"num","0.[0]","0.[0]",5,true)
	indexMetricItems = append(indexMetricItems, GroupMetricItem{
		Key: "fetch_latency",
		Field: "payload.elasticsearch.index_stats.total.search.fetch_time_in_millis",
		ID: util.GetUUID(),
		IsDerivative: true,
		MetricItem: fetchLatencyMetric,
		FormatType: "num",
		Units: "ms",
	})

	//merge时延
	mergeLatencyMetric := newMetricItem("merge_latency", 10, LatencyGroupKey)
	mergeLatencyMetric.AddAxi("Merge latency","group1",common.PositionLeft,"num","0.[0]","0.[0]",5,true)
	indexMetricItems = append(indexMetricItems, GroupMetricItem{
		Key: "merge_latency",
		Field: "payload.elasticsearch.index_stats.total.merges.total_time_in_millis",
		ID: util.GetUUID(),
		IsDerivative: true,
		MetricItem: mergeLatencyMetric,
		FormatType: "num",
		Units: "ms",
	})
	//refresh时延
	refreshLatencyMetric := newMetricItem("refresh_latency", 11, LatencyGroupKey)
	refreshLatencyMetric.AddAxi("Refresh latency","group1",common.PositionLeft,"num","0.[0]","0.[0]",5,true)
	indexMetricItems = append(indexMetricItems, GroupMetricItem{
		Key: "refresh_latency",
		Field: "payload.elasticsearch.index_stats.total.refresh.total_time_in_millis",
		ID: util.GetUUID(),
		IsDerivative: true,
		MetricItem: refreshLatencyMetric,
		FormatType: "num",
		Units: "ms",
	})
	//scroll时延
	scrollLatencyMetric := newMetricItem("scroll_latency", 11, LatencyGroupKey)
	scrollLatencyMetric.AddAxi("Scroll Latency","group1",common.PositionLeft,"num","0.[0]","0.[0]",5,true)
	indexMetricItems = append(indexMetricItems, GroupMetricItem{
		Key: "scroll_latency",
		Field: "payload.elasticsearch.index_stats.total.refresh.total_time_in_millis",
		ID: util.GetUUID(),
		IsDerivative: true,
		MetricItem: scrollLatencyMetric,
		FormatType: "num",
		Units: "ms",
	})
	//flush 时延
	flushLatencyMetric := newMetricItem("flush_latency", 11, LatencyGroupKey)
	flushLatencyMetric.AddAxi("Flush latency","group1",common.PositionLeft,"num","0.[0]","0.[0]",5,true)
	indexMetricItems = append(indexMetricItems, GroupMetricItem{
		Key: "flush_latency",
		Field: "payload.elasticsearch.index_stats.total.search.scroll_time_in_millis",
		ID: util.GetUUID(),
		IsDerivative: true,
		MetricItem: flushLatencyMetric,
		FormatType: "num",
		Units: "ms",
	})
	//queryCache
	queryCacheMetric := newMetricItem("query_cache", 12, CacheGroupKey)
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
	requestCacheMetric := newMetricItem("request_cache", 12, CacheGroupKey)
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
	requestCacheHitMetric:=newMetricItem("request_cache_hit", 11, CacheGroupKey)
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
	requestCacheMissMetric:=newMetricItem("request_cache_miss", 11, CacheGroupKey)
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
	queryCacheCountMetric:=newMetricItem("query_cache_count", 11, CacheGroupKey)
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
	queryCacheHitMetric:=newMetricItem("query_cache_hit", 11, CacheGroupKey)
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

	// Query Cache evictions
	queryCacheEvictionsMetric:=newMetricItem("query_cache_evictions", 11, CacheGroupKey)
	queryCacheEvictionsMetric.AddAxi("query cache evictions","group1",common.PositionLeft,"num","0,0","0,0.[00]",5,true)
	indexMetricItems=append(indexMetricItems, GroupMetricItem{
		Key: "query_cache_evictions",
		Field: "payload.elasticsearch.index_stats.total.query_cache.evictions",
		ID: util.GetUUID(),
		IsDerivative: true,
		MetricItem: queryCacheEvictionsMetric,
		FormatType: "num",
		Units: "evictions",
	})

	// Query Cache Miss
	queryCacheMissMetric:=newMetricItem("query_cache_miss", 11, CacheGroupKey)
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
	fieldDataCacheMetric:=newMetricItem("fielddata_cache", 12, CacheGroupKey)
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

