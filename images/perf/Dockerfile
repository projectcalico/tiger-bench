FROM alpine:3.21 AS build-env
RUN apk add --no-cache build-base git autoconf automake perl perl-doc

# Version 0.4.11 requires ipv6, 0.4.10 does not support ipv6 at all.
# To permit running on nodes where IPv6 is disabled in the kernel, we use 0.4.10.
RUN git clone --branch v0.4.10 --single-branch https://github.com/linux-rdma/qperf.git

# Build qperf from source
WORKDIR /qperf
RUN ./cleanup && \
    ./autogen.sh && \
    ./configure && \
    make

FROM alpine:3.21

RUN apk add --no-cache iperf3 curl tcpdump
COPY --from=build-env /qperf/src/qperf /usr/bin/qperf

USER perf
