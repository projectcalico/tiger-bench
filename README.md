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
   `quay.io/tigeradev/tiger-bench-nginx:latest`
   `quay.io/tigeradev/tiger-bench-perf:latest`
   `quay.io/tigeradev/tiger-bench:latest` - this is the tool itself.

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
   -e WEBSERVER_IMAGE="quay.io/tigeradev/tiger-bench-nginx:latest" \
   -e PERF_IMAGE="quay.io/tigeradev/tiger-bench-perf:latest" \
   quay.io/tigeradev/tiger-bench:latest
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
-e WEBSERVER_IMAGE="quay.io/tigeradev/tiger-bench-nginx:latest" \
-e PERF_IMAGE="quay.io/tigeradev/tiger-bench-perf:latest" \
quay.io/tigeradev/tiger-bench:latest
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
  encap: vxlan
  dataplane: iptables
  iterations: 3
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
```

There are 2 tests requested in this example config.

`testKind` is required - at present you can only ask for `"thruput-latency"`.

`numPolicies`, `numServices`, `numPods` specify the standing config desired for this test. Standing config exists simply to "load" the cluster up with config, they do not take any active part in the tests themselves. The number that you can create is limited by your cluster - you cannot create more standing pods than will fit on your cluster!

`leaveStandingConfig` tells the tool whether it should leave or clean up the standing resources it created for this test. It is sometimes useful to leave standing config up between tests, especially if it takes a long time to set up.

`duration` is the length of each part of the test run in seconds. In this example, each test part will run for 5 seconds.

`hostNetwork` specifies whether the test should run in the pod network (`false`) or the host's network (`true`). It is often helpful to be able to run a test in both networks to compare performance.

`testNamespace` specifies the namespace that should be used for this test. All standing resources and test resources will be created within this namespace. In this example it was omitted, so the test will use `testns`.

`iterations` specifies the number of times the measurement should be repeated. Note that a single iteration will include 2 runs of qperf - one direct pod-pod and the other pod-service-pod. If you just want to set up standing config, `iterations` can be set to zero.

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
- Execute qperf direct between the two pods for the configured duration: `qperf <ip> -t <duration> -vv -ub -lp 4000 -ip 4001 tcp_bw tcp_lat`
- Collects the output from qperf and parses it to collect latency and throughput results.
- Repeat the qperf command, this time using the service IP for that pod.
- Repeat the pair of qperf commands for the given number of iterations (re-using the same pods)
- Collate results and compute min/max/average/50/75/90/99th percentiles
- Output that summary into a JSON format results file.
- Optionally delete the test namespace (which will cause all test resources within it to be deleted)
- Wait for everything to finish being cleaned up.

This test measures Latency and Throughput.

An example result from a "thruput-latency" test looks like:

```
  {
    "config": {
      "TestKind": "thruput-latency",
      "Encap": "none",
      "Dataplane": "bpf",
      "NumPolicies": 5,
      "NumServices": 10,
      "NumPods": 7,
      "HostNetwork": true,
      "TestNamespace": "testns",
      "Iterations": 3,
      "Duration": 5,
      "CalicoNodeCPULimit": "",
      "DNSPerfNumDomains": 0,
      "DNSPerfMode": "",
      "LeaveStandingConfig": false
    },
    "ClusterDetails": {
      "Cloud": "unknown",
      "Provisioner": "kubeadm",
      "NodeType": "linux",
      "NodeOS": "Debian GNU/Linux 12 (bookworm)",
      "NodeKernel": "6.8.0-51-generic",
      "NodeArch": "amd64",
      "NumNodes": 4,
      "Dataplane": "bpf",
      "IPFamily": "ipv4",
      "Encapsulation": "None",
      "WireguardEnabled": false,
      "Product": "calico",
      "CalicoVersion": "v3.29.1-32-gb71c83063aa1",
      "K8SVersion": "v1.32.0",
      "CRIVersion": "containerd://1.7.24",
      "CNIOption": "Calico"
    },
    "thruput-latency": {
      "Latency": {
        "pod-pod": {
          "min": 14.6,
          "max": 16.3,
          "avg": 15.333333333333334,
          "P50": 15.1,
          "P75": 16.3,
          "P90": 16.3,
          "P99": 16.3,
          "unit": "us"
        },
        "pod-svc-pod": {
          "min": 14.9,
          "max": 15.2,
          "avg": 15.033333333333331,
          "P50": 15,
          "P75": 15.2,
          "P90": 15.2,
          "P99": 15.2,
          "unit": "us"
        },
        "ext-svc-pod": {}
      },
      "Throughput": {
        "pod-pod": {
          "min": 23500,
          "max": 24400,
          "avg": 24000,
          "P50": 24100,
          "P75": 24400,
          "P90": 24400,
          "P99": 24400,
          "unit": "Mb/sec"
        },
        "pod-svc-pod": {
          "min": 23400,
          "max": 23900,
          "avg": 23566.666666666668,
          "P50": 23400,
          "P75": 23900,
          "P90": 23900,
          "P99": 23900,
          "unit": "Mb/sec"
        },
        "ext-svc-pod": {}
      }
    }
  },
```

`config` contains the configuration requested in the test definition.
`ClusterDetails` contains information collected about the cluster at the time of the test.
`qperf` contains a statistical summary of the raw qperf results - latency and throughput for a direct pod-pod test and via a service. Units are given in the result.

"ext-svc-pod" tests are coming soon.
