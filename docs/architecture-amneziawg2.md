# Managed AmneziaWG 2.0 architecture

## Scope

Managed AmneziaWG is a server-side protocol runtime. It is not an Xray inbound and it is not implemented by Mihomo's legacy WireGuard outbound. The panel owns endpoint lifecycle, encrypted server/client material, client CRUD, runtime reconciliation, traffic observation, export, and subscription inclusion.

Canonical upstream inputs are pinned to immutable official Amnezia VPN revisions:

- `amnezia-vpn/amneziawg-go` tag `v3.1.20260814`, commit `1b86b2ae0e493e7ea93f8c1a0f0cb6735b1551f1`;
- `amnezia-vpn/amneziawg-tools` tag `v3.1.20260812`, commit `ee0f0a9aa34ff0a0da4b3433b9512781cfe02843`.

The runtime image is built from these sources and consumed by immutable GHCR digest. Mutable tags and third-party AWG images are rejected by the provisioner.

## Data model and secret boundary

`managed_endpoints` is durable endpoint state. `managed_endpoint_clients` is the client registry. Private keys and preshared keys are encrypted in `managed_secrets`; public API projections contain only redacted summaries and safe runtime observations.

A client mutation follows this sequence:

1. lock endpoint state;
2. update the client row and encrypted secret material;
3. rebuild complete desired AWG2 state from the database;
4. atomically persist the host config with mode `0600`;
5. reconcile the running runtime;
6. verify interface health;
7. commit observed/applied hashes and client state;
8. on any runtime failure, restore the previous config and reconcile it again.

The desired state is complete. Add, update, enable, disable, and delete-by-ID never infer missing private material from the running interface.

## Lossless AWG2 contract

The following values are preserved exactly through desired state, native export, raw/JSON/Clash subscription projections, and runtime rendering:

- server and client private/public keys;
- preshared keys;
- addresses, endpoints, and allowed IPs;
- `Jc`, `Jmin`, `Jmax`;
- `S1`, `S2`, `S3`, `S4`;
- `H1`, `H2`, `H3`, `H4`;
- `I1` through `I5` when present.

A profile containing `S3` or `S4` remains AWG2. The panel must not add `Name =`, convert it to AWG3, regenerate keys, or normalize obfuscation values during import/export or apply.

## Runtime process model

The official userspace data plane runs in the foreground:

```text
amneziawg-go -f awg0
```

The container entrypoint is PID-level supervisor logic. It starts the official process, waits for `awg0`, invokes the fixed `awg2-reconcile` helper, propagates process exit, and removes owned interface/firewall state on termination. There is no `sleep infinity`, background daemon hidden behind `awg-quick`, or arbitrary command evaluation.

`awg2-reconcile` accepts only:

```text
awg2-reconcile apply
awg2-reconcile verify
awg2-reconcile down
```

It reads only `/opt/amnezia/awg/awg0.conf`, strips userspace-only interface fields into a `0600` setconf file, applies AWG parameters with official `awg`, configures address/MTU/routes/NAT, and verifies `awg show awg0`. Imported keys and obfuscation fields are passed through unchanged.

The Docker backend uses host networking, `NET_ADMIN`, and `/dev/net/tun`, with the host state directory mounted read-only into the runtime. The provisioner sets `--restart unless-stopped` and uses transactional `next`/`previous` containers for image updates.

## Reconcile and rollback

For an already-running endpoint, apply uses:

```text
docker exec unified-vpn-awg2-runtime awg2-reconcile apply
```

The container is not restarted for each client mutation. If it is stopped, the panel writes the new atomic config first, starts the owned container, then reconciles and verifies it. This makes `disable -> enable` deterministic.

Failure is never reported as success. A failed reconcile or verify restores the previous config and reapplies it. With no previous state, the failed desired config is not retained as an applied state.

The runtime is singleton per node (`awg0`) but supports multiple clients. Client deletion is by durable client row ID and rebuilds the complete peer set, so deleting one client cannot erase unrelated peers.

## Health and traffic

`awg show awg0 dump` is parsed as bounded typed data. Only safe fields leave the node:

- panel-generated client ID;
- enabled state;
- latest handshake timestamp;
- receive and transmit byte counters.

No keys, PSKs, raw configs, or command output are returned by node-command responses.

The managed AWG traffic job runs every ten seconds through the same local/remote driver contract. Server RX is client upload; server TX is client download. Monotonic deltas survive normal polling, and a runtime counter reset starts a new baseline instead of producing negative traffic. Aggregated traffic and latest handshake are exposed in the managed client UI.

## Export and subscriptions

Native export is generated from encrypted durable state, not by scraping a running process. The server public key is derived and checked against stored public material. Exported profiles retain AWG2 parameters including `S3/S4`.

Subscription rendering may emit Mihomo-compatible metadata for client consumption, but Mihomo is not the managed server data plane. The official runtime remains `amneziawg-go` plus `awg`.

## Security invariants

- fixed interface, container, config, and state paths;
- immutable image digest in install/update plans;
- no shell interpolation of user paths or arbitrary commands;
- `0600` host/runtime configs;
- encrypted private key and PSK storage;
- typed and bounded remote command requests/responses;
- no secrets in status, traffic, logs, API errors, release assets, or image layers;
- rollback before reporting an apply failure;
- no automatic AWG2-to-AWG3 conversion.

## Required verification

Before release:

1. Go unit/integration suite and race detector;
2. frontend lint, typecheck, tests, and production build on Node 24;
3. shell syntax and runtime-image static checks;
4. real Docker/TUN smoke with foreground process verification;
5. hot add/delete peer without PID change;
6. failed apply followed by exact rollback verification;
7. restart persistence;
8. immutable multi-arch image publication with provenance and SBOM;
9. secret scan, `git diff --check`, and independent exact-HEAD review.
