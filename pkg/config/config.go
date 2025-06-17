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
	"fmt"
	"net/http"
	"os"
	"time"

	validator "github.com/go-playground/validator/v10"
	"github.com/kelseyhightower/envconfig"
	monitoringv1 "github.com/prometheus-operator/prometheus-operator/pkg/apis/monitoring/v1"
	log "github.com/sirupsen/logrus"
	v3 "github.com/tigera/api/pkg/apis/projectcalico/v3"
	operatorv1 "github.com/tigera/operator/api/v1"
	"golang.org/x/net/proxy"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	networkingv1 "k8s.io/api/networking/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/util/yaml"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/tools/clientcmd"
	ctrlclient "sigs.k8s.io/controller-runtime/pkg/client"
)

var validate *validator.Validate

// Config represents the global configuration for the benchmark.
type Config struct {
	OperatorPromURL string `envconfig:"OPERATOR_PROM_URL" default:""`
	K8sPromURL      string `envconfig:"K8S_PROM_URL" default:""`
	Kubeconfig      string `envconfig:"KUBECONFIG" default:""`
	K8sAPIHost      string `envconfig:"KUBERNETES_SERVICE_HOST" default:""`
	K8sAPIPort      string `envconfig:"KUBERNETES_SERVICE_PORT" default:""`
	ESUrl           string `envconfig:"ELASTICSEARCH_URL" default:""`
	ESUser          string `envconfig:"ELASTICSEARCH_USER" default:"elastic"`
	ESPassword      string `envconfig:"ELASTICSEARCH_TOKEN" default:""`
	ESAPIKey        string `envconfig:"ELASTICSEARCH_KEY" default:""`
	TestTimeout     int    `default:"600"`
	ResultsFile     string `envconfig:"RESULTS_FILE" default:"/results/results.json"`
	CalicoVersion   string `default:""`
	ProxyAddress    string `envconfig:"HTTP_PROXY" default:""`
	TestConfigFile  string `envconfig:"TESTCONFIGFILE" required:"true"`
	LogLevel        string `envconfig:"LOG_LEVEL" default:"info"`
	WebServerImage  string `envconfig:"WEBSERVER_IMAGE" default:"quay.io/tigeradev/tiger-bench-nginx:v0.4.0"`
	PerfImage       string `envconfig:"PERF_IMAGE" default:"quay.io/tigeradev/tiger-bench-perf:v0.4.0"`
	TTFRImage       string `envconfig:"TTFR_IMAGE" default:"quay.io/tigeradev/tiger-bench-ttfr:v0.4.0"`
	TestConfigs     testConfigs
}

// Clients holds the various clients used to talk to things
type Clients struct {
	Clientset  *kubernetes.Clientset
	CtrlClient ctrlclient.Client
	WebClient  *http.Client
}

type testConfigs []*TestConfig

// TestKind represents the kind of test to run.
type TestKind string

// TestKind possible values.
const (
	TestKindDNSPerf TestKind = "dnsperf"
	TestKindIperf   TestKind = "iperf"
	TestKindQperf   TestKind = "thruput-latency"
	TestKindTTFR    TestKind = "ttfr"
)

// Encap represents the encapsulation type to use.
type Encap string

// Encap possible values.
const (
	EncapNone  Encap = "none"
	EncapVXLAN Encap = "vxlan"
	EncapIPIP  Encap = "ipip"
	EncapUnset Encap = ""
)

// DataPlane represents the data plane to use.
type DataPlane string

// DataPlane possible values.
const (
	DataPlaneIPTables DataPlane = "iptables"
	DataPlaneBPF      DataPlane = "bpf"
	DataPlaneNftables DataPlane = "nftables"
	DataPlaneUnset    DataPlane = ""
)

// DNSPerfMode represents the mode to use for DNSPerf.
type DNSPerfMode string

// DNSPerfMode possible values.
const (
	DNSPerfModeInline            DNSPerfMode = "Inline"
	DNSPerfModeNoDelay           DNSPerfMode = "NoDelay"
	DNSPerfModeDelayDeniedPacket DNSPerfMode = "DelayDeniedPacket"
	DNSPerfModeDelayDNSResponse  DNSPerfMode = "DelayDNSResponse"
	DNSPerfModeUnset             DNSPerfMode = ""
)

