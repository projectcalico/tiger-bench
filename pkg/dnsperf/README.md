# DNSPerf tests

The point of this test is to test DNS Policy performance in Calico Enterprise clusters.

Example Config:

```
- testKind: dnsperf
  Dataplane: iptables
  TestNamespace: dnstest
  numServices: 1000
  iterations: 1
  duration: 60
  calicoNodeCPULimit: 10m
  DNSPerf:
    numDomains: 2
    runStress: false
    mode: Inline
    testDNSPolicy: true
    targetURL: http://www.example.com
```

`testKind` sets the test to be a dnsperf test.
Setting `Dataplane` causes the tool to reconfigure your cluster to use a particular dataplane. Leave it blank to test whatever your cluster already uses. Valid values: <blank>, `bpf`, `iptables`, `nftables`
`numServices` causes the tool to set up "standing config", which includes services. This takes the form of 10 pods, each backing N services (where N is the number you set)
`iterations` is not used by dnsperf tests.
`duration` defines the length of time the test will be repeated for
`calicoNodeCPULimit` sets a CPU limit on the calico-node pods. This can be helpful when testing DNS policy modes where calico-node needs to update the dataplane, but is overloaded.
`DNSPerf` is the name of the dnsperf specific config:
`numDomains` - defines the number of domains that should be added to the DNS policy
`RunStress` - controls whether the test should run some control plane stress while the curl is executed - this is useful when testing DNS Policy, because it makes calico-node do work, which may delay DNS policy processing
`TestDNSPolicy` - controls whether or not the test should apply a DNS policy
`targetURL` - specifies the URL to target with curl requests (e.g., `http://www.example.com` or `https://www.example.com`). Tests use external URLs and no longer create target pods. The domain will be extracted where needed for DNS policy rules.

## Prerequisites

This test requires

- a cluster running Calico Enterprise
    - v3.20+ for inline mode in BPF mode
    - v3.21+ for inline mode in iptables mode
- a webserver *outside the cluster* that will reply with 200 OK to a high rate of curl requests.  For meaningful test results, this target should:
    - be near the cluster (in terms of latency)
    - not throttle connections (do not use www.google.com!)
    - have high bandwidth available (so our latency measurements are not affected by queuing)
    - have a DNS name (this test cannot use an IP)
- one node labelled with tigera.io/test-nodepool=default-pool - this is the node where the test will run its pods

There are 4 DNS policy modes:
"DelayDNSResponse", "DelayDeniedPacket", "Inline", "NoDelay". Support for each with different versions and dataplanes varies, please check the Calico Enterprise docs.
See https://docs.tigera.io/calico-enterprise/latest/reference/resources/felixconfig#dnspolicymode for details of how they differ.
Iptables supports all 4 modes.
BPF supports Inline and NoDelay modes
Nftables supports NoDelay, DelayDeniedPacket and DelayDNSResponse modes.

## Operation

The test operates by execing into a test pod and running a curl command. That curl command looks something like this:

```
curl -m 8 -w '{"time_lookup": %{time_namelookup}, "time_connect": %{time_connect}}\n' -s -o /dev/null http://www.example.com
```

The curl therefore outputs a lookup time and a connect time, which are recorded by the test. The lookup time is the time between curl sending a DNS request for the target FQDN and getting a response from CoreDNS. The connect time is the time taken from DNS response to completion of the TCP 3-way handshake. The maximum time curl will wait for a response is 8 seconds.

The test cycles round, creating test pods, running the curl command in them, and tearing them down in a multi-threaded manner.

While this is going on, the test is also running tcpdump on the hosts, and watching how the TCP connections progress. After the test completes, the tcpdump is analysed to look for duplicate SYN and SYNACK packets, which can be indicative of a slow TCP connection setup (e.g. if using NoDelay or DelayDeniedPacket modes).

Finally, the test does some basic statistical analysis on the results, and generates a result json.

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
        "TargetURL": "http://www.example.com"
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

`DuplicateSYN` gives the number of duplicate SYN packets seen in the tcpdump (useful for DNS Policy performance). tcpdump is only run when TestDNSPolicy=true.
`DuplicateSYNACK` gives the number of duplicate SYNACK packets seen in the tcpdump (useful for DNS Policy performance). tcpdump is only run when TestDNSPolicy=true.
`FailedCurls` and `SuccessfulCurls` show the total number of failed and successful curl attempts during that test.

## Understanding the results

Each DNS policy mode is likely to show different behaviour.
- NoDelay allows the DNS lookup to proceed as normal, but the pod will not be able to make a connection until the IP returned in the DNS lookup has been programmed into the dataplane by calico-node.  This can result in connection failures, which need to be retried by the app, which typically happens on a 5 second timer.
- DelayDNSResponse mode delays the DNS response packet until the IP within it has been programmed into the dataplane by calico-node.  This can result in longer lookup times from the PoV of the pod, but the initial connection should succeed.
- DelayDeniedPacket mode permits the DNS lookup to proceed as normal, but holds the outgoing packets until the dataplane is programmed by calico-node.  This can result in duplicated SYNs as the network stack retries the connection setup.

As you can see, these 3 modes all require calico-node to update the dataplane.  If calico-node is starved of CPU (e.g. low limit) and/or very busy (e.g. lots of policy changes or pod creation/deletion), these delays can be noticeable.

The final mode:
- Inline mode uses a BPF program (which run in a VM in kernel space) to update the dataplane as soon as it sees the DNS response, as part of forwarding the packet.  This happens independently of calico-node and is very fast.
