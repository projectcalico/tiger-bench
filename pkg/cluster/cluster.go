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

package cluster

import (
	"context"
	"fmt"
	"net"
	"strings"
	"sync"
	"time"

	"github.com/projectcalico/tiger-bench/pkg/config"
	"github.com/projectcalico/tiger-bench/pkg/policy"
	"github.com/projectcalico/tiger-bench/pkg/utils"

	"github.com/sethvargo/go-retry"
	log "github.com/sirupsen/logrus"
	v3 "github.com/tigera/api/pkg/apis/projectcalico/v3"
	operatorv1 "github.com/tigera/operator/api/v1"
	v1 "github.com/tigera/operator/api/v1"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	ctrlclient "sigs.k8s.io/controller-runtime/pkg/client"
)

// ConfigureCluster reconfigures the cluster, changing the encapsulation and dataplane.
func ConfigureCluster(ctx context.Context, cfg config.Config, clients config.Clients, testConfig config.TestConfig) error {
	// configure the cluster
	log.Debug("entering configureCluster function")
	err := updateEncap(ctx, cfg, clients, testConfig.Encap)
	if err != nil {
		return fmt.Errorf("failed to update encapsulation")
	}
	if testConfig.Dataplane == config.DataPlaneBPF {
		err = enableBPF(ctx, cfg, clients)
		if err != nil {
			return fmt.Errorf("failed to enable BPF")
		}
	} else if testConfig.Dataplane == config.DataPlaneIPTables {
		err = enableIptables(ctx, clients)
		if err != nil {
			return fmt.Errorf("failed to enable iptables")
		}
	} else if testConfig.Dataplane == config.DataPlaneUnset {
		log.Info("No dataplane specified, using whatever is already set")
	} else {
		return fmt.Errorf("invalid dataplane requested: %s", testConfig.Dataplane)
	}

	if testConfig.TestKind == config.TestKindDNSPerf {
		if testConfig.DNSPerf.Mode != config.DNSPerfModeUnset {
			err = patchFelixConfig(ctx, clients, testConfig)
			if err != nil {
				return fmt.Errorf("failed to patch felixconfig")
			}
		} else {
			log.Warn("No DNSPerfMode specified, using whatever is already set")
		}
	}
	if testConfig.CalicoNodeCPULimit != "" {
		err = SetCalicoNodeCPULimit(ctx, clients, testConfig.CalicoNodeCPULimit)
		if err != nil {
			return fmt.Errorf("failed to set calico-node CPU limit")
		}
	} else {
		log.Warn("No CalicoNodeCPULimit specified, using whatever is already set")
	}
	return nil
}

// patchFelixConfig patches the felixconfig to use the specified DNS policy mode.
func patchFelixConfig(ctx context.Context, clients config.Clients, testConfig config.TestConfig) error {

	felixconfig := &v3.FelixConfiguration{}
	err := clients.CtrlClient.Get(ctx, ctrlclient.ObjectKey{Name: "default"}, felixconfig)
	if err != nil {
		return fmt.Errorf("failed to get felixconfig")
	}
	log.Debug("felixconfig is", felixconfig)
	dnsPolicyMode := testConfig.DNSPerf.Mode
	// patching felixconfig to use DNS policy mode
	log.Infof("Patching felixconfig to use %s dnspolicymode", dnsPolicyMode)
	v3PolicyMode := v3.DNSPolicyModeNoDelay
	if dnsPolicyMode == "DelayDNSResponse" {
		v3PolicyMode = v3.DNSPolicyModeDelayDNSResponse
	} else if dnsPolicyMode == "DelayDeniedPacket" {
		v3PolicyMode = v3.DNSPolicyModeDelayDeniedPacket
	}
	// Waiting on the API repo update to add this.
	/* else if dnsPolicyMode == "Inline" {
		v3PolicyMode = v3.DNSPolicyModeInline
	} */
	if testConfig.Dataplane == "iptables" {
		felixconfig.Spec.DNSPolicyMode = &v3PolicyMode
	}
	// Waiting on the API repo update to add this.
	/* else if testConfig.Dataplane == "bpf" {
		felixconfig.Spec.BPFDNSPolicyMode = &v3PolicyMode
	} */
	err = clients.CtrlClient.Update(ctx, felixconfig)

	return err
}

