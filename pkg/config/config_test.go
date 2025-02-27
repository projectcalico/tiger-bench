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

package config

import (
	"context"
	"os"
	"testing"

	"github.com/kelseyhightower/envconfig"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestDecode(t *testing.T) {
	fileContent := `
- testKind: thruput-latency
  numpolicies: 100
  encap: vxlan
  dataplane: bpf
  numpods: 101
  numservices: 102
  iterations: 1
  testnamespace: myns
`
	filePath := "/tmp/test_configs.yaml"
	err := os.WriteFile(filePath, []byte(fileContent), 0644)
	require.NoError(t, err)
	defer os.Remove(filePath)

	var cfg Config
	cfg.TestConfigFile = filePath

	err = loadTestConfigsFromFile(&cfg)
	require.NoError(t, err)
	assert.Equal(t, TestKindQperf, cfg.TestConfigs[0].TestKind)
	assert.Equal(t, Encap("vxlan"), cfg.TestConfigs[0].Encap)
	assert.Equal(t, DataPlane("bpf"), cfg.TestConfigs[0].Dataplane)
	assert.Equal(t, 100, cfg.TestConfigs[0].NumPolicies)
	assert.Equal(t, 101, cfg.TestConfigs[0].NumPods)
	assert.Equal(t, 102, cfg.TestConfigs[0].NumServices)
	assert.Equal(t, 1, cfg.TestConfigs[0].Iterations)
	assert.Equal(t, "myns", cfg.TestConfigs[0].TestNamespace)
}

func TestNoTest(t *testing.T) {
	fileContent := ``
	filePath := "/tmp/test_configs.yaml"
	err := os.WriteFile(filePath, []byte(fileContent), 0644)
	require.NoError(t, err)
	defer os.Remove(filePath)

	var cfg Config
	cfg.TestConfigFile = filePath
	err = loadTestConfigsFromFile(&cfg)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "no test configs found in file")
}

func TestNoFile(t *testing.T) {
	filePath := "/tmp/test_configs.yaml"
	os.Remove(filePath)

	var cfg Config
	cfg.TestConfigFile = filePath
	err := loadTestConfigsFromFile(&cfg)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "no such file or directory")
}

func TestCreateWebclient(t *testing.T) {
	cfg := Config{
		ProxyAddress: "localhost:1080",
	}
	client, err := createWebclient(cfg)
	require.NoError(t, err)
	assert.NotNil(t, client)
}

func TestEnvConfig(t *testing.T) {
	fileContent := ``
	filePath := "/tmp/test_configs.yaml"
	err := os.WriteFile(filePath, []byte(fileContent), 0644)
	require.NoError(t, err)
	defer os.Remove(filePath)
	os.Setenv("TESTCONFIGFILE", filePath)

	var cfg Config
	err = envconfig.Process("bench", &cfg)
	require.NoError(t, err)
	assert.Equal(t, "", cfg.OperatorPromURL)
	assert.Equal(t, "", cfg.K8sPromURL)
	assert.Equal(t, "/results/results.json", cfg.ResultsFile)
	assert.Equal(t, filePath, cfg.TestConfigFile)
}

