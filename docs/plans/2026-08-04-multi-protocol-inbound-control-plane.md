# Multi-Protocol Inbound Control Plane Plan

> Historical design note. The AWG2 runtime assumptions below are superseded by `docs/architecture-amneziawg2.md`; production uses the official pinned `amneziawg-go`/`awg` image and `unified-vpn-awg2-runtime`.

Date: 2026-08-04

## Goal

Turn the existing Inbounds page into a unified control center for server-side endpoints across Xray inbounds, MTProto sidecars, and non-Xray runtimes: Mieru, NaiveProxy, AmneziaWG, WireGuard, plus every protocol already represented in the Unified UI. Admins must create, target, reconcile, inspect, and manage endpoints and clients locally or on connected nodes. Imported protocol connections remain supported, but they are not a substitute for managed server-side endpoints.

Non-goals for the first production PR series:

- No live cross-runtime Docker network E2E in the main fast gate. Keep real lifecycle/traffic E2E as a later isolated Docker-network suite.
- No master-side SSH for node changes. Reference WG/AWG SSH behavior informs driver semantics only.
- No broad rewrite of current Xray inbound APIs, link generation, or frontend route structure.

## Current Gap Analysis

### Existing Strengths

- `model.Inbound` already stores local and node-targeted endpoint rows with `NodeID`, `OriginNodeGuid`, per-inbound traffic fields, settings JSON, and client stats.
- `internal/web/runtime` already enforces local vs remote dispatch over authenticated node HTTPS transport. `Remote` uses node API tokens or mTLS, TLS verify modes, request hashing, zstd negotiation, response caps, and tag mapping.
- Inbound/client state changes already flow through services and `runtime.Runtime`, matching the project rule in `CLAUDE.md`.
- MTProto is already non-Xray at runtime via `internal/mtproto`, but is still shaped through the Xray-ish `Runtime` methods.
- WireGuard exists in the UI and subscription generation, including per-client key material and native config emission. Existing tests cover WireGuard link/config fan-out and CRUD.
- `internal/web/service/protocolconnections` already parses imported WireGuard, Amnezia, Hysteria2, VLESS, Trojan, Mieru, NaiveProxy, VMess, and Shadowsocks client-side connections. It redacts secrets in list/preview and reveals only through explicit endpoints.
- Subscription paths already support raw links, JSON, Clash/Mihomo YAML, Host overrides, node address resolution, external links, traffic headers, and duplicate proxy-name handling.
- Frontend has a schema-driven Inbounds form, slim list loading, per-row stats, node filtering, and explicit hydrate-before-secret workflows.

### Missing Capabilities

- Mieru, NaiveProxy, and AmneziaWG are not first-class server endpoints. They can be imported as client-facing protocol connections, but admins cannot create/manage their server lifecycle.
- Non-Xray runtimes are forced into an Xray-shaped mental model. The current `Runtime` contract has methods such as `AddInbound`, `AddUser`, and `RestartXray`, which do not describe Mieru, NaiveProxy, kernel WireGuard, or AmneziaWG honestly.
- WireGuard currently uses Xray's WireGuard inbound shape. Production-grade native WG/AWG management needs peer CRUD, interface/source detection, atomic config writes, `wg-easy` `wg0.json` handling, post-apply verification, and dump-based traffic counters.
- Node targeting for future non-Xray runtimes needs a stable driver RPC over the existing node HTTPS API. It must not create a second remote-control channel.
- No unified read model exists for heterogeneous endpoint health, runtime capability, client lifecycle, secret reveal/export, firewall policy, and rollback status.
- Subscription output is split between Xray inbounds and imported external links. Managed non-Xray endpoints need to join the same per-client subscription while keeping format-specific support explicit.
- OpenAPI/generated schema/i18n workflows do not yet include the new endpoint/client management DTOs.

### Reference Repo Findings

From `/Users/dogoninpavel/projects/wg-web-gui` and `/Users/dogoninpavel/projects/awg-web-gui`:

- Server detection reads known config paths, derives listen port, subnet, DNS, endpoint, interface, and version.
- WireGuard defaults target `wg-easy`, interface `wg0`, config `/etc/wireguard/wg0.conf`, port `51820`.
- AWG 1.5 defaults target container `amnezia-awg`, interface `wg0`, config `/opt/amnezia/awg/wg0.conf`, port `8723`, tools `wg` then `awg`.
- AWG 2.0 defaults target container `amnezia-awg2`, interface `awg0`, config `/opt/amnezia/awg/awg0.conf`, port `9723`, tools `awg` then `wg`.
- Peer CRUD must remove old peer blocks by public key before appending, use client tunnel address as server-side `AllowedIPs`, and preserve PSK when present.
- `wg-easy` v14 regenerates `wg0.conf` from `wg0.json`; when `wg0.json` exists, it is the source of truth and must be updated instead of editing only `wg0.conf`.
- Apply should backup current config, write atomically, restart/apply via a bounded command surface, and verify peer presence/removal through `wg show <iface>` or `awg show <iface>`.
- Traffic stats come from `wg show <iface> dump` / `awg show <iface> dump`, with reset-aware deltas, latest handshake, endpoint, allowed IPs, and online status by handshake age.
- These repos use SSH and container shell commands. In this repo those operations must live inside local node drivers and be invoked remotely only through existing authenticated node HTTPS transport.

## Target Domain Model

Keep `model.Inbound` stable for current Xray and MTProto endpoints during migration, but introduce explicit multi-runtime tables for all new and migrated functionality.

### Go Types

Add to `internal/database/model/managed_endpoint.go`:

```go
type RuntimeKind string

const (
	RuntimeXray       RuntimeKind = "xray"
	RuntimeMTProto    RuntimeKind = "mtproto"
	RuntimeWireGuard  RuntimeKind = "wireguard"
	RuntimeAmneziaWG  RuntimeKind = "amneziawg"
	RuntimeMieru      RuntimeKind = "mieru"
	RuntimeNaiveProxy RuntimeKind = "naiveproxy"
)

type EndpointStatus string

const (
	EndpointDraft      EndpointStatus = "draft"
	EndpointApplying   EndpointStatus = "applying"
	EndpointActive     EndpointStatus = "active"
	EndpointDegraded   EndpointStatus = "degraded"
	EndpointDisabled   EndpointStatus = "disabled"
	EndpointFailed     EndpointStatus = "failed"
	EndpointDeleting   EndpointStatus = "deleting"
	EndpointDeleted    EndpointStatus = "deleted"
	EndpointRolledBack EndpointStatus = "rolled_back"
)

type ManagedEndpoint struct {
	Id              int            `json:"id" gorm:"primaryKey;autoIncrement" example:"1"`
	UserId          int            `json:"-"`
	InboundId       *int           `json:"inboundId,omitempty" gorm:"index"`
	NodeID          *int           `json:"nodeId,omitempty" gorm:"index"`
	RuntimeKind     RuntimeKind    `json:"runtimeKind" gorm:"index;not null" validate:"required"`
	Protocol        Protocol       `json:"protocol" gorm:"index;not null" validate:"required"`
	Tag             string         `json:"tag" gorm:"uniqueIndex;not null" example:"wg-home"`
	Remark          string         `json:"remark" example:"WireGuard home"`
	Listen          string         `json:"listen"`
	Port            int            `json:"port" validate:"gte=0,lte=65535" example:"51820"`
	Enable          bool           `json:"enable" gorm:"index"`
	Status          EndpointStatus `json:"status" gorm:"index"`
	DesiredConfig   string         `json:"-" gorm:"type:text"`
	ObservedConfig  string         `json:"-" gorm:"type:text"`
	Capabilities    string         `json:"capabilities" gorm:"type:text"`
	LastAppliedHash  string         `json:"lastAppliedHash" gorm:"size:64"`
	LastObservedHash string         `json:"lastObservedHash" gorm:"size:64"`
	LastError        string         `json:"-" gorm:"type:text"`
	LastHealthAt     int64          `json:"lastHealthAt"`
	CreatedAt        int64          `json:"createdAt" gorm:"autoCreateTime"`
	UpdatedAt        int64          `json:"updatedAt" gorm:"autoUpdateTime"`
}
```

Add to `internal/database/model/managed_endpoint_client.go`:

```go
type EndpointClientState string

const (
	EndpointClientPending  EndpointClientState = "pending"
	EndpointClientApplied  EndpointClientState = "applied"
	EndpointClientDisabled EndpointClientState = "disabled"
	EndpointClientFailed   EndpointClientState = "failed"
	EndpointClientDeleting EndpointClientState = "deleting"
	EndpointClientDeleted  EndpointClientState = "deleted"
)

type ManagedEndpointClient struct {
	Id              int                 `json:"id" gorm:"primaryKey;autoIncrement" example:"1"`
	EndpointId      int                 `json:"endpointId" gorm:"uniqueIndex:idx_endpoint_client_identity,priority:1;index"`
	ClientId        int                 `json:"clientId" gorm:"index"`
	Email           string              `json:"email" gorm:"uniqueIndex:idx_endpoint_client_identity,priority:2;index"`
	Enable          bool                `json:"enable" gorm:"index"`
	State           EndpointClientState `json:"state" gorm:"index"`
	PublicIdentity  string              `json:"publicIdentity,omitempty" gorm:"index"`
	Address         string              `json:"address,omitempty"`
	CredentialRef   string              `json:"credentialRef,omitempty" gorm:"index"`
	ClientConfig     string              `json:"-" gorm:"type:text"`
	ObservedConfig  string              `json:"-" gorm:"type:text"`
	LastAppliedHash string              `json:"lastAppliedHash" gorm:"size:64"`
	LastError       string              `json:"-" gorm:"type:text"`
	CreatedAt       int64               `json:"createdAt" gorm:"autoCreateTime"`
	UpdatedAt       int64               `json:"updatedAt" gorm:"autoUpdateTime"`
}
```

Add to `internal/database/model/managed_endpoint_secret.go`:

```go
type ManagedSecret struct {
	Id          int    `json:"id" gorm:"primaryKey;autoIncrement"`
	OwnerType   string `json:"ownerType" gorm:"uniqueIndex:idx_secret_owner,priority:1"`
	OwnerId     int    `json:"ownerId" gorm:"uniqueIndex:idx_secret_owner,priority:2"`
	SecretKind  string `json:"secretKind" gorm:"uniqueIndex:idx_secret_owner,priority:3"`
	Ciphertext  []byte `json:"-"`
	Fingerprint string `json:"fingerprint" gorm:"size:64;index"`
	CreatedAt   int64  `json:"createdAt" gorm:"autoCreateTime"`
	UpdatedAt   int64  `json:"updatedAt" gorm:"autoUpdateTime"`
}
```

Add to `internal/database/model/managed_endpoint_traffic.go`:

```go
type ManagedEndpointClientTraffic struct {
	Id              int    `json:"id" gorm:"primaryKey;autoIncrement"`
	EndpointId      int    `json:"endpointId" gorm:"uniqueIndex:idx_endpoint_client_traffic,priority:1;index"`
	Email           string `json:"email" gorm:"uniqueIndex:idx_endpoint_client_traffic,priority:2;index"`
	NodeGuid        string `json:"nodeGuid" gorm:"uniqueIndex:idx_endpoint_client_traffic,priority:3;index"`
	Up              int64  `json:"up"`
	Down            int64  `json:"down"`
	LastUpCounter   int64  `json:"lastUpCounter"`
	LastDownCounter int64  `json:"lastDownCounter"`
	LatestHandshake int64  `json:"latestHandshake"`
	LastOnline      int64  `json:"lastOnline"`
	Endpoint        string `json:"endpoint,omitempty"`
	UpdatedAt       int64  `json:"updatedAt" gorm:"autoUpdateTime"`
}
```

Add to `internal/web/entity/managed_endpoint.go` for API DTOs:

```go
type ManagedEndpointView struct {
	model.ManagedEndpoint
	SecretSummary SecretSummary `json:"secretSummary"`
	ClientCount    int           `json:"clientCount"`
	Traffic        TrafficView   `json:"traffic"`
	Health         HealthView    `json:"health"`
	NodeName       string        `json:"nodeName,omitempty"`
}

type SecretSummary struct {
	HasSecrets bool     `json:"hasSecrets"`
	Fields     []string `json:"fields"`
}

type RevealSecretRequest struct {
	Fields []string `json:"fields" validate:"required"`
	Reason string   `json:"reason" validate:"omitempty,max=256"`
}

type EndpointApplyRequest struct {
	EndpointId     int    `json:"endpointId" validate:"required"`
	IdempotencyKey string `json:"idempotencyKey" validate:"required,max=128"`
}
```

### Tables

Add to `internal/database/db.go` AutoMigrate:

- `managed_endpoints`
- `managed_endpoint_clients`
- `managed_endpoint_client_traffics`
- `managed_secrets`
- `managed_endpoint_apply_logs`

`managed_endpoint_apply_logs` records idempotency and rollback:

```go
type ManagedEndpointApplyLog struct {
	Id             int    `json:"id" gorm:"primaryKey;autoIncrement"`
	IdempotencyKey string `json:"idempotencyKey" gorm:"uniqueIndex;not null"`
	EndpointId     int    `json:"endpointId" gorm:"index"`
	NodeID         *int    `json:"nodeId,omitempty" gorm:"index"`
	Action         string `json:"action" gorm:"index"`
	Status         string `json:"status" gorm:"index"`
	RequestHash    string `json:"requestHash" gorm:"size:64"`
	BeforeHash     string `json:"beforeHash" gorm:"size:64"`
	AfterHash      string `json:"afterHash" gorm:"size:64"`
	RollbackToken  string `json:"-"`
	Error          string `json:"-" gorm:"type:text"`
	CreatedAt      int64  `json:"createdAt" gorm:"autoCreateTime"`
	UpdatedAt      int64  `json:"updatedAt" gorm:"autoUpdateTime"`
}
```

Migration policy:

- Do not migrate existing Xray inbounds into `managed_endpoints` in the first DB migration.
- Add a read adapter that projects `model.Inbound` rows into the unified read model.
- Add a later idempotent seeder `XrayInboundsManagedEndpointBackfill` only after the UI can read both models without duplication.
- Keep `model.Inbound.Protocol` validator unchanged until each new server protocol has a complete driver and UI schema.

## Driver Interfaces

Add `internal/web/runtime/driver/driver.go`:

```go
package driver

type EndpointRef struct {
	EndpointId int
	InboundId  *int
	Tag        string
	NodeID     *int
}

type ApplyPlan struct {
	RuntimeKind      model.RuntimeKind
	Protocol         model.Protocol
	Endpoint         model.ManagedEndpoint
	Clients          []model.ManagedEndpointClient
	IdempotencyKey   string
	ExpectedPrevHash string
	DryRun           bool
}

type ApplyResult struct {
	Status          model.EndpointStatus
	ObservedHash    string
	RollbackToken   string
	Health          HealthSnapshot
	TrafficSnapshot []TrafficSample
	Warnings        []string
}

type ClientPatch struct {
	Endpoint EndpointRef
	Client   model.ManagedEndpointClient
	Action   string
}

type Capabilities struct {
	RuntimeKind     model.RuntimeKind `json:"runtimeKind"`
	Protocols       []model.Protocol   `json:"protocols"`
	ServerLifecycle bool               `json:"serverLifecycle"`
	ClientCRUD      bool               `json:"clientCrud"`
	NativeExport    []string           `json:"nativeExport"`
	Subscription    []string           `json:"subscription"`
	Traffic         bool               `json:"traffic"`
	Detect          bool               `json:"detect"`
	FirewallPolicy  bool               `json:"firewallPolicy"`
}

type PlanSummary struct {
	DesiredHash     string   `json:"desiredHash"`
	ExpectedActions []string `json:"expectedActions"`
	PortIntents     []PortIntent `json:"portIntents"`
	Warnings        []string `json:"warnings"`
}

type PortIntent struct {
	Network string `json:"network"`
	Listen  string `json:"listen"`
	Port    int    `json:"port"`
	Policy  string `json:"policy"`
}

type HealthSnapshot struct {
	Status    model.EndpointStatus `json:"status"`
	Message   string               `json:"message,omitempty"`
	CheckedAt int64                `json:"checkedAt"`
}

type ObservedEndpoint struct {
	EndpointHash string         `json:"endpointHash"`
	Config       map[string]any `json:"config,omitempty"`
	Health       HealthSnapshot `json:"health"`
	Warnings     []string       `json:"warnings"`
}

type TrafficSample struct {
	Email           string `json:"email"`
	PublicIdentity  string `json:"publicIdentity,omitempty"`
	UpCounter       int64  `json:"upCounter"`
	DownCounter     int64  `json:"downCounter"`
	LatestHandshake int64  `json:"latestHandshake,omitempty"`
	Endpoint        string `json:"endpoint,omitempty"`
	Online          bool   `json:"online"`
}

type ExportResult struct {
	FileName    string `json:"fileName"`
	ContentType string `json:"contentType"`
	Body        string `json:"body"`
}

type Driver interface {
	Kind() model.RuntimeKind
	Capabilities(ctx context.Context) (Capabilities, error)
	Validate(ctx context.Context, plan ApplyPlan) error
	Plan(ctx context.Context, plan ApplyPlan) (PlanSummary, error)
	Apply(ctx context.Context, plan ApplyPlan) (ApplyResult, error)
	Rollback(ctx context.Context, ref EndpointRef, token string) error
	Delete(ctx context.Context, ref EndpointRef) error
	ApplyClient(ctx context.Context, patch ClientPatch) (ApplyResult, error)
	DeleteClient(ctx context.Context, patch ClientPatch) (ApplyResult, error)
	Observe(ctx context.Context, ref EndpointRef) (ObservedEndpoint, error)
	Traffic(ctx context.Context, ref EndpointRef) ([]TrafficSample, error)
	Reveal(ctx context.Context, ref EndpointRef, fields []string) (map[string]string, error)
	ExportClient(ctx context.Context, ref EndpointRef, email string, format string) (ExportResult, error)
}
```

