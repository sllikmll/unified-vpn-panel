# NaiveProxy Runtime Core

This document describes the isolated `internal/naiveproxy` package. It is a pure
runtime core for future unified-vpn-panel integration and is not full protocol
support by itself.

## Upstream Provenance

Behavior is based on the pinned official `klzgrad/naiveproxy` checkout at
`3ba967e2d36cc133a896e81a36257ad4c6ea20f4` (`v150.0.7871.63-1`) and its current
server setup documentation. NaiveProxy is BSD-3-Clause licensed in the upstream
repository.

The supported server deployment is patched Caddy with the `klzgrad/forwardproxy`
fork on the `naive` branch:

```sh
xcaddy build --with github.com/caddyserver/forwardproxy=github.com/klzgrad/forwardproxy@naive
```

The runtime is therefore a Naive-compatible Caddy forward proxy with HTTP/2 or
HTTP/3 CONNECT behavior and padding support from the fork. It is not modeled as
an Xray inbound.

## Runtime Boundaries

The package owns typed endpoint and protocol-user validation, Caddyfile and
Caddy JSON rendering, durable typed server-state persistence, secret-safe
status/list models, `naive+https` client URI export, typed install plan
construction, and a fakeable runtime orchestration interface.

It deliberately does not mutate firewall rules, download release artifacts,
create panel admin accounts, touch subscription builders, or register protocol
controllers. Installation is represented only as an allowlisted typed plan with
pinned version, release URL, and checksum inputs. The plan is explicitly marked
not installable until unified-vpn-panel carries a reproducible patched Caddy
forwardproxy artifact pinned by version, digest, and checksum.

Protocol users are protocol credentials only. Native panel login policy remains
unchanged and protocol users must not become panel admins.

## TLS And Port Ownership

NaiveProxy server operation requires the patched Caddy binary to own the HTTPS
port, normally TCP `443` on an IPv4 address. Caddy is responsible for TLS
automation or operator-provided certificates. Any future integration must
coordinate port ownership before applying a NaiveProxy endpoint, because the
core does not claim ports and does not edit firewall state.

## Apply Safety

The production runtime uses the exact package-owned Caddyfile path
`/etc/caddy-naive/Caddyfile` and the typed state path
`/etc/caddy-naive/server.json`. Request payloads cannot select either path, and
the runtime has no public raw Caddyfile byte application method. Tests may use
trusted package-private constructors for temporary paths.

The runtime always renders through `GenerateCaddyfile(Server)`. It writes the
generated Caddyfile and typed state atomically with mode `0600`, validates with a
fixed `caddy-naive validate --config /etc/caddy-naive/Caddyfile --adapter
caddyfile` argv, reloads with a fixed `caddy-naive reload --config
/etc/caddy-naive/Caddyfile --adapter caddyfile` argv, then invokes a typed HTTPS
health verifier using the endpoint listen IP, SNI domain, port, and bounded
timeouts. The OS runner accepts only fixed `caddy-naive` and `systemctl`
argument vectors and never invokes a shell, caller-provided binary, service,
config path, adapter, or environment.

`ApplyUser`, `DeleteUser`, and `Restart` hydrate the current typed state from
durable storage before acting. User changes upsert by stable ID while enforcing
case-insensitive unique protocol usernames; delete removes the selected protocol
user, then the full generated config is atomically validate/reload/health
verified. There is no process-memory-only user state.

Validation, reload, or post-apply health failures restore the previous Caddyfile
and typed state backup byte-for-byte, reload the old runtime, and verify the old
endpoint. If there was no previous config, rollback removes the attempted files
and stops the fixed service. Status and observation models map missing, stopped,
running, and failed states to typed values while redacting runtime details; they
never expose raw stdout, stderr, or passwords.

## Future Integration

Later node integration should wire this package to the shared secure node
executor for privileged file writes, service lifecycle, release artifact
verification, and health checks. UI/controller work should call the typed models
instead of constructing Caddy config directly. Subscription integration should
consume the explicit `naive+https://user:pass@domain[:port]` export format,
which is intentionally compatible with protocol connection parsers that already
understand scheme-prefixed share links.

Pure core alone is not complete protocol support. Full support still needs node
API wiring, frontend CRUD surfaces, subscription registration, reproducible
patched Caddy artifact distribution with pinned digest/checksum, port-conflict
checks, TLS operational policy, privileged installer execution, and observability
through the shared executor.
