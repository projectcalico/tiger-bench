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

package dnsperf

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode"

	log "github.com/sirupsen/logrus"

	"github.com/projectcalico/tiger-bench/pkg/config"
	"github.com/projectcalico/tiger-bench/pkg/stats"
	"github.com/projectcalico/tiger-bench/pkg/utils"

	v3 "github.com/tigera/api/pkg/apis/projectcalico/v3"
	"github.com/tigera/api/pkg/lib/numorstring"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	ctrlclient "sigs.k8s.io/controller-runtime/pkg/client"
)

// CurlResult holds the result from a single curl
type CurlResult struct {
	Target      string  `json:"target,omitempty"`
	LookupTime  float64 `json:"time_lookup,omitempty"`
	ConnectTime float64 `json:"time_connect,omitempty"`
	Success     bool
}

// Results holds the results from this test
type Results struct {
	LookupTime      stats.ResultSummary
	ConnectTime     stats.ResultSummary
	DuplicateSYN    int
	DuplicateSYNACK int
	FailedCurls     int
	SuccessfulCurls int
}

// MakeDNSPolicy Makes a large DNS policy with a fixed bunch of domains and some generated ones
func MakeDNSPolicy(namespace string, name string, numDomains int, targetDomain string) v3.NetworkPolicy {
	udp := numorstring.ProtocolFromString("UDP")
	orderOne := float64(1)

	var testdomains = []string{
		targetDomain,
	}

	// Add the real domain we need to test against
	domains := make([]string, len(testdomains))
	copy(domains, testdomains)

	// And then a lot of fake domains which we won't test, but do add to the policy
	numAdditionalDomains := numDomains - len(testdomains)
	if numAdditionalDomains < 0 {
		log.Warnf("Cannot create a DNS policy with fewer than %d domains.", len(testdomains))
		numAdditionalDomains = 0
	}
	for i := 0; i < numAdditionalDomains; i++ {
		domains = append(domains, "www."+utils.RandomString(10)+".com")
	}

	return v3.NetworkPolicy{
		ObjectMeta: metav1.ObjectMeta{
			Name:      fmt.Sprintf("default.%s", name),
			Namespace: namespace,
		},
		Spec: v3.NetworkPolicySpec{
			Order:    &orderOne,
			Tier:     "default",
			Selector: "dep == 'dnsperf'",
			Types:    []v3.PolicyType{"Egress"},
			Egress: []v3.Rule{
				{
					Action:   v3.Allow,
					Protocol: &udp,
					Destination: v3.EntityRule{
						Ports: []numorstring.Port{
							numorstring.SinglePort(53),
							numorstring.NamedPort("dns"),
						},
					},
				},
				{
					Action: "Allow",
					Destination: v3.EntityRule{
						Domains: domains,
					},
				},
			},
		},
	}
}

