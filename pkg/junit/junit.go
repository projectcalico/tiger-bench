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
	"fmt"
	"os"
	"time"

	log "github.com/sirupsen/logrus"

	"github.com/projectcalico/tiger-bench/pkg/config"
	"github.com/projectcalico/tiger-bench/pkg/results"
	"github.com/projectcalico/tiger-bench/pkg/stats"
)

// JUnitTestSuites represents the top-level JUnit XML structure
type JUnitTestSuites struct {
	XMLName xml.Name         `xml:"testsuites"`
	Suites  []JUnitTestSuite `xml:"testsuite"`
}

// JUnitTestSuite represents a test suite
type JUnitTestSuite struct {
	Name      string          `xml:"name,attr"`
	Tests     int             `xml:"tests,attr"`
	Failures  int             `xml:"failures,attr"`
	Errors    int             `xml:"errors,attr"`
	Skipped   int             `xml:"skipped,attr"`
	Time      float64         `xml:"time,attr"`
	Timestamp string          `xml:"timestamp,attr"`
	TestCases []JUnitTestCase `xml:"testcase"`
}

// JUnitTestCase represents a single test case
type JUnitTestCase struct {
	Name      string          `xml:"name,attr"`
	Classname string          `xml:"classname,attr"`
	Time      float64         `xml:"time,attr"`
	Failure   *JUnitFailure   `xml:"failure,omitempty"`
	Error     *JUnitError     `xml:"error,omitempty"`
	Skipped   *JUnitSkipped   `xml:"skipped,omitempty"`
	SystemOut string          `xml:"system-out,omitempty"`
}

// JUnitFailure represents a test failure
type JUnitFailure struct {
	Message string `xml:"message,attr"`
	Type    string `xml:"type,attr"`
	Content string `xml:",chardata"`
}

// JUnitError represents a test error
type JUnitError struct {
	Message string `xml:"message,attr"`
	Type    string `xml:"type,attr"`
	Content string `xml:",chardata"`
}

// JUnitSkipped represents a skipped test
type JUnitSkipped struct {
	Message string `xml:"message,attr,omitempty"`
}

// GenerateJUnitReport creates a JUnit XML report from benchmark results
func GenerateJUnitReport(benchmarkResults []results.Result, startTime time.Time) (JUnitTestSuites, error) {
	suites := JUnitTestSuites{}

	// Group results by test type
	testsByType := make(map[config.TestKind][]results.Result)
	for _, result := range benchmarkResults {
		kind := result.Config.TestKind
		testsByType[kind] = append(testsByType[kind], result)
	}

	// Create a suite for each test type
	for testKind, testResults := range testsByType {
		suite := JUnitTestSuite{
			Name:      string(testKind),
			Timestamp: startTime.Format(time.RFC3339),
			TestCases: []JUnitTestCase{},
		}

		for _, result := range testResults {
			testCase := createTestCase(result)
			suite.TestCases = append(suite.TestCases, testCase)
			suite.Tests++

			if testCase.Failure != nil {
				suite.Failures++
			}
			if testCase.Error != nil {
				suite.Errors++
			}
			if testCase.Skipped != nil {
				suite.Skipped++
			}

			suite.Time += testCase.Time
		}

		suites.Suites = append(suites.Suites, suite)
	}

	return suites, nil
}

// createTestCase converts a benchmark result to a JUnit test case
func createTestCase(result results.Result) JUnitTestCase {
	tc := JUnitTestCase{
		Classname: fmt.Sprintf("tiger-bench.%s", result.Config.TestKind),
		Time:      float64(result.Config.Duration),
	}

	// Generate a descriptive test name
	testName := generateTestName(result.Config)
	tc.Name = testName

	// Build system output with key metrics
	tc.SystemOut = buildSystemOutput(result)

	// Determine if test passed or failed based on results
	failed, errorMsg := isTestFailed(result)
	if failed {
		tc.Failure = &JUnitFailure{
			Message: errorMsg,
			Type:    "TestFailure",
			Content: errorMsg,
		}
	}

	return tc
}

// generateTestName creates a descriptive name for the test
func generateTestName(cfg config.TestConfig) string {
	name := fmt.Sprintf("%s", cfg.TestKind)

	if cfg.Dataplane != "" {
		name += fmt.Sprintf("_dataplane=%s", cfg.Dataplane)
	}

	if cfg.NumPolicies > 0 {
		name += fmt.Sprintf("_policies=%d", cfg.NumPolicies)
	}

	if cfg.HostNetwork {
		name += "_hostNetwork"
	}

	if cfg.CalicoNodeCPULimit != "" {
		name += fmt.Sprintf("_cpuLimit=%s", cfg.CalicoNodeCPULimit)
	}

	if cfg.DNSPerf != nil && cfg.DNSPerf.Mode != "" {
		name += fmt.Sprintf("_dnsMode=%s", cfg.DNSPerf.Mode)
	}

	return name
}

