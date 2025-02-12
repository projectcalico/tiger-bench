// Copyright (c) 2024-2025 Tigera, Inc. All rights reserved.

// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package elasticsearch

import (
	"crypto/tls"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/projectcalico/tiger-bench/pkg/config"
	"github.com/projectcalico/tiger-bench/pkg/results"

	es "github.com/elastic/go-elasticsearch/v7"
	log "github.com/sirupsen/logrus"
)

func connect(cfg config.Config) (*es.Client, error) {

	transport := http.DefaultTransport
	tlsClientConfig := &tls.Config{}
	transport.(*http.Transport).TLSClientConfig = tlsClientConfig
	escfg := es.Config{
		Addresses: []string{
			cfg.ESUrl,
		},
		Transport: transport,
	}
	if cfg.ESAPIKey == "" {
		escfg.Username = cfg.ESUser
		escfg.Password = cfg.ESPassword
	} else {
		escfg.APIKey = cfg.ESAPIKey
	}
	es, err := es.NewClient(escfg)
	if err != nil {
		log.Errorf("error creating the client: %s", err)
		return nil, err
	}
	response, err := es.Ping()
	if err != nil {
		log.Infof("ES response: %s", response)
		log.Warnf("error contacting the server: %s", err)
		return nil, err
	}
	log.Infof("Connected to ElasticSearch: %s", cfg.ESUrl)
	return es, nil
}

func createESDoc(result results.Result) (string, error) {
	doc, err := json.Marshal(result)
	if err != nil {
		log.Errorf("error marshaling document: %s", err)
		return "", err
	}
	return string(doc), nil
}

func indexDocFile(es *es.Client, index string, doc string, dryrun bool) error {
	log.Infof("Indexing document to ElasticSearch: %s", doc)
	esIndex := fmt.Sprintf("benchmark_data_%s_%s", index, time.Now().Format("2006-01-02"))
	if !dryrun {
		response, err := es.Index(esIndex, strings.NewReader(doc))
		log.Infof("ES Response: %s", response)
		if err != nil {
			log.Errorf("error indexing document: %s", err)
			return err
		}
	} else {
		log.Infof("Dryrun: Indexing document to ElasticSearch %s: %s", esIndex, doc)
	}
	return nil
}

// UploadResult uploads the result to ElasticSearch
func UploadResult(cfg config.Config, result results.Result, dryrun bool) error {
	if cfg.ESUrl == "" {
		log.Warn("No ElasticSearch URL provided, skipping upload")
		return nil
	}
	if cfg.ESUser == "" || cfg.ESAPIKey == "" {
		log.Warn("Missing ElasticSearch credentials, skipping upload")
		return nil
	}
	es, err := connect(cfg)
	if err != nil {
		log.Errorf("error connecting to ElasticSearch: %s", err)
		return err
	}
	doc, err := createESDoc(result)
	if err != nil {
		log.Errorf("error creating document: %s", err)
		return err
	}
	index := string(result.Config.TestKind)
	return indexDocFile(es, index, doc, dryrun)
}