// RunDNSPerfTests runs a DNS performance test
func RunDNSPerfTests(ctx context.Context, clients config.Clients, testConfig *config.TestConfig, webServerImage string, perfImage string) (*Results, error) {

	var results Results
	log.Debug("entering RunDNSPerfTests function")
	var scaleDep appsv1.Deployment
	if testConfig.DNSPerf.RunStress {
		// setup a deployment to scale up and down repeatedly (to eat felix cpu)
		var err error
		scaleDep, err = utils.GetOrCreateDeployment(ctx, clients,
			makeDeployment(
				testConfig.TestNamespace,
				"dnsscale",
				int32(0),
				false,
				[]string{"default-pool"},
				webServerImage,
				[]string{"sh", "-c", "while true; do echo `date`: MARK; sleep 10; done"},
			),
		)
		if err != nil {
			return &results, err
		}
	}
	// setup test pods (daemonset)
	testpods, err := DeployDNSPerfPods(ctx, clients, false, "dnsperf", testConfig.TestNamespace, perfImage)
	if err != nil {
		return &results, err
	}

	// setup tcpdump on nodes only if TestDNSPolicy is enabled (deploy network tools as host-networked daemonset, figure out main interface, run tcpdump)
	var tcpdumppods []corev1.Pod
	if testConfig.DNSPerf.TestDNSPolicy {
		tcpdumppods, err = DeployDNSPerfPods(ctx, clients, true, "tcpdump", testConfig.TestNamespace, perfImage)
		if err != nil {
			return &results, err
		}
	}

	var testdomains []string
	if testConfig.DNSPerf.TargetDomain != "" {
		log.Infof("Using external target URL: %s", testConfig.DNSPerf.TargetDomain)
		testdomains = []string{testConfig.DNSPerf.TargetDomain}
	} else {
		log.Error("TargetDomain is required but not specified")
		return &results, fmt.Errorf("TargetDomain is required for DNS performance tests")
	}

	err = checkTestPods(ctx, clients, testpods)
	if err != nil {
		return &results, err
	}

	testctx, cancel := context.WithTimeout(ctx, time.Duration(testConfig.Duration)*time.Second)
	defer cancel()
	log.Debugf("Created test context: %+v", testctx)

	if testConfig.DNSPerf.TestDNSPolicy {
		// kick off per-node threads to run tcpdump
		for i, pod := range tcpdumppods {
			go func(idx int, tcpdumpPod corev1.Pod) {
				if e := runTCPDump(testctx, clients, &tcpdumpPod, testpods[idx], testConfig.Duration+60); e != nil {
					log.WithError(e).Error("failed to run tcpdump")
				}
			}(i, pod)
		}
		log.Info("tcpdump threads started")
	}

	if testConfig.DNSPerf.RunStress {
		go scaleDeploymentLoop(testctx, clients, scaleDep, int32(24), 10*time.Second)
	}

	// kick off per-node threads to run curl commands
	resultsChan := make(chan CurlResult, len(testpods)*10)
	var wg sync.WaitGroup

	// Launch worker goroutines that send results to channel
	for _, pod := range testpods {
		wg.Add(1)
		go func(testPod corev1.Pod) {
			defer wg.Done()
			i := 0
			for {
				domain := testdomains[i%(len(testdomains))]
				result, err := runDNSPerfTest(testctx, &testPod, domain)
				if testctx.Err() != nil {
					// Probably ctx expiry or cancellation, don't append result or log errors in this case
					break
				}
				if err != nil {
					log.WithError(err).Errorf("failed to run curl to %s", domain)
				} else if result.Success {
					// Since Connectime includes LookupTime, we need to subtract LookupTime from ConnectTime to get the actual connect time
					result.ConnectTime = result.ConnectTime - result.LookupTime
				}
				log.Debugf("sending result: %+v", result)
				resultsChan <- result
				log.Debugf("current test context: %+v", testctx)
				i++
			}
		}(pod)
	}

	// Close channel when all workers are done
	go func() {
		wg.Wait()
		close(resultsChan)
	}()

	// Aggregate results from channel in single goroutine
	var rawresults []CurlResult
	for result := range resultsChan {
		rawresults = append(rawresults, result)
	}

	log.Debugf("rawresults: %+v", rawresults)
	results = processResults(rawresults)

	if testConfig.DNSPerf.TestDNSPolicy {
		// add up the duplicate SYN numbers from each tcpdump pod
		results.DuplicateSYN = 0
		results.DuplicateSYNACK = 0
		for _, pod := range tcpdumppods {
			duplicateSYN, duplicateSYNACK, err := countDuplicateSYN(ctx, &pod)
			if err != nil {
				return &results, err
			}
			results.DuplicateSYN += duplicateSYN
			results.DuplicateSYNACK += duplicateSYNACK
		}
	}
	log.Infof("Results: %+v", results)
	return &results, nil
}