Add `internal/web/runtime/driver/registry.go`:

```go
type Registry interface {
	Driver(kind model.RuntimeKind) (Driver, bool)
	Register(driver Driver)
	Kinds() []model.RuntimeKind
}
```

Rules:

- `internal/web/runtime.Runtime` remains for existing Xray paths during the transition.
- Add `runtime.ManagedRuntime` alongside it instead of mutating the current interface in one PR.
- Local and remote implementations must execute the same driver contract.
- Xray and MTProto get adapter drivers first: `internal/web/runtime/driver/xray` and `internal/web/runtime/driver/mtproto`.
- Native WG/AWG/Mieru/NaiveProxy drivers live under `internal/web/runtime/driver/<kind>` and own their local process/file/API operations.
- Drivers accept structured config objects. They never accept raw shell strings from API/UI.

### Managed Runtime Contract

Add to `internal/web/runtime/runtime.go`:

```go
type ManagedRuntime interface {
	Runtime
	ApplyEndpoint(ctx context.Context, plan driver.ApplyPlan) (driver.ApplyResult, error)
	DeleteEndpoint(ctx context.Context, ref driver.EndpointRef) error
	ApplyEndpointClient(ctx context.Context, patch driver.ClientPatch) (driver.ApplyResult, error)
	DeleteEndpointClient(ctx context.Context, patch driver.ClientPatch) (driver.ApplyResult, error)
	ObserveEndpoint(ctx context.Context, ref driver.EndpointRef) (driver.ObservedEndpoint, error)
	EndpointTraffic(ctx context.Context, ref driver.EndpointRef) ([]driver.TrafficSample, error)
	RevealEndpointSecret(ctx context.Context, ref driver.EndpointRef, fields []string) (map[string]string, error)
	ExportEndpointClient(ctx context.Context, ref driver.EndpointRef, email string, format string) (driver.ExportResult, error)
}
```

`Local` dispatches to the local registry. `Remote` sends the same plan to node HTTPS endpoints.

## Capability Matrix

| Protocol | Runtime kind | Server lifecycle | Client CRUD | Traffic | Raw sub | JSON sub | Clash/Mihomo | Native export | Notes |
|---|---|---:|---:|---:|---:|---:|---:|---:|---|
| VMess | xray | yes | yes | yes | yes | yes | yes | no | Existing path stable |
| VLESS | xray | yes | yes | yes | yes | yes | yes | no | Existing path stable |
| Trojan | xray | yes | yes | yes | yes | yes | yes | no | Existing path stable |
| Shadowsocks | xray | yes | yes | yes | yes | yes | yes | no | Existing path stable |
| Hysteria | xray | yes | yes | yes | yes | yes | yes | no | Existing protocol remains Xray-backed |
| HTTP | xray | yes | accounts | limited | no | yes | partial | no | Admin/proxy utility protocol |
| Mixed | xray | yes | accounts | limited | no | yes | partial | no | Admin/proxy utility protocol |
| Tunnel | xray | yes | no | inbound | no | no | no | no | Routing utility |
| TUN | xray legacy/read | read-only unless re-enabled | no | inbound | no | no | no | no | Frontend keeps legacy rendering |
| MTProto | mtproto | yes | yes | sidecar/API | yes | no | no | MTProto secret | Existing sidecar stays supported |
| WireGuard | wireguard | yes | peers | dump | wireguard URI | optional outbound JSON | yes | `.conf` | Native driver preferred; current Xray WG stays compatible |
| AmneziaWG | amneziawg | yes | peers | dump | awg/amneziawg URI | optional outbound JSON | yes | `.conf` | Include AWG params and 1.5/2.0 defaults |
| Mieru | mieru | yes | users/profiles | driver stats if available | mieru URI | no initially | skipped with clear reason | native/client JSON if driver supports | Mihomo unsupported; raw only |
| NaiveProxy | naiveproxy | yes | users | process/access stats | naive/https URI | optional outbound JSON | yes as `http`+TLS | URL/export | Caddy/forward-proxy style driver |
| Imported protocol connection | none | no | no | no | external link only | parsed when possible | supported except Mieru skip | reveal raw | Remains separate from managed endpoints |

## Local and Remote RPC Design

### New Node API Endpoints

Add routes under `internal/web/controller/managed_endpoint.go`:

