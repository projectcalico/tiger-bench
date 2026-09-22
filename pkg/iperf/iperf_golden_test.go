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

	"github.com/projectcalico/tiger-bench/pkg/utils"
	"github.com/stretchr/testify/assert"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// The want* functions below are the pre-refactor builders, copied verbatim. Comparing
// against them is what makes the equality check meaningful: an expectation written from the
// new builder's own output would prove nothing.

func wantIperfPod(nodename string, namespace string, podname string, hostnetwork bool, image string, command string, port int) corev1.Pod {
	podname = utils.SanitizeString(podname)
	pod := corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Labels: map[string]string{
				"app": "iperf",
				"pod": podname,
			},
			Name:      podname,
			Namespace: namespace,
		},
		Spec: corev1.PodSpec{
			AutomountServiceAccountToken: utils.BoolPtr(false),
			SecurityContext: &corev1.PodSecurityContext{
				RunAsNonRoot: utils.BoolPtr(true),
				RunAsGroup:   utils.Int64Ptr(1000),
				RunAsUser:    utils.Int64Ptr(1000),
				SeccompProfile: &corev1.SeccompProfile{
					Type: corev1.SeccompProfileTypeRuntimeDefault,
				},
			},
			EnableServiceLinks: utils.BoolPtr(false),
			Containers: []corev1.Container{
				{
					Name:    "iperf",
					Image:   image,
					Command: []string{"/bin/sh", "-c"},
					Args: []string{
						command,
					},
					SecurityContext: &corev1.SecurityContext{
						Privileged:               utils.BoolPtr(false),
						AllowPrivilegeEscalation: utils.BoolPtr(false),
						ReadOnlyRootFilesystem:   utils.BoolPtr(false),
						Capabilities: &corev1.Capabilities{
							Drop: []corev1.Capability{"ALL"},
						},
					},
					ImagePullPolicy: corev1.PullIfNotPresent,
					Ports: []corev1.ContainerPort{
						{
							Name:          "test-port",
							ContainerPort: int32(port),
							Protocol:      corev1.ProtocolTCP,
						},
					},
				},
			},
			NodeName:      nodename,
			RestartPolicy: corev1.RestartPolicyOnFailure,
			HostNetwork:   hostnetwork,
		},
	}
	return pod
}

func wantIperfSvc(namespace string, podname string, port int) corev1.Service {
	podname = utils.SanitizeString(podname)
	svc := corev1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Labels: map[string]string{
				"app": "iperf",
				"pod": podname,
			},
			Name:      podname,
			Namespace: namespace,
		},
		Spec: corev1.ServiceSpec{
			Selector: map[string]string{
				"app": "iperf",
				"pod": podname,
			},
			Ports: []corev1.ServicePort{
				{
					Name: "test-port",
					Port: int32(port),
				},
			},
		},
	}
	return svc
}

func TestMakeIperfPodMatchesPreRefactorSpec(t *testing.T) {
	got := makePod("node-1", "testns", "iperf-node-1", false, "img:latest", "iperf3 -s", 32001)
	want := wantIperfPod("node-1", "testns", "iperf-node-1", false, "img:latest", "iperf3 -s", 32001)

	// No intended changes: iperf was already fully hardened.
	assert.Equal(t, want, got)
}

func TestMakeIperfSvcMatchesPreRefactorSpec(t *testing.T) {
	got := makeSvc("testns", "iperf-srv-node-1", 32001)
	want := wantIperfSvc("testns", "iperf-srv-node-1", 32001)

	assert.Equal(t, want, got)
	assert.Equal(t, map[string]string{"app": "iperf", "pod": "iperf-srv-node-1"}, got.Spec.Selector)
}
