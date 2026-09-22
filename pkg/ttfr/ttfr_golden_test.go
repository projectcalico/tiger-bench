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

package ttfr

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

func wantTTFRPod(nodename string, namespace string, podname string, hostnetwork bool, image string) corev1.Pod {
	podname = utils.SanitizeString(podname)
	pod := corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Labels: map[string]string{
				"app":  "ttfr",
				"pod":  podname,
				"node": nodename,
			},
			Name:      podname,
			Namespace: namespace,
		},
		Spec: corev1.PodSpec{
			AutomountServiceAccountToken: utils.BoolPtr(false),
			EnableServiceLinks:           utils.BoolPtr(false),
			SecurityContext: &corev1.PodSecurityContext{
				RunAsNonRoot: utils.BoolPtr(true),
				RunAsGroup:   utils.Int64Ptr(1000),
				RunAsUser:    utils.Int64Ptr(1000),
				SeccompProfile: &corev1.SeccompProfile{
					Type: corev1.SeccompProfileTypeRuntimeDefault,
				},
			},
			Containers: []corev1.Container{
				{
					Name:  "ttfr",
					Image: image,
					SecurityContext: &corev1.SecurityContext{
						Privileged:               utils.BoolPtr(false),
						AllowPrivilegeEscalation: utils.BoolPtr(false),
						ReadOnlyRootFilesystem:   utils.BoolPtr(false),
					},
					Ports: []corev1.ContainerPort{
						{
							Name:          "http",
							ContainerPort: 8080,
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

func wantTTFRTestPod(nodename string, namespace string, podname string, hostnetwork bool, image string, target string) corev1.Pod {
	podname = utils.SanitizeString(podname)
	pod := corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Labels: map[string]string{
				"app":  "ttfr",
				"pod":  podname,
				"node": nodename,
			},
			Name:      podname,
			Namespace: namespace,
		},
		Spec: corev1.PodSpec{
			SecurityContext: &corev1.PodSecurityContext{
				RunAsNonRoot: utils.BoolPtr(true),
				RunAsGroup:   utils.Int64Ptr(1000),
				RunAsUser:    utils.Int64Ptr(1000),
			},
			AutomountServiceAccountToken: utils.BoolPtr(false),
			EnableServiceLinks:           utils.BoolPtr(false),
			Containers: []corev1.Container{
				{
					Name:  "ttfr",
					Image: image,
					Env: []corev1.EnvVar{
						{
							Name:  "ADDRESS",
							Value: target,
						},
						{
							Name:  "PORT",
							Value: "8080",
						},
						{
							Name:  "PROTOCOL",
							Value: "http",
						},
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
							Name:          "http",
							ContainerPort: 8080,
							Protocol:      corev1.ProtocolTCP,
						},
					},
				},
			},
			NodeName:      nodename,
			RestartPolicy: "Always",
			HostNetwork:   hostnetwork,
		},
	}
	return pod
}

func TestMakeTTFRPodMatchesPreRefactorSpec(t *testing.T) {
	got := makePod("node-1", "testns", "ttfr-node-1", false, "img:latest")

	want := wantTTFRPod("node-1", "testns", "ttfr-node-1", false, "img:latest")
	// Intended: the ttfr server pod was the only one not dropping all capabilities.
	want.Spec.Containers[0].SecurityContext.Capabilities = &corev1.Capabilities{
		Drop: []corev1.Capability{"ALL"},
	}
	want.Spec.Containers[0].ImagePullPolicy = corev1.PullIfNotPresent

	assert.Equal(t, want, got)
}

func TestMakeTTFRTestPodMatchesPreRefactorSpec(t *testing.T) {
	got := makeTestPod("node-1", "testns", "ttfr-test-node-1", false, "img:latest", "10.0.0.1")

	want := wantTTFRTestPod("node-1", "testns", "ttfr-test-node-1", false, "img:latest", "10.0.0.1")
	// Intended: this was the only pod without a seccomp profile.
	want.Spec.SecurityContext.SeccompProfile = &corev1.SeccompProfile{
		Type: corev1.SeccompProfileTypeRuntimeDefault,
	}
	want.Spec.Containers[0].ImagePullPolicy = corev1.PullIfNotPresent

	assert.Equal(t, want, got)
}
