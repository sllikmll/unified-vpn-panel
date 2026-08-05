#!/usr/bin/env sh
set -eu

readonly_config="/etc/caddy-naive/Caddyfile.naiveproxy"

if [ "$#" -ne 0 ]; then
  echo "naive-caddy rejects command overrides." >&2
  exit 1
fi

if [ ! -f "$readonly_config" ]; then
  echo "Missing panel-managed Caddy config at $readonly_config." >&2
  exit 1
fi

owner="$(stat -c '%u:%g' "$readonly_config")"
mode="$(stat -c '%a' "$readonly_config")"

if [ "$owner" != "10001:10001" ]; then
  echo "$readonly_config must be owned by uid:gid 10001:10001." >&2
  exit 1
fi

if [ "$mode" != "600" ]; then
  echo "$readonly_config must have mode 0600." >&2
  exit 1
fi

if ! su -s /bin/sh -c "test -r '$readonly_config'" caddy; then
  echo "$readonly_config is not readable by caddy." >&2
  exit 1
fi

exec su -s /bin/sh -c "exec caddy run --config '$readonly_config' --adapter caddyfile" caddy
