# Unified VPN Panel

[![Latest release](https://img.shields.io/github/v/release/sllikmll/unified-vpn-panel?display_name=tag)](https://github.com/sllikmll/unified-vpn-panel/releases/latest)
[![CI](https://github.com/sllikmll/unified-vpn-panel/actions/workflows/ci.yml/badge.svg)](https://github.com/sllikmll/unified-vpn-panel/actions/workflows/ci.yml)
[![Release](https://github.com/sllikmll/unified-vpn-panel/actions/workflows/release.yml/badge.svg)](https://github.com/sllikmll/unified-vpn-panel/actions/workflows/release.yml)

Единая панель управления VPN-инфраструктурой на базе production-кода 3x-ui. Она управляет Xray-протоколами, полноценными managed server-side runtimes, локальными и удалёнными нодами, клиентами, подписками, трафиком и health state.

**Текущий стабильный релиз:** [`v3.1.0`](https://github.com/sllikmll/unified-vpn-panel/releases/tag/v3.1.0).

## Что уже работает

- пользователи, клиенты, лимиты, сроки действия и traffic accounting;
- локальные и удалённые GUID-ноды с mTLS и typed node-командами;
- SQLite и PostgreSQL;
- raw, JSON, Clash и Mihomo subscriptions;
- React 19 UI, REST/OpenAPI, WebSocket и API-токены;
- VLESS Reality, VLESS TLS/gRPC/XHTTP, VMess, Trojan, Shadowsocks 2022, WireGuard, Hysteria2, HTTP, SOCKS/Mixed, Tunnel/TUN и MTProto;
- managed server-side runtimes AmneziaWG 2.0, Mieru и NaiveProxy;
- one-click provisioning protocol pack на новую ноду;
- runtime health, traffic state, rollback и восстановление после перезапуска.

Managed-протоколы здесь — не импорт конфигурации и не outbound внутри Mihomo. Панель создаёт серверный runtime, управляет его жизненным циклом и клиентами, наблюдает реальный data plane и включает клиентов в subscriptions.

## Быстрая установка

Требуется root-доступ к поддерживаемому Linux-серверу. Стабильная установка `v3.1.0` закрепляет и installer, и release assets на одной версии:

```bash
bash <(curl -fsSL https://raw.githubusercontent.com/sllikmll/unified-vpn-panel/v3.1.0/install.sh) v3.1.0
```

Установка последнего стабильного релиза:

```bash
bash <(curl -fsSL https://raw.githubusercontent.com/sllikmll/unified-vpn-panel/main/install.sh)
```

Dev-канал предназначен только для тестирования текущего `main`:

```bash
bash <(curl -fsSL https://raw.githubusercontent.com/sllikmll/unified-vpn-panel/main/install.sh) dev-latest
```

После установки:

```bash
x-ui
```

Installer создаёт системный сервис `x-ui`, директорию `/etc/x-ui` и случайные первичные учётные данные. Для unattended/cloud-init установки случайные credentials записываются в `/etc/x-ui/install-result.env` с mode `0600`.

Доступные release packages:

- Linux: `386`, `amd64`, `arm64`, `armv5`, `armv6`, `armv7`, `s390x`;
- Windows: `amd64`.

Все архивы опубликованы на странице [GitHub Releases](https://github.com/sllikmll/unified-vpn-panel/releases/latest). Cloud-init и unattended deployment описаны в [`deploy/README.md`](deploy/README.md).

## Первичная настройка

1. Откройте CLI-меню `x-ui`.
2. Смените web base path и проверьте учётные данные администратора.
3. Настройте TLS для панели и сервера подписок.
4. Выберите SQLite для одиночной панели или PostgreSQL для распределённой установки.
5. Добавьте локальные или удалённые ноды.
6. Создайте Xray inbounds и нужные managed endpoints.
7. Создайте клиентов и проверьте subscriptions реальным импортом в клиент.
8. Настройте monitoring, backup и ограничения доступа к административной панели.

Не редактируйте SQLite вручную при штатной эксплуатации. Node lifecycle, provisioning, runtime apply и client CRUD должны проходить через API/панель — прямой SQL обходит multi-node synchronization и rollback.

## Managed AmneziaWG 2.0

### Официальный data plane

`v3.1.0` использует только официальные pinned upstream releases:

- [`amnezia-vpn/amneziawg-go v3.1.20260814`](https://github.com/amnezia-vpn/amneziawg-go/releases/tag/v3.1.20260814);
- [`amnezia-vpn/amneziawg-tools v3.1.20260812`](https://github.com/amnezia-vpn/amneziawg-tools/releases/tag/v3.1.20260812).

Runtime запускается foreground под supervision:

```text
amneziawg-go -f awg0
```

Mihomo может использовать экспортированный профиль на клиентской стороне, но не заменяет managed server data plane. Серверный runtime всегда использует официальный `amneziawg-go` и `awg`.

### Immutable runtime image

```text
ghcr.io/sllikmll/unified-vpn-panel-protocol-awg2@sha256:465febe1b4156b240b0b929b5f180a2696a2501f8bb787b24406034e0d96c059
```

Поддерживаемые image-платформы: `linux/amd64` и `linux/arm64`. Для образа публикуются provenance и SBOM attestations. Provisioner принимает immutable digest и отклоняет mutable/неожиданные AWG images.

Для runtime-ноды нужны Docker, `/dev/net/tun`, host networking и capability `NET_ADMIN`. Provisioning plan создаёт runtime container с проверяемым mount/config contract.

### Transactional lifecycle

Любая мутация endpoint или клиента проходит полный desired-state reconcile:

1. блокируется состояние endpoint;
2. обновляются durable client rows и encrypted secrets;
3. атомарно сохраняется новый config с mode `0600`;
4. выполняется hot reconcile через официальный `awg`;
5. проверяется интерфейс `awg0`;
6. только после успешной проверки фиксируется applied state;
7. при ошибке восстанавливаются предыдущие config и running/stopped state.

Добавление, изменение, включение, отключение и удаление клиента не требуют перезапуска data plane. Ошибка старта, apply или verify не возвращается как успешный API result.

### Lossless AWG2 contract

Панель сохраняет без регенерации и автоматической нормализации:

- server/client `PrivateKey`, `PublicKey` и `PresharedKey`;
- `Address`, `Endpoint` и `AllowedIPs`;
- `Jc`, `Jmin`, `Jmax`;
- `S1`, `S2`, `S3`, `S4`;
- `H1`, `H2`, `H3`, `H4`;
- `I1`–`I5`, когда они присутствуют.

Профиль с `S3` или `S4` остаётся AWG2. Панель не добавляет `Name =`, не конвертирует его автоматически в AWG3 и не пересоздаёт ключи при import/export/apply.

### Address и client pool

Поддерживается схема, где адрес интерфейса и клиентский NAT pool имеют разные prefix lengths:

```text
Address = 10.10.0.1/32
IPv4Pool = 10.10.0.0/24
```

NAT применяется к `IPv4Pool`, а не только к `/32` интерфейса. При изменении пула старая firewall rule удаляется transactional reconcile.

### Health и traffic

Панель читает bounded typed snapshot из:

```text
awg show awg0 dump
```

В API/UI передаются только безопасные runtime observations:

- enabled state;
- latest handshake;
- RX/TX counters;
- агрегированный upload/download traffic.

Private keys, PSK, raw configs и произвольный command output в status response не попадают. Collector работает single-flight каждые 10 секунд и корректно переживает restart/reset runtime counters.

Полный технический контракт: [`docs/architecture-amneziawg2.md`](docs/architecture-amneziawg2.md).

## Managed Mieru и NaiveProxy

Mieru и NaiveProxy используют тот же control-plane подход:

- local/remote provisioning;
- create, update, start, stop, repair, rollback и uninstall;
- client CRUD и encrypted credentials;
- health/traffic state;
- native export и включение в subscriptions, когда выбранный формат поддерживает протокол;
- явный `unsupported` вместо повреждённой ссылки для несовместимого формата.

## One-click full-stack provisioning

Для новой GUID-ноды production API flow начинается с:

```http
POST /panel/api/nodes/provision-full-stack/:id
```

Запрос принимает `basePort` и `clientEmails`, а затем формирует protocol plan без plaintext secrets и прямого SQL:

| Протокол | Порт от `basePort` | Реализация |
| --- | ---: | --- |
| AmneziaWG 2.0 | `+1` | официальный managed runtime, отдельно от WireGuard |
| Mieru | `+2` | managed runtime |
| NaiveProxy | `+3` | managed runtime |
| VMess | `+11` | Xray inbound |
| VLESS Reality | `+12` | Xray inbound с согласованными runtime/export settings |
| Trojan TLS | `+13` | Xray inbound |
| Shadowsocks 2022 | `+14` | TCP+UDP |
| WireGuard | `+15` | обычный WireGuard, не AWG2 |
| Hysteria2 | `+16` | UDP |

Telegram MTProxy остаётся external action и не считается Mihomo proxy node.

## Безопасность эксплуатации

Перед публикацией панели в интернет:

- смените первичные credentials;
- используйте уникальный web base path;
- включите TLS;
- ограничьте доступ к административному интерфейсу;
- храните managed master key вне SQLite и с доступом только для service account;
- настройте backup базы, `/etc/x-ui` и внешнего PostgreSQL;
- не публикуйте node command API без штатной mTLS-аутентификации.

Managed secrets хранятся с AES-256-GCM и contextual AAD. Public API возвращает redacted projections, а typed node-команды не дают произвольный remote shell.

## Документация

- [Архитектура проекта](docs/architecture.md)
- [Managed AmneziaWG 2.0](docs/architecture-amneziawg2.md)
- [Production subscription/runtime contract](docs/subscription-runtime-contract.md)
- [Protocol runtime artifacts](docs/protocol-runtime-artifacts.md)
- [Deployment и cloud-init](deploy/README.md)
- [Contribution guide](CONTRIBUTING.md)
- [Upstream attribution](UPSTREAMS.md)

## Разработка и проверка

Требования для текущего `main`:

- Go `1.26.6` или новее в линии `1.26`;
- Node.js `24`;
- npm `10+`;
- Docker для image/install smoke.

Основные проверки:

```bash
make dist-stub
go test -race -shuffle=on -count=1 ./...
go vet ./...
```

```bash
cd frontend
npm ci
npm run lint
npm run typecheck
npm test -- --run
npm run build
npm run build-storybook
npm audit --omit=dev --audit-level=high
```

Runtime image static contract:

```bash
bash runtime-images/tests/static.sh
```

Функция не считается готовой только потому, что unit tests зелёные. Перед релизом проверяются real runtime lifecycle, hot reconcile, rollback, restart restore, immutable image digest/attestation и release packages.

## Upstreams и лицензирование

- [`MHSanaei/3x-ui`](https://github.com/MHSanaei/3x-ui) — основное runtime/control-plane ядро;
- `remnawave/backend`, `remnawave/frontend`, `remnawave/node` — pinned reference implementations в `_upstream/`;
- official Amnezia VPN repositories — AWG2 data plane.

Основной код сохраняет лицензию исходного проекта. Компоненты, перенесённые из Remnawave, сохраняют AGPL-3.0-only и attribution. Подробности находятся в [`UPSTREAMS.md`](UPSTREAMS.md) и соответствующих LICENSE/LICENCE files.
