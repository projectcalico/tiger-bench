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

package utils

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"time"

	"github.com/projectcalico/tiger-bench/pkg/config"
	"github.com/sethvargo/go-retry"

	log "github.com/sirupsen/logrus"
	ctrlclient "sigs.k8s.io/controller-runtime/pkg/client"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	networkingv1 "k8s.io/api/networking/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/tools/clientcmd"
	"k8s.io/client-go/tools/remotecommand"
	"k8s.io/kubernetes/pkg/client/conditions"
)

// ExecCommandInPod executes a command in a pod
func ExecCommandInPod(ctx context.Context, pod *corev1.Pod, command string, timeout int) (string, string, error) {
	log.Debugf("Entering execCommandInPod function, command=%s", command)
	kubeCfg := clientcmd.NewNonInteractiveDeferredLoadingClientConfig(
		clientcmd.NewDefaultClientConfigLoadingRules(),
		&clientcmd.ConfigOverrides{},
	)
	restCfg, err := kubeCfg.ClientConfig()
	if err != nil {
		return "", "", err
	}
	coreClient, err := kubernetes.NewForConfig(restCfg)
	if err != nil {
		return "", "", err
	}

	buf := &bytes.Buffer{}
	errBuf := &bytes.Buffer{}
	request := coreClient.CoreV1().RESTClient().
		Post().
		Namespace(pod.Namespace).
		Resource("pods").
		Name(pod.Name).
		SubResource("exec").
		VersionedParams(&corev1.PodExecOptions{
			Command: []string{"/bin/sh", "-c", command},
			Stdin:   false,
			Stdout:  true,
			Stderr:  true,
			TTY:     true,
		}, scheme.ParameterCodec)
	exec, err := remotecommand.NewSPDYExecutor(restCfg, "POST", request.URL())
	if err != nil {
		return buf.String(), errBuf.String(), fmt.Errorf("failed creating new SPDY executor: %w", err)
	}
	childCtx, cancel := context.WithTimeout(ctx, time.Duration(timeout)*time.Second)
	defer cancel()
	err = exec.StreamWithContext(childCtx, remotecommand.StreamOptions{
		Stdout: buf,
		Stderr: errBuf,
	})
	if err != nil {
		return buf.String(), errBuf.String(), fmt.Errorf("%w Failed executing command %s on %v/%v", err, command, pod.Namespace, pod.Name)
	}
	log.Debug("Executed command")
	return buf.String(), errBuf.String(), nil
}

// DeletePodsWithLabel deletes pods with a particular label
func DeletePodsWithLabel(ctx context.Context, clients config.Clients, namespace string, label string) error {
	log.Debug("Entering DeletePodsWithLabel function")
	listopts := metav1.ListOptions{
		LabelSelector: label,
		FieldSelector: "metadata.namespace=" + namespace,
	}
	podlist, err := clients.Clientset.CoreV1().Pods("").List(ctx, listopts)
	if err != nil {
		log.Error("failed to list pods")
		return err
	}
	for _, pod := range podlist.Items {
		err = clients.CtrlClient.Delete(ctx, &pod)
		if ctrlclient.IgnoreNotFound(err) != nil { // Since we're deleting pods, don't worry if they're already gone
			log.WithError(err).Errorf("failed to delete pod %v", pod.Name)
			return err
		}
	}
	return nil
}

// DeleteNamespace deletes a namespace
func DeleteNamespace(ctx context.Context, clients config.Clients, namespace string) error {
	log.Debug("Entering deleteNamespace function")

	ns := &corev1.Namespace{}
	err := clients.CtrlClient.Get(ctx, ctrlclient.ObjectKey{Name: namespace}, ns)
	if err != nil {
		log.WithError(err).Errorf("failed to get namespace %s", namespace)
		return err
	}
	err = clients.CtrlClient.Delete(ctx, ns)
	if err != nil {
		log.WithError(err).Errorf("failed to delete ns %v", ns)
		return err
	}

	// Block until namespace is deleted for up to 5 mins
	endWait := time.Now().Add(5 * time.Minute)
	for {
		log.Infof("Waiting for Namespace %s to not exist", namespace)
		time.Sleep(5 * time.Second)
		err = clients.CtrlClient.Get(ctx, ctrlclient.ObjectKey{Name: namespace}, ns)
		if err != nil {
			break
		}
		if time.Now().After(endWait) {
			log.Errorf("namespace %s did not delete within 5 mins", namespace)
			return err
		}
	}
	return nil
}

