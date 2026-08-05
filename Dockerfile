# ========================================================
# Stage: Frontend (Vite)
# ========================================================
FROM --platform=$BUILDPLATFORM node:24-alpine AS frontend
WORKDIR /src/frontend
COPY frontend/package.json frontend/package-lock.json ./
RUN npm ci
COPY frontend/ ./
COPY internal/web/translation /src/internal/web/translation
RUN npm run build

# ========================================================
# Stage: Builder
# ========================================================
FROM --platform=$BUILDPLATFORM tonistiigi/xx:1.7.0 AS xx
FROM --platform=$BUILDPLATFORM golang:1.26-alpine AS builder
COPY --from=xx / /
WORKDIR /app
ARG TARGETPLATFORM
ARG TARGETOS
ARG TARGETARCH
ARG TARGETVARIANT
ARG XRAY_VERSION=26.6.27

RUN apk --no-cache --update add \
  clang \
  lld \
  curl \
  unzip \
  && xx-apk add --no-cache musl-dev gcc

COPY . .
COPY --from=frontend /src/internal/web/dist ./internal/web/dist

ENV CGO_ENABLED=1
ENV CGO_CFLAGS="-D_LARGEFILE64_SOURCE"
RUN xx-go build -ldflags "-w -s" -o build/x-ui main.go \
  && xx-verify build/x-ui
RUN case "$TARGETARCH/$TARGETVARIANT" in \
      386/*) init_arch=i386 ;; \
      arm/v6) init_arch=armv6 ;; \
      arm/v7) init_arch=armv7 ;; \
      *) init_arch="$TARGETARCH" ;; \
    esac \
  && ./DockerInit.sh "$init_arch"

# ========================================================
# Stage: Final Image of 3x-ui
# ========================================================
FROM alpine
ENV TZ=Asia/Tehran
WORKDIR /app

RUN apk add --no-cache --update \
  ca-certificates \
  tzdata \
  fail2ban \
  bash \
  curl \
  openssl

COPY --from=builder /app/build/ /app/
COPY --from=builder /app/DockerEntrypoint.sh /app/
COPY --from=builder /app/x-ui.sh /usr/bin/x-ui
COPY --from=builder /app/internal/web/translation /app/internal/web/translation


# Configure fail2ban
RUN rm -f /etc/fail2ban/jail.d/alpine-ssh.conf \
  && cp /etc/fail2ban/jail.conf /etc/fail2ban/jail.local \
  && sed -i "s/^\[ssh\]$/&\nenabled = false/" /etc/fail2ban/jail.local \
  && sed -i "s/^\[sshd\]$/&\nenabled = false/" /etc/fail2ban/jail.local \
  && sed -i "s/#allowipv6 = auto/allowipv6 = auto/g" /etc/fail2ban/fail2ban.conf

RUN chmod +x \
  /app/DockerEntrypoint.sh \
  /app/x-ui \
  /usr/bin/x-ui

ENV XUI_IN_DOCKER="true"
ENV XUI_MAIN_FOLDER="/app"
ENV XUI_ENABLE_FAIL2BAN="true"
ENV XUI_DB_TYPE=""
ENV XUI_DB_DSN=""
EXPOSE 2053
VOLUME [ "/etc/x-ui" ]
CMD [ "./x-ui" ]
ENTRYPOINT [ "/app/DockerEntrypoint.sh" ]
