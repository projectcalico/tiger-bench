ARG GO_VERSION=1.24.3

FROM golang:${GO_VERSION} AS builder

WORKDIR /benchmark
COPY cmd cmd
COPY pkg pkg
COPY *.go go.* ./

RUN ls -ltr /benchmark
RUN --mount=type=cache,target=/go/pkg/mod/ \
    --mount=type=bind,source=go.sum,target=go.sum \
    --mount=type=bind,source=go.mod,target=go.mod \
    go mod download -x
ENV GOCACHE=/root/.cache/go-build
RUN --mount=type=cache,target=/go/pkg/mod/ \
    --mount=type=cache,target="/root/.cache/go-build" \
    GO111MODULE=on CGO_ENABLED=0 GOOS=linux go build -o /benchmark/benchmark cmd/benchmark.go

RUN mkdir /results

FROM alpine:3.21
ARG TARGETARCH

RUN apk add --update ca-certificates gettext
RUN apk add --no-cache aws-cli iperf3 curl
RUN apk add --no-cache --repository http://dl-3.alpinelinux.org/alpine/edge/testing/ qperf
COPY --from=builder /results /results
COPY --from=builder /benchmark/benchmark /benchmark

RUN set -e; \
    case "${TARGETARCH}" in \
    amd64) ARCH=amd64 ;; \
    arm64) ARCH=arm64 ;; \
    *) echo "Unsupported architecture: ${TARGETARCH}" && exit 1 ;; \
    esac && \
    curl -L --retry 5 --retry-delay 10 \
    https://github.com/kubernetes-sigs/aws-iam-authenticator/releases/download/v0.6.30/aws-iam-authenticator_0.6.30_linux_${ARCH} \
    -o /usr/local/bin/aws-iam-authenticator && \
    chmod +x /usr/local/bin/aws-iam-authenticator

ENV KUBECONFIG="/kubeconfig"
ENV TESTCONFIGFILE="/testconfig.yaml"
CMD ["/benchmark"]