func processResults(rawresults []CurlResult) Results {
	log.Debug("entering processResults function")
	results := Results{
		LookupTime:      stats.ResultSummary{},
		ConnectTime:     stats.ResultSummary{},
		DuplicateSYN:    0,
		FailedCurls:     0,
		SuccessfulCurls: 0,
	}

	var lookupTimes []float64
	var connectTimes []float64

	if len(rawresults) == 0 {
		return results
	}
	for _, result := range rawresults {
		if result.Success {
			// Convert rawresults to 2 slices of floats and update counts of successful/failed curls
			results.SuccessfulCurls++
			lookupTimes = append(lookupTimes, result.LookupTime)
			connectTimes = append(connectTimes, result.ConnectTime)
		} else {
			results.FailedCurls++
		}
	}
	if len(lookupTimes) == 0 {
		log.Info("No successful curls, skipping percentiles")
		return results
	}
	var err error
	results.LookupTime, err = stats.SummarizeResults(lookupTimes)
	if err != nil {
		log.WithError(err).Error("failed to summarize lookup times")
		return results
	}
	results.ConnectTime, err = stats.SummarizeResults(connectTimes)
	if err != nil {
		log.WithError(err).Error("failed to summarize connect times")
	}
	return results
}

func runDNSPerfTest(ctx context.Context, srcPod *corev1.Pod, target string) (CurlResult, error) {
	log.Debug("entering runDNSPerfTest function")
	if ctx.Err() != nil {
		log.Info("Context expired? Quitting scaleDeploymentLoop")
		return CurlResult{}, ctx.Err()
	}
	var result CurlResult
	result.Target = target
	result.Success = true
	cmdfrag := `curl -m 8 -w '{"time_lookup": %{time_namelookup}, "time_connect": %{time_connect}}\n' -s -o /dev/null`
	cmd := fmt.Sprintf("%s %s", cmdfrag, target)
	stdout, _, err := utils.ExecCommandInPod(ctx, srcPod, cmd, 10)
	if err != nil {
		if ctx.Err() == nil { // Only log error if context is still valid
			log.WithError(err).Error("failed to run curl command")
		}
		result.Success = false
	} else {
		err = json.Unmarshal([]byte(stdout), &result)
	}
	log.Infof("result: %+v", result)

	return result, err
}

func checkTestPods(ctx context.Context, clients config.Clients, testpods []corev1.Pod) error {
	log.Debug("entering checkTestPods function")
	for _, testPod := range testpods {
		for retry := 0; retry < 30; retry++ {
			// Update testPod
			err := clients.CtrlClient.Get(ctx, ctrlclient.ObjectKey{Namespace: testPod.Namespace, Name: testPod.Name}, &testPod)
			if err != nil {
				log.WithError(err).Warning("failed to get testPod")
				return err
			}
			log.Infof("testPod: %+v", testPod)

			// Check that testPod IP is sane
			if testPod.Status.PodIP == "" {
				log.Info("testPod IP is blank, rechecking testPod IP")
			} else if testPod.Status.PodIP == testPod.Status.HostIP {
				log.Info("testPod IP is the same as host IP, rechecking testPod IP")
			} else {
				log.Infof("Got testPod IP: %+v", testPod.Status.PodIP)
				break
			}
			time.Sleep(1 * time.Second)
		}
	}
	return nil
}

func runTCPDump(ctx context.Context, clients config.Clients, pod *corev1.Pod, testPod corev1.Pod, timeout int) error {
	log.Debug("entering runTCPDump function")

	err := clients.CtrlClient.Get(ctx, ctrlclient.ObjectKey{Namespace: testPod.Namespace, Name: testPod.Name}, &testPod)
	if err != nil {
		log.WithError(err).Error("failed to get testPod")
		return err
	}

	// Figure out the test pod interface
	cmd := fmt.Sprintf(`ip r | grep %s | awk '{print $3}'`, testPod.Status.PodIP)
	nic, stderr, err := utils.RetryinPod(ctx, clients, pod, cmd, 5)
	if err != nil {
		log.WithError(err).Errorf("failed to run ip route command: %s", stderr)
		return err
	}
	nic = strings.TrimSpace(nic)
	if nic == "" {
		log.Error("failed to find interface for pod - interface name is empty")
		return fmt.Errorf("could not determine interface for pod %s", testPod.Status.PodIP)
	}
	log.Infof("nic=%s", nic)

	// run tcpdump command until timeout
	cmd = fmt.Sprintf(`tcpdump -s0 -w dump.cap -i %s tcp port 80 or tcp port 443`, nic)
	var out string
	out, _, err = utils.ExecCommandInPod(ctx, pod, cmd, timeout+30)
	if err != nil {
		log.WithError(err).Error("failed to run tcpdump command")
		return err
	}
	log.Infof("stdout=%s", out)

	log.Info("exiting runTCPDump function")
	return nil
}