// buildSystemOutput generates detailed test output
func buildSystemOutput(result results.Result) string {
	output := fmt.Sprintf("Test: %s\n", result.Config.TestKind)
	output += fmt.Sprintf("Dataplane: %s\n", result.Config.Dataplane)
	output += fmt.Sprintf("Num Policies: %d\n", result.Config.NumPolicies)
	output += fmt.Sprintf("Duration: %ds\n", result.Config.Duration)
	output += fmt.Sprintf("Iterations: %d\n\n", result.Config.Iterations)

	// Add cluster details
	output += fmt.Sprintf("Provisioner: %s\n", result.ClusterDetails.Provisioner)
	output += fmt.Sprintf("Calico Version: %s\n", result.ClusterDetails.CalicoVersion)
	output += fmt.Sprintf("Kubernetes Version: %s\n\n", result.ClusterDetails.K8SVersion)

	// Add test-specific results
	switch result.Config.TestKind {
	case config.TestKindTTFR:
		if result.TTFR != nil && len(result.TTFR) > 0 {
			output += "TTFR Results:\n"
			for _, ttfr := range result.TTFR {
				output += fmt.Sprintf("  Average: %.2fms (Min: %.2fms, Max: %.2fms)\n",
					ttfr.TTFRSummary.Average, ttfr.TTFRSummary.Min, ttfr.TTFRSummary.Max)
				output += fmt.Sprintf("  p50: %.2fms, p90: %.2fms, p99: %.2fms\n",
					ttfr.TTFRSummary.P50, ttfr.TTFRSummary.P90, ttfr.TTFRSummary.P99)
				output += fmt.Sprintf("  Data points: %d\n", ttfr.TTFRSummary.NumDataPoints)
			}
		}
	case config.TestKindIperf:
		if result.IPerf != nil {
			output += "IPerf Results:\n"
			appendStatsSummary(&output, "  Direct throughput", result.IPerf.Throughput.Direct)
			appendStatsSummary(&output, "  Service throughput", result.IPerf.Throughput.Service)
			appendStatsSummary(&output, "  External throughput", result.IPerf.Throughput.External)
		}
	case config.TestKindQperf:
		if result.QPerf != nil {
			output += "QPerf Results:\n"
			appendStatsSummary(&output, "  Direct throughput", result.QPerf.Throughput.Direct)
			appendStatsSummary(&output, "  Service throughput", result.QPerf.Throughput.Service)
			appendStatsSummary(&output, "  External throughput", result.QPerf.Throughput.External)
			appendStatsSummary(&output, "  Direct latency", result.QPerf.Latency.Direct)
			appendStatsSummary(&output, "  Service latency", result.QPerf.Latency.Service)
			appendStatsSummary(&output, "  External latency", result.QPerf.Latency.External)
		}
	case config.TestKindDNSPerf:
		if result.DNSPerf != nil {
			output += "DNSPerf Results:\n"
			if result.Config.DNSPerf != nil {
				output += fmt.Sprintf("  Mode: %s\n", result.Config.DNSPerf.Mode)
			}
			appendStatsSummary(&output, "  Lookup time", result.DNSPerf.LookupTime)
			appendStatsSummary(&output, "  Connect time", result.DNSPerf.ConnectTime)
			output += fmt.Sprintf("  Successful curls: %d, Failed curls: %d\n", result.DNSPerf.SuccessfulCurls, result.DNSPerf.FailedCurls)
		}
	}

	return output
}

// isTestFailed determines if a test should be marked as failed
func isTestFailed(result results.Result) (bool, string) {
	// Check if we got any results for the test type
	switch result.Config.TestKind {
	case config.TestKindTTFR:
		if result.TTFR == nil || len(result.TTFR) == 0 {
			return true, "No TTFR results collected"
		}
	case config.TestKindIperf:
		if result.IPerf == nil {
			return true, "No IPerf results collected"
		}
		if result.IPerf.Throughput.Direct.NumDataPoints == 0 &&
			result.IPerf.Throughput.Service.NumDataPoints == 0 &&
			result.IPerf.Throughput.External.NumDataPoints == 0 {
			return true, "IPerf throughput has no data points"
		}
	case config.TestKindQperf:
		if result.QPerf == nil {
			return true, "No QPerf results collected"
		}
		if result.QPerf.Throughput.Direct.NumDataPoints == 0 &&
			result.QPerf.Throughput.Service.NumDataPoints == 0 &&
			result.QPerf.Throughput.External.NumDataPoints == 0 &&
			result.QPerf.Latency.Direct.NumDataPoints == 0 &&
			result.QPerf.Latency.Service.NumDataPoints == 0 &&
			result.QPerf.Latency.External.NumDataPoints == 0 {
			return true, "QPerf has no throughput or latency data points"
		}
	case config.TestKindDNSPerf:
		if result.DNSPerf == nil {
			return true, "No DNSPerf results collected"
		}
		if result.DNSPerf.SuccessfulCurls == 0 &&
			result.DNSPerf.LookupTime.NumDataPoints == 0 &&
			result.DNSPerf.ConnectTime.NumDataPoints == 0 {
			return true, "DNSPerf has no successful curls or timing data"
		}
	case config.TestKindNone:
		// Skip tests are not failures
		return false, ""
	}

	return false, ""
}

func appendStatsSummary(output *string, label string, summary stats.ResultSummary) {
	if summary.NumDataPoints == 0 {
		return
	}
	unit := summary.Unit
	if unit != "" {
		unit = " " + unit
	}
	*output += fmt.Sprintf("%s: avg=%.2f%s (min=%.2f, max=%.2f, p50=%.2f, p90=%.2f, p99=%.2f, n=%d)\n",
		label,
		summary.Average,
		unit,
		summary.Min,
		summary.Max,
		summary.P50,
		summary.P90,
		summary.P99,
		summary.NumDataPoints,
	)
}

// WriteJUnitReport writes the JUnit XML report to a file
func WriteJUnitReport(filename string, suites JUnitTestSuites) error {
	log.Infof("Writing JUnit report to %s", filename)

	file, err := os.Create(filename)
	if err != nil {
		return fmt.Errorf("failed to create JUnit report file: %w", err)
	}
	defer file.Close()

	// Write XML header
	_, err = file.WriteString(xml.Header)
	if err != nil {
		return fmt.Errorf("failed to write XML header: %w", err)
	}

	// Marshal and write the XML
	encoder := xml.NewEncoder(file)
	encoder.Indent("", "  ")
	err = encoder.Encode(suites)
	if err != nil {
		return fmt.Errorf("failed to encode JUnit XML: %w", err)
	}

	return nil
}
