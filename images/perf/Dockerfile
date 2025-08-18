FROM alpine:3.21 AS build-env
RUN apk add --no-cache build-base git autoconf automake perl perl-doc

# Version 0.4.11 requires ipv6, 0.4.10 does not support ipv6 at all.
# To permit running on nodes where IPv6 is disabled in the kernel, we use 0.4.10.
# We get versions by git hash for integrity reasons.
# 0.4.11 = c706363815a38ff2c5cbc07b73e2cfaaa59bae0f
# 0.4.10 = aa644b22fff9e939e549a9759629be58b3c5cac2
RUN git clone --single-branch https://github.com/linux-rdma/qperf.git
WORKDIR /qperf
RUN git checkout aa644b22fff9e939e549a9759629be58b3c5cac2

# Build qperf from source
RUN ./cleanup && \
    ./autogen.sh && \
    ./configure && \
    make

FROM alpine:3.21
RUN apk add --no-cache iperf3 curl tcpdump
COPY --from=build-env /qperf/src/qperf /usr/bin/qperf
RUN chmod +x /usr/bin/qperf

USER perf
