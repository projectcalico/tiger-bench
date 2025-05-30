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
	"strconv"
	"time"

	"github.com/projectcalico/tiger-bench/pkg/config"
	"github.com/projectcalico/tiger-bench/pkg/utils"
	yaml "gopkg.in/yaml.v2"

	"github.com/sethvargo/go-retry"
	log "github.com/sirupsen/logrus"
	v3 "github.com/tigera/api/pkg/apis/projectcalico/v3"
	operatorv1 "github.com/tigera/operator/api/v1"
	"golang.org/x/mod/semver"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	ctrlclient "sigs.k8s.io/controller-runtime/pkg/client"
)

func enableNftables(ctx context.Context, clients config.Clients) error {
	// enable Nftables
	log.Debug("entering enableNftables function")
	childCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	installation := &operatorv1.Installation{}
	log.Debug("Getting installation")
	err := clients.CtrlClient.Get(childCtx, ctrlclient.ObjectKey{Name: "default"}, installation)
	if err != nil {
		return fmt.Errorf("failed to get installation")
	}
	if *installation.Spec.CalicoNetwork.LinuxDataplane == operatorv1.LinuxDataplaneNftables {
		log.Info("Nftables already enabled")
		return nil
	}

	// Is this cluster nftable ready?
	kubecm := &corev1.ConfigMap{}
	err = clients.CtrlClient.Get(childCtx, ctrlclient.ObjectKey{Namespace: "kube-system", Name: "kube-proxy"}, kubecm)
	log.Debug("ٰVerifing if kube-proxy config is available")
	if err != nil {
		return fmt.Errorf("failed to get proxymode")
	}
	configStr, ok := kubecm.Data["config.conf"]
	if !ok {
		return fmt.Errorf("config.conf not found in kube-proxy configmap")
	}

	var configMap map[string]interface{}
	err = yaml.Unmarshal([]byte(configStr), &configMap)
	if err != nil {
		return fmt.Errorf("failed to parse YAML: %w", err)
	}

	mode, ok := configMap["mode"].(string)
	if !ok {
		return fmt.Errorf("mode field not found or not a string")
	}
	log.Debug("ٰVerifing if kube-proxy is running in nftables mode")
	if mode != "nftables" {
		return fmt.Errorf("kube-proxy mode is not nftables (found: %s)", mode)
	}

	// kubectl patch installation.operator.tigera.io default --type merge -p '{"spec":{"calicoNetwork":{"linuxDataplane":"Nftables"}}}'
	patch := []byte(`{"spec":{"calicoNetwork":{"linuxDataplane":"Nftables"}}}`)

	installation = &operatorv1.Installation{}
	log.Debug("Getting installation")
	err = clients.CtrlClient.Get(childCtx, ctrlclient.ObjectKey{Name: "default"}, installation)
	if err != nil {
		return fmt.Errorf("failed to get installation")
	}
	log.Debugf("patching with %v", string(patch[:]))
	log.Info("enabling Nftables dataplane")
	err = clients.CtrlClient.Patch(childCtx, installation, ctrlclient.RawPatch(ctrlclient.Merge.Type(), patch))
	if err != nil {
		return fmt.Errorf("failed to patch installation")
	}

	// Nftables bug ?
	// kubectl patch installation.operator.tigera.io default --type merge -p '{"spec":{"calicoNetwork":{"linuxPolicySetupTimeoutSeconds":null}}}'
	time.Sleep(1000 * time.Millisecond)
	patch = []byte(`{"spec":{"calicoNetwork":{"linuxPolicySetupTimeoutSeconds":null}}}`)

	installation = &operatorv1.Installation{}
	log.Debug("Getting installation to patch linuxPolicySetupTimeoutSeconds")
	err = clients.CtrlClient.Get(childCtx, ctrlclient.ObjectKey{Name: "default"}, installation)
	if err != nil {
		return fmt.Errorf("failed to get installation")
	}
	log.Debugf("patching with %v", string(patch[:]))
	log.Info("Making sure linuxPolicySetupTimeoutSeconds is Null")
	err = clients.CtrlClient.Patch(childCtx, installation, ctrlclient.RawPatch(ctrlclient.Merge.Type(), patch))
	if err != nil {
		return fmt.Errorf("failed to patch installation")
	}

	// kubectl patch ds -n kube-system kube-proxy --type merge -p '{"spec":{"template":{"spec":{"nodeSelector":{"non-calico": null}}}}}'
	patch = []byte(`{"spec":{"template":{"spec":{"nodeSelector":{"non-calico": null}}}}}`)
	proxyds := &appsv1.DaemonSet{}
	log.Debug("Getting kube-proxy ds")
	err = clients.CtrlClient.Get(childCtx, ctrlclient.ObjectKey{Namespace: "kube-system", Name: "kube-proxy"}, proxyds)
	if err != nil {
		return fmt.Errorf("failed to get kube-proxy ds")
	}
	log.Debugf("patching with %v", string(patch[:]))
	err = clients.CtrlClient.Patch(childCtx, proxyds, ctrlclient.RawPatch(ctrlclient.Merge.Type(), patch))
	if err != nil {
		return fmt.Errorf("failed to patch kube-proxy ds")
	}

	err = waitForTigeraStatus(ctx, clients)
	if err != nil {
		return fmt.Errorf("error waiting for tigera status")
	}
	return nil
}

