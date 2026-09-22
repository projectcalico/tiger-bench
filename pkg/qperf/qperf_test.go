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

package qperf

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// directResult builds a result of the shape a completed pod-pod iteration produces.
func directResult(latency float64, latencyUnit string, throughput float64, throughputUnit string) *Results {
	r := &Results{}
	r.Direct.Latency = latency
	r.Direct.LatencyUnit = latencyUnit
	r.Direct.Throughput = throughput
	r.Direct.ThroughputUnit = throughputUnit
	return r
}

func TestSummarizeResultsConvertsToMicrosecondsAndMbits(t *testing.T) {
	summary, err := SummarizeResults([]*Results{
		directResult(0.5, "ms", 1, "Gb/sec"),
		directResult(300, "us", 500, "Mb/sec"),
	})
	require.NoError(t, err)

	assert.Equal(t, 2, summary.Latency.Direct.NumDataPoints)
	assert.Equal(t, "us", summary.Latency.Direct.Unit)
	assert.Equal(t, float64(300), summary.Latency.Direct.Min)
	assert.Equal(t, float64(500), summary.Latency.Direct.Max)

	assert.Equal(t, "Mb/sec", summary.Throughput.Direct.Unit)
	assert.Equal(t, float64(500), summary.Throughput.Direct.Min)
	assert.Equal(t, float64(1000), summary.Throughput.Direct.Max)
}

// Modes that were not configured stay zeroed and must not count as measurements.
func TestSummarizeResultsIgnoresModesThatDidNotRun(t *testing.T) {
	summary, err := SummarizeResults([]*Results{directResult(300, "us", 500, "Mb/sec")})
	require.NoError(t, err)

	assert.Equal(t, 1, summary.Latency.Direct.NumDataPoints)
	assert.Equal(t, 0, summary.Latency.Service.NumDataPoints)
	assert.Equal(t, 0, summary.Latency.External.NumDataPoints)
}

// The shape a failed iteration leaves behind: a throughput recorded, but no units. Feeding
// one of these in aborts the whole summary, which is why they are no longer collected.
func TestSummarizeResultsRejectsPartiallyFilledResult(t *testing.T) {
	partial := &Results{}
	partial.Direct.Throughput = 500

	_, err := SummarizeResults([]*Results{
		directResult(300, "us", 500, "Mb/sec"),
		partial,
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "unknown direct latency unit")
}

func TestSummarizeResultsRejectsUnknownUnits(t *testing.T) {
	_, err := SummarizeResults([]*Results{directResult(300, "ns", 500, "Mb/sec")})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "unknown direct latency unit: ns")

	_, err = SummarizeResults([]*Results{directResult(300, "us", 500, "Kb/sec")})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "unknown direct throughput unit: Kb/sec")
}

func TestSummarizeResultsWithNoResults(t *testing.T) {
	summary, err := SummarizeResults(nil)
	require.NoError(t, err)
	assert.Equal(t, 0, summary.Latency.Direct.NumDataPoints)
}
