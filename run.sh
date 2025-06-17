#!/bin/bash
set -ex
docker build -t quay.io/tigeradev/tiger-bench:v0.4.0 .
docker run --rm --net=host \
-v "${PWD}":/results \
-v ${KUBECONFIG}:/kubeconfig \
-v ${PWD}/testconfig.yaml:/testconfig.yaml \
-v $HOME/.aws:/root/.aws \
-e AWS_SECRET_ACCESS_KEY \
-e AWS_ACCESS_KEY_ID \
-e AWS_SESSION_TOKEN \
-e LOG_LEVEL=INFO \
quay.io/tigeradev/tiger-bench:v0.4.0