func countDuplicateSYN(ctx context.Context, pod *corev1.Pod) (int, int, error) {
	log.Debug("entering countDuplicateSYN function")
	var stdout, stderr string
	var err error

	for i := 0; i < 10; i++ {
		cmd := `tcpdump -n -r dump.cap`
		stdout, stderr, err := utils.ExecCommandInPod(ctx, pod, cmd, 10)

		// Check if we got reasonable tcpdump output (contains packet data)
		hasPacketData := strings.Contains(stdout, " IP ") && strings.Contains(stdout, "Flags [")

		if err == nil {
			return processTCPDumpOutput(stdout)
		} else if hasPacketData {
			// We got packet data even though there was an error (likely truncated/corrupted file at the end)
			log.WithError(err).Info("tcpdump reported errors but output contains valid packet data, processing anyway")
			return processTCPDumpOutput(stdout)
		} else {
			log.WithError(err).Infof("Hit error running command %s, retrying.  stderr: %s stdout: %s", cmd, stderr, stdout)
		}
		time.Sleep(1 * time.Second)
	}
	return 0, 0, fmt.Errorf("failed to run tcpdump command in pod %+v: %+v, %s, %s", pod.Name, err, stdout, stderr)
}

func processTCPDumpOutput(out string) (int, int, error) {
	log.Debug("entering processTCPDumpOutput function")
	// write out to a file for debugging
	err := os.WriteFile("tcpdump_output.txt", []byte(out), 0644)
	if err != nil {
		log.WithError(err).Error("failed to write tcpdump output to file")
	}

	duplicateSYN := 0
	duplicateSYNACK := 0
	scanner := bufio.NewScanner(strings.NewReader(out))

	// Track sequence numbers per TCP flow (connection tuple)
	synSeqMap := make(map[string]int)    // Map of "srcIP:srcPort->dstIP:dstPort" to last SYN seq
	synackSeqMap := make(map[string]int) // Map of "srcIP:srcPort->dstIP:dstPort" to last SYN-ACK seq

	for scanner.Scan() {
		line := scanner.Text()
		log.Debugf("line=%s", line)

		if !strings.Contains(line, "IP") || !strings.Contains(line, "Flags [S") || strings.Contains(line, "HTTP") {
			continue
		}

		// Parse connection tuple and sequence number
		// Expected format: "timestamp IP srcIP.srcPort > dstIP.dstPort: Flags [S], seq NNNN"
		tokens := strings.Split(line, " ")

		var srcAddr, dstAddr string
		var seqNum int
		foundSeq := false

		// Find source and destination addresses
		for i, token := range tokens {
			if token == "IP" && i+3 < len(tokens) {
				srcAddr = tokens[i+1]
				if tokens[i+2] == ">" {
					// Remove trailing colon from destination
					dstAddr = strings.TrimSuffix(tokens[i+3], ":")
				}
			}
			if token == "seq" && i+1 < len(tokens) {
				// Extract sequence number, removing non-numeric characters
				seqstr := tokens[i+1]
				var builder strings.Builder
				for _, r := range seqstr {
					if unicode.IsDigit(r) {
						builder.WriteRune(r)
					}
				}
				seqstr = builder.String()
				seqNum, err = strconv.Atoi(seqstr)
				if err != nil {
					log.WithError(err).Warnf("failed to parse seq number from: %s", tokens[i+1])
					continue
				}
				foundSeq = true
				break
			}
		}

		if !foundSeq || srcAddr == "" || dstAddr == "" {
			continue
		}

		// Create connection tuple as key
		connectionKey := fmt.Sprintf("%s->%s", srcAddr, dstAddr)

		if strings.Contains(line, "Flags [S.]") {
			// SYN-ACK packet
			log.Debugf("SYN-ACK on connection %s: seq=%d", connectionKey, seqNum)
			if lastSeq, exists := synackSeqMap[connectionKey]; exists && lastSeq == seqNum {
				duplicateSYNACK++
				log.Debugf("Duplicate SYN-ACK detected on %s: seq=%d", connectionKey, seqNum)
			}
			synackSeqMap[connectionKey] = seqNum
		} else if strings.Contains(line, "Flags [S]") {
			// SYN packet (but not SYN-ACK)
			log.Debugf("SYN on connection %s: seq=%d", connectionKey, seqNum)
			if lastSeq, exists := synSeqMap[connectionKey]; exists && lastSeq == seqNum {
				duplicateSYN++
				log.Debugf("Duplicate SYN detected on %s: seq=%d", connectionKey, seqNum)
			}
			synSeqMap[connectionKey] = seqNum
		}
	}

	if err := scanner.Err(); err != nil {
		log.WithError(err).Error("error reading tcpdump output")
		return duplicateSYN, duplicateSYNACK, err
	}

	log.Infof("found %d duplicate SYNs across %d connections", duplicateSYN, len(synSeqMap))
	log.Infof("found %d duplicate SYNACKs across %d connections", duplicateSYNACK, len(synackSeqMap))
	return duplicateSYN, duplicateSYNACK, nil
}

