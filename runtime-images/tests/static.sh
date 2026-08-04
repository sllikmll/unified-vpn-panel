#!/usr/bin/env bash
set -Eeuo pipefail

root="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
cd "$root"

status=0

run() {
  echo "+ $*"
  "$@" || status=$?
}

run bash -n runtime-images/awg2/entrypoint.sh
run bash -n runtime-images/naive-caddy/entrypoint.sh
run bash -n runtime-images/mieru/verify-mieru-manifest.sh

if command -v hadolint >/dev/null 2>&1; then
  run hadolint runtime-images/awg2/Dockerfile runtime-images/naive-caddy/Dockerfile
else
  echo "+ hadolint ... skipped (not installed)"
fi

python3 - <<'PY' || status=$?
import json
import re
from pathlib import Path

manifest = json.load(open("runtime-images/mieru/mita-v3.35.0.manifest.json", encoding="utf-8"))
for item in manifest.get("artifacts", []):
    sha256 = item.get("sha256", "")
    if "UNFETCHED" in sha256 or not re.fullmatch(r"[0-9a-f]{64}", sha256):
        raise SystemExit(f"manifest contains invalid or placeholder sha256: {item}")

for path in [Path("runtime-images/awg2/Dockerfile"), Path("runtime-images/naive-caddy/Dockerfile")]:
    text = path.read_text(encoding="utf-8")
    if "curl | bash" in text or "latest" in text:
        raise SystemExit(f"forbidden runtime image pattern in {path}")
PY

python3 - <<'PY' || status=$?
from pathlib import Path

awg_dockerfile = Path("runtime-images/awg2/Dockerfile").read_text(encoding="utf-8")
awg_entrypoint = Path("runtime-images/awg2/entrypoint.sh").read_text(encoding="utf-8")
naive_dockerfile = Path("runtime-images/naive-caddy/Dockerfile").read_text(encoding="utf-8")
naive_entrypoint = Path("runtime-images/naive-caddy/entrypoint.sh").read_text(encoding="utf-8")
naive_example = Path("runtime-images/naive-caddy/Caddyfile.naiveproxy").read_text(encoding="utf-8")
workflow = Path(".github/workflows/protocol-runtime-images.yml").read_text(encoding="utf-8")
docs = Path("docs/protocol-runtime-artifacts.md").read_text(encoding="utf-8")

if "FROM --platform=$BUILDPLATFORM alpine:${ALPINE_VERSION} AS awg-tools-builder" in awg_dockerfile:
    raise SystemExit("awg tools builder must not use BUILDPLATFORM")
if "FROM --platform=$TARGETPLATFORM alpine:${ALPINE_VERSION} AS awg-tools-builder" not in awg_dockerfile:
    raise SystemExit("awg tools builder must use TARGETPLATFORM")
for required in [
    "ARG TARGETARCH",
    "file /out/usr/local/bin/awg",
    "/out/usr/local/bin/awg --version",
]:
    if required not in awg_dockerfile:
        raise SystemExit(f"missing AWG target architecture sanity check: {required}")

for token in ["AWG_" + "CONFIG", "AWG_" + "INTERFACE"]:
    for name, text in {
        "runtime-images/awg2/Dockerfile": awg_dockerfile,
        "runtime-images/awg2/entrypoint.sh": awg_entrypoint,
    }.items():
        if token in text:
            raise SystemExit(f"{name} contains forbidden AWG override token {token}")
if 'readonly config="/opt/amnezia/awg/awg0.conf"' not in awg_entrypoint:
    raise SystemExit("AWG config path is not fixed")
if 'readonly iface="awg0"' not in awg_entrypoint:
    raise SystemExit("AWG interface is not fixed")

