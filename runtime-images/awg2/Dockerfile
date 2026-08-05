# syntax=docker/dockerfile:1.7
ARG GO_VERSION=1.26-alpine
ARG ALPINE_VERSION=3.22
ARG AMNEZIAWG_GO_COMMIT=cf9d2dd202821301f7039093b0a1b3d4b574c47c
ARG AMNEZIAWG_TOOLS_COMMIT=d09ecc38425082e472368dd2bf8c4c42d10cae03

FROM --platform=$BUILDPLATFORM golang:${GO_VERSION} AS awg-go-builder
ARG AMNEZIAWG_GO_COMMIT
ARG TARGETOS
ARG TARGETARCH
WORKDIR /src/amneziawg-go
RUN apk add --no-cache file git make
RUN git init . \
  && git remote add origin https://github.com/amnezia-vpn/amneziawg-go.git \
  && git fetch --depth 1 origin "${AMNEZIAWG_GO_COMMIT}" \
  && git checkout --detach FETCH_HEAD
ENV CGO_ENABLED=0
RUN GOOS="${TARGETOS}" GOARCH="${TARGETARCH}" make \
  && install -Dm0755 amneziawg-go /out/usr/local/bin/amneziawg-go
RUN case "${TARGETARCH}" in \
      amd64) expected='x86-64' ;; \
      arm64) expected='aarch64|ARM aarch64' ;; \
      *) echo "unsupported TARGETARCH=${TARGETARCH}" >&2; exit 1 ;; \
    esac \
  && file /out/usr/local/bin/amneziawg-go \
  && file /out/usr/local/bin/amneziawg-go | grep -E "$expected"

FROM --platform=$TARGETPLATFORM alpine:${ALPINE_VERSION} AS awg-tools-builder
ARG AMNEZIAWG_TOOLS_COMMIT
ARG TARGETARCH
WORKDIR /src/amneziawg-tools
RUN apk add --no-cache build-base file git linux-headers make
RUN git init . \
  && git remote add origin https://github.com/amnezia-vpn/amneziawg-tools.git \
  && git fetch --depth 1 origin "${AMNEZIAWG_TOOLS_COMMIT}" \
  && git checkout --detach FETCH_HEAD
RUN make -C src \
  && make -C src install \
    DESTDIR=/out \
    PREFIX=/usr/local \
    WITH_BASHCOMPLETION=no \
    WITH_SYSTEMDUNITS=no \
    WITH_WGQUICK=yes
RUN case "${TARGETARCH}" in \
      amd64) expected='x86-64' ;; \
      arm64) expected='aarch64|ARM aarch64' ;; \
      *) echo "unsupported TARGETARCH=${TARGETARCH}" >&2; exit 1 ;; \
    esac \
  && file /out/usr/local/bin/awg \
  && file /out/usr/local/bin/awg | grep -E "$expected" \
  && /out/usr/local/bin/awg --version >/dev/null

FROM alpine:${ALPINE_VERSION}
ARG AMNEZIAWG_GO_COMMIT
ARG AMNEZIAWG_TOOLS_COMMIT
LABEL org.opencontainers.image.title="unified-vpn-panel AWG2 runtime prerequisite" \
      org.opencontainers.image.description="Pinned AmneziaWG userspace runtime artifact for unified-vpn-panel protocol prerequisites" \
      org.opencontainers.image.source="https://github.com/sllikmll/unified-vpn-panel" \
      org.opencontainers.image.licenses="MIT AND GPL-2.0-only" \
      org.opencontainers.image.version="awg-go-${AMNEZIAWG_GO_COMMIT}-tools-${AMNEZIAWG_TOOLS_COMMIT}"

RUN apk add --no-cache \
    bash \
    iproute2 \
    iptables \
    iptables-legacy \
    libcap \
    openresolv \
    procps \
  && mkdir -p /opt/amnezia/awg /var/run/amneziawg /dev/net \
  && ln -sf /usr/local/bin/awg /usr/bin/awg \
  && ln -sf /usr/local/bin/awg-quick /usr/bin/awg-quick \
  && ln -sf /usr/local/bin/amneziawg-go /usr/bin/amneziawg-go \
  && if command -v update-alternatives >/dev/null 2>&1; then \
       update-alternatives --install /usr/sbin/iptables iptables /usr/sbin/iptables-legacy 10; \
     fi

COPY --from=awg-go-builder /out/ /
COPY --from=awg-tools-builder /out/ /
COPY entrypoint.sh /usr/local/bin/awg2-entrypoint

RUN chmod 0755 /usr/local/bin/awg2-entrypoint

ENV AWG_QUICK_USERSPACE_IMPLEMENTATION=amneziawg-go \
    WG_QUICK_USERSPACE_IMPLEMENTATION=amneziawg-go

VOLUME ["/opt/amnezia/awg"]
STOPSIGNAL SIGTERM
HEALTHCHECK --interval=30s --timeout=5s --retries=3 CMD awg show awg0 >/dev/null 2>&1 || exit 1
ENTRYPOINT ["/usr/local/bin/awg2-entrypoint"]