// DeployDNSPerfPods deploys a pod on each node in the cluster
func DeployDNSPerfPods(ctx context.Context, clients config.Clients, hostnet bool, name string, namespace string, perfImage string) ([]corev1.Pod, error) {
	log.Debug("entering deploydnsperfPods function")
	var podlist = []corev1.Pod{}
	if name == "" {
		name = "dnsperf"
	}

	// get node names
	nodelist := &corev1.NodeList{}
	err := clients.CtrlClient.List(ctx, nodelist)
	if err != nil {
		return podlist, fmt.Errorf("failed to list nodes")
	}
	for _, node := range nodelist.Items {
		if node.Labels["tigera.io/test-nodepool"] == "default-pool" {
			// deploy server pods
			nodename := node.ObjectMeta.Name
			log.Infof("found nodename: %s", nodename)
			podname := fmt.Sprintf("%s-%s", name, nodename)
			pod := makeDNSPerfPod(nodename, namespace, podname, perfImage, hostnet)
			pod, err = utils.GetOrCreatePod(ctx, clients, pod)
			if err != nil {
				return podlist, err
			}
			podlist = append(podlist, pod)
		}
	}
	return podlist, nil
}

func makeDNSPerfPod(nodename string, namespace string, podname string, image string, hostnetwork bool) corev1.Pod {
	podname = utils.SanitizeString(podname)
	runAsUser := int64(1000)
	runAsGroup := int64(1000)
	if hostnetwork {
		// tcpdump needs to run as root
		runAsUser = 0
		runAsGroup = 0
	}
	pod := corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Labels: map[string]string{
				"app": "dnsperf",
				"pod": podname,
				"dep": "dnsperf",
			},
			Name:      podname,
			Namespace: namespace,
		},
		Spec: corev1.PodSpec{
			AutomountServiceAccountToken: utils.BoolPtr(false),
			EnableServiceLinks:           utils.BoolPtr(false),
			SecurityContext: &corev1.PodSecurityContext{
				RunAsNonRoot: utils.BoolPtr(!hostnetwork), // tcpdump needs to run as root
				RunAsGroup:   utils.Int64Ptr(runAsGroup),
				RunAsUser:    utils.Int64Ptr(runAsUser),
				SeccompProfile: &corev1.SeccompProfile{
					Type: corev1.SeccompProfileTypeRuntimeDefault,
				},
			},
			Containers: []corev1.Container{
				{
					Name:    "dnsperf",
					Image:   image,
					Command: []string{"/bin/sh", "-c"},
					Args: []string{
						"while true; do echo `date`: MARK; sleep 10; done",
					},
					SecurityContext: &corev1.SecurityContext{
						Privileged:               utils.BoolPtr(false),
						AllowPrivilegeEscalation: utils.BoolPtr(false),
						ReadOnlyRootFilesystem:   utils.BoolPtr(false),
						Capabilities: &corev1.Capabilities{
							Drop: []corev1.Capability{"ALL"},
							Add: func() []corev1.Capability {
								if hostnetwork {
									return []corev1.Capability{"NET_RAW", "NET_ADMIN"}
								}
								return nil
							}(),
						},
					},
					ImagePullPolicy: corev1.PullIfNotPresent,
				},
			},
			NodeName:      nodename,
			RestartPolicy: "Never",
			HostNetwork:   hostnetwork,
		},
	}
	return pod
}