// SetCalicoNodeCPULimit sets the CPU limit for calico-node pods. 0 means remove the limit.
func SetCalicoNodeCPULimit(ctx context.Context, clients config.Clients, limit string) error {
	// set calico-node CPU limit
	log.Debug("entering setCalicoNodeCPULimit function")
	log.Infof("Setting calico-node CPU limit to %s", limit)

	// kubectl patch installations default --type=merge --patch='{"spec": {"calicoNodeDaemonSet":{"spec": {"template": {"spec": {"containers":[{"name":"calico-node","resources":{"requests":{"cpu":"100m", "memory":"100Mi"}, "limits":{"cpu":"1", "memory":"1000Mi"}}}]}}}}}}'

	installation := &operatorv1.Installation{}
	err := clients.CtrlClient.Get(ctx, ctrlclient.ObjectKey{Name: "default"}, installation)
	if err != nil {
		log.Warning("failed to get installation")
		return err
	}
	log.Debugf("installation is %#v", installation)

	cpuLimit, err := resource.ParseQuantity(limit)
	if err != nil {
		log.Warning("failed to parse CPU limit")
		return err
	}

	// Do the quick checks to see if we need to do anything.
	if limit == "0" && installation.Spec.CalicoNodeDaemonSet == nil {
		log.Info("Calico-node CPU limit already removed")
		return nil
	} else if installation.Spec.CalicoNodeDaemonSet != nil {
		for _, container := range installation.Spec.CalicoNodeDaemonSet.Spec.Template.Spec.Containers {
			if container.Name == "calico-node" {
				if container.Resources.Limits.Cpu().String() == cpuLimit.String() {
					log.Infof("Calico-node CPU limit already set to %s", cpuLimit.String())
					return nil
				}
			}
		}
	}

	// OK, so we've got this far, we need to update the installation.
	if limit == "0" {
		installation.Spec.CalicoNodeDaemonSet = nil
	} else {
		installation.Spec.CalicoNodeDaemonSet = &v1.CalicoNodeDaemonSet{
			Spec: &v1.CalicoNodeDaemonSetSpec{
				Template: &v1.CalicoNodeDaemonSetPodTemplateSpec{
					Spec: &v1.CalicoNodeDaemonSetPodSpec{
						Containers: []v1.CalicoNodeDaemonSetContainer{
							{
								Name: "calico-node",
								Resources: &corev1.ResourceRequirements{
									Limits: corev1.ResourceList{
										corev1.ResourceCPU: cpuLimit,
									},
								},
							},
						},
					},
				},
			},
		}
	}

	err = clients.CtrlClient.Update(ctx, installation)
	if err != nil {
		log.Warning("error updating installation")
		return err
	}

	// delete all calico-node pods so they all restart in parallel, since this is going to be slow if they update one-by-one
	err = utils.DeletePodsWithLabel(ctx, clients, "calico-system", "k8s-app=calico-node")
	if err != nil {
		log.Warning("failed to delete calico-node pods")
		// we're not going to return an error here, since the pods will eventually restart, just slower
	}

	err = waitForTigeraStatus(ctx, clients)
	if err != nil {
		return fmt.Errorf("error waiting for tigera status")
	}
	return err
}

// Details is a struct that contains details about the cluster for reporting with results
type Details struct {
	Cloud            string
	Provisioner      string
	NodeType         string
	NodeOS           string
	NodeKernel       string
	NodeArch         string
	NumNodes         int
	Dataplane        string
	IPFamily         string
	Encapsulation    string
	WireguardEnabled bool
	Product          string
	CalicoVersion    string
	K8SVersion       string
	CRIVersion       string
	CNIOption        string
}

