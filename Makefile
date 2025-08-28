# Makefile for building and testing images in tiger-bench

IMAGE_NAMES = perf nginx ttfr tool
IMAGES_PATH = images
REGISTRY?=quay.io
ORGANISATION?=tigeradev
E2E_CLUSTER_NAME?=tb-e2e

.PHONY: all build test clean tool test-tool e2e-test clean-ttfr clean-e2e

all: build

build: $(IMAGE_NAMES)

perf:
	docker build -t $(REGISTRY)/$(ORGANISATION)/tiger-bench-perf -f images/perf/Dockerfile .

nginx:
	docker build -t $(REGISTRY)/$(ORGANISATION)/tiger-bench-nginx -f images/nginx/Dockerfile .

ttfr:
	docker build -t $(REGISTRY)/$(ORGANISATION)/tiger-bench-ttfr -f images/ttfr/Dockerfile .

tool:
	docker build -t $(REGISTRY)/$(ORGANISATION)/tiger-bench -f Dockerfile .

test: $(addprefix test-,$(IMAGE_NAMES))

test-tool:
	go test ./pkg/... ./cmd/...

test-perf:
	@echo "No tests defined for perf image."

test-nginx:
	@echo "No tests defined for nginx image."

test-ttfr:
	cd images/ttfr && go test -v ./pingo_test.go

clean: clean-perf clean-nginx clean-ttfr clean-tool clean-e2e

clean-perf:
	docker rmi $(REGISTRY)/$(ORGANISATION)/tiger-bench-perf || true

clean-nginx:
	docker rmi $(REGISTRY)/$(ORGANISATION)/tiger-bench-nginx || true

clean-ttfr:
	docker rmi $(REGISTRY)/$(ORGANISATION)/tiger-bench-ttfr || true

clean-tool:
	docker rmi $(REGISTRY)/$(ORGANISATION)/tiger-bench || true

clean-e2e:
	kind delete cluster --name $(E2E_CLUSTER_NAME) || true
	@rm -f kubeconfig

e2e-test: build clean-e2e
	KIND_CLUSTER_NAME=$(E2E_CLUSTER_NAME) REGISTRY=$(REGISTRY) ORGANISATION=$(ORGANISATION) bash ./e2e-test.sh
	$(MAKE) clean-e2e
