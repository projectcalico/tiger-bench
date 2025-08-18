# DNSPerf tests

This test has 2 use cases:
- to test DNS Policy performance in Calico Enterprise clusters
- to test the latency of a service using Curl

Example Config:
```
- testKind: dnsperf
  Dataplane: iptables
  TestNamespace: dnstest
  numServices: 1000
  iterations: 1
  duration: 60
  DNSPerf:
    numDomains: 0
    RunStress: false
    TestDNSPolicy: false
    numTargetPods: 10
    targetType: service
```
`testKind` sets the test to be a dnsperf test.
Setting `Dataplane` causes the tool to reconfigure your cluster to use a particular dataplane.  Leave it blank to test whatever your cluster already uses.  Valid values: <blank>, `bpf`, `iptables`, `nftables`
`numServices` causes the tool to set up "standing config", which includes services.  This takes the form of 10 pods, each backing N services (where N is the number you set)
`iterations` is not used by dnsperf tests.
`duration` defines the length of time the test will be repeated for
`DNSPerf` is the name of the dnsperf specific config:
    `numDomains` - defines the number of domains that should be added to the DNS policy
    `RunStress` - controls whether the test should run some control plane stress while the curl is executed - this is useful when testing DNS Policy, because it makes calico-node do work.
    `TestDNSPolicy` - controls whether or not the test should be a DNS policy test
    `numTargetPods` - controls the number of target pods that should be created.  Curls will be round-robined to the targets. Must be at least 1.
    `targetType` - controls whether the target passed to curl is a pod FQDN or a service FQDN.  Valid values: `pod`, `service`


## Operation
The test operates by execing into a test pod and running a curl command.  That curl command looks something like this:
```
curl -m 8 -w '{"time_lookup": %{time_namelookup}, "time_connect": %{time_connect}}\n' -s -o /dev/null http://service.cluster.local:8080
```
The curl therefore outputs a lookup time and a connect time, which are recorded by the test.  The lookup time is the time between curl sending a DNS request for the target FQDN and getting a response from CoreDNS.  The connect time is the time taken from DNS response to completion of the TCP 3-way handshake.

The test cycles round, creating test pods, running the curl command in them, and tearing them down.


## Result
Example result:
```
[
  {
    "config": {
      "TestKind": "dnsperf",
      "Encap": "",
      "Dataplane": "iptables",
      "NumPolicies": 0,
      "NumServices": 1000,
      "NumPods": 0,
      "HostNetwork": false,
      "TestNamespace": "dnstest",
      "Iterations": 1,
      "Duration": 60,
      "DNSPerf": {
        "NumDomains": 0,
        "Mode": "",
        "RunStress": false,
        "TestDNSPolicy": false,
        "NumTargetPods": 10,
        "TargetType": "service"
      },
      "Perf": null,
      "TTFRConfig": null,
      "CalicoNodeCPULimit": "",
      "LeaveStandingConfig": false
    },
    "ClusterDetails": {
      "Cloud": "unknown",
      "Provisioner": "kubeadm",
      "NodeType": "linux",
      "NodeOS": "Ubuntu 20.04.6 LTS",
      "NodeKernel": "5.15.0-1081-gcp",
      "NodeArch": "amd64",
      "NumNodes": 4,
      "Dataplane": "iptables",
      "IPFamily": "ipv4",
      "Encapsulation": "VXLANCrossSubnet",
      "WireguardEnabled": false,
      "Product": "calico",
      "CalicoVersion": "v3.30.0-0.dev-852-g389eae30ae5d",
      "K8SVersion": "v1.33.1",
      "CRIVersion": "containerd://1.7.27",
      "CNIOption": "Calico"
    },
    "dnsperf": {
      "LookupTime": {
        "min": 0.009491,
        "max": 0.037909,
        "avg": 0.022658038461538466,
        "P50": 0.023875,
        "P75": 0.025672,
        "P90": 0.02856,
        "P99": 0.03434,
        "datapoints": 104
      },
      "ConnectTime": {
        "min": 0.00018300000000000087,
        "max": 0.0031160000000000007,
        "avg": 0.001034471153846154,
        "P50": 0.0008029999999999982,
        "P75": 0.0014260000000000002,
        "P90": 0.0020359999999999996,
        "P99": 0.0029660000000000016,
        "datapoints": 104
      },
      "DuplicateSYN": 0,
      "DuplicateSYNACK": 0,
      "FailedCurls": 0,
      "SuccessfulCurls": 104
    }
  }
]
```
the dnsperf section contains statistical summaries of the curl results for LookupTime and ConnectTime.

`DuplicateSYN` gives the number of duplicate SYN packets seen in the tcpdump (useful for DNS Policy performance).  tcpdump is only run when TestDNSPolicy=true.
`DuplicateSYNACK` gives the number of duplicate SYNACK packets seen in the tcpdump (useful for DNS Policy performance). tcpdump is only run when TestDNSPolicy=true.
`FailedCurls` and `SuccessfulCurls` show the total number of failed and successful curl attempts during that test.