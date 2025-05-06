// Copyright (c) 2017 Tigera, Inc. All rights reserved.
//
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

package main

import (
	"bufio"
	"encoding/json"
	"fmt"
	"math/rand"
	"net"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/push"
	log "github.com/sirupsen/logrus"
)

// status codes we want to generate in the tests
var httpStatusCodes = [20]int{100, 101, 200, 201, 204, 300, 301, 302, 304, 400, 401, 402, 403, 405, 500, 501, 502, 503, 504, 505}
var httpRequestMethods = [5]string{http.MethodHead, http.MethodGet, http.MethodPost, http.MethodPatch, http.MethodDelete}

const letters = "abcdefghijklmnopqrstuvwxyz"

// list of all available URLS for testing https://httpbin.org/
var httpGetMethods = []string{"/image/jpeg", "/response-headers", "/uuid", "/headers", "/user-agent", "/anything", "/ip", ""}

var startTime = time.Now()
var urlLengthFactor int64

var (
	numPings                   prometheus.Counter
	numResponses               prometheus.Counter
	numFailPings               prometheus.Counter
	numFailResponses           prometheus.Counter
	numUncorrectableRateMisses prometheus.Counter
	ttfrs                      prometheus.Gauge

	timeout float64
)

type Target interface {
	Ping() error
}

type HTTPTarget struct {
	client         *http.Client
	url            string
	protocol       string
	sentMetric     prometheus.Counter
	responseMetric prometheus.Counter
	logResponses   bool
}

// Ping sends an HTTP request to the target URL and logs the response.
func (t *HTTPTarget) Ping() error {
	t.sentMetric.Add(1)
	var method string
	var userAgent = ""
	var url = ""

	switch t.protocol {
	case "tcp":
		method = http.MethodHead
		url = t.url
	case "http":
		// randomly generate method, status for the request with equal likelihood for all status listed
		method = httpRequestMethods[rand.Intn(len(httpRequestMethods))]
		status := httpStatusCodes[rand.Intn(len(httpStatusCodes))]
		// creates a distribution for user agents, %d = 5 more likely than %d = 1
		userAgent = fmt.Sprintf("Go-http/1.1-%d-%s", status/100, strings.ToLower(method))
		url = fmt.Sprintf("%s/status/%d", t.url, status)

		// randomly overwrite about 40% percentage of urls to generate urls of multiple url paths with multiple number of params
		randIndex := rand.Intn(10)
		if randIndex >= 6 {
			var urlBuilder strings.Builder
			rBool := rand.Intn(2) == 0
			if rBool {
				// This path might result in 404 based on which and how many url paths added to url in later steps,
				// which is fine since we care about making the requests not the response.
				_, _ = fmt.Fprintf(&urlBuilder, "%s", t.url)
			} else {
				// starts with /anything results in 200. Check API spec here for more info https://httpbin.org/
				_, _ = fmt.Fprintf(&urlBuilder, "%s/anything", t.url)
			}
			for index := 0; index < rand.Intn(int(urlLengthFactor)); index++ {
				urlBuilder.WriteString(httpGetMethods[rand.Intn(len(httpGetMethods))])
			}
			urlBuilder.WriteString(fmt.Sprintf("?status=%d", status))
			for index := 0; index < rand.Intn(int(urlLengthFactor)); index++ {
				urlBuilder.WriteString(fmt.Sprintf("&param_%d=%s", index, randString(6)))
			}
			url = urlBuilder.String()
		}
	}
	req, rerr := http.NewRequest(method, url, nil)
	if rerr != nil {
		log.Warn("error creating http request: ", rerr)
		return rerr
	}

	if userAgent != "" {
		req.Header.Set("User-Agent", userAgent)
	}

	rsp, err := t.client.Do(req)

	if rsp != nil {
		log.Info("Response status code ", rsp.StatusCode, " protocol ", t.protocol)
	}

	if err == nil {
		t.responseMetric.Add(1)
		if t.logResponses {
			log.Warn("Response from unreachable target: ", rsp)
		}
	}
	return err
}

// UDPTarget is a target that sends a UDP packet to a URL.
type UDPTarget struct {
	url            string
	sentMetric     prometheus.Counter
	responseMetric prometheus.Counter
	logResponses   bool
}