func TestDefaults(t *testing.T) {
	fileContent := `- testKind: thruput-latency`
	filePath := "/tmp/test_configs.yaml"
	err := os.WriteFile(filePath, []byte(fileContent), 0644)
	require.NoError(t, err)
	defer os.Remove(filePath)
	os.Setenv("TESTCONFIGFILE", filePath)

	var cfg Config
	err = envconfig.Process("bench", &cfg)
	require.NoError(t, err)

	err = loadTestConfigsFromFile(&cfg)
	require.NoError(t, err)

	assert.Equal(t, "", cfg.OperatorPromURL)
	assert.Equal(t, "", cfg.K8sPromURL)
	assert.Equal(t, "/results/results.json", cfg.ResultsFile)
	assert.Equal(t, filePath, cfg.TestConfigFile)
	assert.Equal(t, 1, len(cfg.TestConfigs))
	assert.Equal(t, Encap(""), cfg.TestConfigs[0].Encap)
	assert.Equal(t, DataPlane(""), cfg.TestConfigs[0].Dataplane)
	assert.Equal(t, 0, cfg.TestConfigs[0].NumPolicies)
	assert.Equal(t, 0, cfg.TestConfigs[0].NumPods)
	assert.Equal(t, 0, cfg.TestConfigs[0].NumServices)
	assert.Equal(t, 0, cfg.TestConfigs[0].Iterations)
	assert.Equal(t, 60, cfg.TestConfigs[0].Duration)
	assert.Equal(t, "", cfg.TestConfigs[0].CalicoNodeCPULimit)
	assert.Equal(t, 0, cfg.TestConfigs[0].DNSPerfNumDomains)
	assert.Equal(t, DNSPerfMode(""), cfg.TestConfigs[0].DNSPerfMode)
	assert.Equal(t, "testns", cfg.TestConfigs[0].TestNamespace)
}

func TestInvalidTestKind(t *testing.T) {
	fileContent := `- testKind: nosuchtest`
	filePath := "/tmp/test_configs.yaml"
	err := os.WriteFile(filePath, []byte(fileContent), 0644)
	require.NoError(t, err)
	defer os.Remove(filePath)
	os.Setenv("TESTCONFIGFILE", filePath)

	var cfg Config
	err = envconfig.Process("bench", &cfg)
	require.NoError(t, err)

	err = loadTestConfigsFromFile(&cfg)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "Field validation for 'TestKind' failed on the 'oneof' tag")
}

func TestNew(t *testing.T) {
	fileContent := `
- testKind: thruput-latency
`
	filePath := "/tmp/test_configs.yaml"
	err := os.WriteFile(filePath, []byte(fileContent), 0644)
	require.NoError(t, err)
	defer os.Remove(filePath)
	os.Setenv("OPERATOR_PROM_URL", "http://prometheus-operated:9090")
	os.Setenv("K8S_PROM_URL", "http://prometheus-k8s:9090")
	os.Setenv("RESULTS_FILE", "/results/results.json")
	os.Setenv("TESTCONFIGFILE", filePath)
	cfg, _, err := New(context.TODO())
	assert.NoError(t, err)
	assert.Equal(t, "http://prometheus-operated:9090", cfg.OperatorPromURL)
	assert.Equal(t, "http://prometheus-k8s:9090", cfg.K8sPromURL)
	assert.Equal(t, "/results/results.json", cfg.ResultsFile)
}
func TestLoadTestConfigsFromFile(t *testing.T) {
	fileContent := `
- testKind: thruput-latency
  numpolicies: 100
  numservices: 102
  numpods: 101
  duration: 5
  hostnetwork: false
  encap: vxlan
  dataplane: bpf
  iterations: 3
  TestNamespace: myns
`
	filePath := "/tmp/test_configs.yaml"
	err := os.WriteFile(filePath, []byte(fileContent), 0644)
	require.NoError(t, err)
	defer os.Remove(filePath)

	var cfg Config
	cfg.TestConfigFile = filePath
	err = loadTestConfigsFromFile(&cfg)
	require.NoError(t, err)
	assert.Equal(t, 1, len(cfg.TestConfigs))
	assert.Equal(t, TestKindQperf, cfg.TestConfigs[0].TestKind)
	assert.Equal(t, Encap("vxlan"), cfg.TestConfigs[0].Encap)
	assert.Equal(t, DataPlane("bpf"), cfg.TestConfigs[0].Dataplane)
	assert.Equal(t, 100, cfg.TestConfigs[0].NumPolicies)
	assert.Equal(t, 101, cfg.TestConfigs[0].NumPods)
	assert.Equal(t, 102, cfg.TestConfigs[0].NumServices)
	assert.Equal(t, 3, cfg.TestConfigs[0].Iterations)
	assert.Equal(t, "myns", cfg.TestConfigs[0].TestNamespace)
}
