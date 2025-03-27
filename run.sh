#!/bin/bash
set -ex
docker build -t quay.io/tigeradev/tiger-bench:v0.1.0 .
docker run --rm --net=host \
-v "${PWD}":/results \
-v ${KUBECONFIG}:/kubeconfig \
-v ${PWD}/testconfig.yaml:/testconfig.yaml \
-v $HOME/.aws:/root/.aws \
-e AWS_SECRET_ACCESS_KEY \
-e AWS_ACCESS_KEY_ID \
-e LOG_LEVEL=INFO \
-e WEBSERVER_IMAGE="quay.io/tigeradev/tiger-bench-nginx:main" \
-e PERF_IMAGE="quay.io/tigeradev/tiger-bench-perf:main" \
quay.io/tigeradev/tiger-bench:v0.1.0
