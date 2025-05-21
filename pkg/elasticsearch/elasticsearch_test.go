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
	"testing"

	"github.com/projectcalico/tiger-bench/pkg/config"
	"github.com/projectcalico/tiger-bench/pkg/dnsperf"
	"github.com/projectcalico/tiger-bench/pkg/results"
	"github.com/stretchr/testify/require"
)

func TestCreateESDoc(t *testing.T) {
	dnsperf := dnsperf.Results{
		LookupTime: map[int]float64{
			50: 0.003640,
			75: 0.004745,
			90: 0.006914,
			95: 0.008833,
			99: 0.013641,
		},
		ConnectTime: map[int]float64{
			50: 0.000439,
			75: 0.000542,
			90: 0.000880,
			95: 0.001406,
			99: 0.003917,
		},
		DuplicateSYN:    101,
		DuplicateSYNACK: 0,
		FailedCurls:     16,
		SuccessfulCurls: 1326,
	}
	result := results.Result{
		Config: config.TestConfig{
			TestKind:           "dnsperf",
			Encap:              "none",
			Dataplane:          "bpf",
			NumPolicies:        30,
			NumServices:        20,
			NumPods:            10,
			CalicoNodeCPULimit: "40m",
			DNSPerf: &config.DNSConfig{
			  NumDomains:  0,
			  Mode:        "Inline",
			},
		},
		DNSPerf: &dnsperf,
	}

	expectedDoc := `{"config":{"TestKind":"dnsperf","Encap":"none","Dataplane":"bpf","NumPolicies":30,"NumServices":20,"NumPods":10,"HostNetwork":false,"TestNamespace":"","Iterations":0,"Duration":0,"DNSPerf":{"NumDomains":0,"Mode":"Inline"},"Perf":null,"TTFRConfig":null,"CalicoNodeCPULimit":"40m","LeaveStandingConfig":false},"ClusterDetails":{"Cloud":"","Provisioner":"","NodeType":"","NodeOS":"","NodeKernel":"","NodeArch":"","NumNodes":0,"Dataplane":"","IPFamily":"","Encapsulation":"","WireguardEnabled":false,"Product":"","CalicoVersion":"","K8SVersion":"","CRIVersion":"","CNIOption":""},"dnsperf":{"LookupTime":{"50":0.00364,"75":0.004745,"90":0.006914,"95":0.008833,"99":0.013641},"ConnectTime":{"50":0.000439,"75":0.000542,"90":0.00088,"95":0.001406,"99":0.003917},"DuplicateSYN":101,"DuplicateSYNACK":0,"FailedCurls":16,"SuccessfulCurls":1326}}`

	doc, err := createESDoc(result)
	require.NoError(t, err)

	if doc != expectedDoc {
		t.Errorf("unexpected document. Expected: %s Got: %s", expectedDoc, doc)
	}
}

func TestCreateESDocBlank(t *testing.T) {
	dnsperf := dnsperf.Results{}
	result := results.Result{
		DNSPerf: &dnsperf,
	}

	expectedDoc := `{"config":{"TestKind":"","Encap":"","Dataplane":"","NumPolicies":0,"NumServices":0,"NumPods":0,"HostNetwork":false,"TestNamespace":"","Iterations":0,"Duration":0,"DNSPerf":null,"Perf":null,"TTFRConfig":null,"CalicoNodeCPULimit":"","LeaveStandingConfig":false},"ClusterDetails":{"Cloud":"","Provisioner":"","NodeType":"","NodeOS":"","NodeKernel":"","NodeArch":"","NumNodes":0,"Dataplane":"","IPFamily":"","Encapsulation":"","WireguardEnabled":false,"Product":"","CalicoVersion":"","K8SVersion":"","CRIVersion":"","CNIOption":""},"dnsperf":{"LookupTime":null,"ConnectTime":null,"DuplicateSYN":0,"DuplicateSYNACK":0,"FailedCurls":0,"SuccessfulCurls":0}}`
	doc, err := createESDoc(result)
	require.NoError(t, err)

	if doc != expectedDoc {
		t.Errorf("unexpected document. Expected: %s Got: %s", expectedDoc, doc)
	}
}
