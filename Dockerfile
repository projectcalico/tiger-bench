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
ARG AWS_IAM_AUTHENTICATOR_URL=https://github.com/kubernetes-sigs/aws-iam-authenticator/releases/download/v0.6.30/aws-iam-authenticator_0.6.30_linux_amd64

ADD ${AWS_IAM_AUTHENTICATOR_URL} /usr/local/bin/aws-iam-authenticator
RUN apk add --update ca-certificates gettext && \
    chmod +x /usr/local/bin/aws-iam-authenticator

COPY --from=builder /results /results
COPY --from=builder /benchmark/benchmark /benchmark
ENV KUBECONFIG="/kubeconfig"
ENV TESTCONFIGFILE="/testconfig.yaml"
CMD ["/benchmark"]
