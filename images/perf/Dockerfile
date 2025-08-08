FROM alpine:3.21 AS build-env
RUN apk add --no-cache build-base git autoconf automake perl perl-doc

# Version 0.4.10 does not have the patch which breaks qperf on systems that do not have ipv6 enabled in the kernel
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
