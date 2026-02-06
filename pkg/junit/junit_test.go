// Copyright (c) 2026 Tigera, Inc. All rights reserved.

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

package junit

import (
	"encoding/xml"
	"os"
	"testing"
	"time"

	"github.com/projectcalico/tiger-bench/pkg/cluster"
	"github.com/projectcalico/tiger-bench/pkg/config"
	"github.com/projectcalico/tiger-bench/pkg/dnsperf"
	"github.com/projectcalico/tiger-bench/pkg/iperf"
	"github.com/projectcalico/tiger-bench/pkg/qperf"
	"github.com/projectcalico/tiger-bench/pkg/results"
	"github.com/projectcalico/tiger-bench/pkg/stats"
	"github.com/projectcalico/tiger-bench/pkg/ttfr"
)

func TestGenerateJUnitReport(t *testing.T) {
	iperfSummary := &iperf.ResultSummary{}
	iperfSummary.Throughput.Direct = stats.ResultSummary{
		Average:       9850,
		NumDataPoints: 1,
		Unit:          "Mb/sec",
	}

	qperfSummary := &qperf.ResultSummary{}
	qperfSummary.Throughput.Direct = stats.ResultSummary{
		Average:       8950,
		NumDataPoints: 1,
		Unit:          "Mb/sec",
	}
	qperfSummary.Latency.Direct = stats.ResultSummary{
		Average:       42.3,
		NumDataPoints: 1,
		Unit:          "usec",
	}

	dnsperfResults := &dnsperf.Results{
		LookupTime: stats.ResultSummary{
			Average:       1.2,
			NumDataPoints: 10,
			Unit:          "s",
		},
		ConnectTime: stats.ResultSummary{
			Average:       0.5,
			NumDataPoints: 10,
			Unit:          "s",
		},
		SuccessfulCurls: 10,
		FailedCurls:     1,
	}

	// Create sample test results
	testResults := []results.Result{
		{
			Config: config.TestConfig{
				TestKind:   config.TestKindTTFR,
				Dataplane:  "iptables",
				NumPolicies: 10,
				Duration:   30,
				Iterations: 3,
			},
			ClusterDetails: cluster.Details{
				Provisioner:   "kind",
				CalicoVersion: "v3.22.1",
				K8SVersion:    "v1.28.0",
			},
			TTFR: []*ttfr.ResultSummary{
				{
					TTFRSummary: stats.ResultSummary{
						Min:           8.2,
						Max:           15.8,
						Average:       10.5,
						P50:           9.8,
						P90:           14.2,
						P99:           18.5,
						NumDataPoints: 100,
					},
				},
			},
		},
		{
			Config: config.TestConfig{
				TestKind:   config.TestKindIperf,
				Dataplane:  "bpf",
				Duration:   30,
				Iterations: 5,
			},
			ClusterDetails: cluster.Details{
				Provisioner:   "kind",
				CalicoVersion: "v3.22.1",
				K8SVersion:    "v1.28.0",
			},
			IPerf: iperfSummary,
		},
		{
			Config: config.TestConfig{
				TestKind:   config.TestKindQperf,
				Dataplane:  "nftables",
				NumPolicies: 25,
				Duration:   30,
				Iterations: 5,
			},
			ClusterDetails: cluster.Details{
				Provisioner:   "kind",
				CalicoVersion: "v3.22.1",
				K8SVersion:    "v1.28.0",
			},
			QPerf: qperfSummary,
		},
		{
			Config: config.TestConfig{
				TestKind:  config.TestKindDNSPerf,
				Dataplane: "iptables",
				DNSPerf: &config.DNSConfig{
					Mode: "Inline",
				},
				Duration:   30,
				Iterations: 1,
			},
			ClusterDetails: cluster.Details{
				Provisioner:   "kind",
				CalicoVersion: "v3.22.1",
				K8SVersion:    "v1.28.0",
			},
			DNSPerf: dnsperfResults,
		},
		{
			Config: config.TestConfig{
				TestKind:  config.TestKindDNSPerf,
				Dataplane: "bpf",
				DNSPerf: &config.DNSConfig{
					Mode: "InvalidMode",
				},
				Duration:   30,
				Iterations: 1,
			},
			ClusterDetails: cluster.Details{
				Provisioner:   "kind",
				CalicoVersion: "v3.22.1",
				K8SVersion:    "v1.28.0",
			},
			DNSPerf: nil, // No results = failure
		},
	}

	startTime := time.Now().Add(-5 * time.Minute)
	report, err := GenerateJUnitReport(testResults, startTime)
	if err != nil {
		t.Fatalf("GenerateJUnitReport failed: %v", err)
	}

	// Verify report structure
	if len(report.Suites) == 0 {
		t.Fatal("Expected at least one test suite")
	}

	// Count total tests
	totalTests := 0
	totalFailures := 0
	for _, suite := range report.Suites {
		totalTests += suite.Tests
		totalFailures += suite.Failures

		if suite.Tests != len(suite.TestCases) {
			t.Errorf("Suite %s: Tests count (%d) doesn't match TestCases length (%d)",
				suite.Name, suite.Tests, len(suite.TestCases))
		}
	}

	if totalTests != len(testResults) {
		t.Errorf("Expected %d total tests, got %d", len(testResults), totalTests)
	}

	// We expect one failure (the DNSPerf with no results)
	if totalFailures != 1 {
		t.Errorf("Expected 1 failure, got %d", totalFailures)
	}
}

