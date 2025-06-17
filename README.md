# Tiger-bench

tiger-bench is a tool which uses qPerf/iPerf to measure the performance of a cluster (usually one running Calico).
It also provides a framework for us to extend later with other tests.

## Quickstart

1. Setup your cluster.
1. export the path to your kubeconfig for this cluster as KUBECONFIG.
1. Label at least 2 nodes in your cluster to run these tests on with `kubectl label node <nodename> tigera.io/test-nodepool=default-pool`
1. Optionally, load the test images into your private registry.
   `docker pull <image>`, `docker tag <private-image-name>`, `docker push <private-image-name>`

   The images are:
   `quay.io/tigeradev/tiger-bench-nginx:v0.4.0`
   `quay.io/tigeradev/tiger-bench-perf:v0.4.0`
   `quay.io/tigeradev/tiger-bench:v0.4.0` - this is the tool itself.

1. Create a `testconfig.yaml` file, containing a list of test definitions you'd like to run (see example provided)
1. Run the tool, substituting the image names in the command below if needed, and modifying the test parameters if desired:
   ```
   docker run --rm --net=host \
   -v "${PWD}":/results \
   -v ${KUBECONFIG}:/kubeconfig \
   -v ${PWD}/testconfig.yaml:/testconfig.yaml \
   -v $HOME/.aws:/root/.aws \
   -e AWS_SECRET_ACCESS_KEY \
   -e AWS_ACCESS_KEY_ID \
   -e AWS_SESSION_TOKEN \
   -e LOG_LEVEL=INFO \
   -e WEBSERVER_IMAGE="quay.io/tigeradev/tiger-bench-nginx:v0.4.0" \
   -e PERF_IMAGE="quay.io/tigeradev/tiger-bench-perf:v0.4.0" \
   quay.io/tigeradev/tiger-bench:v0.4.0
   ```
1. See results in the `results.json` file in your local directory!

## Standing config

tiger-bench can create standing config in the cluster to make the test more realistic. It can create:

- pods
- services (and pods to back them)
- network policy

These resources will be created in the namespace specified by the test (defaults to `testns`).
Pods will be created on nodes which have the `tigera.io/test-nodepool=default-pool` label.

tiger-bench will also create one additional kubernetes network policy to permit traffic required between the performance pods to do their job (so that the standing policies do not break the test). The name of this policy "zzz-perf-test-policy" is chosen so that it gets applied last by Felix.

## Performance tests

tiger-bench will run iperf and qperf daemonsets on the nodes with the `tigera.io/test-nodepool=default-pool` label in the `testns` namespace. It then runs tests between pods on different nodes. From iperf we gather the number of retransmits and the throughput. From qperf we gather the one-way latency and another throughput measurement.

## Operation

The Benchmarker tool is configured via env vars. Here's a typical run (from [`run.sh`](run.sh))

```
docker run --rm --net=host \
-v "${PWD}":/results \
-v ${KUBECONFIG}:/kubeconfig \
-v ${PWD}/testconfig.yaml:/testconfig.yaml \
-v $HOME/.aws:/root/.aws \
-e AWS_SECRET_ACCESS_KEY \
-e AWS_ACCESS_KEY_ID \
-e WEBSERVER_IMAGE="quay.io/tigeradev/tiger-bench-nginx:v0.4.0" \
-e PERF_IMAGE="quay.io/tigeradev/tiger-bench-perf:v0.4.0" \
quay.io/tigeradev/tiger-bench:v0.4.0
```

The tool runs in the hosts network namespace to ensure it has the same access as a user running kubectl on the host.

kubeconfig is passed to the tool by the `KUBECONFIG` env var, which is used to mount the kubeconfig file into the container.
Results are written to the `/results` directory in the container, which has mounted the current directory in this case, so results will appear in the users current directory.

The user's `$HOME/.aws` directory, `AWS_SECRET_ACCESS_KEY`, `AWS_ACCESS_KEY_ID`, and `AWS_SESSION_TOKEN` are mounted into the container to provide AWS authentication. EKS clusters use `aws-iam-authenticator` or `aws eks get-token` in addition to the certificates in the kubeconfig file to provide an additional layer of control. If you're not running this tool against an EKS cluster, these can be left out of the `docker run` command.

`WEBSERVER_IMAGE` and `PERF_IMAGE` specify the images that should be used by the tool. The webserver image is used for standing pods, while the perf image contains the iperf and qperf binaries and is used for the iperf and qperf tests. If not specified, they default to the values in the command above.
If you are in an environment with a private docker registry, you should copy the images into it, and use these env vars to tell the tool the private registry names.

A list of test run definitions are provided as [`testconfig.yaml`](testconfig.yaml).

`testconfig.yaml` might look like this:

```
- testKind: thruput-latency
  numPolicies: 5
  numServices: 10
  numPods: 7
  duration: 5
  hostNetwork: false
  iterations: 1
  leaveStandingConfig: true
  Perf:
    direct: true
    service: true
    external: true
    ControlPort: 32000
    TestPort: 32221
    ExternalIPOrFQDN: "a6c519e4e9d5a46d795e0d41bf82e93f-1429478103.us-west-2.elb.amazonaws.com"
- testKind: thruput-latency
  numPolicies: 20
  numServices: 3
  numPods: 9
  duration: 4
  hostNetwork: true
  encap: none
  dataplane: bpf
  iterations: 1
  leaveStandingConfig: true
- testKind: ttfr
  numPolicies: 100
  numServices: 10
  numPods: 7
  duration: 60
  hostNetwork: false
  iterations: 1
  leaveStandingConfig: false
  TestNamespace: testns2
  TTFRConfig:
    TestPodsPerNode: 53
    Rate: 2.5
```

There are 2 tests requested in this example config.

`testKind` is required - at present you can only ask for `"thruput-latency"` or `ttfr`

`numPolicies`, `numServices`, `numPods` specify the standing config desired for this test. Standing config exists simply to "load" the cluster up with config, they do not take any active part in the tests themselves. The number that you can create is limited by your cluster - you cannot create more standing pods than will fit on your cluster!

`leaveStandingConfig` tells the tool whether it should leave or clean up the standing resources it created for this test. It is sometimes useful to leave standing config up between tests, especially if it takes a long time to set up.

`duration` is the length of each part of the test run in seconds. In this example, each test part will run for 5 seconds.

`hostNetwork` specifies whether the test should run in the pod network (`false`) or the host's network (`true`). It is often helpful to be able to run a test in both networks to compare performance.

`testNamespace` specifies the namespace that should be used for this test. All standing resources and test resources will be created within this namespace. In this example it was omitted, so the test will use `testns`.

`iterations` specifies the number of times the measurement should be repeated. Note that a single iteration will include 2 runs of qperf - one direct pod-pod and the other pod-service-pod. If you just want to set up standing config, `iterations` can be set to zero.

`Perf` defines extra settings for `thruput-latency` tests. if you do not specify Perf, it defaults the following (to preserve behaviour with existing test config files):

```
direct: true
service: true
external: false
```

`direct` is a boolean, which determines whether the test should run a direct pod-to-pod test.
`service` is a boolean, which determines whether the test should run a pod-to-service-to-pod test.
`external` is a boolean, which determines whether the test should run a test from whereever this test is being run to an externally exposed service.
If `external=true`, you must also supply `ExternalIPOrFQDN`, `TestPort` and `ControlPort` (for a thruput-latency test) to tell the test the IP and ports it should connect to. The ExternalIPOrFQDN will be whatever is exposed to the world, and might be a LoadBalancer IP, or a node IP, or something else, depending on how you exposed the service. The Test and Control ports need to be the same as used on the test server pod (because the test tools were not designed to work in an environment with NAT).

Note that the tool will NOT expose the services for you, because there are too many different ways to expose services to the world. You will need to expose pods with the label `app: qperf` in the test namespace to the world for this test to work. An example of exposing these pods using NodePorts can be found in `external_service_example.yaml`. If you wanted to change that to use a LoadBalancer, simply change `type: NodePort` to `type: LoadBalancer`.

For `thruput-latency` tests, you will need to expose 2 ports from those pods: A TCP `TestPort` and a `ControlPort`. You must not map the port numbers between the pod and the external service, but they do NOT need to be consecutive. i.e. if you specify TestPort=32221, the pod will listen on port 32221 and whatever method you use to expose that service to the outside world must also use that port number.

A `ttfr` test may have the following additional config:

```
  TTFRConfig:
    TestPodsPerNode: 80
    Rate: 2.5
```

The `TestPodsPerNode` setting controls the number of pods it will try to set up on each test node

The `Rate` is the rate at which it will send requests to set up pods, in pods per second. Note that the acheivable rate depends on a number of things, including the TestPodsPerNode setting (since it cannot set up more than TestPodsPerNode multiplied by the number of nodes with the test label, the tool will stall if all the permitted pods are in the process of starting or terminating). And that will depend on the speed of the kubernetes control plane, kubelet, etc.

In the event that you ask for a rate higher than the tool can acheive, it will run at the maximum rate it can, while logging warnings that it is "unable to keep up with rate". If the problem is running out of pod slots, it will log that also, and you can fix it by either increasing the pods per node or giving more nodes the test label.

### Settings which can reconfigure your cluster

The following test settings will _reconfigure your cluster_. This could cause disruption to other things running on the cluster, so be careful specifying these in tests.

`encap` specifies which encapsulation you want Calico to use during the test. _The tool will reconfigure the cluster's default ippool_, if needed, to set this, which could cause disruption to other things running on this cluster.

