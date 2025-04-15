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

package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path"
	"runtime"
	"strconv"

	log "github.com/sirupsen/logrus"

	"github.com/projectcalico/tiger-bench/pkg/cluster"
	"github.com/projectcalico/tiger-bench/pkg/config"
	"github.com/projectcalico/tiger-bench/pkg/dnsperf"
	"github.com/projectcalico/tiger-bench/pkg/elasticsearch"
	"github.com/projectcalico/tiger-bench/pkg/iperf"
	"github.com/projectcalico/tiger-bench/pkg/policy"
	"github.com/projectcalico/tiger-bench/pkg/qperf"
	"github.com/projectcalico/tiger-bench/pkg/results"
	"github.com/projectcalico/tiger-bench/pkg/utils"
)

func main() {
	log.SetReportCaller(true)
	log.SetLevel(log.InfoLevel)
	log.SetFormatter(&log.TextFormatter{
		CallerPrettyfier: func(frame *runtime.Frame) (function string, file string) {
			fileName := path.Base(frame.File) + ":" + strconv.Itoa(frame.Line)
			return "", fileName
		},
	})
	// get environment variables
	ctx := context.Background()
	cfg, clients, err := config.New(ctx)
	if err != nil {
		log.WithError(err).Fatal("failed to get config")
	}

	loglevel, err := log.ParseLevel(cfg.LogLevel)
	if err != nil {
		log.WithError(err).Fatal("failed to parse log level")
	}
	log.SetLevel(loglevel)

	const (
		testPolicyName = "zzz-perf-test-policy"
	)
	var benchmarkResults []results.Result
	log.Debug("Starting tests")
	log.Debug("Number of TestConfigs: ", len(cfg.TestConfigs))
	for _, testConfig := range cfg.TestConfigs {
		err = cluster.ConfigureCluster(ctx, cfg, clients, *testConfig)
		if err != nil {
			log.WithError(err).Fatal("failed to configure cluster")
		}
		err = cluster.SetupStandingConfig(ctx, clients, *testConfig, testConfig.TestNamespace, cfg.WebServerImage)
		if err != nil {
			log.WithError(err).Fatal("failed to setup standing config on cluster")
		}
		thisResult := results.Result{}
		thisResult.Config = *testConfig
		switch testConfig.TestKind {
		case config.TestKindIperf:
			var iperfResults []*iperf.Results
			err = policy.CreateTestPolicy(ctx, clients, testPolicyName, testConfig.TestNamespace, []int{testConfig.Perf.TestPort})
			if err != nil {
				log.WithError(err).Fatal("failed to create iperf test policy")
			}
			err = iperf.DeployIperfPods(ctx, clients, testConfig.TestNamespace, testConfig.HostNetwork, cfg.PerfImage, testConfig.Perf.TestPort)
			if err != nil {
				log.WithError(err).Fatal("failed to deploy iperf pods")
			}
			log.Info("Running iperf tests, Iterations=", testConfig.Iterations)
			for j := 0; j < testConfig.Iterations; j++ {
				iperfResult, err := iperf.RunIperfTests(ctx, clients, testConfig.Duration, testConfig.TestNamespace, *testConfig.Perf)
				if err != nil {
					log.WithError(err).Error("failed to get iperf results")
				}
				iperfResults = append(iperfResults, iperfResult)
			}
			if len(iperfResults) > 0 {
				thisResult.IPerf, err = iperf.SummarizeResults(iperfResults)
				if err != nil {
					log.WithError(err).Error("failed to summarize iperf results")
				}
			}
		case config.TestKindQperf:
			var qperfResults []*qperf.Results
			err = policy.CreateTestPolicy(ctx, clients, testPolicyName, testConfig.TestNamespace, []int{testConfig.Perf.ControlPort, testConfig.Perf.TestPort})
			if err != nil {
				log.WithError(err).Fatal("failed to create qperf test policy")
			}
			err = qperf.DeployQperfPods(ctx, clients, testConfig.TestNamespace, testConfig.HostNetwork, cfg.PerfImage, testConfig.Perf.ControlPort, testConfig.Perf.TestPort)
			if err != nil {
				log.WithError(err).Fatal("failed to deploy qperf pods")
			}
			for j := 0; j < testConfig.Iterations; j++ {
				log.Debug("entering qperf loop")
				qperfResult, err := qperf.RunQperfTests(ctx, clients, testConfig.Duration, testConfig.TestNamespace, *testConfig.Perf)
				if err != nil {
					log.WithError(err).Error("failed to get qperf results")
				}
				qperfResults = append(qperfResults, qperfResult)
				log.Debug("length of Results: ", len(qperfResults))
			}
			if len(qperfResults) > 0 {
				thisResult.QPerf, err = qperf.SummarizeResults(qperfResults)
				if err != nil {
					log.WithError(err).Error("failed to summarize qperf results")
				}
			}
		case config.TestKindDNSPerf:
			_, err = policy.GetOrCreateDNSPolicy(ctx, clients, dnsperf.MakeDNSPolicy(testConfig.TestNamespace, testPolicyName, testConfig.DNSPerf.NumDomains))
			if err != nil {
				log.WithError(err).Fatal("failed to create dnsperf policy")
			}
			thisResult.DNSPerf, err = dnsperf.RunDNSPerfTests(ctx, clients, testConfig.Duration, testConfig.TestNamespace, cfg.WebServerImage, cfg.PerfImage)
			if err != nil {
				log.WithError(err).Error("failed to run dnsperf tests")
			}
			log.Infof("dnsperf results: %v", thisResult.DNSPerf)
		default:
			log.Fatal("test type unknown")
		}
		if !testConfig.LeaveStandingConfig {
			// Clean up all the resources we might have created, apart from the namespace, which might have external service config in it
			err = utils.DeleteDeployment(ctx, clients, testConfig.TestNamespace, "standing-deployment")
			if err != nil {
				log.WithError(err).Fatal("failed to delete standing-deployment")
			}
			err = utils.DeleteDeployment(ctx, clients, testConfig.TestNamespace, "standing-svc")
			if err != nil {
				log.WithError(err).Fatal("failed to delete standing-svc")
			}
			err = utils.DeleteServicesWithPrefix(ctx, clients, testConfig.TestNamespace, "standing-svc")
			if err != nil {
				log.WithError(err).Fatal("failed to delete standing-svc")
			}
			err = utils.DeleteServicesWithPrefix(ctx, clients, testConfig.TestNamespace, "iperf-srv")
			if err != nil {
				log.WithError(err).Fatal("failed to delete iperf-srv")
			}
			err = utils.DeleteServicesWithPrefix(ctx, clients, testConfig.TestNamespace, "qperf-srv")
			if err != nil {
				log.WithError(err).Fatal("failed to delete qperf-srv")
			}
			err = utils.DeletePodsWithLabel(ctx, clients, testConfig.TestNamespace, "app=iperf")
			if err != nil {
				log.WithError(err).Fatal("failed to delete iperf pods")
			}
			err = utils.DeletePodsWithLabel(ctx, clients, testConfig.TestNamespace, "app=qperf")
			if err != nil {
				log.WithError(err).Fatal("failed to delete qperf pods")
			}
		}
		// If we set the CPU limit, unset it again.
		if testConfig.CalicoNodeCPULimit != "" {
			err = cluster.SetCalicoNodeCPULimit(ctx, clients, "0")
			if err != nil {
				log.WithError(err).Fatal("failed to reset calico node CPU limit")
			}
		}
		thisResult.ClusterDetails, err = cluster.GetClusterDetails(ctx, clients)
		if err != nil {
			log.WithError(err).Error("error getting cluster details")
		}
		log.Debugf("Result: %+v", thisResult)
		err = elasticsearch.UploadResult(cfg, thisResult, false)
		if err != nil {
			log.WithError(err).Error("failed to upload result to elasticsearch")
		}
		benchmarkResults = append(benchmarkResults, thisResult)
		log.Infof("Results: %+v", benchmarkResults)
		err = writeResultToFile(cfg.ResultsFile, benchmarkResults)
		if err != nil {
			log.WithError(err).Fatal("failed to write results to file")
		}
	}
}

func writeResultToFile(filename string, results []results.Result) (err error) {
	log.Debug("entering writeResultToFile function")
	file, err := os.Create(filename)
	if err != nil {
		return fmt.Errorf("failed to open output file: %s", filename)
	}
	defer func() {
		closeErr := file.Close()
		if closeErr != nil && err == nil {
			err = fmt.Errorf("failure while closing output file: %s", closeErr)
		}
	}()
	output, err := json.MarshalIndent(results, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal results: %s", err)
	}
	_, err = file.Write(output)
	if err != nil {
		return fmt.Errorf("failed to write results to file: %s", err)
	}
	return nil
}
