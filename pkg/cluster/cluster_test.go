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

package cluster

import (
	"context"
	"testing"
	"time"

	"github.com/projectcalico/tiger-bench/pkg/config"
	"github.com/sethvargo/go-retry"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	v3 "github.com/tigera/api/pkg/apis/projectcalico/v3"
	operatorv1 "github.com/tigera/operator/api/v1"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	ctrlclient "sigs.k8s.io/controller-runtime/pkg/client"
	ctrlfake "sigs.k8s.io/controller-runtime/pkg/client/fake"
)

func dataplane(d operatorv1.LinuxDataplaneOption) *operatorv1.LinuxDataplaneOption { return &d }

func TestComputedDataplane(t *testing.T) {
	for _, tc := range []struct {
		name string
		in   operatorv1.LinuxDataplaneOption
		want string
	}{
		{"bpf", operatorv1.LinuxDataplaneBPF, "bpf"},
		{"iptables", operatorv1.LinuxDataplaneIptables, "iptables"},
		{"vpp", operatorv1.LinuxDataplaneVPP, "vpp"},
		{"nftables", operatorv1.LinuxDataplaneNftables, "nftables"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			inst := &operatorv1.Installation{}
			inst.Status.Computed = &operatorv1.InstallationSpec{
				CalicoNetwork: &operatorv1.CalicoNetworkSpec{LinuxDataplane: dataplane(tc.in)},
			}
			assert.Equal(t, tc.want, computedDataplane(inst))
		})
	}
}

// Each of these levels is filled in asynchronously by the operator, and dereferencing any of
// them before it appears used to crash the whole run.
func TestComputedDataplaneWhenStatusNotPopulated(t *testing.T) {
	bare := &operatorv1.Installation{}
	assert.Equal(t, "", computedDataplane(bare))

	noNetwork := &operatorv1.Installation{}
	noNetwork.Status.Computed = &operatorv1.InstallationSpec{}
	assert.Equal(t, "", computedDataplane(noNetwork))

	noDataplane := &operatorv1.Installation{}
	noDataplane.Status.Computed = &operatorv1.InstallationSpec{CalicoNetwork: &operatorv1.CalicoNetworkSpec{}}
	assert.Equal(t, "", computedDataplane(noDataplane))
}

func TestComputedDataplaneWithUnrecognisedValue(t *testing.T) {
	inst := &operatorv1.Installation{}
	inst.Status.Computed = &operatorv1.InstallationSpec{
		CalicoNetwork: &operatorv1.CalicoNetworkSpec{LinuxDataplane: dataplane("Something")},
	}
	assert.Equal(t, "unknown", computedDataplane(inst))
}

func clusterScheme(t *testing.T) *runtime.Scheme {
	t.Helper()
	s := runtime.NewScheme()
	require.NoError(t, corev1.AddToScheme(s))
	require.NoError(t, appsv1.AddToScheme(s))
	require.NoError(t, operatorv1.AddToScheme(s))
	require.NoError(t, v3.AddToScheme(s))
	return s
}

// Reproduces the crash seen in CI: an Installation the operator has not finished filling in.
func TestGetClusterDetailsWithUnpopulatedInstallation(t *testing.T) {
	installationStatusBackoff = func() retry.Backoff {
		return retry.WithMaxRetries(1, retry.NewConstant(time.Millisecond))
	}
	t.Cleanup(func() {
		installationStatusBackoff = func() retry.Backoff {
			return retry.WithMaxRetries(6, retry.NewFibonacci(500*time.Millisecond))
		}
	})

	objs := []ctrlclient.Object{
		&v3.ClusterInformation{ObjectMeta: metav1.ObjectMeta{Name: "default"}},
		&v3.FelixConfiguration{ObjectMeta: metav1.ObjectMeta{Name: "default"}},
		&operatorv1.Installation{ObjectMeta: metav1.ObjectMeta{Name: "default"}},
		&appsv1.DaemonSet{ObjectMeta: metav1.ObjectMeta{Name: "calico-node", Namespace: "calico-system"}},
		&corev1.Node{
			ObjectMeta: metav1.ObjectMeta{Name: "node-1", Labels: map[string]string{"tigera.io/test-nodepool": "default-pool"}},
			Status:     corev1.NodeStatus{NodeInfo: corev1.NodeSystemInfo{OperatingSystem: "linux", Architecture: "amd64"}},
		},
	}
	clients := config.Clients{
		CtrlClient: ctrlfake.NewClientBuilder().WithScheme(clusterScheme(t)).WithObjects(objs...).Build(),
	}

	details, err := GetClusterDetails(context.Background(), clients)
	require.NoError(t, err)
	assert.Equal(t, "unknown", details.Dataplane)
	assert.Equal(t, "unknown", details.Encapsulation)
	assert.Equal(t, "unknown", details.CNIOption)
}