func makeDeployment(namespace string, depname string, replicas int32, hostnetwork bool, nodelist []string, image string, args []string) appsv1.Deployment {
	depname = utils.SanitizeString(depname)
	if len(nodelist) == 0 {
		nodelist = []string{"default-pool"}
	}
	dep := appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{
			Labels: map[string]string{
				"app": "dnsperf",
				"dep": depname,
			},
			Name:      depname,
			Namespace: namespace,
		},
		Spec: appsv1.DeploymentSpec{
			Replicas: &replicas,
			Selector: &metav1.LabelSelector{
				MatchLabels: map[string]string{
					"app": "dnsperf",
					"dep": depname,
				},
			},
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{
					Labels: map[string]string{
						"app": "dnsperf",
						"dep": depname,
					},
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
							Name:  depname,
							Image: image,
							Args:  args,
							SecurityContext: &corev1.SecurityContext{
								Privileged:               utils.BoolPtr(false),
								AllowPrivilegeEscalation: utils.BoolPtr(false),
								ReadOnlyRootFilesystem:   utils.BoolPtr(false),
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
							ImagePullPolicy: corev1.PullIfNotPresent,
						},
					},
					HostNetwork: hostnetwork,
					Affinity: &corev1.Affinity{
						NodeAffinity: &corev1.NodeAffinity{
							RequiredDuringSchedulingIgnoredDuringExecution: &corev1.NodeSelector{
								NodeSelectorTerms: []corev1.NodeSelectorTerm{
									{
										MatchExpressions: []corev1.NodeSelectorRequirement{
											{Key: "tigera.io/test-nodepool", Operator: "In", Values: nodelist}},
									},
								},
							},
						},
					},
				},
			},
		},
	}
	return dep
}

// this function scales a deployment up and down until the context expires
func scaleDeploymentLoop(ctx context.Context, clients config.Clients, deployment appsv1.Deployment, size int32, sleeptime time.Duration) {
	log.Debug("entering scaleDeployment function")
	for {
		if ctx.Err() != nil {
			log.Info("Context expired. Quitting scaleDeploymentLoop")
			return
		}
		err := utils.ScaleDeployment(ctx, clients, deployment, size)
		if err != nil {
			log.Warning("failed to scale deployment	up")
		}
		if ctx.Err() != nil {
			log.Info("Context expired. Quitting scaleDeploymentLoop")
			return
		}
		time.Sleep(sleeptime)
		if ctx.Err() != nil {
			log.Info("Context expired. Quitting scaleDeploymentLoop")
			return
		}
		err = utils.ScaleDeployment(ctx, clients, deployment, 0)
		if err != nil {
			log.Warning("failed to scale deployment	up")
		}
		if ctx.Err() != nil {
			log.Info("Context expired? Quitting scaleDeploymentLoop")
			return
		}
		time.Sleep(sleeptime)
	}
}
