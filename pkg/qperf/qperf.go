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
	External struct {
		Latency        float64 `json:"latency,omitempty"`
		LatencyUnit    string  `json:"latency_unit,omitempty"`
		Throughput     float64 `json:"throughput,omitempty"`
		ThroughputUnit string  `json:"throughput_unit,omitempty"`
	} `json:"external,omitempty"`
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
func RunQperfTests(ctx context.Context, clients config.Clients, testDuration int, namespace string, perfCfg config.PerfConfig) (*Results, error) {
	log.Debug("Entering runQperfTests function")
	results := Results{}
	var err error
	var testpods []corev1.Pod

	if perfCfg.Direct || perfCfg.Service {
		// if this is an internal test, setup test pods, otherwise do not because the user sets up the external test
		testpods, err = utils.WaitForTestPods(ctx, clients, namespace, "app=qperf")
		if err != nil {
			log.WithError(err).Error("failed to get testpods")
			return &results, err
		}
		if len(testpods) < 2 {
			return &results, fmt.Errorf("expected at least 2 qperf pods, got %d", len(testpods))
		}
	}

	if perfCfg.Direct {
		log.Info("Running pod-pod qperf test")
		podIP := testpods[0].Status.PodIP
		results.Direct.Latency, results.Direct.LatencyUnit, results.Direct.Throughput, results.Direct.ThroughputUnit, err = runQperfTest(ctx, clients, &testpods[1], podIP, perfCfg.ControlPort, perfCfg.TestPort, testDuration, namespace)
		if err != nil {
			log.WithError(err).Errorf("error hit running pod-pod qperf test")
			return &results, err
		}
	}

	if perfCfg.Service {
		log.Info("Running pod-svc-pod qperf test")
		svcname := fmt.Sprintf("qperf-srv-%s", utils.SanitizeString(testpods[0].Spec.NodeName))
		svc, err := clients.Clientset.CoreV1().Services(namespace).Get(ctx, svcname, metav1.GetOptions{})
		if err != nil {
			log.WithError(err).Errorf("failed to list services in ns %s", namespace)
			return &results, err
		}
		svcIP := svc.Spec.ClusterIP
		results.Service.Latency, results.Service.LatencyUnit, results.Service.Throughput, results.Service.ThroughputUnit, err = runQperfTest(ctx, clients, &testpods[1], svcIP, perfCfg.ControlPort, perfCfg.TestPort, testDuration, namespace)
		if err != nil {
			log.WithError(err).Error("error hit running pod-svc-pod qperf test")
			return &results, err
		}
	}
	if perfCfg.External {
		log.Info("Running ext-svc-pod qperf test")
		results.External.Latency, results.External.LatencyUnit, results.External.Throughput, results.External.ThroughputUnit, err = runQperfTest(ctx, clients, nil, perfCfg.ExternalIPOrFQDN, perfCfg.ControlPort, perfCfg.TestPort, testDuration, namespace)
		if err != nil {
			log.WithError(err).Error("error hit running ext-svc-pod qperf test")
			return &results, err
		}
	}
	return &results, nil
}