// TestConfig represents a test to run on a cluster, and the configuration for the test.
type TestConfig struct {
	TestKind            TestKind  `validate:"required,oneof=dnsperf iperf thruput-latency ttfr"`
	Encap               Encap     `validate:"omitempty,oneof=none vxlan ipip"`
	Dataplane           DataPlane `validate:"omitempty,oneof=iptables bpf nftables"`
	NumPolicies         int       `validate:"gte=0"`
	NumServices         int       `validate:"gte=0"`
	NumPods             int       `validate:"gte=0"`
	HostNetwork         bool
	TestNamespace       string      `default:"testns"`
	Iterations          int         `default:"1" validate:"gte=0"`
	Duration            int         `default:"60"`
	DNSPerf             *DNSConfig  `validate:"required_if=TestKind dnsperf"`
	Perf                *PerfConfig `validate:"required_if=TestType thruput-latency,required_if=TestType iperf"`
	TTFRConfig          *TTFRConfig `validate:"required_if=TestType ttfr"`
	CalicoNodeCPULimit  string
	LeaveStandingConfig bool
}

// PerfConfig details which tests to run in thruput-latency and iperf tests.
type PerfConfig struct {
	Direct           bool   // Whether to do a direct pod-pod test
	Service          bool   // Whether to do a pod-service-pod test
	External         bool   // Whether to test from this container to the external IP for an external-service-pod test
	ControlPort      int    // The port to use for the control connection in tests.  Used by qperf tests.
	TestPort         int    // The port to use for the test connection in tests.  Used by qperf and iperf tests
	ExternalIPOrFQDN string // The external IP or DNS name to connect to for an external-service-pod test
}

// DNSConfig contains the configuration specific to DNSPerf tests.
type DNSConfig struct {
	NumDomains    int         `validate:"gte=0"`
	Mode          DNSPerfMode `validate:"omitempty,oneof=Inline NoDelay DelayDeniedPacket DelayDNSResponse"`
	RunStress     bool        `default:"true" validate:"omitempty"`
	TestDNSPolicy bool        `default:"true" validate:"omitempty"`
	NumTargetPods int         `default:"100" validate:"gte=1"`
	TargetType    string      `default:"pod" validate:"omitempty,oneof=pod service"`
}

// TTFRConfig contains the configuration specific to TTFR tests.
type TTFRConfig struct {
	TestPodsPerNode int     `validate:"gte=0"`
	Rate            float64 `validate:"gte=0"`
}

// New returns a new instance of Config.
func New(ctx context.Context) (Config, Clients, error) {
	// get environment variables
	var config Config
	var clients Clients
	err := envconfig.Process("bench", &config)
	if err != nil {
		return config, clients, err
	}

	loglevel, err := log.ParseLevel(config.LogLevel)
	if err != nil {
		log.WithError(err).Fatal("failed to parse log level")
	}
	log.SetLevel(loglevel)

	// load testconfigs from file
	err = loadTestConfigsFromFile(&config)
	if err != nil {
		return config, clients, err
	}

	log.Debugf("Config: %+v", config)

	if config.Kubeconfig != "" {
		log.Debug("Creating clients")
		clients.Clientset, clients.CtrlClient = newClientSet(config)

		info := &v3.ClusterInformation{}
		log.Debug("Getting cluster information")
		myctx, cancel := context.WithTimeout(ctx, 30*time.Second)
		defer cancel()
		err = clients.CtrlClient.Get(myctx, ctrlclient.ObjectKey{Name: "default"}, info)
		if err != nil {
			log.WithError(err).Error("failed to get cluster information")
			return config, clients, err
		}
		log.Debug("cluster information is ", info)
		config.CalicoVersion = info.Spec.CalicoVersion
		log.Infof("Calico version is %s", config.CalicoVersion)
	} else {
		log.Warning("Kubeconfig not set, this is only valid for testing")
	}

	clients.WebClient, err = createWebclient(config)
	if err != nil {
		return config, clients, fmt.Errorf("failed to create web client")
	}

	return config, clients, nil
}

func loadTestConfigsFromFile(cfg *Config) error {
	log.Info("Reading test config file", cfg.TestConfigFile)
	yamlFile, err := os.ReadFile(cfg.TestConfigFile)
	log.Info(string(yamlFile))
	if err != nil {
		return err
	}
	err = yaml.Unmarshal(yamlFile, &cfg.TestConfigs)
	if err != nil {
		return err
	}
	if len(cfg.TestConfigs) == 0 {
		return fmt.Errorf("no test configs found in file")
	}
	return defaultAndValidate(cfg)
}

