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

package qperf

import (
	"context"
	"fmt"
	"strconv"
	"strings"

	"github.com/projectcalico/tiger-bench/pkg/config"
	"github.com/projectcalico/tiger-bench/pkg/policy"
	"github.com/projectcalico/tiger-bench/pkg/stats"

	log "github.com/sirupsen/logrus"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/projectcalico/tiger-bench/pkg/utils"
)

// Results holds the results returned from qperf
type Results struct {
	Direct struct {
		Latency        float64 `json:"latency,omitempty"`
		LatencyUnit    string  `json:"latency_unit,omitempty"`
		Throughput     float64 `json:"throughput,omitempty"`
		ThroughputUnit string  `json:"throughput_unit,omitempty"`
	} `json:"direct,omitempty"`
	Service struct {
		Latency        float64 `json:"latency,omitempty"`
		LatencyUnit    string  `json:"latency_unit,omitempty"`
		Throughput     float64 `json:"throughput,omitempty"`
		ThroughputUnit string  `json:"throughput_unit,omitempty"`
	} `json:"service,omitempty"`
}

// ResultSummary holds a statistical summary of the results
type ResultSummary struct {
	Latency struct {
		Direct   stats.ResultSummary `json:"pod-pod,omitempty"`
		Service  stats.ResultSummary `json:"pod-svc-pod,omitempty"`
		External stats.ResultSummary `json:"ext-svc-pod,omitempty"`
	}
	Throughput struct {
		Direct   stats.ResultSummary `json:"pod-pod,omitempty"`
		Service  stats.ResultSummary `json:"pod-svc-pod,omitempty"`
		External stats.ResultSummary `json:"ext-svc-pod,omitempty"`
	}
}

// RunQperfTests runs the qperf test, to pod and to service
func RunQperfTests(ctx context.Context, clients config.Clients, testDuration int, namespace string) (*Results, error) {
	log.Debug("Entering runQperfTests function")
	results := Results{}
	var err error
	testpods, err := utils.WaitForTestPods(ctx, clients, namespace, "app=qperf")
	if err != nil {
		log.WithError(err).Error("failed to get testpods")
		return &results, err
	}
	if len(testpods) < 2 {
		return &results, fmt.Errorf("expected at least 2 qperf pods, got %d", len(testpods))
	}
	podIP := testpods[0].Status.PodIP
	results.Direct.Latency, results.Direct.LatencyUnit, results.Direct.Throughput, results.Direct.ThroughputUnit, err = runQperfTest(ctx, clients, &testpods[1], podIP, testDuration, namespace)
	if err != nil {
		log.WithError(err).Errorf("error hit running pod-pod qperf test")
		return &results, err
	}
	svcname := fmt.Sprintf("qperf-srv-%s", utils.SanitizeString(testpods[0].Spec.NodeName))
	svc, err := clients.Clientset.CoreV1().Services(namespace).Get(ctx, svcname, metav1.GetOptions{})
	if err != nil {
		log.WithError(err).Errorf("failed to list services in ns %s", namespace)
		return &results, err
	}

	svcIP := svc.Spec.ClusterIP
	results.Service.Latency, results.Service.LatencyUnit, results.Service.Throughput, results.Service.ThroughputUnit, err = runQperfTest(ctx, clients, &testpods[1], svcIP, testDuration, namespace)
	if err != nil {
		log.WithError(err).Error("error hit running pod-svc-pod qperf test")
		return &results, err
	}
	return &results, nil
}

