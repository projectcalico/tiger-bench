#!/bin/bash
set -ex
set -o pipefail

docker build -t quay.io/tigeradev/tiger-bench:latest .
docker run --rm --net=host \
-v "${PWD}":/results \
-v ${KUBECONFIG}:/kubeconfig \
-v ${PWD}/testconfig.yaml:/testconfig.yaml \
-v $HOME/.aws:/root/.aws \
-e AWS_SECRET_ACCESS_KEY \
-e AWS_ACCESS_KEY_ID \
-e AWS_SESSION_TOKEN \
-e JUNIT_REPORT_FILE=/results/junit_report.xml \
-e LOG_LEVEL=INFO \
-e WEBSERVER_IMAGE="quay.io/tigeradev/tiger-bench-nginx:main" \
-e PERF_IMAGE="quay.io/tigeradev/tiger-bench-perf:main" \
quay.io/tigeradev/tiger-bench:latest 2>&1 | tee run_output_$(date +%Y%m%d_%H%M%S).log
DOCKER_EXIT=${PIPESTATUS[0]}
if [ $DOCKER_EXIT -ne 0 ]; then
  exit $DOCKER_EXIT
fi

# Move results files if they exist
[ -f results.json ] && mv results.json results_$(date +%Y%m%d_%H%M%S).json
[ -f junit_report.xml ] && mv junit_report.xml junit_report_$(date +%Y%m%d_%H%M%S).xml