// GetOrCreatePod gets or creates a pod if it does not exist
func GetOrCreatePod(ctx context.Context, clients config.Clients, pod corev1.Pod) (corev1.Pod, error) {
	log.Debug("Entering getOrCreatePod function")

	err := clients.CtrlClient.Get(ctx, ctrlclient.ObjectKey{Namespace: pod.Namespace, Name: pod.Name}, &pod)
	if err != nil {
		if ctrlclient.IgnoreNotFound(err) == nil {
			log.Infof("didn't find existing pod %s, creating", pod.Name)
			err := clients.CtrlClient.Create(ctx, &pod)
			if err != nil {
				log.Error("failed to create pod")
				return pod, err
			}
		} else {
			log.Error("failed to get pod")
			return pod, err
		}
	} else {
		log.Infof("found existing pod %s", pod.Name)
	}
	return pod, nil
}

// GetOrCreateSvc gets or creates a service if it does not exist
func GetOrCreateSvc(ctx context.Context, clients config.Clients, svc corev1.Service) (corev1.Service, error) {
	log.Debug("Entering getOrCreateSvc function")

	err := clients.CtrlClient.Get(ctx, ctrlclient.ObjectKey{Namespace: svc.Namespace, Name: svc.Name}, &svc)
	if err != nil {
		if ctrlclient.IgnoreNotFound(err) == nil {
			log.Debugf("didn't find existing svc %s, creating", svc.Name)
			err := clients.CtrlClient.Create(ctx, &svc)
			if err != nil {
				log.Error("failed to create svc")
				return svc, err
			}
		} else {
			log.Error("failed to get svc")
			return svc, err
		}
	} else {
		log.Debugf("found existing svc %s", svc.Name)
	}
	return svc, nil
}

// DeleteServicesWithPrefix deletes services starting with a prefix
func DeleteServicesWithPrefix(ctx context.Context, clients config.Clients, namespace string, serviceNamePrefix string) error {
	log.Debug("Entering DeleteServicesWithPrefix function")
	svcs := corev1.ServiceList{}
	err := clients.CtrlClient.List(ctx, &svcs, ctrlclient.InNamespace(namespace))
	if err != nil {
		log.WithError(err).Error("failed to list services")
		return err
	}
	for _, svc := range svcs.Items {
		if strings.HasPrefix(svc.Name, serviceNamePrefix) {
			log.Debug("Deleting service: ", svc.Name)
			err = clients.CtrlClient.Delete(ctx, &svc)
			if err != nil {
				if ctrlclient.IgnoreNotFound(err) == nil {
					log.Infof("didn't find existing service %s", svc.Name)
				} else {
					log.WithError(err).Errorf("failed to delete svc %v", svc.Name)
					return err
				}
			}
		}
	}
	return nil
}

// DeleteNetPolsInNamespace deletes network policies in a namespace
func DeleteNetPolsInNamespace(ctx context.Context, clients config.Clients, namespace string) error {
	log.Debug("Entering DeleteNetPolInNamespace function")
	netpols := &networkingv1.NetworkPolicyList{}
	err := clients.CtrlClient.List(ctx, netpols, ctrlclient.InNamespace(namespace))
	if err != nil {
		log.WithError(err).Error("failed to list network policies")
		return err
	}
	for _, netpol := range netpols.Items {
		log.Debug("Deleting network policy: ", netpol.Name)
		err = clients.CtrlClient.Delete(ctx, &netpol)
		if err != nil {
			if ctrlclient.IgnoreNotFound(err) == nil {
				log.Infof("didn't find existing network policy %s", netpol.Name)
			} else {
				log.WithError(err).Errorf("failed to delete network policy %v", netpol.Name)
				return err
			}
		}
	}
	return nil
}

// GetOrCreateDeployment gets or creates a deployment if it does not exist
func GetOrCreateDeployment(ctx context.Context, clients config.Clients, deployment appsv1.Deployment) (appsv1.Deployment, error) {
	log.Debug("Entering GetOrCreateDeployment function")

	err := clients.CtrlClient.Get(ctx, ctrlclient.ObjectKey{Namespace: deployment.Namespace, Name: deployment.Name}, &deployment)
	if err != nil {
		if ctrlclient.IgnoreNotFound(err) == nil {
			log.Infof("didn't find existing deployment %s, creating", deployment.Name)
			err := clients.CtrlClient.Create(ctx, &deployment)
			if err != nil {
				log.Error("failed to create deployment")
				return deployment, err
			}
		} else {
			log.Error("failed to get deployment")
			return deployment, err
		}
	} else {
		log.Infof("found existing deployment %s", deployment.Name)
	}
	return deployment, nil
}

