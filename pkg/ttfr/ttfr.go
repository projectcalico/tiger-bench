package ttfr

import (
	"context"
	"fmt"
	"regexp"
	"strconv"
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
		dep := makeDeployment(node.ObjectMeta.Name, testconfig.TestNamespace, depname, testconfig.HostNetwork, cfg.TTFRImage, int32(testconfig.TTFRConfig.TestPodsPerNode), "http://"+podIP)
		_, err = utils.GetOrCreateDeployment(ctx, clients, dep)
		if err != nil {
			log.WithError(err).Error("error making test deployment")
			return ttfrResults, err
		}
		//   Wait for the test deployments to be ready
		//  NOT WORKING YET
		time.Sleep(5 * time.Second)
		err = utils.WaitForDeployment(ctx, clients, dep)
		if err != nil {
			log.WithError(err).Error("error waiting for test deployment to be ready")
			return ttfrResults, err
		}
	}
	endtime := time.Now().Add(time.Duration(testconfig.TTFRConfig.TestDuration) * time.Second)
	for n, node := range nodelist.Items {
		// For each node in the cluster (with the test label):
		if time.Now().After(endtime) {
			log.Info("Test complete")
			break
		}
		//   While we haven't timed out:
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

		//     For each pod in the deployment:
		for i, pod := range pods.Items {
			//       if it is the first time round the loop, delete the pod, don't check result
			if i == 0 {
				log.Info("Deleting pod ", pod.ObjectMeta.Name)
				err = clients.CtrlClient.Delete(ctx, &pod)
				if err != nil {
					log.WithError(err).Error("error deleting pod")
					return ttfrResults, err
				}
				continue
			}
			//       if it isn't the first time round the loop, read pod logs to see if there's a result yet
			logs, err := utils.GetPodLogs(ctx, clients, pod.ObjectMeta.Name, testconfig.TestNamespace)
			if err != nil {
				log.WithError(err).Error("error getting pod logs")
				return ttfrResults, err
			}
			//         if we got a result:
			r := regexp.MustCompile(`{\\"ttfr_seconds\\": ([0-9]\.[0-9].*)}`)
			results := r.FindStringSubmatch(logs)
			log.Info("TTFR result: ", results)
			if len(results) == 0 {
				log.Info("No result found in logs")
				continue
			}
			//           Parse the result and append to list of results
			ttfrSec, err := strconv.ParseFloat(results[1], 64)
			if err != nil {
				log.WithError(err).Error("error parsing ttfr result")
				return ttfrResults, err
			}
			log.Info("TTFR result: ", ttfrSec)
			ttfrResults.TTFR = append(ttfrResults.TTFR, ttfrSec)
			//           delete the pod
			err = clients.CtrlClient.Delete(ctx, &pod)
			if err != nil {
				log.WithError(err).Error("error deleting pod")
				return ttfrResults, err
			}
		}
	}
	return ttfrResults, nil
}

// SummarizeResults summarizes the results
func SummarizeResults(ttfrResults []*Results) ([]*ResultSummary, error) {
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
							},
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
