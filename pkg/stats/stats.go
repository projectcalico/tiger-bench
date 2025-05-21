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

package stats

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"slices"
	strconv "strconv"
	"time"

	"github.com/projectcalico/tiger-bench/pkg/config"

	log "github.com/sirupsen/logrus"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// CalicoNodeCPU is a struct that holds the CPU usage of a Calico node
type CalicoNodeCPU struct {
	Status string `json:"status,omitempty"`
	Data   struct {
		ResultType string `json:"resultType,omitempty"`
		Result     []struct {
			Metric struct {
				Pod string `json:"pod,omitempty"`
			} `json:"metric,omitempty"`
			Value []any `json:"value,omitempty"`
		} `json:"result,omitempty"`
	} `json:"data,omitempty"`
}

// CalicoNodeMem is a struct that holds the memory usage of a Calico node
type CalicoNodeMem struct {
	Status string `json:"status,omitempty"`
	Data   struct {
		ResultType string `json:"resultType,omitempty"`
		Result     []struct {
			Metric struct {
				ID        string `json:"id,omitempty"`
				Instance  string `json:"instance,omitempty"`
				Job       string `json:"job,omitempty"`
				Namespace string `json:"namespace,omitempty"`
				Pod       string `json:"pod,omitempty"`
			} `json:"metric,omitempty"`
			Value []interface{} `json:"value,omitempty"`
		} `json:"result,omitempty"`
	} `json:"data,omitempty"`
}

// type ttfrQuantile struct {
// 	Status string `json:"status,omitempty"`
// 	Data   struct {
// 		ResultType string `json:"resultType"`
// 		Result     []struct {
// 			Metric struct {
// 				Name     string `json:"__name__"`
// 				Quantile string `json:"quantile"`
// 			} `json:"metric"`
// 			Value []any `json:"value"`
// 		} `json:"result"`
// 	} `json:"data"`
// }

// MinMaxAvg stores minimum, maximum, and average values of something
type MinMaxAvg struct {
	Min     float64
	Max     float64
	Average float64
}

// ResultSummary holds a statistical summary of a set of results
type ResultSummary struct {
	Min           float64 `json:"min,omitempty"`
	Max           float64 `json:"max,omitempty"`
	Average       float64 `json:"avg,omitempty"`
	P50           float64 `json:"P50,omitempty"`
	P75           float64 `json:"P75,omitempty"`
	P90           float64 `json:"P90,omitempty"`
	P99           float64 `json:"P99,omitempty"`
	Unit          string  `json:"unit,omitempty"`
	NumDataPoints int     `json:"datapoints,omitempty"`
}

// SummarizeResults summarizes the results
func SummarizeResults(results []float64) (ResultSummary, error) {
	log.Debug("Entering summarizeResults function")
	var err error
	summary := ResultSummary{}
	summary.NumDataPoints = len(results)
	if len(results) == 0 {
		log.Warning("No results to summarize")
		return summary, fmt.Errorf("no results to summarize")
	}
	summary.Min = slices.Min(results)
	summary.Max = slices.Max(results)
	summary.Average, err = average(results)
	if err != nil {
		log.WithError(err).Warning("Error summarizing stats")
		return summary, err
	}
	summary.P50, summary.P75, summary.P90, summary.P99, err = getPercentiles(results)
	if err != nil {
		log.WithError(err).Warning("Error summarizing stats")
		return summary, err
	}
	log.Debugf("Summary: %+v", summary)
	return summary, nil
}

func average(results []float64) (float64, error) {
	length := len(results)
	if length == 0 {
		return 0, fmt.Errorf("no results to average")
	}
	sum := float64(0)
	for i := 0; i < length; i++ {
		sum += results[i]
	}
	return sum / float64(length), nil
}

func getPercentiles(results []float64) (float64, float64, float64, float64, error) {
	log.Debug("Entering getPercentiles function")
	length := len(results)
	if length == 0 {
		return 0, 0, 0, 0, fmt.Errorf("no results to get percentiles from")
	}
	slices.Sort(results)

	return getPercentile(results, 50), getPercentile(results, 75), getPercentile(results, 90), getPercentile(results, 99), nil
}

// getPercentile returns the percentile based on the nearest rank method.  It assumes the input slice is already sorted into ascending order.
func getPercentile(sortedresults []float64, percent float64) float64 {
	log.Debug("Entering getPercentile function")
	log.Debug("    sortedresults: ", sortedresults)
	length := len(sortedresults)

	if percent < 0 || percent > 100 {
		return math.NaN()
	}

	pos := int(math.Ceil(float64(length) * percent / 100))
	log.Debug("    pos: ", pos)

	if pos == 0 {
		return sortedresults[0]
	}
	return sortedresults[pos-1]
}

