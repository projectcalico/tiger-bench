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

// This "time to first response" (TTFR) test spins up a server pod on each
// node in the cluster, and then spins up client pods on each node in the
// cluster.  The client pods start and send requests to the server pod, and
// record the amount of time it takes before they get a response.  This is
// sometimes* a useful proxy for how long its taking for Calico to program the
// rules for that pod (since pods start with a deny-all rule and calico-node
// must program the correct rules before it can talk to anything).
//
// * if `linuxPolicySetupTimeoutSeconds` is set in the CalicoNetworkSpec in
//   the Installation resource, then pod startup will be delayed until policy
//   is applied.
//   This can be handy if your application pod wants its first request to
//   always succeed.
//   This is a Calico-specific feature that is not part of the CNI spec.  See
//   the [Calico documentation](https://docs.tigera.io/calico/latest/reference/configure-cni-plugins#enabling-policy-setup-timeout)
//   for more information on this feature and how to enable it.

package ttfr

import (
	"context"
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/projectcalico/tiger-bench/pkg/config"
	"github.com/projectcalico/tiger-bench/pkg/stats"
	"github.com/projectcalico/tiger-bench/pkg/utils"
	"sigs.k8s.io/controller-runtime/pkg/client"

	log "github.com/sirupsen/logrus"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
)

// Results holds the results returned from ttfr
type Results struct {
	TTFR []float64 `json:"ttfr,omitempty"`
}

// ResultSummary holds a statistical summary of the results
type ResultSummary struct {
	TTFRSummary stats.ResultSummary `json:"ttfrSummary,omitempty"`
}

// RunTTFRTest runs a ttfr test
func RunTTFRTest(ctx context.Context, clients config.Clients, testconfig *config.TestConfig, cfg config.Config) (Results, error) {
	ttfrResults := Results{}

	nodelist := &corev1.NodeList{}
	err := clients.CtrlClient.List(ctx, nodelist, &client.ListOptions{
		LabelSelector: labels.SelectorFromSet(labels.Set{
			"tigera.io/test-nodepool": "default-pool",
		}),
	})
	if err != nil {
		return ttfrResults, fmt.Errorf("failed to list nodes: %w", err)
	}
	if len(nodelist.Items) == 0 {
		return ttfrResults, fmt.Errorf("no nodes found with label tigera.io/test-nodepool=default-pool")
	}
	targets := make([]string, len(nodelist.Items))
	for i, node := range nodelist.Items {
		// For each node in the cluster (with the test label):
		//   Create server pod on this node
		podname := fmt.Sprintf("ttfr-srv-%.2d", i)
		pod := makePod(node.ObjectMeta.Name, testconfig.TestNamespace, podname, testconfig.HostNetwork, cfg.WebServerImage)
		_, err = utils.GetOrCreatePod(ctx, clients, pod)
		if err != nil {
			log.WithError(err).Error("error making server pod")
			return ttfrResults, err
		}
		//   Wait for the server pod to be ready
		pods, err := utils.WaitForTestPods(ctx, clients, testconfig.TestNamespace, fmt.Sprintf("pod=%s", podname))
		if err != nil {
			log.WithError(err).Error("error waiting for server pod to be ready")
			return ttfrResults, err
		}
		if len(pods) == 0 {
			log.Error("no server pod found")
		}
		podIP := pods[0].Status.PodIP
		log.Infof("Server pod IP: %s", podIP)
		targets[i] = podIP
	}
	startTime := time.Now()
	endtime := startTime.Add(time.Duration(testconfig.Duration) * time.Second)
	period := 1000.0 / testconfig.TTFRConfig.Rate
	log.Debug("period: ", period, "ms")
	nextTime := startTime.Add(time.Duration(period) * time.Millisecond)

	var wg sync.WaitGroup

	// Make slices to hold ttfrs and errors
	var ttfrs []float64
	var errors []error

	numThreads := len(nodelist.Items) * testconfig.TTFRConfig.TestPodsPerNode
	sem := make(chan struct{}, numThreads)

outer:
	for loopcount := 0; true; loopcount++ {
		// For each node in the cluster (with the test label):
		for n, node := range nodelist.Items {
			//     For each pod
			for p := range testconfig.TTFRConfig.TestPodsPerNode {
				sem <- struct{}{}
				wg.Add(1)
				go func() {
					defer wg.Done()
					defer func() { <-sem }()
					// Create a pod on this node
					podname := fmt.Sprintf("ttfr-%.2d-%.2d-%.2d", loopcount, n, p)
					pod := makeTestPod(node.ObjectMeta.Name, testconfig.TestNamespace, podname, testconfig.HostNetwork, cfg.TTFRImage, targets[n])
					defer func() {
						// delete the pod
						_ = clients.CtrlClient.Delete(ctx, &pod)
					}()
					err = clients.CtrlClient.Create(ctx, &pod)
					if err != nil {
						log.WithError(err).Error("error making pod")
						ttfrs = append(ttfrs, 99999)
						errors = append(errors, err)
						return
					}

					// Wait for the pod to be ready
					_, err = utils.WaitForTestPods(ctx, clients, testconfig.TestNamespace, fmt.Sprintf("pod=%s", podname))
					if err != nil {
						log.WithError(err).Error("error waiting for pod to be ready")
						ttfrs = append(ttfrs, 99999)
						errors = append(errors, err)
						return
					}

					ttfrSec, err := getPodTTFR(ctx, clients, pod)
					if err != nil {
						if strings.Contains(err.Error(), "not found") {
							err = fmt.Errorf("pod not found: %s", pod.ObjectMeta.Name)
							ttfrs = append(ttfrs, 99999)
							errors = append(errors, err)
							return
						}
						err = fmt.Errorf("error getting pod TTFR: %w", err)
						ttfrs = append(ttfrs, 99999)
						errors = append(errors, err)
						return
					}
					ttfrs = append(ttfrs, ttfrSec)
					errors = append(errors, err)
				}()
				delay := time.Until(nextTime)
				if delay > 0 {
					log.Debugf("Sleeping for %s", delay)
					time.Sleep(delay)
				} else {
					log.Warning("unable to keep up with rate")
					if numThreads-len(sem) == 0 {
						log.Info("Not enough free pods to make requested rate, blocking until one is freed")
					}
				}
				nextTime = nextTime.Add(time.Duration(period) * time.Millisecond)
				// if we are at the end of the test, break out of the loop
				if time.Now().After(endtime) {
					log.Info("Time's up, stopping test (but allowing pods already requested to finish)")
					break outer
				}
			}
		}

	}
	wg.Wait()
	// we now have a slice of errors, and matching slice of ttfrs.
	log.Debugf("Errors: %v+", errors)
	numerrs := 0
	numresults := 0
	for i, err := range errors {
		if err == nil {
			// copy all results that don't have an error to the results
			numresults++
			log.Debug("Copying over TTFR result: ", ttfrs[i])
			ttfrResults.TTFR = append(ttfrResults.TTFR, ttfrs[i])
		} else {
			numerrs++
			switch {
			case strings.Contains(err.Error(), "pod not found"):
				log.Info("error getting pod TTFR")
			case strings.Contains(err.Error(), "pod is deleting"):
				log.Info("Pod is deleting, skipping")
			default:
				log.WithError(err).Error("error getting pod TTFR")
			}
		}
	}
	log.Debugf("Test complete, got %d results and %d errors", numresults, numerrs)
	log.Debug("ttfrResults.TTFR length = ", len(ttfrResults.TTFR))
	return ttfrResults, nil
}