// Ping sends a UDP packet to the target URL and logs the response.
func (t *UDPTarget) Ping() error {
	responseDuration := time.Duration(timeout * 1000000000.0)
	p := make([]byte, 2048)
	t.sentMetric.Add(1)
	conn, err := net.DialTimeout("udp", t.url, responseDuration)
	if err != nil {
		log.Fatal("error dialing UDP target", err)
	}
	fmt.Fprintf(conn, "Ping!")
	err = conn.SetReadDeadline(time.Now().Add(responseDuration))
	if err != nil {
		log.Fatal("error setting UDP read deadline", err)
	}
	bytes, err := bufio.NewReader(conn).Read(p)
	if bytes > 0 {
		t.responseMetric.Add(1)
		if t.logResponses {
			log.Warn("Response from unreachable target: ", bytes)
		}
	}
	conn.Close()
	return err
}

// CalculateTTFR calculates the time to first 'response' for a target.
func CalculateTTFR(target Target) {
	// Use a channel with buffer size 1 and non-blocking writes to allow the first successful
	// response to report back.
	responseResults := make(chan time.Time, 1)
	ttfrInterval := 1 * time.Millisecond
	for {
		// Do each response on its own goroutine so we can have multiple responses in flight.
		go func() {
			log.Info("Sending ping to ", target)
			err := target.Ping()
			if err == nil {
				select {
				case responseResults <- time.Now():
				default:
				}
			}
		}()
		time.Sleep(ttfrInterval)
		select {
		case timeOfFirstPing := <-responseResults:
			ttfr := float64(timeOfFirstPing.Sub(startTime)) / float64(time.Second)
			ttfrs.Set(ttfr)
			log.Info("TTFR found: was ", ttfr)
			return
		default:
		}
		// Exponentially back off the response interval by 2% to a max of ~100ms.
		if ttfrInterval < 99*time.Millisecond {
			ttfrInterval = time.Duration(float64(ttfrInterval.Nanoseconds()) * 1.02)
		}
	}
}

// CheckConnectivity concurrently responses a target at a set rate.
func CheckConnectivity(target Target, rate float64, quitAfter int) {
	// Limit goroutines to 20% more than should be outstanding at once if all responses
	// take the full timeout (e.g. those that fail) to control memory consumption.
	log.Info("Starting connectivity check to ", target, " rate ", rate)
	maxConcurrentPings := int64(1.2*rate*timeout) + 1
	maxGoroutinesSem := make(chan bool, maxConcurrentPings)
	DoEvery(func() {
		maxGoroutinesSem <- true
		go func() {
			_ = target.Ping() // deliberately ignore errors, we want to just spam connections
			<-maxGoroutinesSem
		}()
	}, time.Duration(1000000000.0/rate)*time.Nanosecond, quitAfter)
}

// DoEvery runs a function every period (accurately).
func DoEvery(function func(), period time.Duration, quitAfter int) {
	var windowStart = time.Now()
	var adjustment time.Duration = 0
	var windowCount time.Duration = 0
	var num = 0
	for ; ; windowCount++ {
		// Warning: I tried a number of other methods for this correction that should
		// have worked, but they were all out under high loads.  Inaccuracies build up
		// with repeated time calculations, so instead just correct the whole thing
		// every second.  Only do this for quickish timers.
		if quitAfter > 0 && num >= quitAfter {
			return
		}
		if time.Since(windowStart) > 1*time.Second && period < 100*time.Millisecond {
			currentTime := time.Now()
			windowLength := currentTime.Sub(windowStart)
			adjustment = (windowCount*(period+adjustment) - windowLength) / windowCount
			windowStart = currentTime
			windowCount = 0
			if period+adjustment < 0 {
				log.Warn("Unable to adjust rate target sufficiently over the last second")
				numUncorrectableRateMisses.Add(1)
			}
		}
		function()
		time.Sleep(period + adjustment)
		num++
	}
}

func getEnvParam(name string, defaultValue string) string {
	envValue := os.Getenv(name)
	if envValue == "" {
		return defaultValue
	}
	return envValue
}