// RunQperfTest starts the qperf test
func runQperfTest(ctx context.Context, clients config.Clients, srcPod *corev1.Pod, targetIP string, testDuration int, namespace string) (float64, string, float64, string, error) {
	log.Debug("Entering runQperfTest function")

	var stdout string
	_, err := utils.WaitForTestPods(ctx, clients, namespace, "app=qperf")
	if err != nil {
		return 0, "", 0, "", fmt.Errorf("failed to wait for qperf pods to be ready")
	}
	cmd := fmt.Sprintf("qperf %s -t %d -vv -ub -lp 4000 -ip 4001 tcp_bw tcp_lat", targetIP, testDuration)
	stdout, _, err = utils.RetryinPod(ctx, clients, srcPod, cmd, testDuration*2+60) // the *2 is because we're running two tests: tcp_bw and tcp_lat
	if err != nil {
		return 0, "", 0, "", fmt.Errorf("failed to run qperf command")
	}

	output := parseQperfOutput(stdout)
	log.Debugf("output: %s", output)
	latencystr := output["tcp_lat:"]["latency"]
	// e.g. "12.8 us"
	latency, err := strconv.ParseFloat(strings.Split(latencystr, " ")[0], 64)
	if err != nil {
		return 0, "", 0, "", fmt.Errorf("failed to parse latency data: %s", latencystr)
	}
	latencyUnit := strings.Split(latencystr, " ")[1]
	bandwidthstr := output["tcp_bw:"]["bw"]
	bandwidth, err := strconv.ParseFloat(strings.Split(bandwidthstr, " ")[0], 64)
	if err != nil {
		return 0, "", 0, "", fmt.Errorf("failed to parse bandwidth data: %s", bandwidthstr)
	}
	bandwidthUnit := strings.Split(bandwidthstr, " ")[1]
	log.Infof("tcp_lat: %.1f %s, tcp_bw: %.1f %s", latency, latencyUnit, bandwidth, bandwidthUnit)
	return latency, latencyUnit, bandwidth, bandwidthUnit, nil
}

// CreateTestPolicy creates the policies needed to ensure test pods can run the test on ports 4000 and 4001
func CreateTestPolicy(ctx context.Context, clients config.Clients, name string, namespace string) error {
	log.Debug("Entering createTestPolicy function")
	return policy.CreateTestPolicy(ctx, clients, name, namespace, []int{4000, 4001})
}

// SummarizeResults converts a list of results into the statistical summary of the results
func SummarizeResults(results []*Results) (*ResultSummary, error) {
	var resultSummary ResultSummary
	var directLatencies []float64
	var directThroughputs []float64
	var serviceLatencies []float64
	var serviceThroughputs []float64

	for _, result := range results {
		if result.Direct.LatencyUnit == "ms" {
			directLatencies = append(directLatencies, result.Direct.Latency*1000)
		} else if result.Direct.LatencyUnit == "us" {
			directLatencies = append(directLatencies, result.Direct.Latency)
		} else {
			log.Errorf("unknown direct latency unit: %s", result.Direct.LatencyUnit)
			return &resultSummary, fmt.Errorf("unknown direct latency unit: %s", result.Direct.LatencyUnit)
		}
		if result.Direct.ThroughputUnit == "Gb/sec" {
			directThroughputs = append(directThroughputs, result.Direct.Throughput*1000)
		} else if result.Direct.ThroughputUnit == "Mb/sec" {
			directThroughputs = append(directThroughputs, result.Direct.Throughput)
		} else {
			log.Errorf("unknown direct throughput unit: %s", result.Direct.ThroughputUnit)
			return &resultSummary, fmt.Errorf("unknown direct throughput unit: %s", result.Direct.ThroughputUnit)
		}
		if result.Service.LatencyUnit == "ms" {
			serviceLatencies = append(serviceLatencies, result.Service.Latency*1000)
		} else if result.Service.LatencyUnit == "us" {
			serviceLatencies = append(serviceLatencies, result.Service.Latency)
		} else {
			log.Errorf("unknown service latency unit: %s", result.Service.LatencyUnit)
			return &resultSummary, fmt.Errorf("unknown service latency unit: %s", result.Service.LatencyUnit)
		}
		if result.Service.ThroughputUnit == "Gb/sec" {
			serviceThroughputs = append(serviceThroughputs, result.Service.Throughput*1000)
		} else if result.Service.ThroughputUnit == "Mb/sec" {
			serviceThroughputs = append(serviceThroughputs, result.Service.Throughput)
		} else {
			log.Errorf("unknown service throughput unit: %s", result.Service.ThroughputUnit)
			return &resultSummary, fmt.Errorf("unknown service throughput unit: %s", result.Service.ThroughputUnit)
		}
	}
	var err error
	resultSummary.Latency.Direct, err = stats.SummarizeResults(directLatencies)
	if err != nil {
		return &resultSummary, fmt.Errorf("failed to summarize direct latencies")
	}
	resultSummary.Latency.Direct.Unit = "us"
	resultSummary.Throughput.Direct, err = stats.SummarizeResults(directThroughputs)
	if err != nil {
		return &resultSummary, fmt.Errorf("failed to summarize direct throughputs")
	}
	resultSummary.Throughput.Direct.Unit = "Mb/sec"
	resultSummary.Latency.Service, err = stats.SummarizeResults(serviceLatencies)
	if err != nil {
		return &resultSummary, fmt.Errorf("failed to summarize service latencies")
	}
	resultSummary.Latency.Service.Unit = "us"
	resultSummary.Throughput.Service, err = stats.SummarizeResults(serviceThroughputs)
	if err != nil {
		return &resultSummary, fmt.Errorf("failed to summarize service throughputs")
	}
	resultSummary.Throughput.Service.Unit = "Mb/sec"
	return &resultSummary, nil
}

