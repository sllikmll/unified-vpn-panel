#!/usr/bin/env bash
set -Eeuo pipefail

manifest="${1:-runtime-images/mieru/mita-v3.35.0.manifest.json}"
tmpdir="$(mktemp -d)"
trap 'rm -rf "$tmpdir"' EXIT

python3 - "$manifest" <<'PY' >"$tmpdir/items.tsv"
import json
import re
import sys
from urllib.parse import urlparse

manifest_path = sys.argv[1]
with open(manifest_path, "r", encoding="utf-8") as fh:
    data = json.load(fh)

expected = {
    "host": "github.com",
    "owner": "enfein",
    "repo": "mieru",
    "version": "v3.35.0",
    "component": "mita",
}
upstream = data.get("upstream", {})
for key, value in expected.items():
    if upstream.get(key) != value:
        raise SystemExit(f"upstream {key} mismatch: {upstream.get(key)!r} != {value!r}")

seen = set()
for item in data.get("artifacts", []):
    os_name = item.get("os")
    arch = item.get("arch")
    kind = item.get("kind")
    url = item.get("url")
    checksum_url = item.get("checksum_url")
    sha256 = item.get("sha256")
    key = (os_name, arch, kind)
    if os_name != "linux" or arch not in {"amd64", "arm64"} or kind not in {"tar.gz", "deb"}:
        raise SystemExit(f"unsupported artifact tuple: {key}")
    if key in seen:
        raise SystemExit(f"duplicate artifact tuple: {key}")
    seen.add(key)
    if not isinstance(sha256, str) or not re.fullmatch(r"[0-9a-f]{64}", sha256):
        raise SystemExit(f"invalid sha256 for {key}: {sha256!r}")
    parsed = urlparse(url)
    checksum_parsed = urlparse(checksum_url)
    for parsed_url in (parsed, checksum_parsed):
        if parsed_url.scheme != "https" or parsed_url.netloc != "github.com":
            raise SystemExit(f"unexpected URL host: {parsed_url.geturl()}")
        prefix = "/enfein/mieru/releases/download/v3.35.0/"
        if not parsed_url.path.startswith(prefix):
            raise SystemExit(f"unexpected release path: {parsed_url.path}")
    basename = parsed.path.rsplit("/", 1)[-1]
    expected_name = {
        ("amd64", "tar.gz"): "mita_3.35.0_linux_amd64.tar.gz",
        ("arm64", "tar.gz"): "mita_3.35.0_linux_arm64.tar.gz",
        ("amd64", "deb"): "mita_3.35.0_amd64.deb",
        ("arm64", "deb"): "mita_3.35.0_arm64.deb",
    }[(arch, kind)]
    if basename != expected_name:
        raise SystemExit(f"unexpected artifact name for {key}: {basename}")
    if checksum_parsed.path.rsplit("/", 1)[-1] != f"{expected_name}.sha256.txt":
        raise SystemExit(f"unexpected checksum name for {key}: {checksum_parsed.path}")
    print("\t".join([os_name, arch, kind, url, checksum_url, sha256, basename]))

required = {
    ("linux", "amd64", "tar.gz"),
    ("linux", "arm64", "tar.gz"),
    ("linux", "amd64", "deb"),
    ("linux", "arm64", "deb"),
}
missing = required - seen
if missing:
    raise SystemExit(f"missing required artifacts: {sorted(missing)}")
PY

while IFS=$'\t' read -r os_name arch kind url checksum_url expected_sha filename; do
  sha_file="$tmpdir/${filename}.sha256.txt"
  artifact_file="$tmpdir/${filename}"
  curl -fsSL "$checksum_url" -o "$sha_file"
  official_sha="$(awk '{print $1}' "$sha_file")"
  if [[ ! "$official_sha" =~ ^[0-9a-f]{64}$ ]]; then
    echo "Official checksum is malformed for ${os_name}/${arch}/${kind}: ${official_sha}" >&2
    exit 1
  fi
  if [[ "$official_sha" != "$expected_sha" ]]; then
    echo "Manifest hash mismatch for ${os_name}/${arch}/${kind}: manifest=${expected_sha} official=${official_sha}" >&2
    exit 1
  fi
  curl -fsSL "$url" -o "$artifact_file"
  actual_sha="$(sha256sum "$artifact_file" | awk '{print $1}')"
  if [[ "$actual_sha" != "$expected_sha" ]]; then
    echo "Downloaded byte hash mismatch for ${os_name}/${arch}/${kind}: actual=${actual_sha} expected=${expected_sha}" >&2
    exit 1
  fi
done <"$tmpdir/items.tsv"

echo "Mieru mita manifest verified: ${manifest}"
