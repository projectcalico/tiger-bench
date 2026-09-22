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

package utils

import (
	"context"
	"testing"
	"time"

	"github.com/projectcalico/tiger-bench/pkg/config"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/kubernetes/fake"
	ctrlclient "sigs.k8s.io/controller-runtime/pkg/client"
	ctrlfake "sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/client/interceptor"
)

// Keep the retry loops from costing real seconds.
func fastPolling(t *testing.T) {
	t.Helper()
	oldPod, oldNS := testPodPollInterval, namespaceDeletePollInterval
	testPodPollInterval = time.Millisecond
	namespaceDeletePollInterval = time.Millisecond
	t.Cleanup(func() {
		testPodPollInterval, namespaceDeletePollInterval = oldPod, oldNS
	})
}

func testPod(name string, phase corev1.PodPhase) *corev1.Pod {
	return &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: "testns",
			Labels:    map[string]string{"app": "qperf"},
		},
		Status: corev1.PodStatus{Phase: phase},
	}
}

func TestWaitForTestPodsReturnsRunningPods(t *testing.T) {
	fastPolling(t)
	clients := config.Clients{
		Clientset: fake.NewClientset(
			testPod("qperf-a", corev1.PodRunning),
			testPod("qperf-b", corev1.PodRunning),
		),
	}

	pods, err := WaitForTestPods(context.Background(), clients, "testns", "app=qperf")
	require.NoError(t, err)
	assert.Len(t, pods, 2)
}

// The old implementation slept before its first check, costing 10s even when the pods were up.
func TestWaitForTestPodsDoesNotSleepBeforeFirstCheck(t *testing.T) {
	testPodPollInterval = 30 * time.Second
	t.Cleanup(func() { testPodPollInterval = 10 * time.Second })
	clients := config.Clients{
		Clientset: fake.NewClientset(testPod("qperf-a", corev1.PodRunning)),
	}

	start := time.Now()
	_, err := WaitForTestPods(context.Background(), clients, "testns", "app=qperf")
	require.NoError(t, err)
	assert.Less(t, time.Since(start), 5*time.Second)
}

func TestWaitForTestPodsErrorsWhenPodsNeverStart(t *testing.T) {
	fastPolling(t)
	clients := config.Clients{
		Clientset: fake.NewClientset(
			testPod("qperf-a", corev1.PodRunning),
			testPod("qperf-b", corev1.PodPending),
		),
	}

	pods, err := WaitForTestPods(context.Background(), clients, "testns", "app=qperf")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "qperf-b is Pending, not Running")
	// Returned anyway so callers can log them, but the error is what they must act on.
	assert.Len(t, pods, 2)
}

func TestWaitForTestPodsErrorsWhenNoPodsMatch(t *testing.T) {
	fastPolling(t)
	clients := config.Clients{Clientset: fake.NewClientset()}

	_, err := WaitForTestPods(context.Background(), clients, "testns", "app=qperf")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "no pods match")
}

func TestWaitForTestPodsHonoursContextCancellation(t *testing.T) {
	testPodPollInterval = time.Hour
	t.Cleanup(func() { testPodPollInterval = 10 * time.Second })
	clients := config.Clients{
		Clientset: fake.NewClientset(testPod("qperf-a", corev1.PodPending)),
	}

	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()

	start := time.Now()
	_, err := WaitForTestPods(ctx, clients, "testns", "app=qperf")
	require.ErrorIs(t, err, context.DeadlineExceeded)
	assert.Less(t, time.Since(start), 5*time.Second)
}

func nsScheme(t *testing.T) *runtime.Scheme {
	t.Helper()
	s := runtime.NewScheme()
	require.NoError(t, corev1.AddToScheme(s))
	return s
}

func TestDeleteNamespaceSucceedsWhenNamespaceGoesAway(t *testing.T) {
	fastPolling(t)
	ns := &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: "testns"}}
	clients := config.Clients{
		CtrlClient: ctrlfake.NewClientBuilder().WithScheme(nsScheme(t)).WithObjects(ns).Build(),
	}

	require.NoError(t, DeleteNamespace(context.Background(), clients, "testns"))
}

func TestDeleteNamespaceSucceedsWhenAlreadyGone(t *testing.T) {
	fastPolling(t)
	clients := config.Clients{
		CtrlClient: ctrlfake.NewClientBuilder().WithScheme(nsScheme(t)).Build(),
	}

	require.NoError(t, DeleteNamespace(context.Background(), clients, "testns"))
}

// A namespace stuck Terminating used to be logged and then reported as success.
func TestDeleteNamespaceErrorsWhenNamespaceLingers(t *testing.T) {
	fastPolling(t)
	ns := &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: "testns"}}
	// Always report the namespace as present, as a stuck finalizer would.
	stuck := interceptor.Funcs{
		Get: func(_ context.Context, _ ctrlclient.WithWatch, key ctrlclient.ObjectKey, obj ctrlclient.Object, _ ...ctrlclient.GetOption) error {
			if found, ok := obj.(*corev1.Namespace); ok {
				found.ObjectMeta = metav1.ObjectMeta{Name: key.Name}
			}
			return nil
		},
	}
	clients := config.Clients{
		CtrlClient: ctrlfake.NewClientBuilder().WithScheme(nsScheme(t)).WithObjects(ns).WithInterceptorFuncs(stuck).Build(),
	}

	err := DeleteNamespace(context.Background(), clients, "testns")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "still exists")
}
