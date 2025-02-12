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
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode"

	log "github.com/sirupsen/logrus"

	"github.com/projectcalico/tiger-bench/pkg/config"
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
	LookupTime      map[int]float64 // will be a set of percentiles
	ConnectTime     map[int]float64 // will be a set of percentiles
	DuplicateSYN    int
	DuplicateSYNACK int
	FailedCurls     int
	SuccessfulCurls int
}

// MakeDNSPolicy Makes a large DNS policy with a fixed bunch of domains and some generated ones
func MakeDNSPolicy(namespace string, name string, numDomains int) v3.NetworkPolicy {
	udp := numorstring.ProtocolFromString("UDP")
	orderOne := float64(1)

	var testdomains = []string{
		"*.test.pod.cluster.local",
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
func RunDNSPerfTests(ctx context.Context, clients config.Clients, testDuration int, namespace string, webServerImage string, perfImage string) (*Results, error) {

	var results Results
	log.Debug("entering RunDNSPerfTests function")
	// setup a deployment to scale up and down repeatedly (to eat felix cpu)
	scaleDep, err := utils.GetOrCreateDeployment(ctx, clients,
		makeDeployment(
			namespace,
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
	// setup test pods (daemonset)
	testpods, err := DeployDNSPerfPods(ctx, clients, false, "dnsperf", namespace, perfImage)
	if err != nil {
		return &results, err
	}
	// setup tcpdump on nodes (deploy network tools as host-networked daemonset, figure out main interface, run tcpdump)
	tcpdumppods, err := DeployDNSPerfPods(ctx, clients, true, "tcpdump", namespace, perfImage)
	if err != nil {
		return &results, err
	}
	// setup target pods
	for i := 0; i < 4; i++ {
		thisname := fmt.Sprintf("headless%d", i)
		_, err = utils.GetOrCreateDeployment(ctx, clients,
			makeDeployment(
				namespace,
				thisname,
				int32(25),
				false,
				[]string{"infrastructure"},
				webServerImage,
				[]string{},
			),
		)
		if err != nil {
			return &results, err
		}
	}

	_, err = utils.WaitForTestPods(ctx, clients, namespace, "app=dnsperf")
	if err != nil {
		return &results, err
	}

	testdomains, err := getPodFQDNs(ctx, clients, namespace)
	if err != nil {
		return &results, err
	}

	err = checkTestPods(ctx, clients, testpods)
	if err != nil {
		return &results, err
	}

	testctx, cancel := context.WithTimeout(ctx, time.Duration(testDuration)*time.Second)
	defer cancel()
	log.Debugf("Created test context: %+v", testctx)
	// kick off per-node threads to run tcpdump
	for i, pod := range tcpdumppods {
		go func() {
			err = runTCPDump(testctx, clients, &pod, testpods[i], testDuration+60)
			if err != nil {
				log.WithError(err).Error("failed to run tcpdump")
			}
		}()
	}
	log.Info("tcpdump threads started")

	go scaleDeploymentLoop(testctx, clients, scaleDep, int32(24), 10*time.Second)

	// kick off per-node threads to run curl commands
	var rawresults []CurlResult
	var wg sync.WaitGroup
	for _, pod := range testpods {
		wg.Add(1)
		go func() {
			defer wg.Done()
			i := 0
			for {
				domain := testdomains[i%(len(testdomains))]
				result, err := runDNSPerfTest(testctx, &pod, domain)
				if err != nil {
					log.WithError(err).Errorf("failed to run curl to %s", domain)
				} else if result.Success {
					// Since Connectime includes LookupTime, we need to subtract LookupTime from ConnectTime to get the actual connect time
					result.ConnectTime = result.ConnectTime - result.LookupTime
				}
				log.Infof("appending result: %+v", result)
				rawresults = append(rawresults, result)
				if testctx.Err() != nil {
					// Probably ctx expiry or cancellation
					break
				}
				log.Debugf("current test context: %+v", testctx)
				i++
			}
		}()
	}
	wg.Wait()

	log.Infof("rawresults: %+v", rawresults)
	results = processResults(rawresults)

	// add up the duplicate SYN numbers from each tcpdump pod
	results.DuplicateSYN = 0
	for _, pod := range tcpdumppods {
		duplicateSYN, duplicateSYNACK, err := countDuplicateSYN(ctx, &pod)
		if err != nil {
			return &results, err
		}
		results.DuplicateSYN += duplicateSYN
		results.DuplicateSYNACK += duplicateSYNACK
	}
	log.Infof("Results: %+v", results)
	return &results, nil
}

func getPodFQDNs(ctx context.Context, clients config.Clients, namespace string) ([]string, error) {
	log.Debug("entering getPodFQDNs function")
	var fqdns []string
	podlist := &corev1.PodList{}
	err := clients.CtrlClient.List(ctx, podlist, ctrlclient.InNamespace(namespace))
	if err != nil {
		return fqdns, fmt.Errorf("failed to list pods")
	}

	for _, pod := range podlist.Items {
		if strings.Contains(pod.ObjectMeta.Name, "headless") {
			podname := utils.SanitizeString(pod.Status.PodIP)
			fqdns = append(fqdns, fmt.Sprintf("%s.%s.pod.cluster.local", podname, namespace))
		}
	}
	log.Infof("fqdns: %+v", fqdns)
	return fqdns, nil
}

func processResults(rawresults []CurlResult) Results {
	log.Debug("entering processResults function")
	results := Results{
		LookupTime:      map[int]float64{},
		ConnectTime:     map[int]float64{},
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
	sort.Float64s(lookupTimes)
	sort.Float64s(connectTimes)

	// Now we have sorted slices, we can calculate percentiles by picking the value at the appropriate index
	// (e.g. if we had 100 results, the 50th percentile would be the value at index 50, etc.)
	percentiles := []int{50, 75, 90, 95, 99}
	results.LookupTime = make(map[int]float64)
	results.ConnectTime = make(map[int]float64)
	for _, p := range percentiles {
		results.LookupTime[p] = lookupTimes[int(float64(p)/100*float64(len(lookupTimes)))]
		log.Infof("lookupTime: %d percentile: %f", p, results.LookupTime[p])
		results.ConnectTime[p] = connectTimes[int(float64(p)/100*float64(len(connectTimes)))]
		log.Infof("connectTime: %d percentile: %f", p, results.ConnectTime[p])
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
		log.WithError(err).Error("failed to run curl command")
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
	nic = strings.Map(func(r rune) rune {
		if unicode.IsPrint(r) {
			return r
		}
		return -1
	}, nic)
	log.Infof("nic=%s", nic)

	// run tcpdump command until timeout
	cmd = fmt.Sprintf(`tcpdump -s0 -w dump.cap -i %s port 80`, nic)
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
		if err == nil {
			return processTCPDumpOutput(stdout)
		} else if strings.Contains(stdout, "truncated dump file") {
			log.Info("tcpdump file was truncated, ignoring")
			return processTCPDumpOutput(stdout)
		} else {
			log.Infof("Hit error running command, retrying: %s", stderr)
		}
		time.Sleep(1 * time.Second)
	}
	return 0, 0, fmt.Errorf("failed to run tcpdump command in pod %+v: %+v, %s, %s", pod.Name, err, stdout, stderr)
}

func processTCPDumpOutput(out string) (int, int, error) {
	log.Debug("entering processTCPDumpOutput function")
	duplicateSYN := 0
	duplicateSYNACK := 0
	scanner := bufio.NewScanner(strings.NewReader(out))
	lastSYNSeq := 0
	lastSYNACKSeq := 0

	for scanner.Scan() {
		line := scanner.Text()
		log.Infof("line=%s", line)
		if strings.Contains(line, "IP") && strings.Contains(line, "Flags [S") && !strings.Contains(line, "HTTP") {
			// store off the seq number
			tokens := strings.Split(line, " ")
			for i, token := range tokens {
				if token == "seq" {
					// clean off any non-numeric characters, e.g. commas
					seqstr := tokens[i+1]
					var builder strings.Builder
					for _, r := range seqstr {
						if unicode.IsDigit(r) {
							builder.WriteRune(r)
						}
					}
					seqstr = builder.String()
					seq, err := strconv.Atoi(seqstr)
					if err != nil {
						return 0, 0, fmt.Errorf("failed to parse seq number")
					}
					if strings.Contains(line, "Flags [S]") {
						log.Debugf("lastSYNSeq=%d  Found seq=%d", lastSYNSeq, seq)
						// We have a duplicate SYN if the seq number for this SYN packet is the same as the last one
						if lastSYNSeq == seq {
							duplicateSYN++
						}
						lastSYNSeq = seq
						break
					} else if strings.Contains(line, "Flags [S.]") {
						log.Debugf("lastSYNACKSeq=%d  Found seq=%d", lastSYNACKSeq, seq)
						// We have a duplicate SYNACK if the seq number for this SYNACK packet is the same as the last one
						if lastSYNACKSeq == seq {
							duplicateSYNACK++
						}
						lastSYNACKSeq = seq
						break
					}
				}
			}
		}
	}
	log.Infof("found %d duplicate SYNs", duplicateSYN)
	log.Infof("found %d duplicate SYNACKs", duplicateSYNACK)
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
			Containers: []corev1.Container{
				{
					Name:  "dnsperf",
					Image: image,
					Args: []string{
						"sh", "-c",
						"while true; do echo `date`: MARK; sleep 10; done",
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

// func makeHeadlessSvc(namespace string, svcname string, labels map[string]string) corev1.Service {
// 	svcname = utils.SanitizeString(svcname)
// 	return corev1.Service{
// 		ObjectMeta: metav1.ObjectMeta{
// 			Labels:    labels,
// 			Name:      svcname,
// 			Namespace: namespace,
// 		},
// 		Spec: corev1.ServiceSpec{
// 			ClusterIP: "None",
// 			Selector:  labels,
// 			Ports: []corev1.ServicePort{
// 				{
// 					Protocol:   "TCP",
// 					Port:       80,
// 					TargetPort: intstr.FromInt(80),
// 					Name:       "http",
// 				},
// 				{
// 					Protocol:   "TCP",
// 					Port:       443,
// 					TargetPort: intstr.FromInt(443),
// 					Name:       "https",
// 				},
// 			},
// 		},
// 	}
// }

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
					Containers: []corev1.Container{
						{
							Name:  depname,
							Image: image,
							Args:  args,
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
		err := utils.ScaleDeployment(ctx, clients, deployment, size)
		if err != nil {
			log.Warning("failed to scale deployment	up")
		}
		if ctx.Err() != nil {
			log.Info("Context expired? Quitting scaleDeploymentLoop")
			return
		}
		time.Sleep(sleeptime)
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