- `GET /panel/api/managed-endpoints/list`
- `GET /panel/api/managed-endpoints/:id`
- `POST /panel/api/managed-endpoints/add`
- `POST /panel/api/managed-endpoints/update/:id`
- `POST /panel/api/managed-endpoints/del/:id`
- `POST /panel/api/managed-endpoints/:id/apply`
- `POST /panel/api/managed-endpoints/:id/rollback`
- `POST /panel/api/managed-endpoints/:id/reveal`
- `GET /panel/api/managed-endpoints/:id/export/:format`
- `POST /panel/api/managed-endpoints/:id/clients/add`
- `POST /panel/api/managed-endpoints/:id/clients/update/:email`
- `POST /panel/api/managed-endpoints/:id/clients/del/:email`
- `GET /panel/api/managed-endpoints/:id/clients/:email/export/:format`
- `GET /panel/api/managed-endpoints/capabilities`
- `POST /panel/api/managed-endpoints/detect`
- `POST /panel/api/managed-endpoints/traffic/push`

Add matching entries in `frontend/src/pages/api-docs/endpoints.ts`, then run `make gen` in implementation PRs. Copy `frontend/public/openapi.json` to `docs/public/openapi.json` and run `cd docs && pnpm gen:api` when public docs are updated.

### Remote Transport

Extend `internal/web/runtime/remote.go` with methods that call the same paths on the node:

- `ApplyEndpoint` -> `POST panel/api/managed-endpoints/:id/apply`
- `ApplyEndpointClient` -> `POST panel/api/managed-endpoints/:id/clients/add|update`
- `ObserveEndpoint` -> `GET panel/api/managed-endpoints/:id`
- `EndpointTraffic` -> `GET panel/api/managed-endpoints/:id/traffic`
- `RevealEndpointSecret` -> `POST panel/api/managed-endpoints/:id/reveal`
- `ExportEndpointClient` -> `GET panel/api/managed-endpoints/:id/clients/:email/export/:format`

Transport constraints:

- Reuse `Remote.do`, TLS modes, API token/mTLS auth, zstd, body hash, response cap, and `netsafe.ContextWithAllowPrivate`.
- Every mutating request carries `idempotencyKey`, `desiredHash`, and optional `expectedPrevHash`.
- The node owns local filesystem/process changes. The master never sends shell commands.
- A remote node may reject unsupported runtime kinds via `capabilities`.
- Mixed-version behavior must be explicit: old nodes report no `managed-endpoints` capability and the UI disables non-Xray node targeting for them.

## Endpoint and Client State Machines

### Endpoint State

```text
draft
  -> applying
  -> active
  -> degraded
  -> failed

active -> applying -> active
active -> disabled
disabled -> applying -> active
active|disabled|failed -> deleting -> deleted
failed -> applying -> active
failed -> rolled_back -> active|disabled
```

Rules:

- `draft`: persisted desired state, no runtime side effects yet.
- `applying`: apply log exists and idempotency key is in progress.
- `active`: desired hash equals observed hash and health probe passes.
- `degraded`: endpoint exists but health/traffic verification is stale or partial.
- `failed`: last apply failed and rollback either was not possible or not requested.
- `rolled_back`: rollback succeeded after a failed apply; follow-up observe sets `active` or `disabled`.
- `deleted`: DB row may remain tombstoned until retention cleanup if traffic history exists.

### Client State

```text
pending -> applied
pending -> failed
applied -> disabled
disabled -> applied
applied|disabled|failed -> deleting -> deleted
failed -> pending
```

Rules:

- Client identity remains the existing `ClientRecord.Email` and `ClientRecord.SubID`.
- Protocol credentials live in `ManagedSecret` or protocol-specific encrypted fields, never in slim list responses.
- Disable is preferred over delete when quotas/expiry deactivate a user.
- Delete removes runtime access but preserves accounting unless the existing client delete API requests full deletion.

## Security Boundaries

- Controllers bind and validate DTOs only. No DB queries, process calls, or driver logic in controllers.
- Services own transactions and call `runtime.ManagedRuntime`.
- Drivers receive typed config. They do not receive arbitrary shell snippets, file paths outside allowlisted runtime roots, or unvalidated environment variables.
- Node changes use existing HTTPS runtime transport only. No SSH from master.
- Local drivers may execute local binaries only through small command builders with fixed argv, allowlisted binary names, bounded timeout, bounded output, and no shell interpolation.
- File writes use atomic temp+rename where possible, backups with rollback tokens, and path checks rooted in configured runtime directories.
- Secrets are write-only or redacted in list/detail previews. Reveal/export requires explicit endpoint, field/format, authenticated admin session/API token, and audit log entry.
- Secret redaction applies to logs, errors, WebSocket payloads, OpenAPI examples, UI preview panels, and subscription format warnings.
- Port conflict checks must include `model.Inbound` and `managed_endpoints` scoped by node/local target. Conflict policy is explicit: `reject`, `reuse-same-endpoint`, or future `shared-fallback`.
- Firewall policy is declarative. Drivers return required port/protocol intents, and a separate firewall applicator validates against allowlisted backends. No arbitrary firewall commands in endpoint config.
- Rollback is best-effort but structured: each mutating driver returns `RollbackToken`; failed apply triggers automatic rollback only when the apply changed observed state.

## Subscription Integration

Subscription code paths to extend:

- `internal/sub/service.go`
- `internal/sub/json_service.go`
- `internal/sub/clash_service.go`
- `internal/sub/host_sub.go`
- `internal/web/service/client_link.go`
- `util/link/outbound.go`
- Frontend mirror in `frontend/src/lib/xray/inbound-link.ts`
- Docs mirror in `docs/lib/xray/subscription.ts` and `docs/lib/xray/protocols.ts`

### Raw