// GetClusterDetails gets details about the cluster
func GetClusterDetails(ctx context.Context, clients config.Clients) (Details, error) {
	log.Debug("entering getClusterDetails function")
	details := Details{}

	clusterinfo := &v3.ClusterInformation{}
	if err := retry.Fibonacci(ctx, 1*time.Second, func(ctx context.Context) error {
		if err := clients.CtrlClient.Get(ctx, ctrlclient.ObjectKey{Name: "default"}, clusterinfo); err != nil {
			if ctx.Err() != nil {
				return ctx.Err()
			}
			// This marks the error as retryable
			return retry.RetryableError(err)
		}
		return nil
	}); err != nil {
		return details, fmt.Errorf("failed to get cluster information")
	}
	log.Debug("cluster information is", clusterinfo)

	// Example Felixconfig
	/*
		$ kubectl get felixconfig default -o yaml
		apiVersion: projectcalico.org/v3
		kind: FelixConfiguration
		metadata:
		annotations:
			operator.tigera.io/bpfEnabled: "true"
		creationTimestamp: "2024-10-11T09:28:02Z"
		generation: 1
		name: default
		resourceVersion: "6916"
		uid: 10b5470e-cc7d-4888-b669-e576b4906708
		spec:
		bpfConnectTimeLoadBalancing: TCP
		bpfEnabled: true
		bpfHostNetworkedNATWithoutCTLB: Enabled
		floatingIPs: Disabled
		healthPort: 9099
		logSeverityScreen: Info
		nftablesMode: Disabled
		prometheusMetricsEnabled: true
		reportingInterval: 0s
		vxlanVNI: 4096
	*/

	felixconfig := &v3.FelixConfiguration{}
	if err := retry.Fibonacci(ctx, 1*time.Second, func(ctx context.Context) error {
		if err := clients.CtrlClient.Get(ctx, ctrlclient.ObjectKey{Name: "default"}, felixconfig); err != nil {
			if ctx.Err() != nil {
				return ctx.Err()
			}
			// This marks the error as retryable
			return retry.RetryableError(err)
		}
		return nil
	}); err != nil {
		return details, fmt.Errorf("failed to get felixconfig")
	}
	log.Debug("felixconfig is", felixconfig)
	installation := &operatorv1.Installation{}
	if err := retry.Fibonacci(ctx, 1*time.Second, func(ctx context.Context) error {
		if err := clients.CtrlClient.Get(ctx, ctrlclient.ObjectKey{Name: "default"}, installation); err != nil {
			if ctx.Err() != nil {
				return ctx.Err()
			}
			// This marks the error as retryable
			return retry.RetryableError(err)
		}
		return nil
	}); err != nil {
		// At present we're not supporting manifest installs
		return details, fmt.Errorf("failed to get installation")
	}
	log.Debug("installation is", installation)
	nodelist := &corev1.NodeList{}
	if err := retry.Fibonacci(ctx, 1*time.Second, func(ctx context.Context) error {
		if err := clients.CtrlClient.List(ctx, nodelist); err != nil {
			if ctx.Err() != nil {
				return ctx.Err()
			}
			// This marks the error as retryable
			return retry.RetryableError(err)
		}
		return nil
	}); err != nil {
		return details, fmt.Errorf("failed to list nodes")
	}
	var testnode corev1.Node
	for _, node := range nodelist.Items {
		if node.Labels["tigera.io/test-nodepool"] == "default-pool" {
			log.Debugf("found test node: %s", node.ObjectMeta.Name)
			testnode = node
			break
		}
	}

	// Get CalicoNode
	calicoNodeDS := &appsv1.DaemonSet{}
	if err := retry.Fibonacci(ctx, 1*time.Second, func(ctx context.Context) error {
		if err := clients.CtrlClient.Get(ctx, ctrlclient.ObjectKey{Name: "calico-node", Namespace: "calico-system"}, calicoNodeDS); err != nil {
			if ctx.Err() != nil {
				return ctx.Err()
			}
			// This marks the error as retryable
			return retry.RetryableError(err)
		}
		return nil
	}); err != nil {
		return details, fmt.Errorf("failed to get calico-node daemonset")
	}

	// Assemble Details from the above objects

	if clusterinfo.Spec.CNXVersion != "" {
		details.Product = "calient"
	}

	if *installation.Status.Computed.CalicoNetwork.LinuxDataplane == "BPF" {
		details.Dataplane = "bpf"
	} else if *installation.Status.Computed.CalicoNetwork.LinuxDataplane == "Iptables" {
		details.Dataplane = "iptables"
	} else if *installation.Status.Computed.CalicoNetwork.LinuxDataplane == "VPP" {
		details.Dataplane = "vpp"
	} else {
		details.Dataplane = "unknown"
	}
	details.IPFamily = ""
	for _, pool := range installation.Spec.CalicoNetwork.IPPools {
		ipAddr, _, err := net.ParseCIDR(pool.CIDR)
		if err != nil {
			return details, fmt.Errorf("failed to parse CIDR")
		}
		if details.IPFamily == "" { // first pool
			if ipAddr.To4() == nil {
				details.IPFamily = "ipv6"
			} else {
				details.IPFamily = "ipv4"
			}
		} else if details.IPFamily == "ipv4" && ipAddr.To4() == nil { // subsequent pool, where first was ipv4
			details.IPFamily = "dual"
		} else if details.IPFamily == "ipv6" && ipAddr.To4() != nil { // subsequent pool, where first was ipv6
			details.IPFamily = "dual"
		}
	}
	details.Encapsulation = installation.Spec.CalicoNetwork.IPPools[0].Encapsulation.String()
	// details.NFTablesMode = felixconfig.Spec.NFTablesMode
	details.WireguardEnabled = false
	if felixconfig.Spec.WireguardEnabled != nil {
		details.WireguardEnabled = *felixconfig.Spec.WireguardEnabled
	}
	if installation.Status.Variant == "Calico" {
		details.Product = "calico"
	} else if installation.Status.Variant == "TigeraSecureEnterprise" {
		details.Product = "calient"
	} else {
		details.Product = "unknown"
	}

	details.NodeType = testnode.Status.NodeInfo.OperatingSystem
	details.NodeOS = testnode.Status.NodeInfo.OSImage
	details.NodeKernel = testnode.Status.NodeInfo.KernelVersion
	details.NodeArch = testnode.Status.NodeInfo.Architecture
	details.NumNodes = len(nodelist.Items)

	details.CalicoVersion = installation.Status.CalicoVersion
	details.K8SVersion = testnode.Status.NodeInfo.KubeletVersion
	details.CRIVersion = testnode.Status.NodeInfo.ContainerRuntimeVersion

	details.CNIOption = installation.Spec.CNI.Type.String()

	if strings.Contains(testnode.Name, "ip") && strings.Contains(testnode.Name, "compute.internal") {
		details.Cloud = "aws"
	} else { // TODO: Add more cloud providers
		details.Cloud = "unknown"
	}

	if strings.Contains(clusterinfo.Spec.ClusterType, "kubeadm") {
		details.Provisioner = "kubeadm"
	} else { // TODO: Add more provisioners
		details.Provisioner = "unknown"
	}

	log.Debugf("Cluster details: %+v", details)

	return details, nil
}

