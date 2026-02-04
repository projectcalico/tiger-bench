ARG GO_VERSION=1.24.3

FROM golang:${GO_VERSION} AS builder
ARG TARGETARCH

WORKDIR /build
COPY go.* /build
ADD images/ttfr /build/ttfr
WORKDIR /build/ttfr

RUN go mod download
RUN GO111MODULE=on CGO_ENABLED=0 GOOS=linux GOARCH=${TARGETARCH} go build -o /pingo .

FROM scratch
COPY --from=builder /pingo /pingo
USER pingo
CMD ["/pingo"]
