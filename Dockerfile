ARG GO_VERSION=1.23.6

FROM golang:${GO_VERSION} AS builder

WORKDIR /benchmark
COPY cmd cmd
COPY pkg pkg
COPY *.go go.* ./

RUN ls -ltr /benchmark
RUN go mod download
RUN GO111MODULE=on CGO_ENABLED=0 GOOS=linux go build cmd/benchmark.go
RUN mkdir /results

FROM alpine:3.21
RUN apk add --no-cache iperf3
RUN apk add --no-cache --repository http://dl-3.alpinelinux.org/alpine/edge/testing/ qperf
COPY --from=builder /results /results
COPY --from=builder /benchmark/benchmark /benchmark
ENV KUBECONFIG="/kubeconfig"
ENV TESTCONFIGFILE="/testconfig.yaml"
CMD ["/benchmark"]