// getPodTTFR gets the TTFR from the pod logs (with retry), and deletes the pod when successful
func getPodTTFR(ctx context.Context, clients config.Clients, pod corev1.Pod) (float64, error) {

	// retry getting pod logs
	for j := 0; j < 20; j++ {
		// if pod isn't running yet, wait for it to be running
		podRunning, err := utils.IsPodRunning(ctx, clients, &pod)
		if !podRunning || err != nil {
			log.Info("Pod ", pod.ObjectMeta.Name, " is not running, skipping")
			time.Sleep(1 * time.Second)
			continue
		}
		logs, err := utils.GetPodLogs(ctx, clients, pod.ObjectMeta.Name, pod.ObjectMeta.Namespace)
		if err != nil {
			log.WithError(err).Error("error getting pod logs")
			if strings.Contains(err.Error(), "not found") {
				return 99999, err
			}
			time.Sleep(1 * time.Second)
			continue
		}
		//         if we got a result:
		r := regexp.MustCompile(`{\\"ttfr_seconds\\": ([0-9].*\.[0-9].*)}`)
		results := r.FindStringSubmatch(logs)
		if len(results) == 0 {
			log.Info("No result found in logs")
			time.Sleep(1 * time.Second)
			continue
		}
		//           Parse the result and append to list of results
		ttfrSec, err := strconv.ParseFloat(results[1], 64)
		if err != nil {
			log.WithError(err).Error("error parsing ttfr result")
			return ttfrSec, err
		}
		log.Info("TTFR result: ", ttfrSec, " from pod ", pod.ObjectMeta.Name)
		//           delete the pod
		err = clients.CtrlClient.Delete(ctx, &pod)
		if err != nil {
			if strings.Contains(err.Error(), "not found") {
				return 99999, err
			}
			log.WithError(err).Error("error deleting pod")
			return ttfrSec, err
		}
		// Success! we made it all the way through without error
		return ttfrSec, nil
	}
	return 99999, fmt.Errorf("failed to get pod logs after 10 attempts for pod %s", pod.ObjectMeta.Name)
}

// SummarizeResults summarizes the results
func SummarizeResults(ttfrResults []*Results) ([]*ResultSummary, error) {
	log.Debug("Summarizing results")
	if len(ttfrResults) == 0 {
		return nil, fmt.Errorf("no results to summarize")
	}
	var resultSummaryList []*ResultSummary
	for _, result := range ttfrResults {
		// Summarize the results
		resultSummary := ResultSummary{}
		var err error
		// Calculate the summary statistics
		resultSummary.TTFRSummary, err = stats.SummarizeResults(result.TTFR)
		if err != nil {
			log.WithError(err).Error("error summarizing results")
			return nil, err
		}
		resultSummary.TTFRSummary.Unit = "seconds"
		// Add the summary to the list
		resultSummaryList = append(resultSummaryList, &resultSummary)
	}
	return resultSummaryList, nil
}

func makePod(nodename string, namespace string, podname string, hostnetwork bool, image string) corev1.Pod {
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

func makeTestPod(nodename string, namespace string, podname string, hostnetwork bool, image string, target string) corev1.Pod {
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
