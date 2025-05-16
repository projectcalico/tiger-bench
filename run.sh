#!/bin/bash
set -ex
docker build -t quay.io/tigeradev/tiger-bench:v0.3.0 .
docker run --rm --net=host \
-v "${PWD}":/results \
-v ${KUBECONFIG}:/kubeconfig \
-v ${PWD}/testconfig.yaml:/testconfig.yaml \
-v $HOME/.aws:/root/.aws \
-e AWS_SECRET_ACCESS_KEY \
-e AWS_ACCESS_KEY_ID \
-e AWS_SESSION_TOKEN \
-e LOG_LEVEL=INFO \
-e WEBSERVER_IMAGE="quay.io/tigeradev/tiger-bench-nginx:v0.3.0" \
-e PERF_IMAGE="quay.io/tigeradev/tiger-bench-perf:v0.3.0" \
-e TTFR_IMAGE="quay.io/tigeradev/tiger-bench-ttfr:v0.3.0" \
quay.io/tigeradev/tiger-bench:v0.3.0
