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

package iperf

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/projectcalico/tiger-bench/pkg/config"
	"github.com/projectcalico/tiger-bench/pkg/stats"

	log "github.com/sirupsen/logrus"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/projectcalico/tiger-bench/pkg/utils"
)

type iperfReport struct {
	Start struct {
		Connected []struct {
			Socket     int    `json:"socket"`
			LocalHost  string `json:"local_host"`
			LocalPort  int    `json:"local_port"`
			RemoteHost string `json:"remote_host"`
			RemotePort int    `json:"remote_port"`
		} `json:"connected"`
		Version    string `json:"version"`
		SystemInfo string `json:"system_info"`
		Timestamp  struct {
			Time     string `json:"time"`
			Timesecs int    `json:"timesecs"`
		} `json:"timestamp"`
		ConnectingTo struct {
			Host string `json:"host"`
			Port int    `json:"port"`
		} `json:"connecting_to"`
		Cookie        string `json:"cookie"`
		TCPMssDefault int    `json:"tcp_mss_default"`
		SockBufsize   int    `json:"sock_bufsize"`
		SndbufActual  int    `json:"sndbuf_actual"`
		RcvbufActual  int    `json:"rcvbuf_actual"`
		TestStart     struct {
			Protocol   string `json:"protocol"`
			NumStreams int    `json:"num_streams"`
			Blksize    int    `json:"blksize"`
			Omit       int    `json:"omit"`
			Duration   int    `json:"duration"`
			Bytes      int    `json:"bytes"`
			Blocks     int    `json:"blocks"`
			Reverse    int    `json:"reverse"`
			Tos        int    `json:"tos"`
		} `json:"test_start"`
	} `json:"start"`
	Intervals []struct {
		Streams []struct {
			Socket        int     `json:"socket"`
			Start         float64 `json:"start"`
			End           float64 `json:"end"`
			Seconds       float64 `json:"seconds"`
			Bytes         int     `json:"bytes"`
			BitsPerSecond float64 `json:"bits_per_second"`
			Retransmits   int     `json:"retransmits"`
			SndCwnd       int     `json:"snd_cwnd"`
			Rtt           int     `json:"rtt"`
			Rttvar        int     `json:"rttvar"`
			Pmtu          int     `json:"pmtu"`
			Omitted       bool    `json:"omitted"`
			Sender        bool    `json:"sender"`
		} `json:"streams"`
		Sum struct {
			Start         float64 `json:"start"`
			End           float64 `json:"end"`
			Seconds       float64 `json:"seconds"`
			Bytes         int     `json:"bytes"`
			BitsPerSecond float64 `json:"bits_per_second"`
			Retransmits   int     `json:"retransmits"`
			Omitted       bool    `json:"omitted"`
			Sender        bool    `json:"sender"`
		} `json:"sum"`
	} `json:"intervals"`
	End struct {
		Streams []struct {
			Sender struct {
				Socket        int     `json:"socket"`
				Start         float64 `json:"start"`
				End           float64 `json:"end"`
				Seconds       float64 `json:"seconds"`
				Bytes         int64   `json:"bytes"`
				BitsPerSecond float64 `json:"bits_per_second"`
				Retransmits   int     `json:"retransmits"`
				MaxSndCwnd    int     `json:"max_snd_cwnd"`
				MaxRtt        int     `json:"max_rtt"`
				MinRtt        int     `json:"min_rtt"`
				MeanRtt       int     `json:"mean_rtt"`
				Sender        bool    `json:"sender"`
			} `json:"sender"`
			Receiver struct {
				Socket        int     `json:"socket"`
				Start         float64 `json:"start"`
				End           float64 `json:"end"`
				Seconds       float64 `json:"seconds"`
				Bytes         int64   `json:"bytes"`
				BitsPerSecond float64 `json:"bits_per_second"`
				Sender        bool    `json:"sender"`
			} `json:"receiver"`
		} `json:"streams"`
		SumSent struct {
			Start         float64 `json:"start"`
			End           float64 `json:"end"`
			Seconds       float64 `json:"seconds"`
			Bytes         int64   `json:"bytes"`
			BitsPerSecond float64 `json:"bits_per_second"`
			Retransmits   int     `json:"retransmits"`
			Sender        bool    `json:"sender"`
		} `json:"sum_sent"`
		SumReceived struct {
			Start         float64 `json:"start"`
			End           float64 `json:"end"`
			Seconds       float64 `json:"seconds"`
			Bytes         int64   `json:"bytes"`
			BitsPerSecond float64 `json:"bits_per_second"`
			Sender        bool    `json:"sender"`
		} `json:"sum_received"`
		CPUUtilizationPercent struct {
			HostTotal    float64 `json:"host_total"`
			HostUser     float64 `json:"host_user"`
			HostSystem   float64 `json:"host_system"`
			RemoteTotal  float64 `json:"remote_total"`
			RemoteUser   float64 `json:"remote_user"`
			RemoteSystem float64 `json:"remote_system"`
		} `json:"cpu_utilization_percent"`
		SenderTCPCongestion   string `json:"sender_tcp_congestion"`
		ReceiverTCPCongestion string `json:"receiver_tcp_congestion"`
	} `json:"end"`
}

