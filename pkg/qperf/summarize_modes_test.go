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

func externalOnlyResult(latency float64, latencyUnit string, throughput float64, throughputUnit string) *Results {
	r := &Results{}
	r.External.Latency = latency
	r.External.LatencyUnit = latencyUnit
	r.External.Throughput = throughput
	r.External.ThroughputUnit = throughputUnit
	return r
}

// An external-only run leaves the service fields empty, so a check against the wrong mode's
// unit rejects a perfectly good measurement.
func TestSummarizeResultsExternalOnlyInMicroseconds(t *testing.T) {
	summary, err := SummarizeResults([]*Results{externalOnlyResult(300, "us", 500, "Mb/sec")})
	require.NoError(t, err)

	assert.Equal(t, 1, summary.Latency.External.NumDataPoints)
	assert.Equal(t, float64(300), summary.Latency.External.Min)
	assert.Equal(t, "us", summary.Latency.External.Unit)
	assert.Equal(t, float64(500), summary.Throughput.External.Min)
}

func TestSummarizeResultsExternalOnlyInMilliseconds(t *testing.T) {
	summary, err := SummarizeResults([]*Results{externalOnlyResult(0.5, "ms", 1, "Gb/sec")})
	require.NoError(t, err)

	assert.Equal(t, float64(500), summary.Latency.External.Min)
	assert.Equal(t, float64(1000), summary.Throughput.External.Min)
}

func TestSummarizeResultsServiceOnlyInMicroseconds(t *testing.T) {
	r := &Results{}
	r.Service.Latency = 300
	r.Service.LatencyUnit = "us"
	r.Service.Throughput = 500
	r.Service.ThroughputUnit = "Mb/sec"

	summary, err := SummarizeResults([]*Results{r})
	require.NoError(t, err)
	assert.Equal(t, 1, summary.Latency.Service.NumDataPoints)
	assert.Equal(t, float64(300), summary.Latency.Service.Min)
}

func TestSummarizeResultsExternalRejectsUnknownUnit(t *testing.T) {
	_, err := SummarizeResults([]*Results{externalOnlyResult(300, "ns", 500, "Mb/sec")})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "unknown external latency unit: ns")
}
