#!/usr/bin/env bash
set -Eeuo pipefail

readonly config="/opt/amnezia/awg/awg0.conf"
readonly iface="awg0"

cleanup() {
  trap - TERM INT EXIT
  if ip link show dev "${iface}" >/dev/null 2>&1; then
    awg-quick down "${config}" || ip link delete dev "${iface}" || true
  fi
}

require_root() {
  if [[ "$(id -u)" != "0" ]]; then
    echo "AWG tunnel setup requires root inside the container plus NET_ADMIN and /dev/net/tun." >&2
    exit 1
  fi
}

require_runtime_inputs() {
  if [[ ! -r "${config}" ]]; then
    echo "Missing readable AmneziaWG config at ${config}; mount it at /opt/amnezia/awg/awg0.conf." >&2
    exit 1
  fi
  if [[ ! -c /dev/net/tun ]]; then
    echo "Missing /dev/net/tun; run with --device /dev/net/tun and --cap-add NET_ADMIN." >&2
    exit 1
  fi
}

bring_up_once() {
  if ip link show dev "${iface}" >/dev/null 2>&1; then
    echo "${iface} already exists; leaving existing interface in place."
    return
  fi
  awg-quick up "${config}"
}

require_root
require_runtime_inputs
trap cleanup TERM INT EXIT
bring_up_once

while :; do
  sleep 3600 &
  wait "$!"
done
