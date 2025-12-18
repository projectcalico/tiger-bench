#!/bin/bash
set -euox pipefail

# E2E test for tiger-bench tool using KinD

# Variables
KIND_CLUSTER_NAME="${KIND_CLUSTER_NAME:-tb-e2e}"
KUBECONFIG_PATH="${KUBECONFIG_PATH:-$(pwd)/kubeconfig}"
TEST_YAML="${TEST_YAML:-e2e-testconfig.yaml}"
TOOL_IMAGE="${TOOL_IMAGE:-tiger-bench:v0.5.0}"
CALICO_VERSION="${CALICO_VERSION:-v3.30.2}"
REGISTRY="${REGISTRY:-quay.io}"
ORGANISATION="${ORGANISATION:-tigeradev}"

kind create cluster --kubeconfig "$KUBECONFIG_PATH" --config kind-config.yaml || true

# Install Calico
curl --retry 10 --retry-all-errors -sSL https://raw.githubusercontent.com/projectcalico/calico/$CALICO_VERSION/manifests/operator-crds.yaml    | kubectl --kubeconfig "$KUBECONFIG_PATH" apply --server-side --force-conflicts -f -
curl --retry 10 --retry-all-errors -sSL https://raw.githubusercontent.com/projectcalico/calico/$CALICO_VERSION/manifests/tigera-operator.yaml  | kubectl --kubeconfig "$KUBECONFIG_PATH" apply --server-side --force-conflicts -f -
curl --retry 10 --retry-all-errors -sSL https://raw.githubusercontent.com/projectcalico/calico/$CALICO_VERSION/manifests/custom-resources.yaml | kubectl --kubeconfig "$KUBECONFIG_PATH" apply --server-side --force-conflicts -f -

# Load test images into KinD nodes
for img in tiger-bench-perf tiger-bench-nginx tiger-bench-ttfr; do
  docker image inspect "$REGISTRY/$ORGANISATION/$img:v0.5.0" >/dev/null 2>&1 || { echo "Image $img not found"; exit 1; }
  kind load docker-image "$REGISTRY/$ORGANISATION/$img:v0.5.0" --name "$KIND_CLUSTER_NAME"
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
  -e WEBSERVER_IMAGE="$REGISTRY/$ORGANISATION/tiger-bench-nginx:v0.5.0" \
  -e PERF_IMAGE="$REGISTRY/$ORGANISATION/tiger-bench-perf:v0.5.0" \
  -e TTFR_IMAGE="$REGISTRY/$ORGANISATION/tiger-bench-ttfr:v0.5.0" \
  "$REGISTRY/$ORGANISATION/$TOOL_IMAGE"

# Validate the results file
go run validate_results.go
