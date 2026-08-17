#!/usr/bin/env bash
set -Eeuo pipefail

readonly config="/opt/amnezia/awg/awg0.conf"
readonly iface="awg0"
readonly runtime_dir="/run/amneziawg"
readonly runtime_config="${runtime_dir}/awg0.setconf"
readonly state_address="${runtime_dir}/nat-address"

config_value() {
  local key="$1"
  awk -F= -v wanted="$key" '
    BEGIN { section = "" }
    /^\[Interface\][[:space:]]*$/ { section = "interface"; next }
    /^\[/ { section = "other"; next }
    section == "interface" {
      name = $1
      gsub(/^[[:space:]]+|[[:space:]]+$/, "", name)
      if (tolower(name) == tolower(wanted)) {
        value = substr($0, index($0, "=") + 1)
        gsub(/^[[:space:]]+|[[:space:]]+$/, "", value)
        print value
        exit
      }
    }
  ' "$config"
}

render_setconf() {
  mkdir -p "$runtime_dir"
  umask 077
  awk '
    /^\[Interface\][[:space:]]*$/ { section = "interface"; print; next }
    /^\[Peer\][[:space:]]*$/ { section = "peer"; print; next }
    /^\[/ { section = "other"; print; next }
    section == "interface" {
      line = $0
      sub(/^[[:space:]]*/, "", line)
      key = line
      sub(/[[:space:]]*=.*/, "", key)
      key = tolower(key)
      if (key == "address" || key == "dns" || key == "mtu" || key == "postup" || key == "postdown") next
    }
    { print }
  ' "$config" > "${runtime_config}.tmp"
  chmod 0600 "${runtime_config}.tmp"
  mv -f "${runtime_config}.tmp" "$runtime_config"
}

validate_inputs() {
  [[ -r "$config" ]] || { echo "missing readable AWG2 config" >&2; return 1; }
  local address mtu
  address="$(config_value Address)"
  mtu="$(config_value MTU)"
  [[ "$address" =~ ^([0-9]{1,3}\.){3}[0-9]{1,3}/([0-9]|[12][0-9]|3[0-2])$ ]] || {
    echo "invalid IPv4 Address in AWG2 config" >&2
    return 1
  }
  if [[ -n "$mtu" && ! "$mtu" =~ ^[0-9]{3,4}$ ]]; then
    echo "invalid MTU in AWG2 config" >&2
    return 1
  fi
}

ensure_firewall() {
  local address="$1"
  iptables -C FORWARD -i "$iface" -j ACCEPT >/dev/null 2>&1 || iptables -I FORWARD 1 -i "$iface" -j ACCEPT
  iptables -C FORWARD -o "$iface" -m conntrack --ctstate RELATED,ESTABLISHED -j ACCEPT >/dev/null 2>&1 || iptables -I FORWARD 1 -o "$iface" -m conntrack --ctstate RELATED,ESTABLISHED -j ACCEPT
  iptables -t nat -C POSTROUTING -s "$address" -j MASQUERADE >/dev/null 2>&1 || iptables -t nat -A POSTROUTING -s "$address" -j MASQUERADE
}

remove_firewall() {
	local address
	if [[ -r "$state_address" ]]; then
		address="$(<"$state_address")"
	else
		address="$(config_value Address 2>/dev/null || true)"
	fi
  iptables -D FORWARD -i "$iface" -j ACCEPT >/dev/null 2>&1 || true
  iptables -D FORWARD -o "$iface" -m conntrack --ctstate RELATED,ESTABLISHED -j ACCEPT >/dev/null 2>&1 || true
  if [[ -n "$address" ]]; then
    iptables -t nat -D POSTROUTING -s "$address" -j MASQUERADE >/dev/null 2>&1 || true
  fi
}

apply_config() {
  validate_inputs
  ip link show dev "$iface" >/dev/null 2>&1 || { echo "AWG2 interface is unavailable" >&2; return 1; }
  render_setconf
  awg setconf "$iface" "$runtime_config"

  local address mtu
  address="$(config_value Address)"
  mtu="$(config_value MTU)"
  remove_firewall
  while IFS= read -r route; do
  	[[ -n "$route" ]] && ip route del "$route" dev "$iface" >/dev/null 2>&1 || true
  done < <(ip -o route show dev "$iface" | awk '{print $1}')
  ip address flush dev "$iface"
  ip address add "$address" dev "$iface"
  if [[ -n "$mtu" ]]; then
    ip link set mtu "$mtu" dev "$iface"
  fi
  ip link set up dev "$iface"

  while IFS= read -r allowed; do
    IFS=',' read -ra routes <<< "$allowed"
    for route in "${routes[@]}"; do
      route="${route//[[:space:]]/}"
      [[ -n "$route" && "$route" != "0.0.0.0/0" ]] && ip route replace "$route" dev "$iface"
    done
  done < <(awk -F= '
    /^\[Peer\][[:space:]]*$/ { peer = 1; next }
    /^\[/ { peer = 0; next }
    peer {
      key = $1
      gsub(/^[[:space:]]+|[[:space:]]+$/, "", key)
      if (tolower(key) == "allowedips") {
        value = substr($0, index($0, "=") + 1)
        gsub(/^[[:space:]]+|[[:space:]]+$/, "", value)
        print value
      }
    }
  ' "$config")
  ensure_firewall "$address"
  printf '%s\n' "$address" > "$state_address"
  awg show "$iface" >/dev/null
}

verify_config() {
  ip link show dev "$iface" >/dev/null
  awg show "$iface" >/dev/null
  ip -4 address show dev "$iface" | grep -q 'inet '
}

case "${1:-}" in
  apply) apply_config ;;
  verify) verify_config ;;
  down)
    remove_firewall
		rm -f "$state_address"
    ip link delete dev "$iface" >/dev/null 2>&1 || true
    ;;
  *) echo "usage: awg2-reconcile {apply|verify|down}" >&2; exit 2 ;;
esac