- Xray protocols: unchanged.
- MTProto: keep existing raw MTProto output.
- WireGuard: emit `wireguard://` plus downloadable native config where current behavior exists.
- AmneziaWG: emit `awg://` or `amneziawg://` if clients support it, and always provide native config export.
- Mieru: emit raw Mieru subscription/link when driver can render a usable client profile. Include no Clash/Mihomo proxy for it.
- NaiveProxy: emit `naive://`, `naive+https://`, or HTTPS URL according to driver config.
- Imported external links: keep existing behavior.

### JSON

- Xray protocols: unchanged.
- WireGuard/AWG: emit outbound JSON only when the target JSON client supports the fields; otherwise skip with metadata in response diagnostics where available.
- Mieru: skip initially unless a target JSON format is explicitly supported.
- NaiveProxy: emit an HTTP outbound with TLS fields when compatible.
- External links: keep `parsedExternalOutbound`.

### Clash/Mihomo

- Xray protocols: unchanged.
- WireGuard: emit usable `type: wireguard` with `private-key`, server `public-key`, `pre-shared-key`, `ip`/`ipv6`, DNS, MTU, persistent keepalive, allowed IPs.
- AmneziaWG: emit Mihomo WireGuard plus `amnezia-wg-option` with AWG params. Match parser keys already accepted by `protocolconnections.parseWireGuard`.
- Mieru: clearly skip with a comment or diagnostics entry, never an invalid proxy.
- NaiveProxy: emit `type: http`, `username`, `password`, `tls: true`, `sni`.
- Imported Mieru connections continue to be stored and skipped by Mihomo.

## UI Design in Inbounds

Keep route `frontend/src/pages/inbounds/InboundsPage.tsx`. Add unified controls inside the existing page rather than a new top-level page.

### Read Model

Add frontend schemas:

- `frontend/src/schemas/managed-endpoint.ts`
- `frontend/src/schemas/forms/managed-endpoint-form.ts`
- `frontend/src/api/queries/useManagedEndpointsQuery.ts`

Add backend read service:

- `internal/web/service/managed_endpoint_read.go`

The list merges:

- Existing `model.Inbound` rows projected as `runtimeKind=xray` or `mtproto`.
- New `ManagedEndpoint` rows for native runtimes.
- Imported `ProtocolConnection` rows only in a separate "Imported connections" tab/section, never as managed endpoints.

### Inbounds Page Changes

Files:

- `frontend/src/pages/inbounds/InboundsPage.tsx`
- `frontend/src/pages/inbounds/useInbounds.ts`
- `frontend/src/pages/inbounds/list/InboundList.tsx`
- `frontend/src/pages/inbounds/list/useInboundColumns.tsx`
- `frontend/src/pages/inbounds/form/InboundFormModal.tsx`
- `frontend/src/pages/inbounds/form/protocols/index.ts`
- New `frontend/src/pages/inbounds/form/protocols/mieru.tsx`
- New `frontend/src/pages/inbounds/form/protocols/naiveproxy.tsx`
- New `frontend/src/pages/inbounds/form/protocols/amneziawg.tsx`
- New `frontend/src/pages/inbounds/managed/EndpointHealthDrawer.tsx`
- New `frontend/src/pages/inbounds/managed/EndpointExportModal.tsx`
- New `frontend/src/pages/inbounds/managed/SecretRevealModal.tsx`

UI behavior:

- Primary `Add inbound` opens the same modal, now with a runtime-aware protocol picker.
- Node selector remains in the form and is gated by capabilities returned by `managed-endpoints/capabilities`.
- Existing Xray protocols use current form blocks and current API paths during phase 1.
- Mieru/NaiveProxy/AWG use new protocol blocks with typed config and validation.
- Row columns add runtime kind, target node/local, health, last apply status, traffic, and capability warnings.
- Secret values show `has secret` states. Reveal/export buttons are explicit row actions.
- Clash/Mihomo unsupported formats display a skip warning for Mieru in export/subscription preview.
- Imported protocol connections appear as a compact secondary tab or drawer using existing protocol-library components and APIs.

### I18n

Add keys to all 13 files in `internal/web/translation/`:

- `pages.inbounds.runtimeKind`
- `pages.inbounds.endpointHealth`
- `pages.inbounds.applyEndpoint`
- `pages.inbounds.rollbackEndpoint`
- `pages.inbounds.revealSecrets`
- `pages.inbounds.exportNativeConfig`
- `pages.inbounds.mihomoUnsupported`
- `pages.inbounds.capabilityMissing`
- Protocol labels for `mieru`, `naiveproxy`, `amneziawg`

Implementation PRs must update every locale and run frontend tests that include `i18n-dead-keys`.

## Exact Backend File Paths

New files:

- `internal/database/model/managed_endpoint.go`
- `internal/database/model/managed_endpoint_client.go`
- `internal/database/model/managed_endpoint_secret.go`
- `internal/database/model/managed_endpoint_traffic.go`
- `internal/web/controller/managed_endpoint.go`
- `internal/web/service/managed_endpoint.go`
- `internal/web/service/managed_endpoint_read.go`
- `internal/web/service/managed_endpoint_clients.go`
- `internal/web/service/managed_endpoint_secret.go`
- `internal/web/service/managed_endpoint_traffic.go`
- `internal/web/service/managed_endpoint_detect.go`
- `internal/web/runtime/managed.go`
- `internal/web/runtime/driver/driver.go`
- `internal/web/runtime/driver/registry.go`
- `internal/web/runtime/driver/xray/driver.go`
- `internal/web/runtime/driver/mtproto/driver.go`
- `internal/web/runtime/driver/wireguard/driver.go`
- `internal/web/runtime/driver/amneziawg/driver.go`
- `internal/web/runtime/driver/mieru/driver.go`
- `internal/web/runtime/driver/naiveproxy/driver.go`
- `internal/web/job/managed_endpoint_traffic_job.go`
- `internal/web/job/managed_endpoint_health_job.go`
- `internal/sub/managed_endpoint.go`