func enableBPF(ctx context.Context, cfg config.Config, clients config.Clients) error {
	// enable BPF
	log.Debug("entering enableBPF function")

	installation := &operatorv1.Installation{}
	log.Debug("Getting installation")
	childCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	err := clients.CtrlClient.Get(childCtx, ctrlclient.ObjectKey{Name: "default"}, installation)
	if err != nil {
		log.WithError(err).Error("failed to get installation")
		return err
	}
	if *installation.Spec.CalicoNetwork.LinuxDataplane == operatorv1.LinuxDataplaneBPF {
		log.Info("BPF already enabled")
		return nil
	}
	var host string
	var port string
	if cfg.K8sAPIHost == "" || cfg.K8sAPIPort == "" {
		// get apiserver host and port from kubernetes service endpoints
		kubesvc := &corev1.Endpoints{}
		err := clients.CtrlClient.Get(childCtx, ctrlclient.ObjectKey{Name: "kubernetes", Namespace: "default"}, kubesvc)
		if err != nil {
			log.WithError(err).Error("failed to get kubernetes service endpoints")
			return err
		}
		log.Infof("first kubernetes service endpoint IP is %v", kubesvc.Subsets[0].Addresses[0].IP)
		log.Infof("first kubernetes service endpoint port is %v", kubesvc.Subsets[0].Ports[0].Port)
		host = kubesvc.Subsets[0].Addresses[0].IP
		port = strconv.FormatInt(int64(kubesvc.Subsets[0].Ports[0].Port), 10)
	} else {
		log.Infof("Using user-provided k8s API host %s and port %s", cfg.K8sAPIHost, cfg.K8sAPIPort)
		host = cfg.K8sAPIHost
		port = cfg.K8sAPIPort
	}
	// if it doesn't exist already, create configMap with k8s endpoint data in it
	err = createOrUpdateCM(childCtx, clients, host, port)
	if err != nil {
		log.WithError(err).Error("failed to create or update configMap")
		return err
	}

	// kubectl patch ds -n kube-system kube-proxy -p '{"spec":{"template":{"spec":{"nodeSelector":{"non-calico": "true"}}}}}'
	patch := []byte(`{"spec":{"template":{"spec":{"nodeSelector":{"non-calico": "true"}}}}}`)
	proxyds := &appsv1.DaemonSet{}
	log.Debug("Getting kube-proxy ds")
	err = clients.CtrlClient.Get(childCtx, ctrlclient.ObjectKey{Namespace: "kube-system", Name: "kube-proxy"}, proxyds)
	if err != nil {
		log.WithError(err).Error("failed to get kube-proxy ds")
		return err
	}
	log.Debugf("patching with %v", string(patch[:]))
	log.Info("enabling BPF dataplane")
	err = clients.CtrlClient.Patch(childCtx, proxyds, ctrlclient.RawPatch(ctrlclient.Merge.Type(), patch))
	if err != nil {
		log.WithError(err).Error("failed to patch kube-proxy ds")
		return err
	}

	// kubectl patch installation.operator.tigera.io default --type merge -p '{"spec":{"calicoNetwork":{"linuxDataplane":"BPF"}}}'
	patch = []byte(`{"spec":{"calicoNetwork":{"linuxDataplane":"BPF"}}}`)

	installation = &operatorv1.Installation{}
	log.Debug("Getting installation")
	err = clients.CtrlClient.Get(childCtx, ctrlclient.ObjectKey{Name: "default"}, installation)
	if err != nil {
		log.WithError(err).Error("failed to get installation")
		return err
	}
	log.Debugf("patching with %v", string(patch[:]))
	err = clients.CtrlClient.Patch(childCtx, installation, ctrlclient.RawPatch(ctrlclient.Merge.Type(), patch))
	if err != nil {
		log.WithError(err).Error("failed to patch installation")
		return err
	}
	err = waitForTigeraStatus(ctx, clients)
	if err != nil {
		log.WithError(err).Error("error waiting for tigera status")
		return err
	}
	return nil
}

