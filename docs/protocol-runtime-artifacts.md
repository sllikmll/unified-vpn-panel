# Protocol Runtime Artifacts

These artifacts are install prerequisites for future protocol support in unified-vpn-panel. They do not add UI, API, managed inbound, subscription, or client provisioning support by themselves.

## Scope

- `runtime-images/awg2` builds pinned AmneziaWG userspace runtime binaries for Linux `amd64` and `arm64`.
- `runtime-images/naive-caddy` builds a pinned Caddy NaiveProxy-compatible server for Linux `amd64` and `arm64`.
- `runtime-images/mieru/mita-v3.35.0.manifest.json` records official Mieru `mita` release artifacts for Linux `amd64` and `arm64`; the verifier downloads upstream checksum files and release bytes and rejects mismatches.
- `.github/workflows/protocol-runtime-images.yml` publishes immutable GHCR images only when explicitly dispatched or when scoped protocol-runtime paths change on `main` or matching protocol runtime tags.

## AmneziaWG AWG2

Pinned inputs:

- `amnezia-vpn/amneziawg-go` commit `cf9d2dd202821301f7039093b0a1b3d4b574c47c`, MIT.
- `amnezia-vpn/amneziawg-tools` commit `d09ecc38425082e472368dd2bf8c4c42d10cae03`, GPL-2.0-only.

The image builds `amneziawg-go`, `awg`, and `awg-quick` from source in separate build stages and copies only the runtime outputs into Alpine. Runtime configuration is fixed at `/opt/amnezia/awg/awg0.conf`; no keys or configs are embedded in the image.

The container must run as root inside the container because creating and configuring the tunnel requires network administration privileges. Use the narrow capabilities required for the tunnel:

```sh
docker run --rm \
  --cap-add NET_ADMIN \
  --device /dev/net/tun \
  -v "$PWD/awg0.conf:/opt/amnezia/awg/awg0.conf:ro" \
  ghcr.io/OWNER/unified-vpn-panel-protocol-awg2:awg2-go-cf9d2dd-tools-d09ecc3-SHORTSHA
```

Do not run the container as privileged unless a target host has a documented, unavoidable TUN setup limitation. The entrypoint is idempotent: it leaves an already-present `awg0` interface in place and cleans it up on `SIGTERM` or `SIGINT`.

The runtime is intended for IPv4 operation. Provide IPv4 `Address`, `ListenPort`, `PostUp`, and `PostDown` settings in the mounted config. Avoid IPv6 routes and firewall rules in this artifact until IPv6 support is explicitly designed.

## Naive Caddy

Pinned inputs:

- Caddy `v2.11.4`.
- `xcaddy` `v0.4.5`.
- `klzgrad/forwardproxy` commit `d62c80d3dd2c706b6b87579844d2397bddd18317` from the NaiveProxy branch line, Apache-2.0.
- Naive client release `v150.0.7871.63-1` is a client compatibility reference only. It is not built into the server image.

The image builds Caddy with the pinned forwardproxy module and runs only `/etc/caddy-naive/Caddyfile.naiveproxy`. Arbitrary command and config overrides are rejected. The healthcheck uses Caddy's localhost admin endpoint without proxy credentials.

Secrets are owned by the panel secret store, not by Docker environment variables. The panel must render the complete active config to `/etc/caddy-naive/Caddyfile.naiveproxy` as numeric uid:gid `10001:10001` with mode `0600`. The image pins its unprivileged `caddy` account to those IDs so host bind-mount ownership is deterministic. The image contains only a non-secret fail-closed example at `/etc/caddy-naive/examples/Caddyfile.naiveproxy.example`; it is never used as the active config.

Example generated config shape:

```caddyfile
{
	admin 127.0.0.1:2019
	storage file_system /data/caddy
}

:443 {
	bind 0.0.0.0
	tls /etc/caddy-naive/tls/fullchain.pem /etc/caddy-naive/tls/privkey.pem

	route {
		forward_proxy {
			basic_auth generated-user $2a$14$generatedbcryptcredentialhash
			hide_ip
			hide_via
			probe_resistance
		}
		respond /healthz 204
		respond 404
	}
}
```

Mount the panel-generated config and TLS directory:

```sh
docker run --rm \
  -p 443:443/tcp \
  -v "$PWD/Caddyfile.naiveproxy:/etc/caddy-naive/Caddyfile.naiveproxy:ro" \
  -v "$PWD/tls:/etc/caddy-naive/tls:ro" \
  ghcr.io/OWNER/unified-vpn-panel-protocol-naive-caddy:naive-caddy-v2.11.4-xcaddy-v0.4.5-forwardproxy-d62c80d-SHORTSHA
```

The Caddy admin endpoint is bound to `127.0.0.1:2019` inside the container by default. Keep it unexposed.

## Mieru Mita Manifest

Pinned input:

- `enfein/mieru` release `v3.35.0`, `mita` Linux `amd64` and `arm64` tar/deb assets, GPL-3.0-only.

The manifest is signed by upstream checksum files rather than by local installation. The verifier checks:

- host, owner, repo, version, component, OS, arch, file kind, URL, and checksum URL;
- manifest hash format;
- official `.sha256.txt` value equals the manifest hash;
- downloaded release bytes hash to the manifest hash.

The verifier never installs or executes downloaded packages.

## Workflow Tags

The workflow does not publish `latest` tags and does not alter product version `0.0.1` or Xray `26.6.27`. Image tags include pinned source versions and the short repository SHA, for example:

- `protocol-awg2:awg2-go-cf9d2dd-tools-d09ecc3-SHORTSHA`
- `protocol-naive-caddy:naive-caddy-v2.11.4-xcaddy-v0.4.5-forwardproxy-d62c80d-SHORTSHA`

Each matrix build uploads its own digest artifact containing immutable `image@sha256:...` references after publication. Downstream integration must consume those artifact files rather than aggregate matrix job outputs. Build outputs include image digests, BuildKit provenance, SBOMs, and GitHub build provenance attestations where supported by the runner and registry.