// CollectCPUAndMemoryUsage collects CPU and memory usage stats
func CollectCPUAndMemoryUsage(ctx context.Context, clients config.Clients) (MinMaxAvg, MinMaxAvg, error) {
	// collect max memory and CPU usage

	log.Debug("Entering collectMaxCPUAndMemoryUsage function")
	memMMA := MinMaxAvg{}
	cpuMMA := MinMaxAvg{}

	testcaliconodes, err := GetTestCalicoNodes(ctx, clients)
	if err != nil {
		return memMMA, cpuMMA, err
	}
	cpuData, err := getCPUData(ctx, clients, "max_over_time")
	if err != nil {
		return memMMA, cpuMMA, err
	}
	cpus, err := filterCPUData(cpuData, testcaliconodes)
	if err != nil {
		return memMMA, cpuMMA, err
	}
	cpuMMA.Max = slices.Max(cpus)
	log.Infof("maxcpu %.3f", cpuMMA.Max)
	cpuData, err = getCPUData(ctx, clients, "min_over_time")
	if err != nil {
		return memMMA, cpuMMA, err
	}
	cpus, err = filterCPUData(cpuData, testcaliconodes)
	if err != nil {
		return memMMA, cpuMMA, err
	}
	cpuMMA.Min = slices.Min(cpus)
	log.Infof("mincpu %.3f", cpuMMA.Min)

	cpuData, err = getCPUData(ctx, clients, "avg_over_time")
	if err != nil {
		return memMMA, cpuMMA, err
	}
	cpus, err = filterCPUData(cpuData, testcaliconodes)
	if err != nil {
		return memMMA, cpuMMA, err
	}
	// we are taking an average of an average. this is ONLY OK because we have the same number of data points for each pod
	var sum float64
	for _, avg := range cpus {
		sum += avg
	}
	cpuMMA.Average = sum / float64(len(cpus))
	log.Infof("avgcpu %.3f", cpuMMA.Average)

	memData, err := getMemData(ctx, clients, "max_over_time")
	if err != nil {
		return memMMA, cpuMMA, err
	}
	mems, err := filterMemData(memData, testcaliconodes)
	if err != nil {
		return memMMA, cpuMMA, err
	}
	memMMA.Max = slices.Max(mems)
	log.Infof("maxmem %.0f", memMMA.Max)

	memData, err = getMemData(ctx, clients, "min_over_time")
	if err != nil {
		return memMMA, cpuMMA, err
	}
	mems, err = filterMemData(memData, testcaliconodes)
	if err != nil {
		return memMMA, cpuMMA, err
	}
	memMMA.Min = slices.Min(mems)
	log.Infof("minmem %.0f", memMMA.Min)

	memData, err = getMemData(ctx, clients, "avg_over_time")
	if err != nil {
		return memMMA, cpuMMA, err
	}
	mems, err = filterMemData(memData, testcaliconodes)
	if err != nil {
		return memMMA, cpuMMA, err
	}
	// we are taking an average of an average. this is ONLY OK because we have the same number of data points for each pod
	sum = 0
	for _, avg := range mems {
		sum += float64(avg)
	}
	memMMA.Average = sum / float64(len(mems))
	log.Infof("avgmem %.3f", memMMA.Average)

	// return memory, CPU
	return memMMA, cpuMMA, nil
}

func getCPUData(ctx context.Context, clients config.Clients, q string) (CalicoNodeCPU, error) {
	log.Debugf("Entering getCPUData function with q=%s", q)
	calicoNodeCPU := CalicoNodeCPU{}
	query := fmt.Sprintf("%s(cpu_calico_node[1d])", q)
	resp, err := GetData(ctx, clients, query, "prometheus-k8s")
	if err != nil {
		log.Error("failed to get CPU data")
		return calicoNodeCPU, err
	}
	log.Debugf("response: %s", string(resp))
	err = json.Unmarshal(resp, &calicoNodeCPU)
	if err != nil {
		log.Error("failed to unmarshal CPU data")
		return calicoNodeCPU, err
	}
	return calicoNodeCPU, nil
}

func getMemData(ctx context.Context, clients config.Clients, q string) (CalicoNodeMem, error) {
	log.Debug("Entering getMemData function")
	calicoNodeMem := CalicoNodeMem{}
	query := fmt.Sprintf("%s(calico_node_mem[1d])", q)
	resp, err := GetData(ctx, clients, query, "prometheus-k8s")
	if err != nil {
		log.Error("failed to get memory data")
		return calicoNodeMem, err
	}
	log.Debugf("response: %s", string(resp))
	err = json.Unmarshal(resp, &calicoNodeMem)
	if err != nil {
		log.Error("failed to unmarshal Mem data")
		return calicoNodeMem, err
	}
	return calicoNodeMem, nil
}

