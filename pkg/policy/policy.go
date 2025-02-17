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

package policy

import (
	"context"
	"fmt"
	"sync"
	"time"

	log "github.com/sirupsen/logrus"

	"github.com/rung/go-safecast"
	v3 "github.com/tigera/api/pkg/apis/projectcalico/v3"
	networkingv1 "k8s.io/api/networking/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/intstr"
	ctrlclient "sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/projectcalico/tiger-bench/pkg/config"
	"github.com/projectcalico/tiger-bench/pkg/utils"
)

// DeployPolicies deploys policies to the cluster.
func DeployPolicies(ctx context.Context, clients config.Clients, numPolicies int, namespace string) error {
	log.Debug("Entering deployPolicies function")
	log.Infof("Deploying %d policies", numPolicies)
	const numThreads = 20

	_, err := utils.GetOrCreateNS(ctx, clients, namespace)
	if err != nil {
		return err
	}

	// deploy policies
	currentNumPolicies, err := countPolicies(ctx, clients, namespace, "policy-")
	if err != nil {
		return err
	}
	if numPolicies > currentNumPolicies {
		// create policies
		podSelector := metav1.LabelSelector{
			MatchExpressions: []metav1.LabelSelectorRequirement{
				{Key: "app", Operator: metav1.LabelSelectorOpExists},
			},
		}

		ingressPeers := []networkingv1.NetworkPolicyPeer{
			{
				PodSelector: &podSelector,
			},
		}

		// Spin up a channel with multiple threads to create policies, because a single thread is limited to 5 per second
		var policyIndexes []int
		for i := currentNumPolicies; i < numPolicies; i++ {
			policyIndexes = append(policyIndexes, i)
		}
		var wg sync.WaitGroup
		errors := make([]error, len(policyIndexes))
		sem := make(chan struct{}, numThreads)
		for i, v := range policyIndexes {
			name := fmt.Sprintf("policy-%.5d", v)
			sem <- struct{}{}
			wg.Add(1)
			go func() {
				defer wg.Done()
				defer func() { <-sem }()
				errors[i] = createPolicy(ctx, clients, name, namespace, podSelector, ingressPeers, []int{80})
			}()
		}
		wg.Wait()
		// we now have a list of errors, let's see if they all succeeded
		log.Debugf("Errors: %v+", errors)
		var overallError error
		for _, err := range errors {
			if err != nil {
				overallError = err
				break
			}
		}
		log.Debugf("overallError: %v", overallError)
		return overallError

	} else if numPolicies < currentNumPolicies {
		// delete policies
		// Spin up a channel with multiple threads to delete policies, because a single thread is limited to 5 per second

		// make a list of ints from currentNumPolicies to numPolicies
		var policyIndexes []int
		for i := currentNumPolicies; i > numPolicies; i-- {
			policyIndexes = append(policyIndexes, i)
		}
		var wg sync.WaitGroup
		errors := make([]error, len(policyIndexes))
		sem := make(chan struct{}, numThreads)
		for i, v := range policyIndexes {
			name := fmt.Sprintf("policy-%.5d", v)
			sem <- struct{}{}
			wg.Add(1)
			go func() {
				defer wg.Done()
				defer func() { <-sem }()
				errors[i] = DeletePolicy(ctx, clients, name, namespace)
			}()
		}
		wg.Wait()
		// we now have a list of errors, let's see if they all succeeded
		log.Debugf("Errors: %v+", errors)
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
	return nil
}

// CreateTestPolicy creates the policies needed to ensure test pods can run the test
func CreateTestPolicy(ctx context.Context, clients config.Clients, testPolicyName string, namespace string, ports []int) error {
	log.Debug("Entering createTestPolicy function")
	// create policy
	podSelector := metav1.LabelSelector{
		MatchExpressions: []metav1.LabelSelectorRequirement{
			{Key: "app", Operator: metav1.LabelSelectorOpExists},
		},
	}

	ingressPeers := []networkingv1.NetworkPolicyPeer{}
	// delete policy to ensure it does not exist
	err := DeletePolicy(ctx, clients, testPolicyName, namespace)
	if err != nil {
		// ignore the "doesn't exist" error
		if err.Error() != fmt.Sprintf("networkpolicies.networking.k8s.io \"%s\" not found", testPolicyName) {
			log.WithError(err).Errorf("failed to delete test policy %s", testPolicyName)
			return err
		}
	}
	return createPolicy(ctx, clients, testPolicyName, namespace, podSelector, ingressPeers, ports)
}

func countPolicies(ctx context.Context, clients config.Clients, namespace string, prefix string) (int, error) {
	log.Debug("Entering countPolicies function")
	// count policies
	count := 0
	childCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	policies, err := clients.Clientset.NetworkingV1().NetworkPolicies(namespace).List(childCtx, metav1.ListOptions{})
	for _, policy := range policies.Items {
		// if policy name starts with prefix, increment count
		if policy.Name[:len(prefix)] == prefix {
			count++
		}
	}
	if err != nil {
		return 0, fmt.Errorf("failed to list policies")
	}
	return count, nil
}

// DeletePolicy deletes a policy
func DeletePolicy(ctx context.Context, clients config.Clients, name string, namespace string) error {
	log.Debugf("Deleting policy %s in namespace %s", name, namespace)
	// delete policy
	childCtx, cancel := context.WithTimeout(ctx, 1*time.Second)
	defer cancel()
	err := clients.Clientset.NetworkingV1().NetworkPolicies(namespace).Delete(childCtx, name, metav1.DeleteOptions{})
	return err
}

func createPolicy(ctx context.Context, clients config.Clients, name string, namespace string, podSelector metav1.LabelSelector, ingressPeers []networkingv1.NetworkPolicyPeer, ports []int) error {
	log.Debug("Entering createPolicy function")
	log.Debugf("Creating policy %s in namespace %s with ports %v", name, namespace, ports)
	// create policy
	policy := &networkingv1.NetworkPolicy{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: namespace,
		},
		Spec: networkingv1.NetworkPolicySpec{
			PodSelector: podSelector,
			Ingress: []networkingv1.NetworkPolicyIngressRule{
				{
					From: ingressPeers,
				},
			},
			PolicyTypes: []networkingv1.PolicyType{
				networkingv1.PolicyTypeIngress,
			},
		},
	}
	for _, port := range ports {
		log.Debugf("Port: %d", port)
		p32, err := safecast.Int32(port)
		if err != nil {
			log.WithError(err).Errorf("failed to cast port %d to int32", port)
			return err
		}
		p := intstr.FromInt32(p32)
		policy.Spec.Ingress[0].Ports = append(policy.Spec.Ingress[0].Ports, networkingv1.NetworkPolicyPort{Port: &p})
	}
	childCtx, cancel := context.WithTimeout(ctx, 1*time.Second)
	defer cancel()
	_, err := clients.Clientset.NetworkingV1().NetworkPolicies(namespace).Create(childCtx, policy, metav1.CreateOptions{})
	return err
}

// GetOrCreateDNSPolicy gets or creates a DNS policy if it does not exist
func GetOrCreateDNSPolicy(ctx context.Context, clients config.Clients, policy v3.NetworkPolicy) (v3.NetworkPolicy, error) {

	err := clients.CtrlClient.Get(ctx, ctrlclient.ObjectKey{Namespace: policy.Namespace, Name: policy.Name}, &policy)
	if err != nil {
		if ctrlclient.IgnoreNotFound(err) == nil {
			log.Infof("didn't find existing policy %s, creating", policy.Name)
			err := clients.CtrlClient.Create(ctx, &policy)
			if err != nil {
				return policy, fmt.Errorf("failed to create policy")
			}
		} else {
			return policy, fmt.Errorf("failed to get policy")
		}
	} else {
		log.Infof("found existing policy %s", policy.Name)
	}
	return policy, nil
}
