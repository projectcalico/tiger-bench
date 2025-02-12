#!/bin/bash
set -ex
docker build -t benchmark .
docker run --rm --net=host \
-v "${PWD}":/results \
-v ${KUBECONFIG}:/kubeconfig \
-v ${PWD}/testconfig.yaml:/testconfig.yaml \
-e WEBSERVER_IMAGE="quay.io/tigeradev/tiger-bench-nginx:latest" \
-e PERF_IMAGE="quay.io/tigeradev/tiger-bench-perf:latest" \
quay.io/tigeradev/tiger-bench:latest