func defaultAndValidate(cfg *Config) error {
	log.Debug("Entering defaultAndValidate")
	validate = validator.New(validator.WithRequiredStructEnabled())

	for _, tcfg := range cfg.TestConfigs {
		err := validate.Struct(tcfg)
		if err != nil {
			return err
		}
		if tcfg.Duration == 0 {
			tcfg.Duration = 60
		}
		if tcfg.TestNamespace == "" {
			tcfg.TestNamespace = "testns"
		}
		if tcfg.TestKind == "dnsperf" {
			if tcfg.DNSPerf.NumDomains < 0 {
				return fmt.Errorf("NumDomains must be non-negative for a dnsperf test")
			}
		}
		if tcfg.TestKind == "thruput-latency" || tcfg.TestKind == "iperf" {
			if tcfg.Perf == nil {
				tcfg.Perf = &PerfConfig{true, true, false, 32000, 0, ""} // Default so that old configs don't break
				continue
			}
			if tcfg.Perf.External {
				if tcfg.Perf.ExternalIPOrFQDN == "" {
					return fmt.Errorf("ExternalIPOrFQDN is required for an external thruput-latency test")
				}
				if tcfg.TestKind == "thruput-latency" {
					if tcfg.Perf.ControlPort == 0 {
						return fmt.Errorf("ControlPort is required for an external thruput-latency test")
					}
					if tcfg.Perf.ControlPort > 65535 || tcfg.Perf.ControlPort < 1 {
						return fmt.Errorf("ControlPort must be between 1 and 65535")
					}
				}
				if tcfg.Perf.TestPort == 0 {
					return fmt.Errorf("TestPort is required for an external thruput-latency test")
				}
				if tcfg.Perf.TestPort > 65535 || tcfg.Perf.TestPort < 1 {
					return fmt.Errorf("TestPort must be between 1 and 65535")
				}
			}
		}
		if tcfg.TestKind == "dnsperf" {
			if tcfg.DNSPerf.TestDNSPolicy {
				if tcfg.DNSPerf.Mode == DNSPerfModeUnset {
					return fmt.Errorf("Mode must be set for a dnsperf test with TestDNSPolicy enabled")
				}
				if tcfg.DNSPerf.NumDomains < 0 {
					return fmt.Errorf("NumDomains must be non-negative for a dnsperf test with TestDNSPolicy enabled")
				}
			}
		}
	}
	return nil
}

func newClientSet(config Config) (*kubernetes.Clientset, ctrlclient.Client) {
	log.Debug("Entering newClientSet function")

	kconfig, err := clientcmd.BuildConfigFromFlags("", config.Kubeconfig)
	if err != nil {
		log.WithError(err).Panic("failed to build config")
	}
	kconfig.QPS = 1000
	kconfig.Burst = 2000
	clientset, err := kubernetes.NewForConfig(kconfig)
	if err != nil {
		log.WithError(err).Panic("failed to create clientset")
	}

	scheme := runtime.NewScheme()
	err = networkingv1.AddToScheme(scheme)
	if err != nil {
		log.WithError(err).Panic("failed to add networkingv1 to scheme")
	}
	err = operatorv1.AddToScheme(scheme)
	if err != nil {
		log.WithError(err).Panic("failed to add operatorv1 to scheme")
	}
	err = metav1.AddMetaToScheme(scheme)
	if err != nil {
		log.WithError(err).Panic("failed to add metav1 to scheme")
	}
	err = appsv1.AddToScheme(scheme)
	if err != nil {
		log.WithError(err).Panic("failed to add appsv1 to scheme")
	}
	err = corev1.AddToScheme(scheme)
	if err != nil {
		log.WithError(err).Panic("failed to add corev1 to scheme")
	}
	err = v3.AddToScheme(scheme)
	if err != nil {
		log.WithError(err).Panic("failed to add v3 to scheme")
	}
	err = monitoringv1.AddToScheme(scheme)
	if err != nil {
		log.WithError(err).Panic("failed to add monitoringv1 to scheme")
	}
	ctrlClient, err := ctrlclient.New(kconfig, ctrlclient.Options{Scheme: scheme})
	if err != nil {
		log.WithError(err).Panic("failed to create ctrlclient")
	}

	return clientset, ctrlClient
}

func createWebclient(config Config) (*http.Client, error) {
	var err error
	client := &http.Client{
		Transport: &http.Transport{},
	}
	if config.ProxyAddress != "" {
		// setup socks5 proxy
		dialer, err := proxy.SOCKS5("tcp", config.ProxyAddress, nil, proxy.Direct)
		if err != nil {
			return client, fmt.Errorf("failed to create proxy dialer")
		}
		client = &http.Client{
			Transport: &http.Transport{
				Dial: dialer.Dial,
			},
		}
	}
	return client, err
}
