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
	"strconv"
	"testing"

	"github.com/projectcalico/tiger-bench/pkg/utils"
	"github.com/stretchr/testify/assert"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// The want* functions below are the pre-refactor builders, copied verbatim. Comparing
// against them is what makes the equality check meaningful: an expectation written from the
// new builder's own output would prove nothing.

func wantQperfPod(nodename string, namespace string, podname string, image string, hostnetwork bool, controlPort int, testPort int) corev1.Pod {
	podname = utils.SanitizeString(podname)
	controlPortStr := strconv.Itoa(controlPort)

	pod := corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Labels: map[string]string{
				"app": "qperf",
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
					Name:    "qperf",
					Image:   image,
					Command: []string{"/usr/bin/qperf"},
					Args: []string{
						"-lp",
						controlPortStr,
					},
					SecurityContext: &corev1.SecurityContext{
						Privileged:               utils.BoolPtr(false),
						AllowPrivilegeEscalation: utils.BoolPtr(false),
						ReadOnlyRootFilesystem:   utils.BoolPtr(true),
						Capabilities: &corev1.Capabilities{
							Drop: []corev1.Capability{"ALL"},
						},
					},
					Ports: []corev1.ContainerPort{
						{
							Name:          "control",
							ContainerPort: int32(controlPort),
							Protocol:      corev1.ProtocolTCP,
						},
						{
							Name:          "data",
							ContainerPort: int32(testPort),
							Protocol:      corev1.ProtocolTCP,
						},
					},
				},
			},
			NodeName:      nodename,
			RestartPolicy: "OnFailure",
			HostNetwork:   hostnetwork,
		},
	}
	return pod
}

func wantQperfSvc(namespace string, podname string, controlPort int, testPort int) corev1.Service {
	podname = utils.SanitizeString(podname)
	svc := corev1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Labels: map[string]string{
				"app": "qperf",
				"pod": podname,
			},
			Name:      podname,
			Namespace: namespace,
		},
		Spec: corev1.ServiceSpec{
			Selector: map[string]string{
				"app": "qperf",
				"pod": podname,
			},
			Ports: []corev1.ServicePort{
				{
					Name: "control",
					Port: int32(controlPort),
				},
				{
					Name: "data",
					Port: int32(testPort),
				},
			},
		},
	}
	return svc
}

func TestMakeQperfPodMatchesPreRefactorSpec(t *testing.T) {
	got := makeQperfPod("node-1", "testns", "qperf-node-1", "img:latest", false, 32000, 32001)

	want := wantQperfPod("node-1", "testns", "qperf-node-1", "img:latest", false, 32000, 32001)
	// Intended: qperf set no pull policy, so kubelet defaulted to Always for :latest tags.
	want.Spec.Containers[0].ImagePullPolicy = corev1.PullIfNotPresent

	assert.Equal(t, want, got)
}

func TestMakeQperfSvcMatchesPreRefactorSpec(t *testing.T) {
	got := makeSvc("testns", "qperf-srv-node-1", 32000, 32001)
	want := wantQperfSvc("testns", "qperf-srv-node-1", 32000, 32001)

	assert.Equal(t, want, got)
	// The selector must match both keys, or the service spans every pod of the app.
	assert.Equal(t, map[string]string{"app": "qperf", "pod": "qperf-srv-node-1"}, got.Spec.Selector)
}