- `""` - use the cluster as-is without reconfiguring (default)
- `none` - reconfigure the cluster if needed to use no encapsulation at all. Note that your underlying network will need to support this.
- `vxlan` - reconfigure the cluster if needed to use vxlan-always encapsulation.
- `ipip` - reconfigure the cluster if needed to use ipip-always encapsulation.

`dataplane` specifies which dataplane you want Calico to use during the test. _The tool will reconfigure the cluster_, if needed, to set this, which could cause disruption to other things running on this cluster.

- `""` - use the cluster as-is without reconfiguring (default)
- `bpf` - reconfigure the cluster if needed to use the BPF dataplane.
- `iptables` - reconfigure the cluster if needed to use the iptables dataplane.

`calicoNodeCPULimit` specifies the CPU limit you want calico-node pods to be configured with during the test. The tool will trigger a restart of calico-node pods to speed up the rollout of this which could cause disruption to other things running on this cluster. Leave blank to use the cluster as-is without reconfiguring.

tiger-bench runs through the list of tests one by one, reconfiguring the cluster as needed and deploying the correct number of standing resources before running each specified test.

Output is a json file (`/results/results.json` in the container unless you override it) containing a json list of test results.

### The "thruput-latency" test

For a "thruput-latency" test, the tool will:

- Create a test namespace
- Create a deployment of `numPods` pods that are unrelated to the test and apply `numPolicies` policies to them (standing pods and policies).
- Create another deployment of 10 pods, and create `numServices` that point to those 10 pods.
- Wait for those to come up.
- Create a pair of (optionally host-networked) pods, on different nodes (with the `tigera.io/test-nodepool=default-pool` label) to run the test between. Wait for them to be running.
- Create a service for each of the test pods (for use in the pod-svc-pod scenario)
- Execute qperf direct between the two pods for the configured duration (if configured to do so): `qperf <ip> -t <duration> -vv -ub -lp 32000 -ip 32001 tcp_bw tcp_lat`
- Collects the output from qperf and parses it to collect latency and throughput results.
- Repeat the qperf command, this time using the service IP for that pod (if configured to do so)
- Collects the output from qperf and parses it to collect latency and throughput results.
- Repeat the qperf command, this time using the External IP and Ports for the exposed service (if configured to do so)
- Repeat the set of qperf commands for the given number of iterations (re-using the same pods)
- Collate results and compute min/max/average/50/75/90/99th percentiles
- Output that summary into a JSON format results file.
- Optionally delete the test namespace (which will cause all test resources within it to be deleted)
- Wait for everything to finish being cleaned up.

This test measures Latency and Throughput.

An example result from a "thruput-latency" test might look like:

```
  {
    "config": {
      "TestKind": "thruput-latency",
      "Encap": "",
      "Dataplane": "",
      "NumPolicies": 5,
      "NumServices": 10,
      "NumPods": 7,
      "HostNetwork": false,
      "TestNamespace": "testns",
      "Iterations": 1,
      "Duration": 10,
      "DNSPerf": null,
      "Perf": {
        "Direct": true,
        "Service": true,
        "External": true,
        "ControlPort": 32000,
        "TestPort": 32221,
        "ExternalIPOrFQDN": "a6c519e4e9d5a46d795e0d41bf82e93f-1429478103.us-west-2.elb.amazonaws.com"
      },
      "CalicoNodeCPULimit": "",
      "LeaveStandingConfig": false
    },
    "ClusterDetails": {
      "Cloud": "unknown",
      "Provisioner": "kubeadm",
      "NodeType": "linux",
      "NodeOS": "Ubuntu 20.04.6 LTS",
      "NodeKernel": "5.15.0-1078-gcp",
      "NodeArch": "amd64",
      "NumNodes": 7,
      "Dataplane": "bpf",
      "IPFamily": "ipv4",
      "Encapsulation": "VXLANCrossSubnet",
      "WireguardEnabled": false,
      "Product": "calico",
      "CalicoVersion": "v3.30.0-0.dev-722-g4ff3cb04272f",
      "K8SVersion": "v1.31.7",
      "CRIVersion": "containerd://1.7.25",
      "CNIOption": "Calico"
    },
    "thruput-latency": {
      "Latency": {
        "pod-pod": {
          "min": 204,
          "max": 204,
          "avg": 204,
          "P50": 204,
          "P75": 204,
          "P90": 204,
          "P99": 204,
          "unit": "us"
        },
        "pod-svc-pod": {
          "min": 195,
          "max": 195,
          "avg": 195,
          "P50": 195,
          "P75": 195,
          "P90": 195,
          "P99": 195,
          "unit": "us"
        },
        "ext-svc-pod": {
          "min": 55600,
          "max": 55600,
          "avg": 55600,
          "P50": 55600,
          "P75": 55600,
          "P90": 55600,
          "P99": 55600,
          "unit": "us"
        }
      },
      "Throughput": {
        "pod-pod": {
          "min": 2120,
          "max": 2120,
          "avg": 2120,
          "P50": 2120,
          "P75": 2120,
          "P90": 2120,
          "P99": 2120,
          "unit": "Mb/sec"
        },
        "pod-svc-pod": {
          "min": 2120,
          "max": 2120,
          "avg": 2120,
          "P50": 2120,
          "P75": 2120,
          "P90": 2120,
          "P99": 2120,
          "unit": "Mb/sec"
        },
        "ext-svc-pod": {
          "min": 9.8,
          "max": 9.8,
          "avg": 9.8,
          "P50": 9.8,
          "P75": 9.8,
          "P90": 9.8,
          "P99": 9.8,
          "unit": "Mb/sec"
        }
      }
    }
  },
```