Files to extend:

- `internal/database/db.go`
- `internal/database/model/model.go`
- `internal/web/web.go`
- `internal/web/controller/api.go`
- `internal/web/runtime/runtime.go`
- `internal/web/runtime/local.go`
- `internal/web/runtime/remote.go`
- `internal/web/runtime/manager.go`
- `internal/web/service/port_conflict.go`
- `internal/web/service/client_crud.go`
- `internal/web/service/client_inbound_apply.go`
- `internal/web/service/inbound.go`
- `internal/web/service/inbound_node.go`
- `internal/sub/service.go`
- `internal/sub/json_service.go`
- `internal/sub/clash_service.go`
- `internal/sub/links.go`
- `tools/openapigen/main.go`
- `frontend/src/pages/api-docs/endpoints.ts`

Keep stable:

- Existing `/panel/api/inbounds/*` routes.
- Existing `/panel/api/clients/*` behavior for Xray/MTProto.
- Existing `model.Inbound` JSON shape for current frontend paths.
- Existing `internal/xray` config generation and hot-diff behavior.

## Phased Bite-Sized TDD Tasks

### Phase 0: Contracts and Read Model

1. Add failing Go tests for unified read projection in `internal/web/service/managed_endpoint_read_test.go`.
2. Add models and migrations with no behavior change.
3. Add OpenAPI allowlist entries in `tools/openapigen/main.go`.
4. Add frontend schemas and tests parsing projected Xray/MTProto/native rows.
5. Verify `make gen` produces schemas and OpenAPI without hand edits.

Acceptance:

- Existing inbounds still load through old endpoints.
- New read endpoint can list projected existing inbounds and empty native endpoints.
- No runtime side effects.

### Phase 1: Driver Registry and Xray/MTProto Adapters

1. Add tests for driver registry lookup and local adapter dispatch.
2. Add Xray adapter that delegates to existing `runtime.Runtime` methods.
3. Add MTProto adapter that delegates to existing MTProto sidecar behavior.
4. Add `ManagedRuntime` methods to `Local` and no-op-compatible remote stubs behind capability checks.
5. Add controller/service paths for apply/observe using Xray/MTProto only.

Acceptance:

- Xray and MTProto behavior is unchanged.
- New driver path can apply an existing Xray inbound in tests through a fake runtime.
- Old `/panel/api/inbounds/*` tests remain green.

### Phase 2: Remote RPC

1. Add `httptest` tests for `Remote.ApplyEndpoint`, idempotency key propagation, TLS client reuse, and oversize/error handling.
2. Add node capability advertisement and mixed-version rejection.
3. Add route registry entries and generated OpenAPI.
4. Add node dirty/reconcile hooks for native endpoints parallel to `inbound_node.go`.

Acceptance:

- Remote state changes use only authenticated HTTPS node APIs.
- Offline/old nodes mark endpoint/node dirty and reconcile later.
- No master-side SSH path exists.

### Phase 3: Secret Store and Redaction

1. Add tests that list/detail redact secrets and reveal/export returns only requested fields.
2. Add `ManagedSecret` encryption using the repository's existing crypto/settings pattern.
3. Add audit logging for reveal/export.
4. Extend redaction helpers to new DTOs, logs, and preview output.

Acceptance:

- Slim list never contains private keys/passwords/tokens.
- Detail preview contains fingerprints/booleans only.
- Reveal/export is explicit and covered by API tests.

### Phase 4: Native WireGuard Driver

1. Add pure parser/render tests for WG config: interface fields, peer blocks, remove-by-public-key, append idempotency, `AllowedIPs` host route.
2. Add key generation tests using `internal/util/wireguard`.
3. Add subnet allocation tests with exhausted subnet and collision handling.
4. Add `wg-easy` `wg0.json` read/write tests matching the reference repo behavior.
5. Add apply-plan tests with backup, rollback token, bounded commands, and verification.
6. Add dump parser tests for handshake/traffic/online and counter reset deltas.

Acceptance:

- Native WG endpoint can render client `.conf` and Mihomo YAML.
- Apply is idempotent by public key and desired hash.
- Driver never shells through untrusted strings.

### Phase 5: AmneziaWG Driver

1. Add AWG 1.5/2.0 detection tests for default container/interface/config path/port.
2. Add AWG params parse/render tests for `Jc`, `Jmin`, `Jmax`, `S1..S4`, `H1..H4`, `I1`.
3. Add native config export tests that include AWG interface params.
4. Add Mihomo YAML tests for `amnezia-wg-option`.
5. Reuse WG traffic dump parser with `awg`/`wg` tool fallback tests.

Acceptance:

- AWG 1.5 and 2.0 are distinct capability profiles.
- Native config and Mihomo output are usable and deterministic.

### Phase 6: Mieru Driver

1. Add typed settings schema and validation tests.
2. Add process/config render tests for server profiles and users.
3. Add raw subscription renderer tests.
4. Add Clash/Mihomo skip tests with a clear unsupported message.
5. Add observe/health tests using fake command/API results.

Acceptance:

- Mieru endpoints are first-class in Inbounds.
- Raw subscription includes Mieru when assigned to a client.
- Clash/Mihomo never receives invalid Mieru entries.

### Phase 7: NaiveProxy Driver

1. Add typed settings schema and validation tests for listen, TLS, users, upstream policy.
2. Add config render and process lifecycle tests.
3. Add raw URL and Mihomo `http`+TLS renderer tests.
4. Add health test for process and port probe.