// DeleteDeploymentsWithPrefix deletes deployments, starting with a prefix
func DeleteDeploymentsWithPrefix(ctx context.Context, clients config.Clients, namespace string, deploymentName string) error {
	log.Debug("Entering DeleteDeploymentsWithPrefix function")
	deployments := appsv1.DeploymentList{}
	err := clients.CtrlClient.List(ctx, &deployments, ctrlclient.InNamespace(namespace))
	if err != nil {
		log.WithError(err).Error("failed to list deployments")
		return err
	}
	for _, deployment := range deployments.Items {
		if strings.HasPrefix(deployment.Name, deploymentName) {
			log.Debug("Deleting deployment: ", deployment.Name)
			err = clients.CtrlClient.Delete(ctx, &deployment)
			if err != nil {
				if ctrlclient.IgnoreNotFound(err) == nil {
					log.Infof("didn't find existing deployment %s", deployment.Name)
				} else {
					log.WithError(err).Errorf("failed to delete deployment %v", deployment.Name)
					return err
				}
			}
		}
	}
	return nil
}

// GetOrCreateDS gets or creates a deployment if it does not exist
func GetOrCreateDS(ctx context.Context, clients config.Clients, ds appsv1.DaemonSet) (appsv1.DaemonSet, error) {
	log.Debug("Entering GetOrCreateDS function")

	err := clients.CtrlClient.Get(ctx, ctrlclient.ObjectKey{Namespace: ds.Namespace, Name: ds.Name}, &ds)
	if err != nil {
		if ctrlclient.IgnoreNotFound(err) == nil {
			log.Infof("didn't find existing daemonset %s, creating", ds.Name)
			err := clients.CtrlClient.Create(ctx, &ds)
			if err != nil {
				log.Error("failed to create ds")
				return ds, err
			}
		} else {
			log.Error("failed to get daemonset")
			return ds, err
		}
	} else {
		log.Infof("found existing daemonset %s", ds.Name)
	}
	return ds, nil
}

// GetOrCreateCM gets or creates a configMap if it does not exist
func GetOrCreateCM(ctx context.Context, clients config.Clients, cm corev1.ConfigMap) (corev1.ConfigMap, error) {
	log.Debug("Entering getOrCreateCM function")

	err := clients.CtrlClient.Get(ctx, ctrlclient.ObjectKey{Namespace: cm.Namespace, Name: cm.Name}, &cm)
	if err != nil {
		if ctrlclient.IgnoreNotFound(err) == nil {
			log.Infof("didn't find existing pod %s, creating", cm.Name)
			err := clients.CtrlClient.Create(ctx, &cm)
			if err != nil {
				log.Error("failed to create configMap")
				return cm, err
			}
		} else {
			log.Error("failed to get configMap")
			return cm, err
		}
	} else {
		log.Infof("found existing configMap %s", cm.Name)
	}
	return cm, nil
}

// GetOrCreateNS gets or creates a namespace if it does not exist
func GetOrCreateNS(ctx context.Context, clients config.Clients, namespace string) (*corev1.Namespace, error) {
	// check namespace exists
	var ns *corev1.Namespace
	ns, err := clients.Clientset.CoreV1().Namespaces().Get(ctx, namespace, metav1.GetOptions{})
	if err != nil {
		log.Infof("didn't find existing namespace: %s, creating", namespace)
		ns, err = clients.Clientset.CoreV1().Namespaces().Create(ctx, &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: namespace}}, metav1.CreateOptions{})
		if err != nil {
			log.Error("failed to create namespace")
			return ns, err
		}
	}
	return ns, nil
}

// WaitForDeployment waits for a deployment to be ready
func WaitForDeployment(ctx context.Context, clients config.Clients, deployment appsv1.Deployment) error {
	log.Debug("Entering waitForDeployment function")
	backoff := retry.NewFibonacci(1 * time.Second)
	if err := retry.Do(ctx, retry.WithMaxRetries(10, backoff), func(ctx context.Context) error {
		err := clients.CtrlClient.Get(ctx, ctrlclient.ObjectKey{Namespace: deployment.Namespace, Name: deployment.Name}, &deployment)
		if err != nil {
			if ctx.Err() != nil {
				return ctx.Err()
			}
			return err
		}
		if deployment.Status.ReadyReplicas == deployment.Status.Replicas {
			return nil
		}
		return retry.RetryableError(fmt.Errorf("deployment %s/%s not ready, ready=%d, replicas=%d", deployment.Namespace, deployment.Name, deployment.Status.ReadyReplicas, deployment.Status.Replicas))
	}); err != nil {
		return err
	}
	return nil
}

