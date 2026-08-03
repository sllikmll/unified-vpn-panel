# Unified VPN Panel Implementation Plan

> **For Hermes:** Execute task-by-task with test-first changes and real deployment verification.

**Goal:** Production-ready unified VPN control plane with full multi-protocol management and SSH-based node provisioning.

**Architecture:** Fresh 3x-ui is the runtime/control-plane base. New functionality extends the existing Go service/controller/runtime layers and React UI. Remnawave repositories are pinned references for user/squad/profile/analytics behavior. State-changing node operations remain behind services and never bypass runtime abstractions.

**Tech Stack:** Go 1.26, Gin, GORM, SQLite/PostgreSQL, Xray-core, React 19, TypeScript, Ant Design 6, Vite 8, Docker.

---

## Acceptance gates

A feature is complete only when:

- backend unit/integration tests pass;
- frontend typecheck, lint, test and build pass;
- Go build and Docker build pass;
- API is documented in OpenAPI;
- secrets are write-only and encrypted/hashed where applicable;
- canary deploy on `msknew` passes login/API/node/protocol smoke tests;
- rollback has been tested from a backup.

## Phase 1 — Reproducible base

1. Import current 3x-ui production source.
2. Pin Remnawave backend/frontend/node references.
3. Run baseline frontend and Go suites.
4. Build a tagged Docker image from this repository.
5. Add CI for backend, frontend and Docker.

## Phase 2 — SSH node provisioning

1. Add write-only SSH credential contract supporting password and private key.
2. Add encrypted credential storage; never return secrets through API.
3. Add SSH preflight endpoint: OS, arch, root/sudo, ports, Docker/systemd, disk.
4. Add idempotent installer for child node/panel runtime.
5. Add install progress stream and durable operation history.
6. Register installed node with token/mTLS and immediately probe it.
7. Add React wizard with exact preflight/install/verify states.
8. Add rollback/uninstall operation restricted to artifacts owned by this panel.

## Phase 3 — Protocol profiles

1. Define reusable profile schema for inbounds, ports, certificates and subscription labels.
2. Ship a complete profile for:
   - VLESS Reality TCP/RAW;
   - VLESS TLS gRPC;
   - VLESS TLS XHTTP;
   - VMess;
   - Trojan TLS;
   - Shadowsocks 2022;
   - WireGuard;
   - Hysteria2;
   - HTTP/Mixed/Tunnel/TUN;
   - MTProto.
3. Add port-conflict and firewall preflight.
4. Add dry-run diff before rollout.
5. Apply profiles through the node runtime with transactional rollback.
6. Generate and verify raw/JSON/Clash/Mihomo subscriptions.

## Phase 4 — Remnawave feature parity

1. Extend clients into richer users with status, traffic strategy and lifecycle dates.
2. Add squads/groups with inbound/profile assignment.
3. Add config profiles and profile-to-node assignment.
4. Add user and node usage analytics/history.
5. Add bulk operations and import/migration from existing 3x-ui and Remnawave installations.
6. Add webhook/Telegram notifications and audit events.

## Phase 5 — Premium UX and operations

1. Unified dashboard: fleet health, protocol health, traffic, expiring users, incidents.
2. Node inventory with topology, install/update/restart/rollback actions.
3. Protocol matrix and batch rollout UI.
4. User/squad/profile management with bulk actions.
5. Audit log and operation timeline.
6. Responsive dark/light UI, Russian and English localization.
7. Backup/restore and disaster-recovery workflow.
8. Security review, rate limits, CSRF, SSRF guards, credential redaction and permissions.

## Phase 6 — Canary and release

1. Back up the existing `msknew` panel DB/config/certificates.
2. Deploy on non-conflicting ports beside existing production services.
3. Bootstrap admin without printing credentials.
4. Add `msknew` as the first managed node.
5. Roll out complete protocol profile on dedicated canary ports.
6. Run full-tunnel HTTP/DNS/IP tests for every protocol.
7. Run browser smoke tests for login, nodes, clients, profiles and subscriptions.
8. Publish signed/tagged release with image and rollback instructions.
