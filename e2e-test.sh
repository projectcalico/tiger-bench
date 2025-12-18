#!/bin/bash
set -euox pipefail

# E2E test for tiger-bench tool using KinD

# Variables
KIND_CLUSTER_NAME="${KIND_CLUSTER_NAME:-tb-e2e}"
KUBECONFIG_PATH="${KUBECONFIG_PATH:-$(pwd)/kubeconfig}"
TEST_YAML="${TEST_YAML:-e2e-testconfig.yaml}"
CALICO_VERSION="${CALICO_VERSION:-v3.30.2}"
REGISTRY="${REGISTRY:-quay.io}"
ORGANISATION="${ORGANISATION:-tigeradev}"
# VERSION can be set by the Makefile; default to v0.5.0 for local runs.
VERSION="${VERSION:-v0.5.0}"
# Default image names (Makefile will pass explicit image names including tags).
TOOL_IMAGE="${TOOL_IMAGE:-$REGISTRY/$ORGANISATION/tiger-bench:${VERSION}}"
PERF_IMAGE="${PERF_IMAGE:-$REGISTRY/$ORGANISATION/tiger-bench-perf:${VERSION}}"
WEBSERVER_IMAGE="${WEBSERVER_IMAGE:-$REGISTRY/$ORGANISATION/tiger-bench-nginx:${VERSION}}"
TTFR_IMAGE="${TTFR_IMAGE:-$REGISTRY/$ORGANISATION/tiger-bench-ttfr:${VERSION}}"

kind create cluster --kubeconfig "$KUBECONFIG_PATH" --config kind-config.yaml || true

# Install Calico
curl --retry 10 --retry-all-errors -sSL https://raw.githubusercontent.com/projectcalico/calico/$CALICO_VERSION/manifests/operator-crds.yaml    | kubectl --kubeconfig "$KUBECONFIG_PATH" apply --server-side --force-conflicts -f -
curl --retry 10 --retry-all-errors -sSL https://raw.githubusercontent.com/projectcalico/calico/$CALICO_VERSION/manifests/tigera-operator.yaml  | kubectl --kubeconfig "$KUBECONFIG_PATH" apply --server-side --force-conflicts -f -
curl --retry 10 --retry-all-errors -sSL https://raw.githubusercontent.com/projectcalico/calico/$CALICO_VERSION/manifests/custom-resources.yaml | kubectl --kubeconfig "$KUBECONFIG_PATH" apply --server-side --force-conflicts -f -

# Load test images into KinD nodes. The Makefile must pass the explicit
# image names that were built; be strict and fail if an image is missing so
# the test is explicit about what it's validating.
images=("$WEBSERVER_IMAGE" "$PERF_IMAGE" "$TTFR_IMAGE")
for img in "${images[@]}"; do
  if docker image inspect "$img" >/dev/null 2>&1; then
    kind load docker-image "$img" --name "$KIND_CLUSTER_NAME"
  else
    echo "Required image not found locally: $img"
    echo "Build the images via 'make build' or set the image variables when running this script."
    exit 1
  fi
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
# Require the exact tool image be present locally (Makefile must pass it).
if ! docker image inspect "$TOOL_IMAGE" >/dev/null 2>&1; then
  echo "Tool image not found locally: $TOOL_IMAGE"
  echo "Build the tool image via 'make tool' or pass TOOL_IMAGE explicitly."
  exit 1
fi

docker run --rm --net=host \
  -v "${PWD}":/results \
  -v "$KUBECONFIG_PATH:/kubeconfig:ro" \
  -v "${PWD}/$TEST_YAML:/testconfig.yaml:ro" \
  -e WEBSERVER_IMAGE="$WEBSERVER_IMAGE" \
  -e PERF_IMAGE="$PERF_IMAGE" \
  -e TTFR_IMAGE="$TTFR_IMAGE" \
  "$TOOL_IMAGE"

# Validate the results file
go run validate_results.go