// WaitForTestPods waits for test pods to be running and returns them in a slice
func WaitForTestPods(ctx context.Context, clients config.Clients, namespace string, label string) ([]corev1.Pod, error) {
	log.Debug("Entering waitForPods function")
	var testpods []corev1.Pod
outer:
	for retry := 0; retry < 10; retry++ {
		time.Sleep(10 * time.Second)
		listopts := metav1.ListOptions{
			LabelSelector: label,
		}
		podlist, err := clients.Clientset.CoreV1().Pods(namespace).List(ctx, listopts)
		if err != nil {
			continue outer
		}
		testpods = podlist.Items
		for _, pod := range testpods {
			if pod.Status.Phase != "Running" {
				continue outer
			}
		}
		break outer
	}
	return testpods, nil
}

// RetryinPod retries a command in a pod
func RetryinPod(ctx context.Context, clients config.Clients, pod *corev1.Pod, cmd string, timeout int) (string, string, error) {
	log.Debug("Entering RetryinPod function")
	var stdout string
	var stderr string
	var err error
	for retry := 0; retry < 10; retry++ {
		stdout, stderr, err = ExecCommandInPod(ctx, pod, cmd, timeout)
		if err == nil {
			break
		} else {
			log.Infof("Hit error running command, retrying: %s", err)
			log.Info("stdout: ", stdout)
			log.Info("stderr: ", stderr)
		}
		time.Sleep(1 * time.Second)
	}
	log.Debug("Done")
	return stdout, stderr, err
}

type patchUInt32Value struct {
	Op    string `json:"op"`
	Path  string `json:"path"`
	Value uint32 `json:"value"`
}

// ScaleDeployment scales a deployment
func ScaleDeployment(ctx context.Context, clients config.Clients, deployment appsv1.Deployment, size int32) error {
	log.Infof("scaling deployment %s to %d", deployment.Name, size)
	err := clients.CtrlClient.Get(ctx, ctrlclient.ObjectKey{Name: deployment.Name, Namespace: deployment.Namespace}, &deployment)
	if err != nil {
		log.WithError(err).Error("failed to get deployment to scale")
		return err
	}

	payload := []patchUInt32Value{{
		Op:    "replace",
		Path:  "/spec/replicas",
		Value: uint32(size),
	}}
	payloadBytes, _ := json.Marshal(payload)
	_, err = clients.Clientset.AppsV1().Deployments(deployment.Namespace).Patch(ctx, deployment.Name, types.JSONPatchType, payloadBytes, metav1.PatchOptions{})
	if err != nil {
		log.WithError(err).Error("failed to scale deployment")
		return err
	}
	return nil
}

// GetPodLogs retrieves logs from a pod
func GetPodLogs(ctx context.Context, clients config.Clients, podName string, namespace string) (string, error) {
	log.Debug("Entering GetPodLogs function")
	podLogOpts := corev1.PodLogOptions{}
	req := clients.Clientset.CoreV1().Pods(namespace).GetLogs(podName, &podLogOpts)
	logs, err := req.Stream(ctx)
	if err != nil {
		log.WithError(err).Error("failed to get pod logs")
		return "", err
	}
	defer logs.Close()
	buf := new(bytes.Buffer)
	_, err = buf.ReadFrom(logs)
	if err != nil {
		log.WithError(err).Error("failed to read pod logs")
		return "", err
	}
	return buf.String(), nil
}

// IsPodRunning checks if a pod is running
func IsPodRunning(ctx context.Context, clients config.Clients, pod *corev1.Pod) (bool, error) {
	log.Debug("Entering isPodRunning function")
	pod, err := clients.Clientset.CoreV1().Pods(pod.Namespace).Get(ctx, pod.Name, metav1.GetOptions{})
	if err != nil {
		return false, err
	}

	switch pod.Status.Phase {
	case corev1.PodRunning:
		return true, nil
	case corev1.PodFailed, corev1.PodSucceeded:
		return false, conditions.ErrPodCompleted
	}
	return false, nil
}

// Int64Ptr returns a pointer to the given int64 value.
func Int64Ptr(i int64) *int64 {
	return &i
}

// BoolPtr returns a pointer to the given bool value.
func BoolPtr(b bool) *bool {
	return &b
}