// Results holds the results returned from iperf
type Results struct {
	Direct struct {
		Retries        int     `json:"retries,omitempty"`
		Throughput     float64 `json:"throughput,omitempty"`
		ThroughputUnit string  `json:"throughput_unit,omitempty"`
	} `json:"direct,omitempty"`
	Service struct {
		Retries        int     `json:"retries,omitempty"`
		Throughput     float64 `json:"throughput,omitempty"`
		ThroughputUnit string  `json:"throughput_unit,omitempty"`
	} `json:"service,omitempty"`
	External struct {
		Retries        int     `json:"retries,omitempty"`
		Throughput     float64 `json:"throughput,omitempty"`
		ThroughputUnit string  `json:"throughput_unit,omitempty"`
	} `json:"external,omitempty"`
}

// ResultSummary holds a statistical summary of the results
type ResultSummary struct {
	Retries struct {
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

// RunIperfTests runs the iperf test, to pod and to service
func RunIperfTests(ctx context.Context, clients config.Clients, testDuration int, namespace string, perfCfg config.PerfConfig) (*Results, error) {
	log.Debug("Entering runIperfTests function")
	results := Results{}
	var err error
	var testpods []corev1.Pod

	testpods, err = utils.WaitForTestPods(ctx, clients, namespace, "app=iperf")
	if err != nil {
		log.WithError(err).Error("failed to get testpods")
		return &results, err
	}
	if len(testpods) < 2 {
		return &results, fmt.Errorf("expected at least 2 iperf pods, got %d", len(testpods))
	}
	if perfCfg.Direct {
		log.Info("Running pod-pod iperf test")
		podIP := testpods[0].Status.PodIP
		results.Direct.Retries, results.Direct.Throughput, results.Direct.ThroughputUnit, err = runIperfTest(ctx, clients, &testpods[1], podIP, perfCfg.TestPort, testDuration)
		if err != nil {
			log.WithError(err).Error("error hit running pod-pod iperf test")
			return &results, err
		}
	}

	if perfCfg.Service {
		log.Info("Running pod-svc-pod iperf test")
		svcname := fmt.Sprintf("iperf-srv-%s", utils.SanitizeString(testpods[0].Spec.NodeName))
		svc, err := clients.Clientset.CoreV1().Services(namespace).Get(ctx, svcname, metav1.GetOptions{})
		if err != nil {
			log.WithError(err).Errorf("failed to list services in ns %s", namespace)
			return &results, err
		}
		svcIP := svc.Spec.ClusterIP
		results.Service.Retries, results.Service.Throughput, results.Service.ThroughputUnit, err = runIperfTest(ctx, clients, &testpods[1], svcIP, perfCfg.TestPort, testDuration)
		if err != nil {
			log.WithError(err).Error("error hit running pod-svc-pod iperf test")
			return &results, err
		}
	}
	if perfCfg.External {
		log.Info("Running ext-svc-pod iperf test")
		results.External.Retries, results.External.Throughput, results.External.ThroughputUnit, err = runIperfTest(ctx, clients, nil, perfCfg.ExternalIPOrFQDN, perfCfg.TestPort, testDuration)
		if err != nil {
			log.WithError(err).Error("error hit running external-svc-pod iperf test")
			return &results, err
		}
	}
	return &results, nil
}

// RunIperfTest starts the iperf test
func runIperfTest(ctx context.Context, clients config.Clients, srcPod *corev1.Pod, targetIP string, port int, testDuration int) (int, float64, string, error) {
	log.Debug("Entering runIperfTest function")

	cmd := fmt.Sprintf("iperf3 -c %s -P 8 -J -t %d -p %d", targetIP, testDuration, port)
	var stdout string
	var stderr string
	var err error
	if srcPod != nil {
		stdout, stderr, err = utils.RetryinPod(ctx, clients, srcPod, cmd, testDuration+30)
		log.Debug("stdout: ", stdout)
		log.Debug("stderr: ", stderr)
		if err != nil {
			return 0, 0, "", fmt.Errorf("failed to run iperf command")
		}
	} else {
		// srcPod is nil, so we're running the test from this host
		stdout, stderr, err = utils.Shellout(cmd, 5) //  retry a few times, since one is unreliable
		log.Debug("stdout: ", stdout)
		log.Debug("stderr: ", stderr)
		if err != nil {
			return 0, 0, "", fmt.Errorf("%s", stderr)
		}
	}
	retransmits, throughput, unit, err := parseIperfOutput(stdout)
	log.Infof("retransmits: %d throughput: %.1f, unit: %s", retransmits, throughput, unit)
	return retransmits, throughput, unit, err
}

// DeployIperfPods deploys iperf pods and services
func DeployIperfPods(ctx context.Context, clients config.Clients, namespace string, hostnet bool, image string, port int) error {
	log.Debug("Entering deployIperfPods function")
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
			podname := fmt.Sprintf("iperf-srv-%s", nodename)
			cmd := fmt.Sprintf("iperf3 -s -p %d", port)
			pod := makePod(nodename, namespace, podname, hostnet, image, cmd, port)
			_, err = utils.GetOrCreatePod(ctx, clients, pod)
			if err != nil {
				log.WithError(err).Error("error making iperf pod")
				return err
			}
			// We're creating a service per pod, so that we can select the node we run the test between.
			svc := makeSvc(namespace, podname, port)
			_, err = utils.GetOrCreateSvc(ctx, clients, svc)
			if err != nil {
				log.WithError(err).Error("error making iperf svc")
				return err
			}
		}
	}
	return nil
}