for token in [
    "NAIVE_" + "USER",
    "NAIVE_" + "PASSWORD",
    "NAIVE_" + "TLS_CERT",
    "NAIVE_" + "TLS_KEY",
    "CADDY_" + "CONFIG",
]:
    for name, text in {
        "runtime-images/naive-caddy/Dockerfile": naive_dockerfile,
        "runtime-images/naive-caddy/entrypoint.sh": naive_entrypoint,
        "runtime-images/naive-caddy/Caddyfile.naiveproxy": naive_example,
        ".github/workflows/protocol-runtime-images.yml": workflow,
        "docs/protocol-runtime-artifacts.md": docs,
    }.items():
        if token in text:
            raise SystemExit(f"{name} contains forbidden secret/config env token {token}")

if "COPY Caddyfile.naiveproxy /etc/caddy-naive/Caddyfile.naiveproxy" in naive_dockerfile:
    raise SystemExit("naive image must not bake an active Caddy config")
if "/etc/caddy-naive/examples/Caddyfile.naiveproxy.example" not in naive_dockerfile:
    raise SystemExit("naive image must keep only a non-active example config")
for required in [
    "readonly_config=\"/etc/caddy-naive/Caddyfile.naiveproxy\"",
    'owner="$(stat -c \'%u:%g\' "$readonly_config")"',
    'mode="$(stat -c \'%a\' "$readonly_config")"',
    '[ "$owner" != "10001:10001" ]',
    '[ "$mode" != "600" ]',
    "caddy run --config",
]:
    if required not in naive_entrypoint:
        raise SystemExit(f"missing naive config permission/runtime check: {required}")
if "USER caddy" in naive_dockerfile:
    raise SystemExit("naive entrypoint must start as root to validate mount ownership before dropping to caddy")

for token in ["awg2_digest", "naive_caddy_digest"]:
    if token in workflow:
        raise SystemExit(f"workflow contains unreliable aggregate matrix output {token}")
for required in ["actions/upload-artifact@", "actions/download-artifact@", "@sha256:"]:
    if required not in workflow:
        raise SystemExit(f"workflow missing digest artifact/integration contract: {required}")
for path, text in {
    ".github/workflows/protocol-runtime-images.yml": workflow,
    "docs/protocol-runtime-artifacts.md": docs,
}.items():
    if ":latest" in text:
        raise SystemExit(f"{path} contains forbidden deployment ref token :latest")
PY

if python3 - <<'PY'
try:
    import yaml
except Exception:
    raise SystemExit(1)
yaml.safe_load(open(".github/workflows/protocol-runtime-images.yml", encoding="utf-8"))
PY
then
  echo "+ workflow yaml parse: python yaml"
elif command -v ruby >/dev/null 2>&1; then
  run ruby -e 'require "yaml"; YAML.load_file(".github/workflows/protocol-runtime-images.yml")'
elif command -v yq >/dev/null 2>&1; then
  run yq e . .github/workflows/protocol-runtime-images.yml >/dev/null
else
  echo "workflow YAML parse blocked: no PyYAML, ruby, or yq parser available" >&2
  status=1
fi

if [[ -x runtime-images/mieru/verify-mieru-manifest.sh ]]; then
  run runtime-images/mieru/verify-mieru-manifest.sh runtime-images/mieru/mita-v3.35.0.manifest.json
else
  echo "verifier is not executable" >&2
  status=1
fi

if command -v docker >/dev/null 2>&1 && docker info >/dev/null 2>&1; then
  arch="$(uname -m)"
  case "$arch" in
    x86_64) platform=linux/amd64 ;;
    arm64|aarch64) platform=linux/arm64 ;;
    *) platform="" ;;
  esac
  if [[ -n "$platform" ]]; then
    run docker build --pull=false --platform "$platform" -t uvp-awg2-runtime:test runtime-images/awg2
    run docker build --pull=false --platform "$platform" -t uvp-naive-caddy-runtime:test runtime-images/naive-caddy
  else
    echo "+ docker build ... skipped (unsupported local arch: $arch)"
  fi
else
  echo "+ docker build ... skipped (Docker unavailable)"
fi

exit "$status"