func filterCPUData(data CalicoNodeCPU, testcaliconodes []string) ([]float64, error) {
	log.Debug("Entering filterData function")
	var floats []float64
	for _, result := range data.Data.Result {
		// filter in calico-nodes under test only
		if slices.Contains(testcaliconodes, result.Metric.Pod) {
			log.Infof("found pod: %s, with value: %s", result.Metric.Pod, result.Value[1].(string))
			thisresult := result.Value[1].(string)
			floatresult, err := strconv.ParseFloat(thisresult, 64)
			if err != nil {
				log.Error("failed to parse data")
				return nil, err
			}
			floats = append(floats, floatresult)
		}
	}
	return floats, nil
}

func filterMemData(data CalicoNodeMem, testcaliconodes []string) ([]float64, error) {
	log.Debug("Entering filterData function")
	var mems []float64
	pods := map[string]float64{}
	for _, result := range data.Data.Result {
		if slices.Contains(testcaliconodes, result.Metric.Pod) {
			log.Infof("found pod: %s, with value: %s", result.Metric.Pod, result.Value[1].(string))
			// result contains multiple entries for each pod.  We need the biggest one
			thisresult := result.Value[1].(string)
			floatresult, err := strconv.ParseFloat(thisresult, 64)
			if err != nil {
				log.Error("failed to parse memory data")
				return nil, err
			}
			if _, ok := pods[result.Metric.Pod]; !ok {
				pods[result.Metric.Pod] = 0
			}
			if pods[result.Metric.Pod] < floatresult {
				pods[result.Metric.Pod] = floatresult
			}
		}
	}
	for _, mem := range pods {
		mems = append(mems, mem)
	}
	return mems, nil
}

// GetData gets data from a service
func GetData(ctx context.Context, clients config.Clients, query string, serviceName string) ([]byte, error) {
	// get data
	log.Debug("Entering getData function")
	childCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	k8sProm, err := clients.Clientset.CoreV1().Services("sto-system").Get(childCtx, serviceName, metav1.GetOptions{})
	if err != nil {
		return nil, fmt.Errorf("failed to get services")
	}
	k8sPromIP := k8sProm.Spec.ClusterIP
	calicoNodeCPU := fmt.Sprintf("http://%s:9090/api/v1/query?query=%s", k8sPromIP, query)
	return HTTPGet(ctx, clients, calicoNodeCPU, 5)
}

// HTTPGet performs an HTTP GET request, with retries
func HTTPGet(ctx context.Context, clients config.Clients, target string, retries int) ([]byte, error) {
	log.Infof("Getting %s", target)
	childCtx, cancel := context.WithTimeout(ctx, 1*time.Minute)
	defer cancel()
	for i := 0; i < retries; i++ {
		if childCtx.Err() != nil {
			return nil, errors.New("Context cancelled")
		}
		r, err := clients.WebClient.Get(target)
		if err == nil {
			defer r.Body.Close()
			body, err := io.ReadAll(r.Body)
			log.Debugf("Got http reply: %s", string(body))
			return body, err
		}
		log.Error("failed to get http response, retrying")
	}
	log.Error("failed to get http response after retries")
	return nil, errors.New("failed to get http response")
}

// GetTestCalicoNodes gets list of calico-nodes on the nodes under test
func GetTestCalicoNodes(ctx context.Context, clients config.Clients) ([]string, error) {
	var calicoNodes []string
	nodes, err := GetTestNodes(ctx, clients)
	if err != nil {
		return calicoNodes, err
	}
	podlist := &corev1.PodList{}
	err = clients.CtrlClient.List(ctx, podlist)
	if err != nil {
		return calicoNodes, fmt.Errorf("failed to list pods")
	}
	for _, pod := range podlist.Items {
		if pod.Labels["k8s-app"] == "calico-node" {
			log.Debugf("found pod: %s", pod.ObjectMeta.Name)
			if slices.Contains(nodes, pod.Spec.NodeName) {
				calicoNodes = append(calicoNodes, pod.ObjectMeta.Name)
			}
		}
	}
	return calicoNodes, nil
}

// GetTestNodes gets nodes with label "tigera.io/test-nodepool=default-pool"
func GetTestNodes(ctx context.Context, clients config.Clients) ([]string, error) {
	var nodes []string
	nodelist := &corev1.NodeList{}
	err := clients.CtrlClient.List(ctx, nodelist)
	if err != nil {
		return nodes, fmt.Errorf("failed to list nodes")
	}
	for _, node := range nodelist.Items {
		if node.Labels["tigera.io/test-nodepool"] == "default-pool" {
			nodename := node.ObjectMeta.Name
			log.Debugf("found nodename: %s", nodename)
			nodes = append(nodes, nodename)
		}
	}
	return nodes, nil
}