func enableIptables(ctx context.Context, clients config.Clients) error {
	// enable iptables
	log.Debug("entering enableIptables function")
	childCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	installation := &operatorv1.Installation{}
	log.Debug("Getting installation")
	err := clients.CtrlClient.Get(childCtx, ctrlclient.ObjectKey{Name: "default"}, installation)
	if err != nil {
		log.WithError(err).Error("failed to get installation")
		return err
	}
	if *installation.Spec.CalicoNetwork.LinuxDataplane == operatorv1.LinuxDataplaneIptables {
		log.Info("IPtables already enabled")
		return nil
	}

	// kubectl patch installation.operator.tigera.io default --type merge -p '{"spec":{"calicoNetwork":{"linuxDataplane":"Iptables"}}}'
	patch := []byte(`{"spec":{"calicoNetwork":{"linuxDataplane":"Iptables"}}}`)

	installation = &operatorv1.Installation{}
	log.Debug("Getting installation")
	err = clients.CtrlClient.Get(childCtx, ctrlclient.ObjectKey{Name: "default"}, installation)
	if err != nil {
		log.WithError(err).Error("failed to get installation")
		return err
	}
	log.Debugf("patching with %v", string(patch[:]))
	log.Info("enabling iptables dataplane")
	err = clients.CtrlClient.Patch(childCtx, installation, ctrlclient.RawPatch(ctrlclient.Merge.Type(), patch))
	if err != nil {
		log.WithError(err).Error("failed to patch installation")
		return err
	}

	// kubectl patch ds -n kube-system kube-proxy --type merge -p '{"spec":{"template":{"spec":{"nodeSelector":{"non-calico": null}}}}}'
	patch = []byte(`{"spec":{"template":{"spec":{"nodeSelector":{"non-calico": null}}}}}`)
	proxyds := &appsv1.DaemonSet{}
	log.Debug("Getting kube-proxy ds")
	err = clients.CtrlClient.Get(childCtx, ctrlclient.ObjectKey{Namespace: "kube-system", Name: "kube-proxy"}, proxyds)
	if err != nil {
		log.WithError(err).Error("failed to get kube-proxy ds")
		return err
	}
	log.Debugf("patching with %v", string(patch[:]))
	err = clients.CtrlClient.Patch(childCtx, proxyds, ctrlclient.RawPatch(ctrlclient.Merge.Type(), patch))
	if err != nil {
		log.WithError(err).Error("failed to patch kube-proxy ds")
		return err
	}

	err = waitForTigeraStatus(ctx, clients)
	if err != nil {
		log.WithError(err).Error("error waiting for tigera status")
		return err
	}
	return nil
}

