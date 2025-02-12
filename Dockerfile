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

FROM scratch
COPY --from=builder /results /results
COPY --from=builder /benchmark/benchmark /benchmark
ENV KUBECONFIG="/kubeconfig"
ENV TESTCONFIGFILE="/testconfig.yaml"
CMD ["/benchmark"]