func TestWriteJUnitReport(t *testing.T) {
	// Create a simple test report
	suites := JUnitTestSuites{
		Suites: []JUnitTestSuite{
			{
				Name:      "ttfr",
				Tests:     1,
				Failures:  0,
				Errors:    0,
				Time:      30.5,
				Timestamp: time.Now().Format(time.RFC3339),
				TestCases: []JUnitTestCase{
					{
						Name:      "ttfr_dataplane=iptables",
						Classname: "tiger-bench.ttfr",
						Time:      30.0,
						SystemOut: "Test completed successfully",
					},
				},
			},
		},
	}

	// Write to temporary file
	tmpFile := "/tmp/test-junit-report.xml"
	defer os.Remove(tmpFile)

	err := WriteJUnitReport(tmpFile, suites)
	if err != nil {
		t.Fatalf("WriteJUnitReport failed: %v", err)
	}

	// Verify file exists and contains valid XML
	data, err := os.ReadFile(tmpFile)
	if err != nil {
		t.Fatalf("Failed to read report file: %v", err)
	}

	// Try to unmarshal to verify it's valid XML
	var parsedSuites JUnitTestSuites
	err = xml.Unmarshal(data, &parsedSuites)
	if err != nil {
		t.Fatalf("Generated XML is invalid: %v", err)
	}

	// Verify structure
	if len(parsedSuites.Suites) != 1 {
		t.Errorf("Expected 1 suite, got %d", len(parsedSuites.Suites))
	}

	if parsedSuites.Suites[0].Tests != 1 {
		t.Errorf("Expected 1 test, got %d", parsedSuites.Suites[0].Tests)
	}
}

