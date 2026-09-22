// Copyright (c) 2025 Tigera, Inc. All rights reserved.

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

package iperf

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Only sum_sent.retransmits and sum_received.bits_per_second are read.
const iperfStdout = `{
  "end": {
    "sum_sent":     { "bytes": 1234, "bits_per_second": 9600000000, "retransmits": 42 },
    "sum_received": { "bytes": 1234, "bits_per_second": 9500000000 }
  }
}`

func TestParseIperfOutput(t *testing.T) {
	retransmits, throughput, unit, err := parseIperfOutput(iperfStdout)
	require.NoError(t, err)

	assert.Equal(t, 42, retransmits)
	assert.Equal(t, float64(9500), throughput) // bits/sec reported as Mbits/sec
	assert.Equal(t, "Mbits/sec", unit)
}

func TestParseIperfOutputOnInvalidJSON(t *testing.T) {
	_, _, _, err := parseIperfOutput("iperf3: error - unable to connect")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed to unmarshal iperf data")
}

// Valid JSON that is not an iperf report unmarshals cleanly and yields a silent zero,
// which reads downstream as a real measurement of no throughput.
func TestParseIperfOutputOnUnrelatedJSON(t *testing.T) {
	retransmits, throughput, unit, err := parseIperfOutput(`{"error": "unable to connect"}`)
	require.NoError(t, err)

	assert.Equal(t, 0, retransmits)
	assert.Equal(t, float64(0), throughput)
	assert.Equal(t, "Mbits/sec", unit)
}

func directIperfResult(retries int, throughput float64, unit string) *Results {
	r := &Results{}
	r.Direct.Retries = retries
	r.Direct.Throughput = throughput
	r.Direct.ThroughputUnit = unit
	return r
}

func TestSummarizeResultsAggregatesThroughputAndRetries(t *testing.T) {
	summary, err := SummarizeResults([]*Results{
		directIperfResult(10, 9000, "Mbits/sec"),
		directIperfResult(20, 9500, "Mbits/sec"),
	})
	require.NoError(t, err)

	assert.Equal(t, 2, summary.Throughput.Direct.NumDataPoints)
	assert.Equal(t, float64(9000), summary.Throughput.Direct.Min)
	assert.Equal(t, float64(9500), summary.Throughput.Direct.Max)
	assert.Equal(t, "Mb/sec", summary.Throughput.Direct.Unit)

	assert.Equal(t, float64(10), summary.Retries.Direct.Min)
	assert.Equal(t, float64(20), summary.Retries.Direct.Max)
}

func TestSummarizeResultsIgnoresModesThatDidNotRun(t *testing.T) {
	summary, err := SummarizeResults([]*Results{directIperfResult(0, 9000, "Mbits/sec")})
	require.NoError(t, err)

	assert.Equal(t, 1, summary.Throughput.Direct.NumDataPoints)
	assert.Equal(t, 0, summary.Throughput.Service.NumDataPoints)
	assert.Equal(t, 0, summary.Throughput.External.NumDataPoints)
}

func TestSummarizeResultsRejectsUnknownUnit(t *testing.T) {
	_, err := SummarizeResults([]*Results{directIperfResult(0, 9000, "Gbits/sec")})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "unknown direct throughput unit: Gbits/sec")
}

// The shape a failed iteration leaves behind: retries recorded, but no unit.
func TestSummarizeResultsRejectsPartiallyFilledResult(t *testing.T) {
	_, err := SummarizeResults([]*Results{directIperfResult(5, 0, "")})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "unknown direct throughput unit")
}
