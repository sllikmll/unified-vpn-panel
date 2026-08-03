# Unified VPN Panel

Единая панель управления VPN-инфраструктурой на базе production-кода 3x-ui и проверенных архитектурных решений Remnawave.

## Цель проекта

Одна центральная панель должна закрывать полный цикл управления:

- пользователи, клиенты, лимиты, сроки действия, трафик и подписки;
- локальные и удалённые ноды;
- добавление серверов по SSH-ключу или паролю;
- автоматическая установка, обновление и регистрация ноды;
- массовое развертывание протокольных профилей;
- мониторинг, история метрик, health-check и восстановление;
- VLESS Reality, VLESS TLS/gRPC/XHTTP, VMess, Trojan, Shadowsocks 2022, WireGuard, Hysteria2, HTTP, SOCKS/Mixed, Tunnel/TUN и MTProto;
- raw/JSON/Clash/Mihomo подписки;
- SQLite и PostgreSQL;
- современный React UI, REST/OpenAPI, WebSocket и API-токены.

## Архитектурная база

Основой выбран свежий `MHSanaei/3x-ui`, потому что он уже содержит:

- Xray lifecycle и hot-apply;
- multi-node runtime и mTLS;
- управление inbounds/clients/groups;
- все основные протоколы, включая WireGuard, Hysteria2 и MTProto;
- React 19 frontend;
- PostgreSQL и SQLite;
- subscriptions, metrics, API и OpenAPI.

Исходники Remnawave подключены как pinned submodules в `_upstream/` для контролируемого переноса функций и UX, без потери происхождения кода.

## Upstreams

- `MHSanaei/3x-ui` — основное runtime/control-plane ядро.
- `remnawave/backend` — reference для users, squads, config profiles и analytics.
- `remnawave/frontend` — reference для UX и управления сущностями.
- `remnawave/node` — reference для node lifecycle и telemetry.

## Статус качества

Каждая новая функция проходит:

1. failing test;
2. реализацию;
3. unit/integration suite;
4. frontend typecheck/lint/test/build;
5. Go test/build;
6. Docker build;
7. deploy и smoke/e2e на canary `msknew`.

Проект не считает функцию готовой, пока она не проверена реальным запуском.

## Лицензирование

Основной код сохраняет лицензию исходного проекта. Компоненты, перенесенные из Remnawave, сохраняют AGPL-3.0-only и attribution. Детали — в `UPSTREAMS.md` и исходных LICENSE/LICENCE файлах.