func main() {
	log.Info("Pingo started at ", startTime)

	// Load configuration from environment.
	// If adding more, may be worth using https://github.com/kelseyhightower/envconfig.
	var err error
	reachableAddr := getEnvParam("ADDRESS", "localhost")
	log.Info("Using reachable address ", reachableAddr)
	unreachableAddr := getEnvParam("UNREACHABLE_ADDRESS", "")
	port := getEnvParam("PORT", "5005")
	prometheusEndpointsBlob := getEnvParam("PROM_GATEWAYS", "")
	protocol := strings.ToLower(getEnvParam("PROTOCOL", "tcp"))
	timeout, err = strconv.ParseFloat(getEnvParam("TIMEOUT", "0.100"), 64)
	if err != nil || timeout < 0.001 {
		timeout = 0.1
	}
	sleepTime, err := strconv.ParseFloat(getEnvParam("SLEEPTIME", "0"), 64)
	if err != nil || sleepTime < 0 {
		sleepTime = 0
	}
	hostName := getEnvParam("HOSTNAME", "unknown")
	nodeName := getEnvParam("NODE_NAME", "unknown")
	connRate, err := strconv.ParseFloat(getEnvParam("CONN_PER_SECOND", "1"), 64)
	if err != nil || connRate <= 0 {
		connRate = 1
	}

	// This is the number of responses to send before quitting.  0 means "repeat forever".
	quitAfter64, err := strconv.ParseInt(getEnvParam("QUIT_AFTER", "0"), 10, 64)
	if err != nil || quitAfter64 <= 0 {
		quitAfter64 = 0
	}
	quitAfter := int(quitAfter64)

	failRate, err := strconv.ParseFloat(getEnvParam("FAIL_CONN_PER_SECOND", "1"), 64)
	if err != nil || failRate <= 0 {
		failRate = 1
	}
	urlLengthFactor, err = strconv.ParseInt(getEnvParam("URL_LENGTH_FACTOR", "0"), 10, 64)
	if err != nil || urlLengthFactor <= 0 {
		urlLengthFactor = 1
	}
	// If a SLEEPTIME is configured, sleep for that time before starting.
	log.Info("Sleeping for ", sleepTime, " seconds")
	if sleepTime > 0 {
		time.Sleep(time.Duration(sleepTime * float64(time.Second)))
		startTime = time.Now()
	}

	// Set up Prometheus metrics.
	// These metrics aren't Prometheus best practice: can/can't reach would be better
	// distinguished by a label; not label + metric name for example.  Leaving it for
	// compatibility.
	log.Info("Setting up Prometheus metrics")
	registry := prometheus.NewRegistry()
	reachLabels := prometheus.Labels{
		"node":           nodeName,
		"target":         reachableAddr + ":" + port,
		"podname":        hostName,
		"expectCanReach": "True",
	}
	failLabels := prometheus.Labels{
		"node":           nodeName,
		"target":         unreachableAddr + ":" + port,
		"podname":        hostName,
		"expectCanReach": "False",
	}
	numPings = prometheus.NewCounter(prometheus.CounterOpts{
		Name:        "ttfr_responses",
		Help:        "Total number of attempted connections",
		ConstLabels: reachLabels,
	})
	err = registry.Register(numPings)
	if err != nil {
		log.Fatal("error registering ttfr_responses", err)
	}
	numResponses = prometheus.NewCounter(prometheus.CounterOpts{
		Name:        "ttfr_response_responses",
		Help:        "Total number of connection responses",
		ConstLabels: reachLabels,
	})
	err = registry.Register(numResponses)
	if err != nil {
		log.Fatal("error registering ttfr_response_responses", err)
	}
	numFailPings = prometheus.NewCounter(prometheus.CounterOpts{
		Name:        "ttfr_responses_cant_reach",
		Help:        "Total number of attempted connections",
		ConstLabels: failLabels,
	})
	err = registry.Register(numFailPings)
	if err != nil {
		log.Fatal("error registering ttfr_responses_cant_reach", err)
	}
	numFailResponses = prometheus.NewCounter(prometheus.CounterOpts{
		Name:        "ttfr_response_responses_cant_reach",
		Help:        "Total number of connection responses",
		ConstLabels: failLabels,
	})
	err = registry.Register(numFailResponses)
	if err != nil {
		log.Fatal("error registering ttfr_response_responses_cant_reach", err)
	}
	ttfrs = prometheus.NewGauge(prometheus.GaugeOpts{
		Name:        "ttfr_seconds",
		Help:        "Time to first response/connectivity.",
		ConstLabels: reachLabels,
	})
	err = registry.Register(ttfrs)
	if err != nil {
		log.Fatal("error registering ttfr_seconds", err)
	}
	numUncorrectableRateMisses = prometheus.NewCounter(prometheus.CounterOpts{
		Name:        "ttfr_response_uncorrectable_rate_misses",
		Help:        "Total number of seconds of responseing where responseers were unable to correct to the desired rate [indicates overload].",
		ConstLabels: prometheus.Labels{"node": nodeName, "podname": hostName},
	})
	err = registry.Register(numUncorrectableRateMisses)
	if err != nil {
		log.Fatal("error registering ttfr_response_uncorrectable_rate_misses", err)
	}

	// Select a random Prometheus push gateway to use for load balancing.
	var prometheusEndpoints []string
	var pusher *push.Pusher
	if prometheusEndpointsBlob != "" {
		log.Info("Will push results to Prometheus push gateway ", prometheusEndpointsBlob)
		err = json.Unmarshal([]byte(prometheusEndpointsBlob), &prometheusEndpoints)
		if err != nil || len(prometheusEndpoints) == 0 {
			log.Fatal("fatal error ", err, " decoding Prometheus endpoints ", prometheusEndpointsBlob)
		}
		prometheusEndpoint := prometheusEndpoints[rand.Intn(len(prometheusEndpoints))]
		pusher = push.New(prometheusEndpoint, hostName)
		pusher.Gatherer(registry)
		// Prometheus is now set up
	}

	// Start the responses!
	var reachTarget Target
	var failTarget Target
	switch protocol {
	case "http", "tcp":
		// Create a non-default HTTP transport that doesn't cache TCP connections
		// and limits the dial and GET timeout to half the configured total each.
		log.Info("Setting up HTTP transport")
		halfTimeout := time.Duration(timeout * float64(time.Second) / 2)
		transport := &http.Transport{
			DisableKeepAlives: true,
			DialContext: (&net.Dialer{
				Timeout:   halfTimeout,
				KeepAlive: 0 * time.Second,
				DualStack: true,
			}).DialContext,
		}
		reachTarget = &HTTPTarget{
			client:         &http.Client{Transport: transport, Timeout: halfTimeout},
			url:            fmt.Sprintf("http://%s:%s", reachableAddr, port),
			protocol:       protocol,
			sentMetric:     numPings,
			responseMetric: numResponses,
		}
		failTarget = &HTTPTarget{
			client:         &http.Client{Transport: transport, Timeout: halfTimeout},
			url:            fmt.Sprintf("http://%s:%s", unreachableAddr, port),
			sentMetric:     numFailPings,
			protocol:       protocol,
			responseMetric: numFailResponses,
			logResponses:   true,
		}
	case "udp":
		log.Info("Setting up UDP transport")
		reachTarget = &UDPTarget{
			url:            fmt.Sprintf("%s:%s", reachableAddr, port),
			sentMetric:     numPings,
			responseMetric: numResponses,
		}
		failTarget = &UDPTarget{
			url:            fmt.Sprintf("%s:%s", unreachableAddr, port),
			sentMetric:     numFailPings,
			responseMetric: numFailResponses,
			logResponses:   true,
		}
	}

	if reachableAddr != "" {
		log.Info("Starting connectivity check to ", reachableAddr, ":", port, " rate ", connRate)
		CalculateTTFR(reachTarget)
		go CheckConnectivity(reachTarget, connRate, quitAfter)
	}

	if unreachableAddr != "" {
		log.Info("Starting connectivity check to ", unreachableAddr, ":", port, " rate ", failRate)
		go CheckConnectivity(failTarget, failRate, quitAfter)
	}

	log.Info("Started everything!")
	// Report to Prometheus every 10 seconds.
	gather, err := registry.Gather()
	if err != nil {
		log.Fatal("error gathering metrics: ", err)
	}
	for _, metric := range gather {
		if *metric.Name == "ttfr_seconds" {
			log.Info(`{"ttfr_seconds": `, *metric.Metric[0].Gauge.Value, "}")
		}
	}
	DoEvery(func() {
		if prometheusEndpointsBlob != "" {
			err = pusher.Push()
			if err != nil {
				log.Fatal("error pushing response success update to gateway: ", err)
			}
		}
	}, 10*time.Second, quitAfter)
}

func randString(n int) string {
	b := make([]rune, n)
	for i := range b {
		b[i] = rune(letters[rand.Intn(len(letters))])
	}
	return string(b)
}

// kubectl debug -it ephemeral-demo --image=busybox:1.28 --target=ephemeral-demo
