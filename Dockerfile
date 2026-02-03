ARG GO_VERSION=1.24.3

FROM golang:${GO_VERSION} AS builder
ARG TARGETARCH

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
    GO111MODULE=on CGO_ENABLED=0 GOOS=linux GOARCH=${TARGETARCH} go build -o /benchmark/benchmark cmd/benchmark.go

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
    amd64) \
    ARCH=amd64; \
    EXPECTED_SHA256="d2c7a53c7cc6ff2e0e1e9ed86c85e6e4fd50da86adb26767b1e8f0518afa6e4b" ;; \
    arm64) \
    ARCH=arm64; \
    EXPECTED_SHA256="ad94ae69b8d70c1d2de5a1eab8e9ce01f50f7be64c35f89ff1ea9cf57c45ce8c" ;; \
    *) echo "Unsupported architecture: ${TARGETARCH}" >&2 ; exit 1 ;; \
    esac && \
    echo "Downloading aws-iam-authenticator for ${ARCH}..." && \
    curl -L --retry 5 --retry-delay 10 \
    https://github.com/kubernetes-sigs/aws-iam-authenticator/releases/download/v0.6.30/aws-iam-authenticator_0.6.30_linux_${ARCH} \
    -o /usr/local/bin/aws-iam-authenticator && \
    echo "Verifying checksum..." && \
    echo "${EXPECTED_SHA256}  /usr/local/bin/aws-iam-authenticator" | sha256sum -c - && \
    chmod +x /usr/local/bin/aws-iam-authenticator && \
    echo "aws-iam-authenticator installed successfully"

ENV KUBECONFIG="/kubeconfig"
ENV TESTCONFIGFILE="/testconfig.yaml"
CMD ["/benchmark"]
