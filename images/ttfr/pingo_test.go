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

package main

import (
	"bytes"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"regexp"
	"strconv"
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/stretchr/testify/assert"

	log "github.com/sirupsen/logrus"
)

func TestHTTPTarget_Ping(t *testing.T) {
	tests := []struct {
		name       string
		protocol   string
		statusCode int
	}{
		{"HTTP 200", "http", http.StatusOK},
		{"HTTP 404", "http", http.StatusNotFound},
		{"TCP", "tcp", http.StatusOK},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Create a test server
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(tt.statusCode)
			}))
			defer server.Close()

			// Create a HTTPTarget instance
			target := &HTTPTarget{
				client:         &http.Client{Timeout: 1 * time.Second},
				url:            server.URL,
				protocol:       tt.protocol,
				sentMetric:     prometheus.NewCounter(prometheus.CounterOpts{}),
				responseMetric: prometheus.NewCounter(prometheus.CounterOpts{}),
			}

			// Call the Ping method
			urlLengthFactor = 1
			err := target.Ping()
			if err != nil && tt.protocol != "tcp" {
				t.Errorf("Ping() error = %v, wantErr %v", err, false)
			}
		})
	}
}

type envVar struct {
	key   string
	value string
}

func Test_main(t *testing.T) {
	// Create a fake prom-pushgateway
	prom := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer prom.Close()

	// Create a test server (that responds after a short delay)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(10 * time.Millisecond)
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	host, port, err := net.SplitHostPort(server.Listener.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	basicEnv := []envVar{
		{key: "PROM_GATEWAYS", value: fmt.Sprint(`["`, prom.Listener.Addr().String(), `"]`)},
		{key: "ADDRESS", value: host},
		{key: "PORT", value: port},
		{key: "QUIT_AFTER", value: "2"},
	}

	tests := []struct {
		name    string
		envvars []envVar
	}{
		{name: "default0", envvars: basicEnv},
	}
	for _, tt := range tests {
		for _, envvar := range tt.envvars {
			t.Setenv(envvar.key, envvar.value)
		}
		t.Run(tt.name, func(t *testing.T) {
			main()
		})
	}
}
func Test_randString(t *testing.T) {
	tests := []struct {
		name string
		n    int
	}{
		{"n=0", 0},
		{"n=5", 5},
		{"n=10", 10},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := randString(tt.n)
			if len(got) != tt.n {
				t.Errorf("randString() = %v, want %v", got, tt.n)
			}
		})
	}

}

func Test_noPushGW(t *testing.T) {

	// Create a test server (that responds after a short delay)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(15 * time.Millisecond)
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	host, port, err := net.SplitHostPort(server.Listener.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	basicEnv := []envVar{
		{key: "ADDRESS", value: host},
		{key: "PORT", value: port},
	}

	tests := []struct {
		name    string
		envvars []envVar
	}{
		{name: "default0", envvars: basicEnv},
	}
	for _, tt := range tests {
		for _, envvar := range tt.envvars {
			t.Setenv(envvar.key, envvar.value)
		}
		t.Run(tt.name, func(t *testing.T) {
			// run main and capture logs from it
			var str bytes.Buffer
			log.SetOutput(&str)

			main()

			capturedLogs := str.String()
			if capturedLogs == "" {
				t.Errorf("Expected logs to be captured, but got none")
			}
			assert.Contains(t, capturedLogs, "ttfr_seconds")
			r := regexp.MustCompile(`{\\"ttfr_seconds\\": ([0-9]\.[0-9].*)}`)
			matches := r.FindStringSubmatch(capturedLogs)
			ttfrString := matches[1]
			if len(ttfrString) == 0 {
				t.Errorf("Expected regex to match, but got none")
			}
			ttfrSec, err := strconv.ParseFloat(ttfrString, 64)
			if err != nil {
				t.Errorf("Expected to parse float, but got error: %v", err)
			}
			if ttfrSec == 0 {
				t.Errorf("Expected ttfrSec to be non-zero, but got %v", ttfrSec)
			}
		})
	}
}

func Test_regex(t *testing.T) {
	capturedLogs := "time=\"2025-05-02T14:04:07+01:00\" level=info msg=\"Pingo started at 2025-05-02 14:04:07.029670625 +0100 BST m=+0.000956863\"\ntime=\"2025-05-02T14:04:07+01:00\" level=info msg=\"Response status code 200 protocol tcp\"\ntime=\"2025-05-02T14:04:07+01:00\" level=info msg=\"Response status code 200 protocol tcp\"\ntime=\"2025-05-02T14:04:07+01:00\" level=info msg=\"TTFR found: was 0.016565863\"\ntime=\"2025-05-02T14:04:07+01:00\" level=info msg=\"Started everything!\"\ntime=\"2025-05-02T14:04:07+01:00\" level=info msg=\"Starting connectivity check to &{0xc00021c5a0 http://127.0.0.1:40379 tcp 0xc0002001e0 0xc000200300 false} rate 1\"\ntime=\"2025-05-02T14:04:07+01:00\" level=info msg=\"Response status code 200 protocol tcp\"\ntime=\"2025-05-02T14:04:07+01:00\" level=info msg=\"{\\\"ttfr_seconds\\\": 0.016565863}\"\n"
	r := regexp.MustCompile(`{\\"ttfr_seconds\\": ([0-9]\.[0-9].*)}`)
	matches := r.FindStringSubmatch(capturedLogs)
	if len(matches) < 2 {
		t.Errorf("Expected regex to match, but didn't get enough matches: %v", matches)
	}
	ttfrString := matches[1]
	if ttfrString == "" {
		t.Error("Expected regex to match, but got none")
	}
	ttfrSec, err := strconv.ParseFloat(ttfrString, 64)
	if err != nil {
		t.Errorf("Expected to parse float, but got error: %v", err)
	}
	if ttfrSec == 0 {
		t.Errorf("Expected ttfrSec to be non-zero, but got %v", ttfrSec)
	}
	// Check if the ttfrSec is within a reasonable range
	if ttfrSec != 0.016565863 {
		t.Errorf("Expected ttfrSec to be 0.016565863, but got %v", ttfrSec)
	}
}