// RunQperfTest starts the qperf test
func runQperfTest(ctx context.Context, clients config.Clients, srcPod *corev1.Pod, targetIP string, controlPort int, testPort int, testDuration int, namespace string) (float64, string, float64, string, error) {
	log.Debug("Entering runQperfTest function")

	var stdout string
	var stderr string
	var err error
	cmd := fmt.Sprintf("qperf %s -t %d -vv -ub -lp %d -ip %d tcp_bw tcp_lat", targetIP, testDuration, controlPort, testPort)

	if srcPod != nil {
		_, err := utils.WaitForTestPods(ctx, clients, namespace, "app=qperf")
		if err != nil {
			return 0, "", 0, "", fmt.Errorf("failed to wait for qperf pods to be ready")
		}
		stdout, stderr, err = utils.RetryinPod(ctx, clients, srcPod, cmd, testDuration*2+60) // the *2 is because we're running two tests: tcp_bw and tcp_lat
		log.Debug("stdout: ", stdout)
		log.Debug("stderr: ", stderr)
		if err != nil {
			return 0, "", 0, "", fmt.Errorf("%s", stderr)
		}
	} else {
		// srcPod is nil, so we're running the test from this host
		stdout, stderr, err = utils.Shellout(cmd, 5) // retry a few times, since one seems unreliable
		log.Debug("stdout: ", stdout)
		log.Debug("stderr: ", stderr)
		if err != nil {
			return 0, "", 0, "", fmt.Errorf("%s", stderr)
		}
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
	log.Infof("tcp_lat: %.1f unit:%s, tcp_bw: %.1f unit:%s", latency, latencyUnit, bandwidth, bandwidthUnit)
	return latency, latencyUnit, bandwidth, bandwidthUnit, nil
}

// SummarizeResults converts a list of results into the statistical summary of the results
// measurement is one mode's numbers from one iteration. The fields of Results are anonymous
// structs, so they have to be copied out rather than pointed at.
type measurement struct {
	latency, throughput         float64
	latencyUnit, throughputUnit string
}

// mode ties a test mode's name, its place in Results, and its place in ResultSummary.
type mode struct {
	name    string
	sample  func(*Results) measurement
	summary func(*ResultSummary) (latency, throughput *stats.ResultSummary)
}

func modes() []mode {
	return []mode{
		{
			name: "direct",
			sample: func(r *Results) measurement {
				return measurement{r.Direct.Latency, r.Direct.Throughput, r.Direct.LatencyUnit, r.Direct.ThroughputUnit}
			},
			summary: func(s *ResultSummary) (*stats.ResultSummary, *stats.ResultSummary) {
				return &s.Latency.Direct, &s.Throughput.Direct
			},
		},
		{
			name: "service",
			sample: func(r *Results) measurement {
				return measurement{r.Service.Latency, r.Service.Throughput, r.Service.LatencyUnit, r.Service.ThroughputUnit}
			},
			summary: func(s *ResultSummary) (*stats.ResultSummary, *stats.ResultSummary) {
				return &s.Latency.Service, &s.Throughput.Service
			},
		},
		{
			name: "external",
			sample: func(r *Results) measurement {
				return measurement{r.External.Latency, r.External.Throughput, r.External.LatencyUnit, r.External.ThroughputUnit}
			},
			summary: func(s *ResultSummary) (*stats.ResultSummary, *stats.ResultSummary) {
				return &s.Latency.External, &s.Throughput.External
			},
		},
	}
}

func normaliseLatency(mode string, value float64, unit string) (float64, error) {
	switch unit {
	case "ms":
		return value * 1000, nil
	case "us":
		return value, nil
	}
	return 0, fmt.Errorf("unknown %s latency unit: %s", mode, unit)
}

func normaliseThroughput(mode string, value float64, unit string) (float64, error) {
	switch unit {
	case "Gb/sec":
		return value * 1000, nil
	case "Mb/sec":
		return value, nil
	}
	return 0, fmt.Errorf("unknown %s throughput unit: %s", mode, unit)
}

// SummarizeResults converts a list of results into the statistical summary of the results
func SummarizeResults(results []*Results) (*ResultSummary, error) {
	log.Debug("Entering summarizeResults function")
	var resultSummary ResultSummary

	for _, m := range modes() {
		var latencies, throughputs []float64
		for _, result := range results {
			s := m.sample(result)
			// A mode that did not run leaves every field zero.
			if s.latencyUnit == "" && s.latency == 0 && s.throughput == 0 && s.throughputUnit == "" {
				continue
			}
			latency, err := normaliseLatency(m.name, s.latency, s.latencyUnit)
			if err != nil {
				log.Error(err)
				return &resultSummary, err
			}
			throughput, err := normaliseThroughput(m.name, s.throughput, s.throughputUnit)
			if err != nil {
				log.Error(err)
				return &resultSummary, err
			}
			latencies = append(latencies, latency)
			throughputs = append(throughputs, throughput)
		}
		if len(latencies) == 0 {
			continue
		}

		latencySummary, throughputSummary := m.summary(&resultSummary)
		var err error
		if *latencySummary, err = stats.SummarizeResults(latencies); err != nil {
			return &resultSummary, fmt.Errorf("failed to summarize %s latencies", m.name)
		}
		latencySummary.Unit = "us"
		if *throughputSummary, err = stats.SummarizeResults(throughputs); err != nil {
			return &resultSummary, fmt.Errorf("failed to summarize %s throughputs", m.name)
		}
		throughputSummary.Unit = "Mb/sec"
	}
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
func DeployQperfPods(ctx context.Context, clients config.Clients, namespace string, hostnet bool, image string, controlPort int, testPort int) error {
	log.Debug("Entering deployQperfPods function")
	// get node names
	nodelist := &corev1.NodeList{}
	err := clients.CtrlClient.List(ctx, nodelist)
	if err != nil {
		log.WithError(err).Error("failed to list nodes")
		return err
	}
	for _, node := range nodelist.Items {
		if node.Labels["tigera.io/test-nodepool"] == "default-pool" {
			// deploy server pods
			nodename := node.ObjectMeta.Name
			log.Debugf("found nodename: %s", nodename)
			podname := fmt.Sprintf("qperf-srv-%s", nodename)
			pod := makeQperfPod(nodename, namespace, podname, image, hostnet, controlPort, testPort)
			_, err = utils.GetOrCreatePod(ctx, clients, pod)
			if err != nil {
				log.WithError(err).Error("error making qperf pod")
				return err
			}
			// We're creating a service per pod, so that we can select the node we run the test between.
			svc := makeSvc(namespace, podname, controlPort, testPort)
			_, err = utils.GetOrCreateSvc(ctx, clients, svc)
			if err != nil {
				log.WithError(err).Error("error making qperf svc")
				return err
			}
		}
	}
	return nil
}

func makeQperfPod(nodename string, namespace string, podname string, image string, hostnetwork bool, controlPort int, testPort int) corev1.Pod {
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

func makeSvc(namespace string, podname string, controlPort int, testPort int) corev1.Service {
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
