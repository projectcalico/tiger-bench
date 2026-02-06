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

package results

import (
	"github.com/projectcalico/tiger-bench/pkg/cluster"
	"github.com/projectcalico/tiger-bench/pkg/config"
	"github.com/projectcalico/tiger-bench/pkg/dnsperf"
	"github.com/projectcalico/tiger-bench/pkg/iperf"
	"github.com/projectcalico/tiger-bench/pkg/qperf"
	"github.com/projectcalico/tiger-bench/pkg/ttfr"
	// "github.com/projectcalico/tiger-bench/pkg/stats"
)

// Result represents the benchmark result.
type Result struct {
	Config         config.TestConfig `json:"config"`
	ClusterDetails cluster.Details   `json:"ClusterDetails"`
	// CalicoNodeCPU    stats.MinMaxAvg      `json:"CalicoNodeCPU,omitempty"`
	// CalicoNodeMemory stats.MinMaxAvg      `json:"CalicoNodeMemory,omitempty"`
	TTFR    []*ttfr.ResultSummary `json:"ttfr,omitempty"`
	IPerf   *iperf.ResultSummary  `json:"iperf,omitempty"`
	QPerf   *qperf.ResultSummary  `json:"thruput-latency,omitempty"`
	DNSPerf *dnsperf.Results      `json:"dnsperf,omitempty"`
	Error   string                `json:"error,omitempty"`
	Status  string                `json:"status,omitempty"`
}
