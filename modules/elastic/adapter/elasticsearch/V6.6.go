/* Copyright © INFINI Ltd. All rights reserved.
 * Web: https://infinilabs.com
 * Email: hello#infini.ltd */

package elasticsearch

import (
	"fmt"
	"infini.sh/framework/core/util"
)

type ESAPIV6_6 struct {
	ESAPIV6
}

func (s *ESAPIV6_6) UpdateMapping(indexName string, docType string, mappings []byte) ([]byte, error) {
	indexName = util.UrlEncode(indexName)
	if docType == "" {
		docType = TypeName0
	}

	url := fmt.Sprintf("%s/%s/%s/_mapping", s.GetEndpoint(), indexName, docType)

	resp, err := s.Request(nil, util.Verb_POST, url, mappings)

	if err != nil {
		panic(err)
	}

	return resp.Body, nil
}