// SetupStandingConfig sets up standing config in the cluster, if needed
func SetupStandingConfig(ctx context.Context, clients config.Clients, testConfig config.TestConfig, namespace string, webServerImage string) error {
	log.Debug("entering setupStandingConfig function")
	// Deploy policies
	err := policy.DeployPolicies(ctx, clients, testConfig.NumPolicies, namespace)
	if err != nil {
		return err
	}
	// Deploy pods
	deployment := makeDeployment(namespace, "standing-deployment", int32(testConfig.NumPods), false, webServerImage, []string{})
	deployment, err = utils.GetOrCreateDeployment(ctx, clients, deployment)
	if err != nil {
		return err
	}
	err = utils.ScaleDeployment(ctx, clients, deployment, int32(testConfig.NumPods)) // When deployment exists but isn't scaled right, this might be needed.
	if err != nil {
		return err
	}
	//wait for pods to deploy
	log.Info("Waiting for all pods to be running")
	err = utils.WaitForDeployment(ctx, clients, deployment)
	if err != nil {
		return fmt.Errorf("error waiting for pods to deploy in standing-deployment")
	}

	// Deploy services
	// start by making a 10-pod deployment to back the services
	deployment = makeDeployment(namespace, "standing-svc", 10, false, webServerImage, []string{})
	deployment, err = utils.GetOrCreateDeployment(ctx, clients, deployment)
	if err != nil {
		return fmt.Errorf("error creating deployment standing-svc")
	}
	err = utils.ScaleDeployment(ctx, clients, deployment, 10) // When deployment exists but isn't scaled right, this might be needed.
	if err != nil {
		return fmt.Errorf("error scaling deployment standing-svc")
	}
	//wait for pods to deploy
	log.Info("Waiting for all pods to be running")
	err = utils.WaitForDeployment(ctx, clients, deployment)
	if err != nil {
		return fmt.Errorf("error waiting for pods to deploy in standing-svc")
	}
	// Spin up a channel with multiple threads to create services, because a single thread is limited to 5 actions per second
	const numThreads = 10
	var wg sync.WaitGroup
	errors := make([]error, testConfig.NumServices)
	sem := make(chan struct{}, numThreads)
	for svcIdx := range testConfig.NumServices {
		svc := makeSvc(namespace, "standing-svc", fmt.Sprintf("standing-svc-%.5d", svcIdx))
		if svcIdx%100 == 0 || svcIdx >= testConfig.NumServices-1 {
			log.Infof("Creating service %d", svcIdx)
		}
		sem <- struct{}{}
		wg.Add(1)
		go func() {
			defer wg.Done()
			defer func() { <-sem }()
			_, errors[svcIdx] = utils.GetOrCreateSvc(ctx, clients, svc)
		}()
	}
	wg.Wait()
	// we now have a list of errors, let's coalesce them into a single error
	log.Debugf("errors: %v+", errors)
	var overallError error
	for _, err := range errors {
		if err != nil {
			overallError = err
			break
		}
	}
	log.Debugf("overallError: %v", overallError)
	return overallError
}