func createOrUpdateCM(ctx context.Context, clients config.Clients, host string, port string) error {
	// if it doesn't exist already, create configMap with k8s endpoint data in it
	configMapName := "kubernetes-services-endpoint"
	namespace := "tigera-operator"
	configMap := &corev1.ConfigMap{}

	newConfigMap := &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name:      configMapName,
			Namespace: namespace,
		},
		Data: map[string]string{
			"KUBERNETES_SERVICE_HOST": host,
			"KUBERNETES_SERVICE_PORT": port,
		},
	}
	childCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	err := clients.CtrlClient.Get(childCtx, ctrlclient.ObjectKey{Name: configMapName, Namespace: namespace}, configMap)
	if err != nil {
		log.Infof("ConfigMap %s does not exist in namespace %s, creating it\n", configMapName, namespace)
		err := clients.CtrlClient.Create(childCtx, newConfigMap)
		return err
	}
	log.Infof("ConfigMap %s exists in namespace %s, updating it\n", configMapName, namespace)
	err = clients.CtrlClient.Update(childCtx, newConfigMap)
	return err

}

func waitForTigeraStatus(ctx context.Context, clients config.Clients) error {
	// wait for tigera status
	timeout := 600 * time.Second
	log.Debug("entering waitForTigeraStatus function")
	apiStatus := &operatorv1.TigeraStatus{}
	calicoStatus := &operatorv1.TigeraStatus{}
	childCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	time.Sleep(7 * time.Second) // give the operator time to update the status following whatever might have changed

	for childCtx.Err() == nil {
		log.Info("Waiting for tigerastatus")
		time.Sleep(10 * time.Second)
		err := clients.CtrlClient.Get(childCtx, ctrlclient.ObjectKey{Name: "apiserver"}, apiStatus)
		if err != nil {
			log.WithError(err).Error("failed to get apiserver status")
			return err
		}
		err = clients.CtrlClient.Get(childCtx, ctrlclient.ObjectKey{Name: "calico"}, calicoStatus)
		if err != nil {
			log.WithError(err).Error("failed to get calico status")
			return err
		}
		for _, apiCondition := range apiStatus.Status.Conditions {
			log.Debugf("apiserver condition: %v", apiCondition)
			if apiCondition.Type == "Available" && apiCondition.Status == "True" {
				log.Debug("apiserver is available")
				for _, calicoCondition := range calicoStatus.Status.Conditions {
					log.Debugf("calico condition: %v", calicoCondition)
					if calicoCondition.Type == "Available" && calicoCondition.Status == "True" {
						log.Debug("calico is available")
						return nil
					}
				}
			}
		}
	}
	return childCtx.Err()
}

func updateEncap(ctx context.Context, cfg config.Config, clients config.Clients, encap config.Encap) error {
	// update encap
	log.Debug("entering updateEncap function")
	log.Infof("Updating encapsulation to %s", encap)
	var patch []byte
	var err error

	if encap == config.EncapNone {
		// kubectl patch ippool default-ipv4-ippool -p '{"spec": {"ipipMode": "Never"}, {vxlanMode: "Never"}}'
		patch = []byte(`{"spec":{"ipipMode":"Never","vxlanMode":"Never"}}`)
		err = patchInstallation(ctx, clients, "None")
		if err != nil {
			log.WithError(err).Error("failed to patch installation")
			return err
		}
	} else if encap == config.EncapIPIP {
		// kubectl patch ippool default-ipv4-ippool -p '{"spec": {"ipipMode": "Always"}, {vxlanMode: "Never"}}'
		patch = []byte(`{"spec":{"ipipMode":"Always","vxlanMode":"Never"}}`)
		err = patchInstallation(ctx, clients, "IPIP")
		if err != nil {
			log.WithError(err).Error("failed to patch installation")
			return err
		}
	} else if encap == config.EncapVXLAN {
		// kubectl patch ippool default-ipv4-ippool -p '{"spec": {"ipipMode": "Never"}, {vxlanMode: "Always"}}'
		patch = []byte(`{"spec":{"ipipMode":"Never","vxlanMode":"Always"}}`)
		err = patchInstallation(ctx, clients, "VXLAN")
		if err != nil {
			log.WithError(err).Error("failed to patch installation")
			return err
		}
	} else if encap == config.EncapUnset {
		log.Info("No encapsulation specified, using whatever is already set")
	} else {
		return fmt.Errorf("invalid encapsulation %s", encap)
	}

	if semver.Compare(cfg.CalicoVersion, "v3.28.0") < 0 {
		log.Debug("Calico version is less than v3.28.0, patching IPPool")
		err = patchIPPool(ctx, clients, patch)
		if err != nil {
			log.WithError(err).Error("failed to patch IPPool")
			return err
		}
	}
	err = waitForTigeraStatus(ctx, clients)
	if err != nil {
		log.WithError(err).Error("error waiting for tigera status")
		return err
	}
	return nil
}

