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
	appsv1 "k8s.io/api/apps/v1"
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

		//   Create a test pod deployment (of pingo pods), which will contact the server pod on the same node (we are testing control plane performance, not node-node latency)
		depname := fmt.Sprintf("ttfr-test-%.2d", i)
		dep := makeDeployment(node.ObjectMeta.Name, testconfig.TestNamespace, depname, testconfig.HostNetwork, cfg.TTFRImage, int32(testconfig.TTFRConfig.TestPodsPerNode), podIP)
		_, err = utils.GetOrCreateDeployment(ctx, clients, dep)
		if err != nil {
			log.WithError(err).Error("error making test deployment")
			return ttfrResults, err
		}
		//   Wait for the test deployment pods to be ready
		_, err = utils.WaitForTestPods(ctx, clients, testconfig.TestNamespace, fmt.Sprintf("app=ttfr,pod=%s,node=%s", depname, node.ObjectMeta.Name))
		if err != nil {
			log.WithError(err).Error("error waiting for test deployment to be ready")
			return ttfrResults, err
		}
	}
	endtime := time.Now().Add(time.Duration(testconfig.TTFRConfig.TestDuration) * time.Second)
	period := 1.0 / float64(testconfig.TTFRConfig.Rate)
	nextTime := time.Now().Add(time.Duration(period) * time.Second)
outer:
	for loopcount := 0; true; loopcount++ {
		for n, node := range nodelist.Items {
			// For each node in the cluster (with the test label):
			//     Get the list of running pods in the test deployment on this node
			pods := &corev1.PodList{}
			listOptions := &client.ListOptions{
				Namespace: testconfig.TestNamespace,
				LabelSelector: labels.SelectorFromSet(labels.Set{
					"app":  "ttfr",
					"pod":  fmt.Sprintf("ttfr-test-%.2d", n),
					"node": node.ObjectMeta.Name,
				}),
			}
			err = clients.CtrlClient.List(ctx, pods, listOptions)
			if err != nil {
				log.WithError(err).Error("error listing pods")
				return ttfrResults, err
			}
			log.Info("Pods in deployment: ", len(pods.Items))

			numThreads := 10
			// Spin up a channel with multiple threads to get TTFRs from pods
			var wg sync.WaitGroup
			ttfrs := make([](float64), len(pods.Items))
			errors := make([]error, len(pods.Items))
			sem := make(chan struct{}, numThreads)

			//     For each pod in the deployment:
		nextpod:
			for p, pod := range pods.Items {
				if time.Now().After(endtime) {
					log.Info("Time's up, stopping test")
					break outer
				}
				// if it is the first time round the loop, delete the pod, don't check result
				if loopcount == 0 {
					log.Info("Deleting pod ", pod.ObjectMeta.Name)
					err = clients.CtrlClient.Delete(ctx, &pod)
					if err != nil {
						log.WithError(err).Error("error deleting pod")
						return ttfrResults, err
					}
					// we haven't started the test yet, so update the next time
					delay := time.Until(nextTime)
					if delay > 0 {
						log.Infof("Sleeping for %s", delay)
						time.Sleep(delay)
					} else {
						log.Warning("unable to keep up with rate")
					}
					nextTime = time.Now().Add(time.Duration(period) * time.Second)
					continue nextpod
				}
				sem <- struct{}{}
				wg.Add(1)
				go func() {
					defer wg.Done()
					defer func() { <-sem }()
					// if pod is deleting skip it
					if pod.DeletionTimestamp != nil {
						log.Info("Pod ", pod.ObjectMeta.Name, " is deleting, skipping")
						err = fmt.Errorf("pod %s is deleting", pod.ObjectMeta.Name)
						ttfrs[p] = 99999
						errors[p] = err
						return
					}
					ttfrSec, err := getPodTTFR(ctx, clients, pod)
					if err != nil {
						if strings.Contains(err.Error(), "not found") {
							err = fmt.Errorf("pod not found: %s", pod.ObjectMeta.Name)
							ttfrs[p] = 99999
							errors[p] = err
							return
						}
						err = fmt.Errorf("error getting pod TTFR: %w", err)
						ttfrs[p] = 99999
						errors[p] = err
						return
					}
					ttfrs[p] = ttfrSec
					errors[p] = nil
					return
				}()
				delay := time.Until(nextTime)
				if delay > 0 {
					log.Infof("Sleeping for %s", delay)
					time.Sleep(delay)
				} else {
					log.Warning("unable to keep up with rate")
				}
				nextTime = time.Now().Add(time.Duration(period) * time.Second)
			}
			wg.Wait()
			// we now have a list of errors, and list of ttfrs.
			log.Debugf("Errors: %v+", errors)
			for t, err := range errors {
				if err == nil {
					// copy all results that don't have an error to the results
					log.Info("Copying over TTFR result: ", ttfrs[t])
					ttfrResults.TTFR = append(ttfrResults.TTFR, ttfrs[t])
				} else {
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
		}
	}
	log.Info("Test complete, got ", len(ttfrResults.TTFR), " results")
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
			Containers: []corev1.Container{
				{
					Name:  "ttfr",
					Image: image,
				},
			},
			NodeName:      nodename,
			RestartPolicy: "OnFailure",
			HostNetwork:   hostnetwork,
		},
	}
	return pod
}

func makeDeployment(nodename string, namespace string, depname string, hostnetwork bool, image string, replicas int32, target string) appsv1.Deployment {
	depname = utils.SanitizeString(depname)
	deployment := appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{
			Labels: map[string]string{
				"app":  "ttfr",
				"pod":  depname,
				"node": nodename,
			},
			Name:      depname,
			Namespace: namespace,
		},
		Spec: appsv1.DeploymentSpec{
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{
					Labels: map[string]string{
						"app":  "ttfr",
						"pod":  depname,
						"node": nodename,
					},
				},
				Spec: corev1.PodSpec{

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
							// ImagePullPolicy: corev1.PullAlways,
						},
					},
					NodeName:      nodename,
					RestartPolicy: "Always",
					HostNetwork:   hostnetwork,
				},
			},
			Replicas: &replicas,
			Selector: &metav1.LabelSelector{
				MatchLabels: map[string]string{
					"app":  "ttfr",
					"pod":  depname,
					"node": nodename,
				},
			},
		},
	}
	return deployment
}
