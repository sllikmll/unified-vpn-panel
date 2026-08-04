# AmneziaWG 2.0 Backend Tracer Bullet

This backend tracer bullet adds an IPv4-only AmneziaWG 2.0 runtime path for managed endpoints. It is intentionally backend-only: no frontend flows, subscriptions, fleet deployment automation, Mieru, or NaiveProxy support are included here.

## Scope

- Runtime kind: `amneziawg`.
- Inbound protocol carried by the managed runtime driver: `amneziawg`.
- Addressing: IPv4 only. Server and peer validation rejects IPv6 server mode and IPv6 client allowed IPs.
- Config shape: native WireGuard-style config with AmneziaWG 2.0 obfuscation fields `Jc`, `Jmin`, `Jmax`, `S1`, `S2`, `S3`, `S4`, `H1`-`H4`, and `I1`.
- Lifecycle operations: endpoint apply, observe/status/health/detect, and delete. The v1 contract defines start/stop/restart verbs, but this repository has not yet wired typed AWG start/stop/restart methods through the managed driver interface; those verbs remain unsupported for AWG node-command execution.
- Client operations: AWG client create, update, delete, enable/disable, status, and export are intentionally not advertised. The node process cannot safely reconstruct client private-key export or per-peer desired state from process memory after a panel/node restart. These operations remain blocked until the control plane can send an atomically persisted complete desired endpoint config after its durable transaction.

## Behavioral Attribution

The AmneziaWG 2.0 parameter model and operational behavior are attributed to the `coinman-dev/3ax-ui` AmneziaWG work. This implementation adapts the backend control-plane behavior to this codebase's managed runtime abstraction, strict typed node-command contract, replay guard, and existing authenticated HTTPS node transport.

The implementation is not a wholesale import of `coinman-dev/3ax-ui`; it keeps Xray behavior unchanged and preserves the existing node-management transport boundaries.

## Backend Selection

The command backend detects and uses one of two allowlisted execution modes:

- Docker: existing `amnezia-awg2` container, operated only through fixed `docker container inspect`, `docker start`, `docker restart`, `docker exec ... awg show`, and `docker stop` argv forms. The fixed Docker profile writes `0600` configs atomically to host `/opt/amnezia/state/amnezia-awg2`; the container must mount that directory at `/opt/amnezia/awg` and read `/opt/amnezia/awg/awg0.conf`. Almaty custom entrypoint deployments are compatible only when they use that same mount and config destination.
- Native: host `awg` and `awg-quick`, operated only through fixed `awg show` and `awg-quick up/down` argv forms. Native config is written under `/etc/amnezia/amneziawg`.

No node-command request accepts raw shell commands, raw argv, environment variables, file paths, container names, image names, or arbitrary config paths. Docker mount verification rejects a container whose `/opt/amnezia/awg` destination is not backed by the fixed host state directory when `docker inspect` output is available. The file store writes interface config atomically under the selected fixed backend store root and rolls back on apply or verify failure.

## Compatibility With awg-web-gui Docker Fleets

The Docker backend assumes an existing `amnezia-awg2` container name, matching the common awg-web-gui deployment pattern. The panel does not create or mutate Docker Compose definitions in this tracer bullet. It writes the rendered AmneziaWG config to `/opt/amnezia/state/amnezia-awg2/awg0.conf`, restarts the fixed container, and verifies the active `awg0` interface, allowing current Docker fleets to continue owning image versioning, networking, volumes, and host firewall policy.

If a fleet uses a different container name or config mount, that remains outside this tracer bullet. The contract should be extended with explicit typed fleet metadata before supporting those variants.

## Install Plan

The managed endpoint API exposes a typed install-plan surface, but AWG2 runtime installation is blocked. Current fleets use local `amnezia-awg2:latest` images with inconsistent image IDs and no canonical digest. This repo must first build and publish a reproducible GHCR AWG2 runtime image pinned by digest; until then the API must not execute an unpinned latest image, `curl | bash`, arbitrary image names, arbitrary paths, or arbitrary environment variables.

## Node Command Security

Remote execution uses the existing authenticated HTTPS node transport and a strict node-command envelope:

- `targetGuid` must match the receiving panel GUID.
- Requests carry bounded `commandId`, `idempotencyKey`, `nodeId`, `endpointId`, runtime, operation, generation, and validity timestamps.
- A replay guard stores completed responses by idempotency key and rejects conflicting replays.
- Sealed v1 request material requires an explicitly presented non-empty Bearer token that has already passed API-token validation. Cookie-authenticated browser sessions and mTLS-only requests can use generic panel APIs, but they cannot execute sealed node-command v1 requests. This remains true until a dedicated command key exists.
- Secret request material is accepted only as `sealedPayload`, decrypted with the validated node API bearer token, and never serialized back in request JSON.
- Client export material is not supported for AWG node commands because node-side private client keys are not retained solely for export.
- Responses expose summary/status codes and typed result metadata only. Raw stdout, stderr, rollback tokens, private keys, preshared keys, and full config text are not returned in public response fields.

If future secret transport needs cross-node recipients or rotating keys independent of node API tokens, the current contract should fail closed rather than adding plaintext fields.
