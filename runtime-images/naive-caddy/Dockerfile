# syntax=docker/dockerfile:1.7
ARG GO_VERSION=1.26-alpine
ARG ALPINE_VERSION=3.22
ARG CADDY_VERSION=v2.11.4
ARG XCADDY_VERSION=v0.4.5
ARG FORWARDPROXY_COMMIT=d62c80d3dd2c706b6b87579844d2397bddd18317

FROM --platform=$BUILDPLATFORM golang:${GO_VERSION} AS builder
ARG CADDY_VERSION
ARG XCADDY_VERSION
ARG FORWARDPROXY_COMMIT
ARG TARGETOS
ARG TARGETARCH
WORKDIR /src
RUN apk add --no-cache git
RUN go install "github.com/caddyserver/xcaddy/cmd/xcaddy@${XCADDY_VERSION}"
RUN GOOS="${TARGETOS}" GOARCH="${TARGETARCH}" CGO_ENABLED=0 \
  xcaddy build "${CADDY_VERSION}" \
    --output /out/usr/local/bin/caddy \
    --with "github.com/caddyserver/forwardproxy=github.com/klzgrad/forwardproxy@${FORWARDPROXY_COMMIT}"

FROM alpine:${ALPINE_VERSION}
ARG CADDY_VERSION
ARG XCADDY_VERSION
ARG FORWARDPROXY_COMMIT
ENV XDG_CONFIG_HOME=/config \
    XDG_DATA_HOME=/data
LABEL org.opencontainers.image.title="unified-vpn-panel Naive Caddy runtime prerequisite" \
      org.opencontainers.image.description="Pinned Caddy NaiveProxy server runtime artifact for unified-vpn-panel protocol prerequisites" \
      org.opencontainers.image.source="https://github.com/sllikmll/unified-vpn-panel" \
      org.opencontainers.image.licenses="Apache-2.0" \
      org.opencontainers.image.version="caddy-${CADDY_VERSION}-xcaddy-${XCADDY_VERSION}-forwardproxy-${FORWARDPROXY_COMMIT}"

RUN apk add --no-cache ca-certificates coreutils libcap tzdata wget \
  && addgroup -S -g 10001 caddy \
  && adduser -S -D -H -u 10001 -h /var/lib/caddy -s /sbin/nologin -G caddy caddy \
  && mkdir -p /etc/caddy-naive/examples /data/caddy /config/caddy \
  && chown -R caddy:caddy /data/caddy /config/caddy

COPY --from=builder /out/ /
COPY Caddyfile.naiveproxy /etc/caddy-naive/examples/Caddyfile.naiveproxy.example
COPY entrypoint.sh /usr/local/bin/naive-caddy-entrypoint

RUN chmod 0644 /etc/caddy-naive/examples/Caddyfile.naiveproxy.example \
  && chmod 0755 /usr/local/bin/naive-caddy-entrypoint \
  && setcap cap_net_bind_service=+ep /usr/local/bin/caddy \
  && chown root:root /etc/caddy-naive/examples/Caddyfile.naiveproxy.example /usr/local/bin/naive-caddy-entrypoint

EXPOSE 443/tcp
VOLUME ["/data/caddy", "/config/caddy", "/etc/caddy-naive"]
STOPSIGNAL SIGTERM
HEALTHCHECK --interval=30s --timeout=5s --retries=3 CMD wget -qO- "http://127.0.0.1:2019/config/" >/dev/null || exit 1
ENTRYPOINT ["/usr/local/bin/naive-caddy-entrypoint"]
