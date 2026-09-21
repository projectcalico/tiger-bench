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

package main

import (
	"context"
	"testing"
	"time"

	"github.com/projectcalico/tiger-bench/pkg/config"
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

// fakeClients seeds everything GetClusterDetails reads, with a fully populated Installation
// so it returns without waiting on the operator.
func fakeClients(t *testing.T) config.Clients {
	t.Helper()
	scheme := runtime.NewScheme()
	require.NoError(t, corev1.AddToScheme(scheme))
	require.NoError(t, appsv1.AddToScheme(scheme))
	require.NoError(t, operatorv1.AddToScheme(scheme))
	require.NoError(t, v3.AddToScheme(scheme))

	dataplane := operatorv1.LinuxDataplaneIptables
	installation := &operatorv1.Installation{ObjectMeta: metav1.ObjectMeta{Name: "default"}}
	installation.Status.Computed = &operatorv1.InstallationSpec{
		CalicoNetwork: &operatorv1.CalicoNetworkSpec{LinuxDataplane: &dataplane},
	}
	// Spec must be populated too: GetClusterDetails walks Spec.CalicoNetwork and Spec.CNI
	// unguarded on this branch (see #48).
	installation.Spec.CalicoNetwork = &operatorv1.CalicoNetworkSpec{
		IPPools: []operatorv1.IPPool{{CIDR: "192.168.0.0/16", Encapsulation: operatorv1.EncapsulationVXLAN}},
	}
	installation.Spec.CNI = &operatorv1.CNISpec{Type: operatorv1.PluginCalico}

	objs := []ctrlclient.Object{
		&v3.ClusterInformation{ObjectMeta: metav1.ObjectMeta{Name: "default"}},
		&v3.FelixConfiguration{ObjectMeta: metav1.ObjectMeta{Name: "default"}},
		installation,
		&appsv1.DaemonSet{ObjectMeta: metav1.ObjectMeta{Name: "calico-node", Namespace: "calico-system"}},
		&corev1.Node{
			ObjectMeta: metav1.ObjectMeta{Name: "node-1", Labels: map[string]string{"tigera.io/test-nodepool": "default-pool"}},
			Status:     corev1.NodeStatus{NodeInfo: corev1.NodeSystemInfo{OperatingSystem: "linux", Architecture: "amd64"}},
		},
	}

	// Clientset is left unset: nothing on the paths under test reads it. Faking it needs
	// the kubernetes.Interface change from #45.
	return config.Clients{
		CtrlClient: ctrlfake.NewClientBuilder().WithScheme(scheme).WithObjects(objs...).Build(),
	}
}

// A test that cannot be set up must come back with a non-nil error and a result the caller
// can record but must not publish.
func TestRunOneTestReportsSetupFailure(t *testing.T) {
	testConfig := &config.TestConfig{
		TestKind:      config.TestKindNone,
		Encap:         "not-a-real-encap",
		TestNamespace: "testns",
		// Keep teardown from touching anything; cleanup itself is not under test here.
		LeaveStandingConfig: true,
	}

	done := make(chan struct{})
	go func() {
		defer close(done)
		res, err := runOneTest(context.Background(), config.Config{}, fakeClients(t), testConfig)

		require.Error(t, err)
		assert.Equal(t, "failed", res.Status)
		assert.Contains(t, res.Error, "failed to configure cluster")
		// The config is recorded even on the failure path, so the result is still reportable.
		assert.Equal(t, config.TestKindNone, res.Config.TestKind)
	}()

	select {
	case <-done:
	case <-time.After(30 * time.Second):
		t.Fatal("runOneTest did not return; a retry loop is probably unbounded against the fake client")
	}
}
