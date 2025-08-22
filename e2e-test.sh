#!/bin/bash
set -euox pipefail

# E2E test for tiger-bench tool using KinD

# Variables
KIND_CLUSTER_NAME="tb-e2e"
KUBECONFIG_PATH="$(pwd)/kubeconfig"
TEST_YAML="e2e-testconfig.yaml"
TOOL_IMAGE="tiger-bench-tool:latest"


kind create cluster --kubeconfig "$KUBECONFIG_PATH" --config kind-config.yaml

# Install Calico
kubectl --kubeconfig "$KUBECONFIG_PATH" create -f https://raw.githubusercontent.com/projectcalico/calico/v3.30.2/manifests/operator-crds.yaml
kubectl --kubeconfig "$KUBECONFIG_PATH" create -f https://raw.githubusercontent.com/projectcalico/calico/v3.30.2/manifests/tigera-operator.yaml
kubectl --kubeconfig "$KUBECONFIG_PATH" create -f https://raw.githubusercontent.com/projectcalico/calico/v3.30.2/manifests/custom-resources.yaml

# Load test images into KinD nodes
for img in tiger-bench-perf tiger-bench-nginx tiger-bench-ttfr; do
  docker image inspect "$img" >/dev/null 2>&1 || { echo "Image $img not found"; exit 1; }
  kind load docker-image "$img:latest" --name "$KIND_CLUSTER_NAME"
done

# Wait for nodes to be ready
kubectl --kubeconfig "$KUBECONFIG_PATH" wait --for=condition=Ready nodes --all --timeout=600s

# Label nodes as described in README
kubectl --kubeconfig "$KUBECONFIG_PATH" label node tb-e2e-worker tigera.io/test-nodepool=default-pool
kubectl --kubeconfig "$KUBECONFIG_PATH" label node control-plane tigera.io/test-nodepool=default-pool

# Run tiger-bench container with kubeconfig and test yaml
# Assumes testconfig.yaml is present in the repo root

docker run --rm --net=host \
  -v "$KUBECONFIG_PATH:/kubeconfig:ro" \
  -v "$(pwd)/$TEST_YAML:/testconfig.yaml:ro" \
  -e WEBSERVER_IMAGE="tiger-bench-nginx:latest" \
  -e PERF_IMAGE="tiger-bench-perf:latest" \
  -e TTFR_IMAGE="tiger-bench-ttfr:latest" \
  "$TOOL_IMAGE"

# Cleanup
kind delete cluster --name "$KIND_CLUSTER_NAME"
rm -f "$KUBECONFIG_PATH"
