# Mieru Runtime Core

This branch adds only an isolated `internal/mieru` runtime core package. It is not wired into the shared registry, controllers, frontend, node executor, or subscription server.

## Upstream Provenance

Behavior is based on the official `enfein/mieru` upstream checkout at `/tmp/mieru-upstream`, pinned by the branch environment. The server-side executable is `mita`; relevant upstream references are `pkg/appctl/proto/servercfg.proto`, `pkg/appctl/proto/clientcfg.proto`, `pkg/appctl/url.go`, `pkg/appctl/server.go`, `pkg/cli/server.go`, `docs/server-install.md`, `docs/client-install.md`, and `docs/operation.md`.

Mieru is licensed under GNU GPL v3 or later in the upstream repository. Any future distribution that embeds or ships Mieru-derived binaries or source must satisfy the upstream license obligations.

## Package Scope

`internal/mieru` provides typed Go models for the Mita server JSON fields that current upstream accepts for the narrow runtime use case:

- `portBindings` with `port`, expanded `portRange`, and `protocol` values `TCP` or `UDP`
- `users` with `name`, `password` or `hashedPassword`, quotas, and private/loopback destination flags
- optional `mtu` in the upstream server range `[1280, 1500]`

IPv4 is the only supported address family in this package. Server listen addresses are intentionally not modeled because current upstream Mita server config binds from `portBindings` rather than an explicit listen endpoint. Client export endpoints reject IPv6 literals.

The package can produce deterministic canonical JSON accepted by current upstream Mita JSON config loading. It also implements upstream-compatible `mierus://` simple links for client and subscription export paths. The golden link behavior is checked against `/tmp/mieru-upstream/pkg/appctl/url.go`: repeated `port` and `protocol` query parameters are emitted in endpoint order, and explicit upstream `portRange` values are preserved in exports instead of expanded. Secret-safe public models expose credential presence but never raw passwords or hashed passwords.

This pure package alone is not full protocol support. It does not run a VPN service by itself, implement Mieru cryptography, expose panel APIs, update protocol registries, or modify subscription delivery.

## Runtime Boundary

Runtime operations use a small typed runner interface that accepts only a fixed executable name, `mita`, and explicit argv arrays. The package does not build shell strings, accept arbitrary executable paths, pass custom environment variables, or expose stdout/stderr to callers. Runner output is converted inside the package to typed observations such as `missing`, `stopped`, `running`, or `error`.

The production runtime constructor accepts only the official current Mita server config path, `/etc/mita/server.conf.pb`, matching `/tmp/mieru-upstream/pkg/appctl/server.go`. Tests may use a typed trusted-local constructor, but no node request is allowed to supply a config path.

Supported runtime methods are:

- detect: invokes `mita version` and maps a missing binary separately from command errors
- install plan: validates an allowlisted typed plan with pinned version, URL, destination, and SHA-256 checksum inputs
- apply install plan: represented but unsupported until a pinned artifact matrix and checksums are supplied; validation alone does not mean installation execution is supported
- start, stop, restart, delete: use fixed Mita lifecycle argv
- observe: invokes `mita status`, maps current upstream output such as `mita server status is "RUNNING"` to `running`, and keeps textual output outside the public model
- apply config/user/delete user: validate and write canonical JSON atomically, invoke `mita apply config <path>`, restart the runtime, then verify with `mita describe config`
- traffic: explicitly returns unsupported because current upstream traffic data is exposed through daemon RPC/CLI metrics such as `mita get users` / `GetMetrics`, not a stable safe counter API for this isolated package boundary

Config writes use a same-directory temporary file, backup, rename, and rollback path with mode `0600`. If apply, restart, or post-apply verification fails, the previous config is restored, re-applied, restarted, and verified. If there was no previous config, the attempted runtime is stopped and the newly written file is removed. Rollback errors are joined and sanitized so command output and config material are not returned to callers.

The concrete OS runner uses `exec.CommandContext` directly with fixed `mita` argv. It rejects shell execution, unknown lifecycle verbs, and any `apply config` path other than the official default. The concrete filesystem writes through same-directory temporary files with `0600` mode and atomic rename.

`Delete` is endpoint state deletion, not uninstall. It stops the runtime and removes the config plus backup state. Package uninstall is a separate typed operation and remains unsupported in this core.

## Future Integration

Later integration with the shared secure node executor should provide the concrete runner and filesystem implementations with sandboxed file access, binary provenance checks, service supervision, and privileged operations. That executor should be the only layer allowed to download releases, install binaries, alter system services, or manage firewall policy.

Panel API and UI integration should consume only the public/status/export models from this package. Subscription integration should use generated `mierus://` links or equivalent parser-compatible exports and must not read server-side secret fields from public models.