func TestGenerateTestName(t *testing.T) {
	tests := []struct {
		name     string
		config   config.TestConfig
		expected string
	}{
		{
			name: "Basic TTFR test",
			config: config.TestConfig{
				TestKind:  config.TestKindTTFR,
				Dataplane: "iptables",
			},
			expected: "ttfr_dataplane=iptables",
		},
		{
			name: "DNSPerf with mode",
			config: config.TestConfig{
				TestKind:  config.TestKindDNSPerf,
				Dataplane: "bpf",
				DNSPerf: &config.DNSConfig{
					Mode: "Inline",
				},
			},
			expected: "dnsperf_dataplane=bpf_dnsMode=Inline",
		},
		{
			name: "IPerf with policies and host network",
			config: config.TestConfig{
				TestKind:    config.TestKindIperf,
				Dataplane:   "nftables",
				NumPolicies: 100,
				HostNetwork: true,
			},
			expected: "iperf_dataplane=nftables_policies=100_hostNetwork",
		},
		{
			name: "QPerf with CPU limit",
			config: config.TestConfig{
				TestKind:            config.TestKindQperf,
				Dataplane:           "iptables",
				CalicoNodeCPULimit:  "2",
			},
			expected: "thruput-latency_dataplane=iptables_cpuLimit=2",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := generateTestName(tt.config)
			if result != tt.expected {
				t.Errorf("generateTestName() = %q, want %q", result, tt.expected)
			}
		})
	}
}

func TestIsTestFailed(t *testing.T) {
	tests := []struct {
		name          string
		result        results.Result
		expectFailure bool
		errorMsg      string
	}{
		{
			name: "Successful TTFR test",
			result: results.Result{
				Config: config.TestConfig{TestKind: config.TestKindTTFR},
				TTFR: []*ttfr.ResultSummary{
					{
						TTFRSummary: stats.ResultSummary{
							Average: 10.5,
						},
					},
				},
			},
			expectFailure: false,
		},
		{
			name: "Failed TTFR test - no results",
			result: results.Result{
				Config: config.TestConfig{TestKind: config.TestKindTTFR},
				TTFR:   nil,
			},
			expectFailure: true,
			errorMsg:      "No TTFR results collected",
		},
		{
			name: "Successful IPerf test",
			result: results.Result{
				Config: config.TestConfig{TestKind: config.TestKindIperf},
				IPerf: func() *iperf.ResultSummary {
					summary := &iperf.ResultSummary{}
					summary.Throughput.Direct = stats.ResultSummary{Average: 9500, NumDataPoints: 1}
					return summary
				}(),
			},
			expectFailure: false,
		},
		{
			name: "Failed IPerf test - zero throughput",
			result: results.Result{
				Config: config.TestConfig{TestKind: config.TestKindIperf},
				IPerf:  &iperf.ResultSummary{},
			},
			expectFailure: true,
			errorMsg:      "IPerf throughput has no data points",
		},
		{
			name: "Successful QPerf test",
			result: results.Result{
				Config: config.TestConfig{TestKind: config.TestKindQperf},
				QPerf: func() *qperf.ResultSummary {
					summary := &qperf.ResultSummary{}
					summary.Throughput.Direct = stats.ResultSummary{Average: 8800, NumDataPoints: 1}
					summary.Latency.Direct = stats.ResultSummary{Average: 40.2, NumDataPoints: 1}
					return summary
				}(),
			},
			expectFailure: false,
		},
		{
			name: "Failed QPerf test - no data points",
			result: results.Result{
				Config: config.TestConfig{TestKind: config.TestKindQperf},
				QPerf:  &qperf.ResultSummary{},
			},
			expectFailure: true,
			errorMsg:      "QPerf has no throughput or latency data points",
		},
		{
			name: "Failed DNSPerf test - no results",
			result: results.Result{
				Config:  config.TestConfig{TestKind: config.TestKindDNSPerf},
				DNSPerf: nil,
			},
			expectFailure: true,
			errorMsg:      "No DNSPerf results collected",
		},
		{
			name: "Test kind none - not a failure",
			result: results.Result{
				Config: config.TestConfig{TestKind: config.TestKindNone},
			},
			expectFailure: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			failed, msg := isTestFailed(tt.result)
			if failed != tt.expectFailure {
				t.Errorf("isTestFailed() failed = %v, want %v", failed, tt.expectFailure)
			}
			if tt.expectFailure && msg != tt.errorMsg {
				t.Errorf("isTestFailed() msg = %q, want %q", msg, tt.errorMsg)
			}
		})
	}
}