func parseIperfOutput(stdout string) (int, float64, string, error) {
	log.Debug("Entering parseIperfOutput function")

	iperfReport := iperfReport{}
	err := json.Unmarshal([]byte(stdout), &iperfReport)
	if err != nil {
		return 0, 0, "", fmt.Errorf("failed to unmarshal iperf data")
	}
	retransmits := iperfReport.End.SumSent.Retransmits
	bps := iperfReport.End.SumReceived.BitsPerSecond

	throughput := bps / 1e6
	log.Debugf("retries: %d", retransmits)
	log.Debugf("throughput: %.3f Mbits/sec", throughput)

	return retransmits, throughput, "Mbits/sec", nil
}

func makePod(nodename string, namespace string, podname string, hostnetwork bool, image string, command string, port int) corev1.Pod {
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

func makeSvc(namespace string, podname string, port int) corev1.Service {
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

// SummarizeResults converts a list of results into the statistical summary of the results
func SummarizeResults(results []*Results) (*ResultSummary, error) {
	var resultSummary ResultSummary
	var directRetries []float64
	var directThroughputs []float64
	var serviceRetries []float64
	var serviceThroughputs []float64
	var externalRetries []float64
	var externalThroughputs []float64

	for _, result := range results {
		if result.Direct.Retries != 0 || result.Direct.Throughput != 0 || result.Direct.ThroughputUnit != "" {
			directRetries = append(directRetries, float64(result.Direct.Retries))
			if result.Direct.ThroughputUnit != "Mbits/sec" {
				log.Errorf("unknown direct throughput unit: %s", result.Direct.ThroughputUnit)
				return &resultSummary, fmt.Errorf("unknown direct throughput unit: %s", result.Direct.ThroughputUnit)
			}
			directThroughputs = append(directThroughputs, result.Direct.Throughput)
		}
		if result.Service.Retries != 0 || result.Service.Throughput != 0 || result.Service.ThroughputUnit != "" {
			serviceRetries = append(serviceRetries, float64(result.Service.Retries))
			if result.Service.ThroughputUnit != "Mbits/sec" {
				log.Errorf("unknown service throughput unit: %s", result.Service.ThroughputUnit)
				return &resultSummary, fmt.Errorf("unknown service throughput unit: %s", result.Service.ThroughputUnit)
			}
			serviceThroughputs = append(serviceThroughputs, result.Service.Throughput)
		}
		if result.External.Retries != 0 || result.External.Throughput != 0 || result.External.ThroughputUnit != "" {
			externalRetries = append(externalRetries, float64(result.External.Retries))
			if result.External.ThroughputUnit != "Mbits/sec" {
				log.Errorf("unknown external throughput unit: %s", result.External.ThroughputUnit)
				return &resultSummary, fmt.Errorf("unknown external throughput unit: %s", result.External.ThroughputUnit)
			}
			externalThroughputs = append(externalThroughputs, result.External.Throughput)
		}
	}
	var err error
	if len(directThroughputs) > 0 {
		resultSummary.Throughput.Direct.Unit = "Mb/sec"
		resultSummary.Retries.Direct, err = stats.SummarizeResults(directRetries)
		if err != nil {
			log.Warning("failed to summarize direct retries")
			return &resultSummary, err
		}
		resultSummary.Retries.Direct.Unit = "none"
		resultSummary.Throughput.Direct, err = stats.SummarizeResults(directThroughputs)
		if err != nil {
			log.Warning("failed to summarize direct throughput")
			return &resultSummary, err
		}
	}
	if len(serviceThroughputs) > 0 {
		resultSummary.Throughput.Service.Unit = "Mb/sec"
		resultSummary.Retries.Service, err = stats.SummarizeResults(serviceRetries)
		if err != nil {
			log.Warning("failed to summarize service retries")
			return &resultSummary, err
		}

		resultSummary.Retries.Service.Unit = "none"
		resultSummary.Throughput.Service, err = stats.SummarizeResults(serviceThroughputs)
		if err != nil {
			log.Warning("failed to summarize service throughput")
			return &resultSummary, err

		}
		resultSummary.Throughput.Service.Unit = "Mb/sec"
	}
	if len(externalThroughputs) > 0 {
		resultSummary.Throughput.External.Unit = "Mb/sec"
		resultSummary.Retries.External, err = stats.SummarizeResults(externalRetries)
		if err != nil {
			log.Warning("failed to summarize external retries")
			return &resultSummary, err
		}

		resultSummary.Retries.External.Unit = "none"
		resultSummary.Throughput.External, err = stats.SummarizeResults(externalThroughputs)
		if err != nil {
			log.Warning("failed to summarize external throughput")
			return &resultSummary, err

		}
		resultSummary.Throughput.External.Unit = "Mb/sec"
	}
	return &resultSummary, nil
}