func parseQperfOutput(stdout string) map[string]map[string]string {
	var qperf = map[string]map[string]string{}

	log.Debug("Entering parseQperfOutput function")
	lines := strings.Split(stdout, "\n")
	section := "None"
	for _, line := range lines {
		line = strings.TrimSpace(line)
		// if line doesn't contain "=" then it's a section header
		if !strings.Contains(line, "=") {
			log.Debugf("found section: %s", line)
			section = line
		} else {
			// if line contains "=" then it's a key value pair
			log.Debugf("found key value pair %s for section: %s", line, section)
			parts := strings.Split(line, "=")
			key := strings.TrimSpace(parts[0])
			value := strings.TrimSpace(parts[1])
			if _, ok := qperf[section]; !ok {
				qperf[section] = map[string]string{}
			}
			qperf[section][key] = value
		}
	}
	return qperf
}

// DeployQperfPods deploys qperf pods
func DeployQperfPods(ctx context.Context, clients config.Clients, namespace string, hostnet bool, image string) error {
	log.Debug("Entering deployQperfPods function")
	// get node names
	nodelist := &corev1.NodeList{}
	err := clients.CtrlClient.List(ctx, nodelist)
	if err != nil {
		return fmt.Errorf("failed to list nodes: %w", err)
	}
	for _, node := range nodelist.Items {
		if node.Labels["tigera.io/test-nodepool"] == "default-pool" {
			// deploy server pods
			nodename := node.ObjectMeta.Name
			log.Debugf("found nodename: %s", nodename)
			podname := fmt.Sprintf("qperf-srv-%s", nodename)
			pod := makeQperfPod(nodename, namespace, podname, image, hostnet)
			_, err = utils.GetOrCreatePod(ctx, clients, pod)
			if err != nil {
				log.WithError(err).Error("error making qperf pod")
				return err
			}
			// We're creating a service per pod, so that we can select the node we run the test between.
			svc := makeSvc(namespace, podname)
			_, err = utils.GetOrCreateSvc(ctx, clients, svc)
			if err != nil {
				log.WithError(err).Error("error making qperf svc")
				return err
			}
		}
	}
	return nil
}

func makeQperfPod(nodename string, namespace string, podname string, image string, hostnetwork bool) corev1.Pod {
	podname = utils.SanitizeString(podname)
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
			Containers: []corev1.Container{
				{
					Name:  "qperf",
					Image: image,
					Args: []string{
						"qperf",
						"-lp",
						"4000",
					},
				},
			},
			NodeName:      nodename,
			RestartPolicy: "Never",
			HostNetwork:   hostnetwork,
		},
	}
	return pod
}

func makeSvc(namespace string, podname string) corev1.Service {
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
					Port: 4000,
				},
				{
					Name: "data",
					Port: 4001,
				},
			},
		},
	}
	return svc
}
