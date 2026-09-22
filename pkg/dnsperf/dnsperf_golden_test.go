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

package dnsperf

import (
	"testing"

	"github.com/projectcalico/tiger-bench/pkg/utils"
	"github.com/stretchr/testify/assert"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// The want* functions below are the pre-refactor builders, copied verbatim. Comparing
// against them is what makes the equality check meaningful: an expectation written from the
// new builder's own output would prove nothing.

func wantDNSPerfPod(nodename string, namespace string, podname string, image string, hostnetwork bool) corev1.Pod {
	podname = utils.SanitizeString(podname)
	runAsUser := int64(1000)
	runAsGroup := int64(1000)
	if hostnetwork {
		// tcpdump needs to run as root
		runAsUser = 0
		runAsGroup = 0
	}
	pod := corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Labels: map[string]string{
				"app": "dnsperf",
				"pod": podname,
				"dep": "dnsperf",
			},
			Name:      podname,
			Namespace: namespace,
		},
		Spec: corev1.PodSpec{
			AutomountServiceAccountToken: utils.BoolPtr(false),
			EnableServiceLinks:           utils.BoolPtr(false),
			SecurityContext: &corev1.PodSecurityContext{
				RunAsNonRoot: utils.BoolPtr(!hostnetwork), // tcpdump needs to run as root
				RunAsGroup:   utils.Int64Ptr(runAsGroup),
				RunAsUser:    utils.Int64Ptr(runAsUser),
				SeccompProfile: &corev1.SeccompProfile{
					Type: corev1.SeccompProfileTypeRuntimeDefault,
				},
			},
			Containers: []corev1.Container{
				{
					Name:    "dnsperf",
					Image:   image,
					Command: []string{"/bin/sh", "-c"},
					Args: []string{
						"while true; do echo `date`: MARK; sleep 10; done",
					},
					SecurityContext: &corev1.SecurityContext{
						Privileged:               utils.BoolPtr(false),
						AllowPrivilegeEscalation: utils.BoolPtr(false),
						ReadOnlyRootFilesystem:   utils.BoolPtr(false),
						Capabilities: &corev1.Capabilities{
							Drop: []corev1.Capability{"ALL"},
							Add: func() []corev1.Capability {
								if hostnetwork {
									return []corev1.Capability{"NET_RAW", "NET_ADMIN"}
								}
								return nil
							}(),
						},
					},
					ImagePullPolicy: corev1.PullIfNotPresent,
				},
			},
			NodeName:      nodename,
			RestartPolicy: "Never",
			HostNetwork:   hostnetwork,
		},
	}
	return pod
}

func TestMakeDNSPerfPodMatchesPreRefactorSpec(t *testing.T) {
	got := makeDNSPerfPod("node-1", "testns", "dnsperf-node-1", "img:latest", false)
	want := wantDNSPerfPod("node-1", "testns", "dnsperf-node-1", "img:latest", false)

	// No intended changes: dnsperf was already fully hardened.
	assert.Equal(t, want, got)
}

func TestMakeDNSPerfPodHostNetworkMatchesPreRefactorSpec(t *testing.T) {
	got := makeDNSPerfPod("node-1", "testns", "dnsperf-node-1", "img:latest", true)
	want := wantDNSPerfPod("node-1", "testns", "dnsperf-node-1", "img:latest", true)

	assert.Equal(t, want, got)
	// tcpdump needs these; guard them explicitly as well as via the whole-spec compare.
	assert.Equal(t, []corev1.Capability{"NET_RAW", "NET_ADMIN"}, got.Spec.Containers[0].SecurityContext.Capabilities.Add)
	assert.False(t, *got.Spec.SecurityContext.RunAsNonRoot)
}
