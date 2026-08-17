#!/usr/bin/env bash
set -Eeuo pipefail

readonly config="/opt/amnezia/awg/awg0.conf"
readonly iface="awg0"
awg_pid=""

cleanup() {
  trap - TERM INT EXIT
  awg2-reconcile down || true
  if [[ -n "$awg_pid" ]] && kill -0 "$awg_pid" >/dev/null 2>&1; then
    kill -TERM "$awg_pid" >/dev/null 2>&1 || true
    wait "$awg_pid" || true
  fi
}

require_runtime_inputs() {
  [[ "$(id -u)" == "0" ]] || { echo "AWG2 runtime requires root" >&2; exit 1; }
  [[ -r "$config" ]] || { echo "missing readable AWG2 config" >&2; exit 1; }
  [[ -c /dev/net/tun ]] || { echo "missing /dev/net/tun" >&2; exit 1; }
}

start_foreground_runtime() {
  amneziawg-go -f "$iface" &
  awg_pid=$!
  for _ in $(seq 1 100); do
    kill -0 "$awg_pid" >/dev/null 2>&1 || { wait "$awg_pid"; return 1; }
    ip link show dev "$iface" >/dev/null 2>&1 && return 0
    sleep 0.05
  done
  echo "amneziawg-go did not create $iface" >&2
  return 1
}

require_runtime_inputs
trap cleanup TERM INT EXIT
start_foreground_runtime
awg2-reconcile apply
wait "$awg_pid"
