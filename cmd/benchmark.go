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
	"time"

	log "github.com/sirupsen/logrus"
	ctrllog "sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/log/zap"

	"github.com/projectcalico/tiger-bench/pkg/cluster"
	"github.com/projectcalico/tiger-bench/pkg/config"
	"github.com/projectcalico/tiger-bench/pkg/dnsperf"
	"github.com/projectcalico/tiger-bench/pkg/elasticsearch"
	"github.com/projectcalico/tiger-bench/pkg/iperf"
	"github.com/projectcalico/tiger-bench/pkg/junit"
	"github.com/projectcalico/tiger-bench/pkg/policy"
	"github.com/projectcalico/tiger-bench/pkg/qperf"
	"github.com/projectcalico/tiger-bench/pkg/results"
	"github.com/projectcalico/tiger-bench/pkg/stats"
	"github.com/projectcalico/tiger-bench/pkg/ttfr"
	"github.com/projectcalico/tiger-bench/pkg/utils"
)

func main() {
	// Initialize controller-runtime logger to avoid "log.SetLogger(...) was never called" warning
	ctrllog.SetLogger(zap.New())

	log.SetReportCaller(true)
	log.SetLevel(log.InfoLevel)
	customFormatter := &log.TextFormatter{
		CallerPrettyfier: func(frame *runtime.Frame) (function string, file string) {
			fileName := path.Base(frame.File) + ":" + strconv.Itoa(frame.Line)
			return "", fileName
		},
	}
	customFormatter.TimestampFormat = "2006-01-02 15:04:05.000"
	customFormatter.FullTimestamp = true
	log.SetFormatter(customFormatter)

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

	// Validate test node prerequisites
	if clients.CtrlClient != nil {
		err = validateTestNodes(ctx, clients, cfg.TestConfigs)
		if err != nil {
			log.WithError(err).Fatal("test node validation failed")
		}
	} else {
		log.Warn("Skipping test node validation - no kubeconfig configured")
	}

	var benchmarkResults []results.Result
	startTime := time.Now()
	log.Debug("Starting tests")
	log.Debug("Number of TestConfigs: ", len(cfg.TestConfigs))
	for _, testConfig := range cfg.TestConfigs {
		// Clean up any leftover resources from previous runs
		cleanupNamespace(ctx, clients, testConfig)

		thisResult := results.Result{}
		thisResult.Config = *testConfig
		thisResult.ClusterDetails, _ = cluster.GetClusterDetails(ctx, clients)
		thisResult.Status = "failed"

		if err != nil {
			log.WithError(err).Error("failed to configure cluster")
			thisResult.Error = fmt.Sprintf("failed to configure cluster: %v", err)
			benchmarkResults = append(benchmarkResults, thisResult)
			cleanupNamespace(ctx, clients, testConfig)
			continue
		}
		// update cluster details after reconfig
		thisResult.ClusterDetails, _ = cluster.GetClusterDetails(ctx, clients)

		err = cluster.SetupStandingConfig(ctx, clients, *testConfig, testConfig.TestNamespace, cfg.WebServerImage)
		if err != nil {
			log.WithError(err).Error("failed to setup standing config on cluster")
			thisResult.Error = fmt.Sprintf("failed to setup standing config: %v", err)
			benchmarkResults = append(benchmarkResults, thisResult)
			cleanupNamespace(ctx, clients, testConfig)
			continue
		}
		switch testConfig.TestKind {
		case config.TestKindNone:
			// No test to run
		case config.TestKindIperf:
			var iperfResults []*iperf.Results
			err = policy.CreateTestPolicy(ctx, clients, testPolicyName, testConfig.TestNamespace, []int{testConfig.Perf.TestPort})
			if err != nil {
				log.WithError(err).Error("failed to create iperf test policy")
				thisResult.Error = fmt.Sprintf("failed to create iperf test policy: %v", err)
				benchmarkResults = append(benchmarkResults, thisResult)
				cleanupNamespace(ctx, clients, testConfig)
				continue
			}
			err = iperf.DeployIperfPods(ctx, clients, testConfig.TestNamespace, testConfig.HostNetwork, cfg.PerfImage, testConfig.Perf.TestPort)
			if err != nil {
				log.WithError(err).Error("failed to deploy iperf pods")
				thisResult.Error = fmt.Sprintf("failed to deploy iperf pods: %v", err)
				benchmarkResults = append(benchmarkResults, thisResult)
				cleanupNamespace(ctx, clients, testConfig)
				continue
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
				log.WithError(err).Error("failed to create qperf test policy")
				thisResult.Error = fmt.Sprintf("failed to create qperf test policy: %v", err)
				benchmarkResults = append(benchmarkResults, thisResult)
				cleanupNamespace(ctx, clients, testConfig)
				continue
			}
			err = qperf.DeployQperfPods(ctx, clients, testConfig.TestNamespace, testConfig.HostNetwork, cfg.PerfImage, testConfig.Perf.ControlPort, testConfig.Perf.TestPort)
			if err != nil {
				log.WithError(err).Error("failed to deploy qperf pods")
				thisResult.Error = fmt.Sprintf("failed to deploy qperf pods: %v", err)
				benchmarkResults = append(benchmarkResults, thisResult)
				cleanupNamespace(ctx, clients, testConfig)
				continue
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
			if testConfig.DNSPerf.TestDNSPolicy {
				mypol, err := dnsperf.MakeDNSPolicy(testConfig.TestNamespace, testPolicyName, testConfig.DNSPerf.NumDomains, testConfig.DNSPerf.TargetURL)
				if err != nil {
					log.WithError(err).Error("failed to create dnsperf DNS policy object")
					thisResult.Error = fmt.Sprintf("failed to create dnsperf DNS policy object: %v", err)
					benchmarkResults = append(benchmarkResults, thisResult)
					cleanupNamespace(ctx, clients, testConfig)
					continue
				}
				_, err = policy.GetOrCreateDNSPolicy(ctx, clients, mypol)
				if err != nil {
					log.WithError(err).Error("failed to create dnsperf DNS policy")
					thisResult.Error = fmt.Sprintf("failed to create dnsperf DNS policy: %v", err)
					benchmarkResults = append(benchmarkResults, thisResult)
					cleanupNamespace(ctx, clients, testConfig)
					continue
				}
			}
			thisResult.DNSPerf, err = dnsperf.RunDNSPerfTests(ctx, clients, testConfig, cfg.WebServerImage, cfg.PerfImage)
			if err != nil {
				log.WithError(err).Error("failed to run dnsperf tests")
			}
			log.Infof("dnsperf results: %v", thisResult.DNSPerf)
		case config.TestKindTTFR:
			var ttfrResultsList []*ttfr.Results
			// Apply standing policy (that applies to both server and test pods)
			err := policy.CreateTestPolicy(ctx, clients, testPolicyName, testConfig.TestNamespace, []int{8080})
			if err != nil {
				log.WithError(err).Error("failed to create ttfr test policy")
				thisResult.Error = fmt.Sprintf("failed to create ttfr test policy: %v", err)
				benchmarkResults = append(benchmarkResults, thisResult)
				cleanupNamespace(ctx, clients, testConfig)
				continue
			}
			log.Info("Running ttfr tests, Iterations=", testConfig.Iterations)
			for j := 0; j < testConfig.Iterations; j++ {
				ttfrResult, err := ttfr.RunTTFRTest(ctx, clients, testConfig, cfg)
				if err != nil {
					log.WithError(err).Error("failed to get ttfr results")
					continue
				}
				ttfrResultsList = append(ttfrResultsList, &ttfrResult)
			}
			if len(ttfrResultsList) > 0 {
				thisResult.TTFR, err = ttfr.SummarizeResults(ttfrResultsList)
				if err != nil {
					log.WithError(err).Error("failed to summarize ttfr results")
				}
			}
		default:
			log.Error("test type unknown")
			thisResult.Error = fmt.Sprintf("unknown test type: %s", testConfig.TestKind)

			benchmarkResults = append(benchmarkResults, thisResult)
			cleanupNamespace(ctx, clients, testConfig)
			continue
		}
		if thisResult.Error == "" {
			thisResult.Status = "success"
		}
		// If we set the CPU limit, unset it again.
		if testConfig.CalicoNodeCPULimit != "" {
			err = cluster.SetCalicoNodeCPULimit(ctx, clients, "0")
			if err != nil {
				log.WithError(err).Error("failed to reset calico node CPU limit")
			}
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
			log.WithError(err).Error("failed to write results to file")
		}

		// Clean up after test completes
		cleanupNamespace(ctx, clients, testConfig)
	}

	// Generate and write JUnit report after all tests complete
	if cfg.JUnitReportFile != "" {
		junitReport, err := junit.GenerateJUnitReport(benchmarkResults, startTime)
		if err != nil {
			log.WithError(err).Error("failed to generate JUnit report")
		} else {
			err = junit.WriteJUnitReport(cfg.JUnitReportFile, junitReport)
			if err != nil {
				log.WithError(err).Error("failed to write JUnit report")
			} else {
				log.Infof("JUnit report written to %s", cfg.JUnitReportFile)
			}
		}
	}
}

func cleanupNamespace(ctx context.Context, clients config.Clients, testConfig *config.TestConfig) {
	log.Debug("entering cleanupNamespace function")
	if !testConfig.LeaveStandingConfig {
		// Clean up all the resources we might have created, apart from the namespace, which might have external service config in it
		log.Info("Cleaning up namespace: ", testConfig.TestNamespace)
		err := utils.DeleteDeploymentsWithPrefix(ctx, clients, testConfig.TestNamespace, "standing-deployment")
		if err != nil {
			log.WithError(err).Error("failed to delete standing-deployment")
		}
		err = utils.DeleteDeploymentsWithPrefix(ctx, clients, testConfig.TestNamespace, "standing-svc")
		if err != nil {
			log.WithError(err).Error("failed to delete standing-svc")
		}
		err = utils.DeleteServicesWithPrefix(ctx, clients, testConfig.TestNamespace, "standing-svc")
		if err != nil {
			log.WithError(err).Error("failed to delete standing-svc")
		}
		err = utils.DeleteDeploymentsWithPrefix(ctx, clients, testConfig.TestNamespace, "ttfr-test-")
		if err != nil {
			log.WithError(err).Error("failed to delete ttfr deployments")
		}
		err = utils.DeleteDeploymentsWithPrefix(ctx, clients, testConfig.TestNamespace, "headless")
		if err != nil {
			log.WithError(err).Error("failed to delete headless deployments")
		}
		err = utils.DeleteServicesWithPrefix(ctx, clients, testConfig.TestNamespace, "iperf-srv")
		if err != nil {
			log.WithError(err).Error("failed to delete iperf-srv")
		}
		err = utils.DeleteServicesWithPrefix(ctx, clients, testConfig.TestNamespace, "qperf-srv")
		if err != nil {
			log.WithError(err).Error("failed to delete qperf-srv")
		}
		err = utils.DeletePodsWithLabel(ctx, clients, testConfig.TestNamespace, "app=iperf")
		if err != nil {
			log.WithError(err).Error("failed to delete iperf pods")
		}
		err = utils.DeletePodsWithLabel(ctx, clients, testConfig.TestNamespace, "app=qperf")
		if err != nil {
			log.WithError(err).Error("failed to delete qperf pods")
		}
		err = utils.DeletePodsWithLabel(ctx, clients, testConfig.TestNamespace, "app=ttfr")
		if err != nil {
			log.WithError(err).Error("failed to delete ttfr pods")
		}
		err = utils.DeletePodsWithLabel(ctx, clients, testConfig.TestNamespace, "app=dnsperf")
		if err != nil {
			log.WithError(err).Error("failed to delete dnsperf pods")
		}
		err = utils.DeleteNetPolsInNamespace(ctx, clients, testConfig.TestNamespace)
		if err != nil {
			log.WithError(err).Error("failed to delete netpols")
		}
		err = utils.DeleteCalicoNetworkPoliciesInNamespace(ctx, clients, testConfig.TestNamespace)
		if err != nil {
			log.WithError(err).Error("failed to delete Calico network policies")
		}
		err = utils.DeleteDeploymentsWithPrefix(ctx, clients, testConfig.TestNamespace, "dnsscale")
		if err != nil {
			log.WithError(err).Error("failed to delete dnsscale deployments")
		}
		log.Info("Cleanup complete")
	}
}

func validateTestNodes(ctx context.Context, clients config.Clients, testConfigs []*config.TestConfig) error {
	log.Info("Validating test nodes...")
	testNodes, err := stats.GetTestNodes(ctx, clients)
	if err != nil {
		return fmt.Errorf("failed to get test nodes: %w", err)
	}

	if len(testNodes) == 0 {
		return fmt.Errorf("no nodes found with label tigera.io/test-nodepool=default-pool. Please label nodes with: kubectl label node <nodename> tigera.io/test-nodepool=default-pool")
	}

	// Check if any test requires 2+ nodes
	requiresTwoNodes := false
	for _, testConfig := range testConfigs {
		if testConfig.TestKind == config.TestKindQperf || testConfig.TestKind == config.TestKindIperf {
			requiresTwoNodes = true
		}
	}

	if len(testNodes) < 2 && requiresTwoNodes {
		return fmt.Errorf("only %d node(s) found with label tigera.io/test-nodepool=default-pool, but thruput-latency or iperf tests require at least 2 nodes. Please label at least 2 nodes with: kubectl label node <nodename> tigera.io/test-nodepool=default-pool", len(testNodes))
	}

	log.Infof("Found %d test node(s) with label tigera.io/test-nodepool=default-pool: %v", len(testNodes), testNodes)
	return nil
}

func writeResultToFile(filename string, results []results.Result) (err error) {
	log.Debug("entering writeResultToFile function")
	file, err := os.Create(filename)
	if err != nil {
		log.WithError(err).Errorf("failed to open output file: %s", filename)
		return err
	}
	defer func() {
		closeErr := file.Close()
		if closeErr != nil && err == nil {
			err = fmt.Errorf("failure while closing output file: %s", closeErr)
		}
	}()
	output, err := json.MarshalIndent(results, "", "  ")
	if err != nil {
		log.WithError(err).Errorf("failed to marshal results: %s", err)
		return err
	}
	_, err = file.Write(output)
	if err != nil {
		log.WithError(err).Errorf("failed to write results to file: %s", err)
		return err
	}
	return nil
}
