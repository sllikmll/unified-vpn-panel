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
- полноценные managed server-side runtimes AmneziaWG 2.0, Mieru и NaiveProxy;
- raw/JSON/Clash/Mihomo подписки;
- SQLite и PostgreSQL;
- современный React UI, REST/OpenAPI, WebSocket и API-токены.


## Установка и настройка панели

Рекомендуемый путь установки — официальный shell-установщик из выбранного релиза. Для стабильной установки используйте тег релиза, для тестирования свежих изменений — dev-канал.

После установки панель создаёт системный сервис `x-ui`, базовую директорию `/etc/x-ui` и случайные первичные учётные данные. Управление сервисом и первичными настройками выполняется через CLI-команду `x-ui`.

Минимальная последовательность настройки:

1. установить панель на поддерживаемый Linux-сервер;
2. открыть CLI-меню `x-ui`;
3. проверить или сменить web base path, пользователя и пароль администратора;
4. настроить TLS-сертификат для панели и сервера подписок;
5. выбрать хранилище: SQLite для одиночной панели или PostgreSQL для более крупной установки;
6. создать пользователей и subscription-клиентов;
7. добавить локальные или удалённые ноды;
8. включить нужные Xray inbounds и managed runtimes;
9. проверить raw/Clash/Mihomo subscriptions реальным импортом в клиент;
10. включить мониторинг, backups и ограничения доступа к панели.

Для удалённых нод используйте встроенный node lifecycle: регистрация ноды, проверка API-доступа, provisioning protocol pack, health-check и синхронизация runtime. Не редактируйте SQLite вручную для штатной установки: это аварийный путь, а не нормальная эксплуатация.

Перед публикацией панели в интернет обязательно задайте уникальный web base path, включите TLS, смените первичные учётные данные и ограничьте доступ к административному интерфейсу.

## Managed-протоколы

AmneziaWG 2.0, Mieru и NaiveProxy — не шаблоны для импорта. Панель управляет полным серверным lifecycle локально или на удалённой GUID-node:

- установка runtime по immutable digest/checksum;
- authenticated typed node-команды без произвольного remote shell API;
- create, update, start, stop, repair, rollback и uninstall;
- client CRUD, encrypted credentials, health и traffic state;
- raw/JSON/QR export и включение клиентов в общую подписку, когда формат поддерживает протокол;
- one-click full-stack provisioning plan для новой ноды: managed AWG2/Mieru/NaiveProxy плюс Xray VMess/VLESS Reality/Trojan/SS2022/WireGuard/Hysteria2 без ручного SQL;
- явный статус `unsupported` вместо повреждённых ссылок для несовместимых форматов.

Managed-секреты защищены AES-256-GCM с contextual AAD. Master key хранится вне SQLite и доступен только сервисному аккаунту панели.

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
7. deploy и smoke/e2e на тестовом или production-стенде перед публикацией релиза.

Проект не считает функцию готовой, пока она не проверена реальным запуском.

## Production subscription/runtime contract

Для managed-подписок и Xray runtime действует отдельный контракт: [`docs/subscription-runtime-contract.md`](docs/subscription-runtime-contract.md).

## One-click node full-stack provisioning

Для новой GUID-node штатный API flow начинается с:

```http
POST /panel/api/nodes/provision-full-stack/:id
```

Запрос принимает `basePort` и список `clientEmails`, возвращает каноничный production plan без plaintext secrets и без прямого доступа к SQLite. Protocol pack на каждую ноду:

| Протокол | Порт от `basePort` | Контракт |
| --- | ---: | --- |
| AmneziaWG 2.0 | `+1` | managed runtime, отдельный от WireGuard |
| Mieru | `+2` | managed runtime |
| NaiveProxy | `+3` | managed runtime |
| VMess | `+11` | Xray inbound |
| VLESS Reality | `+12` | Reality settings берутся из runtime-конфигурации ноды и должны совпадать с экспортом подписки |
| Trojan TLS | `+13` | Xray inbound |
| Shadowsocks 2022 | `+14` | TCP+UDP |
| WireGuard | `+15` | обычный WG, не AWG2 |
| Hysteria2 | `+16` | UDP |

Telegram MTProxy остаётся external action, не Mihomo proxy node.

Коротко:

- VLESS Reality должен использовать согласованные `serverName`, `target`, ключи Reality и параметры клиента, соответствующие runtime-конфигурации сервера.
- `spiderX` экспортируется буквально из server runtime; per-client `spx` запрещён.
- Generated Xray config обязан сохранять Reality `privateKey`.
- `dest` и `target` Reality не должны расходиться между серверной конфигурацией и подпиской.
- Подписка не считается готовой без полного protocol matrix: VMess, VLESS Reality, Trojan, Shadowsocks 2022, WireGuard/AWG2, Hysteria2, Mieru и NaiveProxy.
- Telegram MTProxy (`tg://`) — external action, а не замена Mieru/NaiveProxy.

## Лицензирование

Основной код сохраняет лицензию исходного проекта. Компоненты, перенесенные из Remnawave, сохраняют AGPL-3.0-only и attribution. Детали — в `UPSTREAMS.md` и исходных LICENSE/LICENCE файлах.
