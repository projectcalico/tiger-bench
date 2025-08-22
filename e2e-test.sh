#!/bin/bash
set -euox pipefail

# E2E test for tiger-bench tool using KinD

# Variables
KIND_CLUSTER_NAME="${KIND_CLUSTER_NAME:-tb-e2e}"
KUBECONFIG_PATH="${KUBECONFIG_PATH:-$(pwd)/kubeconfig}"
TEST_YAML="${TEST_YAML:-e2e-testconfig.yaml}"
TOOL_IMAGE="${TOOL_IMAGE:-tiger-bench:latest}"
CALICO_VERSION="${CALICO_VERSION:-v3.30.2}"
REGISTRY="${REGISTRY:-quay.io}"
ORGANISATION="${ORGANISATION:-tigeradev}"

kind create cluster --kubeconfig "$KUBECONFIG_PATH" --config kind-config.yaml || true

# Install Calico
kubectl --kubeconfig "$KUBECONFIG_PATH" create -f https://raw.githubusercontent.com/projectcalico/calico/$CALICO_VERSION/manifests/operator-crds.yaml || true
kubectl --kubeconfig "$KUBECONFIG_PATH" create -f https://raw.githubusercontent.com/projectcalico/calico/$CALICO_VERSION/manifests/tigera-operator.yaml || true
kubectl --kubeconfig "$KUBECONFIG_PATH" create -f https://raw.githubusercontent.com/projectcalico/calico/$CALICO_VERSION/manifests/custom-resources.yaml || true

# Load test images into KinD nodes
for img in tiger-bench-perf tiger-bench-nginx tiger-bench-ttfr; do
  docker image inspect "$REGISTRY/$ORGANISATION/$img:latest" >/dev/null 2>&1 || { echo "Image $img not found"; exit 1; }
  kind load docker-image "$REGISTRY/$ORGANISATION/$img:latest" --name "$KIND_CLUSTER_NAME"
done

# Wait for nodes to be ready
kubectl --kubeconfig "$KUBECONFIG_PATH" wait --for=condition=Ready nodes --all --timeout=600s

# Label nodes as described in README
kubectl --kubeconfig "$KUBECONFIG_PATH" label node $KIND_CLUSTER_NAME-worker tigera.io/test-nodepool=default-pool
kubectl --kubeconfig "$KUBECONFIG_PATH" label node $KIND_CLUSTER_NAME-control-plane tigera.io/test-nodepool=default-pool

# Wait for Calico to be ready
kubectl --kubeconfig "$KUBECONFIG_PATH" wait --for=condition=Available tigerastatus --all --timeout=600s

# Run tiger-bench container with kubeconfig and test yaml
# Assumes testconfig.yaml is present in the repo root
docker run --rm --net=host \
  -v "${PWD}":/results \
  -v "$KUBECONFIG_PATH:/kubeconfig:ro" \
  -v "$(pwd)/$TEST_YAML:/testconfig.yaml:ro" \
  -e WEBSERVER_IMAGE="$REGISTRY/$ORGANISATION/tiger-bench-nginx:latest" \
  -e PERF_IMAGE="$REGISTRY/$ORGANISATION/tiger-bench-perf:latest" \
  -e TTFR_IMAGE="$REGISTRY/$ORGANISATION/tiger-bench-ttfr:latest" \
  "$REGISTRY/$ORGANISATION/$TOOL_IMAGE"

# Validate the results file
# create a virtual environment
python3 -m venv venv
source venv/bin/activate
pip install -r requirements.txt
# Run tests
python3 -m unittest test_validate_results.py