Acceptance:

- NaiveProxy endpoints are manageable locally and on nodes.
- Client credentials are redacted by default and exportable on demand.

### Phase 8: UI Integration

1. Add Vitest schema tests for new endpoint forms.
2. Add component tests for protocol picker capability gating.
3. Add list tests for runtime kind, health badges, node filter, redacted secrets.
4. Add modal tests for reveal/export explicit actions.
5. Add subscription preview tests showing Mieru skipped in Mihomo.
6. Add all 13 locale keys.

Acceptance:

- Existing Inbounds page remains the entry point.
- Existing Xray form flows still submit to stable paths in phase 1.
- New native protocol flows use managed endpoint APIs.

### Phase 9: Subscription and Client Join

1. Add service tests for `ClientRecord` + `ManagedEndpointClient` membership by `SubID`.
2. Add raw/JSON/Clash tests for each managed protocol.
3. Add Host override compatibility tests for native endpoint address resolution.
4. Add docs mirror tests under `docs/lib/xray`.

Acceptance:

- One per-client subscription contains Xray, MTProto, native WG/AWG, Mieru raw, NaiveProxy, and external links as supported by format.
- Traffic headers aggregate managed endpoint traffic with existing client totals.

### Phase 10: Later Docker E2E

Create an opt-in suite, skipped by default unless `XUI_MULTI_RUNTIME_E2E=1`:

- Local native WG in isolated network.
- AWG 1.5 and 2.0 containers.
- `wg-easy` `wg0.json` lifecycle.
- Mieru process lifecycle.
- NaiveProxy lifecycle.
- Master + node HTTPS/mTLS dispatch.
- Traffic generation and subscription fetch from a test client container.

## Acceptance Tests

Backend:

- `internal/web/service/managed_endpoint_read_test.go`: projects old and new rows without duplication.
- `internal/web/service/managed_endpoint_secret_test.go`: redacts by default, reveals explicitly, audits.
- `internal/web/runtime/managed_remote_test.go`: remote RPC uses existing HTTPS client and carries idempotency/hash fields.
- `internal/web/runtime/driver/wireguard/*_test.go`: config parse/render, keys, `wg-easy`, stats, rollback.
- `internal/web/runtime/driver/amneziawg/*_test.go`: AWG defaults, params, stats, Mihomo.
- `internal/web/runtime/driver/mieru/*_test.go`: raw subscription and Mihomo skip.
- `internal/web/runtime/driver/naiveproxy/*_test.go`: lifecycle config and Mihomo.
- `internal/sub/managed_endpoint_test.go`: same `SubID` emits all endpoint links by format.
- `internal/database/managed_endpoint_migration_test.go`: SQLite and Postgres-safe migration behavior.

Frontend:

- `frontend/src/test/managed-endpoint-schema.test.ts`
- `frontend/src/test/inbound-managed-list.test.tsx`
- `frontend/src/test/inbound-managed-form.test.tsx`
- `frontend/src/test/managed-secret-reveal.test.tsx`
- `frontend/src/test/managed-subscription-format.test.ts`
- Existing inbound form/link tests stay green.

Manual or opt-in:

- `make verify`
- `XUI_MULTI_RUNTIME_E2E=1 make test-go`
- `XUI_DB_TYPE=postgres XUI_DB_DSN=... make test-go`

## Deployment and Rollback Plan

### Deployment

1. Ship schema/read-model migration with dormant UI.
2. Ship Xray/MTProto adapter path while keeping old endpoints as canonical.
3. Ship remote managed RPC and node capability detection.
4. Enable native WG locally behind capability checks.
5. Enable native WG on nodes after mixed-version tests pass.
6. Enable AWG, then NaiveProxy, then Mieru in separate PRs.
7. Merge subscription output per protocol only after driver export tests exist.
8. Turn on unified UI sections protocol by protocol.

### Runtime Rollback

- Each apply stores `beforeHash`, `afterHash`, and `rollbackToken`.
- File-backed drivers create backup snapshots before write and restore only via token.
- Process-backed drivers keep previous rendered config and restart/apply back to it.
- Client CRUD rollback is scoped by endpoint/client identity and public key or username.
- If rollback fails, endpoint enters `failed` with `LastError` and node dirty state remains set for reconciliation.

### Database Rollback

- New tables are additive and can remain unused.
- Existing `inbounds` rows are not rewritten in early phases.
- Backfill seeders must be idempotent and recorded in `HistoryOfSeeders`.
- Removing the feature flag leaves existing Xray/MTProto paths operational.

### Operational Safeguards

- Port conflicts are checked before DB commit and again in driver `Validate`.
- Apply plans are hash-addressed and idempotent.
- Command execution is allowlisted, argv-based, timeout-bound, and output-capped.
- Health jobs degrade status but do not delete runtime state.
- Traffic jobs use reset-aware counter deltas.
- Reconcile never sends secrets in logs or WebSocket payloads.

## Fixed Architecture Decisions

- New WireGuard endpoints use the native `wireguard` driver by default. Existing Xray-backed WireGuard rows remain projected as `runtimeKind=xray` and are never migrated automatically.
- WireGuard, AmneziaWG, Mieru, and NaiveProxy runtimes ship as version-pinned OCI artifacts controlled by the panel. Drivers never install an unpinned latest package from the network during endpoint apply.
- Firewall lifecycle is part of the first native-driver release: drivers emit typed port intents, the local node applies them through allowlisted UFW/nftables adapters, and rollback removes only rules owned by the endpoint.
- Imported `ProtocolConnection` rows remain imported outbounds, but admins may attach them to `ClientRecord` subscriptions through the common client picker. They never masquerade as managed server endpoints.