func patchInstallation(ctx context.Context, clients config.Clients, encap string) error {
	log.Infof("Patching installation to use %s encapsulation", encap)
	var v1encap operatorv1.EncapsulationType
	if encap == "None" {
		v1encap = operatorv1.EncapsulationNone
	} else if encap == "IPIP" {
		v1encap = operatorv1.EncapsulationIPIP
	} else if encap == "VXLAN" {
		v1encap = operatorv1.EncapsulationVXLAN
	}

	installation := &operatorv1.Installation{}
	err := clients.CtrlClient.Get(ctx, ctrlclient.ObjectKey{Name: "default"}, installation)
	if err != nil {
		log.WithError(err).Error("failed to get installation")
		return err
	}
	log.Debug("installation is", installation)
	installation.Spec.CalicoNetwork.IPPools[0].Encapsulation = v1encap
	err = clients.CtrlClient.Update(ctx, installation)

	return err
}

func patchIPPool(ctx context.Context, clients config.Clients, patch []byte) error {
	// We retry this because I've seen it fail, possibly due to the apiserver not being ready after dataplane switch
	log.Infof("Patching IPPool with %s", string(patch[:]))
	ippool := &v3.IPPool{}
	backoff := retry.NewFibonacci(1 * time.Second)
	if err := retry.Do(ctx, retry.WithMaxRetries(10, backoff), func(ctx context.Context) error {
		if err := clients.CtrlClient.Get(ctx, ctrlclient.ObjectKey{Name: "default-ipv4-ippool"}, ippool); err != nil {
			if ctx.Err() != nil {
				return ctx.Err()
			}
			return retry.RetryableError(err)
		}
		return nil
	}); err != nil {
		return err
	}
	log.Debug("ippool is", ippool)
	err := clients.CtrlClient.Patch(ctx, ippool, ctrlclient.RawPatch(ctrlclient.Merge.Type(), patch))
	return err
}

func makeSvc(namespace string, depname, svcname string) corev1.Service {
	svcname = utils.SanitizeString(svcname)
	svc := corev1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Labels: map[string]string{
				"app": "standing",
				"dep": depname,
			},
			Name:      svcname,
			Namespace: namespace,
		},
		Spec: corev1.ServiceSpec{
			Selector: map[string]string{
				"app": "standing",
				"dep": depname,
			},
			Ports: []corev1.ServicePort{
				{
					Port: 8080,
				},
			},
		},
	}
	return svc
}

func makeDeployment(namespace string, depname string, replicas int32, hostnetwork bool, image string, args []string) appsv1.Deployment {
	depname = utils.SanitizeString(depname)
	dep := appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{
			Labels: map[string]string{
				"app": "standing",
				"dep": depname,
			},
			Name:      depname,
			Namespace: namespace,
		},
		Spec: appsv1.DeploymentSpec{
			Replicas: &replicas,
			Selector: &metav1.LabelSelector{
				MatchLabels: map[string]string{
					"app": "standing",
					"dep": depname,
				},
			},
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{
					Labels: map[string]string{
						"app": "standing",
						"dep": depname,
					},
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
						},
					},
					HostNetwork: hostnetwork,
				},
			},
		},
	}
	return dep
}
