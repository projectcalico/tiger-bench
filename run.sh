#!/bin/bash
set -ex
docker build -t quay.io/tigeradev/tiger-bench:latest .
docker run --rm --net=host \
-v "${PWD}":/results \
-v ${KUBECONFIG}:/kubeconfig \
-v ${PWD}/testconfig.yaml:/testconfig.yaml \
-e WEBSERVER_IMAGE="quay.io/tigeradev/tiger-bench-nginx:main" \
-e PERF_IMAGE="quay.io/tigeradev/tiger-bench-perf:main" \
quay.io/tigeradev/tiger-bench:latest