`config` contains the configuration requested in the test definition.
`ClusterDetails` contains information collected about the cluster at the time of the test.
`thruput-latency` contains a statistical summary of the raw qperf results - latency and throughput for a direct pod-pod test and via a service. Units are given in the result.

### The "Time To First Response" test

This "time to first response" (TTFR) test spins up a server pod on each node in the cluster, and then spins up client pods on each node in the cluster. The client pods start and send requests to the server pod, and record the amount of time it takes before they get a response. This is sometimes[1] a useful proxy for how long its taking for Calico to program the rules for that pod (since pods start with a deny-all rule and calico-node must program the correct rules before it can talk to anything). A better measure of the time it takes Calico to program rules for pods is to look in the [Felix Prometheus metrics](https://docs.tigera.io/calico/latest/reference/felix/prometheus#common-data-plane-metrics) at the `felix_int_dataplane_apply_time_seconds` statistic.

[1] if `linuxPolicySetupTimeoutSeconds` is set in the CalicoNetworkSpec in the Installation resource, then pod startup will be delayed until policy is applied. This can be handy if your application pod wants its first request to always succeed. This is a Calico-specific feature that is not part of the CNI spec. See the [Calico documentation](https://docs.tigera.io/calico/latest/reference/configure-cni-plugins#enabling-policy-setup-timeout) for more information on this feature and how to enable it.

For a "ttfr" test, the tool will:

- Create a test namespace
- Create a deployment of `numPods` pods that are unrelated to the test and apply `numPolicies` policies to them (standing pods and policies).
- Create another deployment of 10 pods, and create `numServices` that point to those 10 pods.
- Wait for those to come up.
- Create a server pod on each node with the `tigera.io/test-nodepool=default-pool` label
- Loop round:
  - creating test pods on those nodes, at the rate defined by Rate in the test config
  - test pods are then checked until they produce a ttfr result in their log, which is read by the tool
  - and a delete is sent for the test pod.
- ttfr results are recorded
- Collate results and compute min/max/average/50/75/90/99th percentiles
- Output that summary into a JSON format results file.
- Optionally delete the test namespace (which will cause all test resources within it to be deleted)
- Wait for everything to finish being cleaned up.

This test measures Time to First Response in seconds. i.e. the time between a pod starting up, and it getting a response from a server pod on the same node.

An example result from a "ttfr" test might look like:

```
[
  {
    "config": {
      "TestKind": "ttfr",
      "Encap": "",
      "Dataplane": "",
      "NumPolicies": 100,
      "NumServices": 10,
      "NumPods": 7,
      "HostNetwork": false,
      "TestNamespace": "testns2",
      "Iterations": 1,
      "Duration": 60,
      "DNSPerf": null,
      "Perf": null,
      "TTFRConfig": {
        "TestPodsPerNode": 80,
        "Rate": 10
      },
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
      "NumNodes": 3,
      "Dataplane": "bpf",
      "IPFamily": "ipv4",
      "Encapsulation": "VXLANCrossSubnet",
      "WireguardEnabled": false,
      "Product": "calico",
      "CalicoVersion": "v3.30.0-0.dev-852-g389eae30ae5d",
      "K8SVersion": "v1.32.4",
      "CRIVersion": "containerd://1.7.27",
      "CNIOption": "Calico"
    },
    "ttfr": [
      {
        "ttfrSummary": {
          "min": 0.001196166,
          "max": 0.01283499,
          "avg": 0.0033952200330330333,
          "P50": 0.002893934,
          "P75": 0.003768213,
          "P90": 0.005621623,
          "P99": 0.011158944,
          "unit": "seconds",
          "datapoints": 333
        }
      }
    ]
  }
]
```

`config` contains the configuration requested in the test definition.
`ClusterDetails` contains information collected about the cluster at the time of the test.
`ttfr` contains a statistical summary of the raw results. Units are given in the result.
