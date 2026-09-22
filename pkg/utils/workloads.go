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
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// Port defaults, container hardening and restart policy are shared by every test pod, so
// they live here rather than being restated (and drifting) in each package.

// PodOptions describes a single-container test pod. The zero value of each opt-out keeps
// the hardened default; set one only where a workload genuinely cannot run under it.
type PodOptions struct {
	Name        string
	Namespace   string
	NodeName    string
	Image       string
	App         string // "app" label and container name
	HostNetwork bool
	ExtraLabels map[string]string

	Command []string
	Args    []string
	Env     []corev1.EnvVar
	Ports   []corev1.ContainerPort

	WritableRootFilesystem bool
	AddCapabilities        []corev1.Capability
	RunAsRoot              bool
	RestartPolicy          corev1.RestartPolicy // "" means OnFailure
	ImagePullPolicy        corev1.PullPolicy    // "" means IfNotPresent
}

// MakePod builds a hardened single-container test pod.
func MakePod(opts PodOptions) corev1.Pod {
	podname := SanitizeString(opts.Name)

	labels := map[string]string{}
	for k, v := range opts.ExtraLabels {
		labels[k] = v
	}
	// Builder-owned keys win: callers select on these.
	labels["app"] = opts.App
	labels["pod"] = podname

	runAsNonRoot, uid, gid := true, int64(1000), int64(1000)
	if opts.RunAsRoot {
		runAsNonRoot, uid, gid = false, 0, 0
	}

	restartPolicy := opts.RestartPolicy
	if restartPolicy == "" {
		restartPolicy = corev1.RestartPolicyOnFailure
	}
	pullPolicy := opts.ImagePullPolicy
	if pullPolicy == "" {
		pullPolicy = corev1.PullIfNotPresent
	}

	return corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Labels:    labels,
			Name:      podname,
			Namespace: opts.Namespace,
		},
		Spec: corev1.PodSpec{
			AutomountServiceAccountToken: BoolPtr(false),
			EnableServiceLinks:           BoolPtr(false),
			SecurityContext: &corev1.PodSecurityContext{
				RunAsNonRoot: BoolPtr(runAsNonRoot),
				RunAsGroup:   Int64Ptr(gid),
				RunAsUser:    Int64Ptr(uid),
				SeccompProfile: &corev1.SeccompProfile{
					Type: corev1.SeccompProfileTypeRuntimeDefault,
				},
			},
			Containers: []corev1.Container{
				{
					Name:    opts.App,
					Image:   opts.Image,
					Command: append([]string(nil), opts.Command...),
					Args:    append([]string(nil), opts.Args...),
					Env:     append([]corev1.EnvVar(nil), opts.Env...),
					SecurityContext: &corev1.SecurityContext{
						Privileged:               BoolPtr(false),
						AllowPrivilegeEscalation: BoolPtr(false),
						ReadOnlyRootFilesystem:   BoolPtr(!opts.WritableRootFilesystem),
						Capabilities: &corev1.Capabilities{
							Drop: []corev1.Capability{"ALL"},
							Add:  append([]corev1.Capability(nil), opts.AddCapabilities...),
						},
					},
					ImagePullPolicy: pullPolicy,
					Ports:           append([]corev1.ContainerPort(nil), opts.Ports...),
				},
			},
			NodeName:      opts.NodeName,
			RestartPolicy: restartPolicy,
			HostNetwork:   opts.HostNetwork,
		},
	}
}

// MakeSvc builds the ClusterIP service fronting a single test pod. The selector matches on
// both keys so the service reaches only the pod on the target node.
func MakeSvc(namespace string, podname string, app string, ports []corev1.ServicePort) corev1.Service {
	podname = SanitizeString(podname)
	selector := map[string]string{
		"app": app,
		"pod": podname,
	}
	return corev1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Labels:    map[string]string{"app": app, "pod": podname},
			Name:      podname,
			Namespace: namespace,
		},
		Spec: corev1.ServiceSpec{
			Selector: selector,
			Ports:    append([]corev1.ServicePort(nil), ports...),
		},
	}
}
