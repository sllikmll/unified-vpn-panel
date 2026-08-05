# Контракт подписок и runtime для Unified VPN Panel

Этот документ фиксирует инварианты, которые нельзя ломать при изменениях в генерации подписок, Xray runtime config и managed-node flow.

## Полный набор протоколов

Для production/device-подписки задача считается закрытой только если в подписке и runtime проверены все управляемые протоколы:

| Протокол | Где должен быть виден | Что проверить |
|---|---|---|
| VMess | raw subscription + Mihomo YAML | запись присутствует, health/delay проходит |
| VLESS Reality | raw subscription + Mihomo YAML + Xray runtime | `security=reality`, SNI `yandex.ru`, корректный public/private key, shortId, реальный HTTP 204 через tunnel |
| Trojan TLS | raw subscription + Mihomo YAML | TLS-запись присутствует и проходит health/delay |
| Shadowsocks 2022 | raw subscription + Mihomo YAML | метод/пароль не теряются, health/delay проходит |
| WireGuard / AWG2 | raw subscription + Mihomo YAML | peer/server параметры присутствуют; `alive=false` в Mihomo не считать финальным без datapath-проверки |
| Hysteria2 | raw subscription + Mihomo YAML | UDP/runtime listener жив, health/delay проходит |
| Mieru | raw subscription + Mihomo YAML/static exception | не заменять Telegram action; отдельная запись Mieru обязательна |
| NaiveProxy | raw subscription + Mihomo YAML/static exception | отдельная HTTPS/HTTP proxy запись обязательна |

Telegram MTProxy — это external action, а не Mihomo outbound. Наличие `tg://` не закрывает Mieru/NaiveProxy.

## Reality: SNI, spiderX и ключи

Для инфраструктуры `sllikmll` VLESS Reality по умолчанию должен использовать:

```text
serverName / SNI: yandex.ru
spiderX: /
```

Критичные правила:

1. `spiderX` — server-owned runtime path, а не per-client seed. Подписки должны экспортировать literal server value. Если серверный `spiderX` пустой — использовать `/`.
2. Нельзя генерировать per-client `spx` из `subId`, email или UUID: Mihomo может получить ссылку, не совпадающую с Xray runtime.
3. При генерации Xray runtime config нельзя удалять Reality private key. В 3x-ui DB ключ может лежать в UI-структуре `realitySettings.settings` или top-level `realitySettings.privateKey`; итоговый runtime обязан содержать top-level `realitySettings.privateKey`.
4. `dest` и legacy/compat `target` должны указывать на один и тот же Reality target. Для этой инфраструктуры — `yandex.ru:443`. Нельзя оставлять `dest=yandex.ru:443` рядом с `target=www.cloudflare.com:443`.
5. Проверка Reality не ограничивается тем, что URL красиво выглядит. Обязательный smoke: Xray client или Mihomo через VLESS Reality должен получить `HTTP 204` с `https://www.gstatic.com/generate_204`.

## Normalized clients vs `inbounds.settings.clients`

3x-ui хранит клиентов в двух слоях:

- normalized tables: `clients`, `client_inbounds`, `client_traffics`;
- legacy/runtime-facing JSON: `inbounds.settings.clients`.

Инвариант:

```text
Каждый managed client, который виден в подписке, обязан быть виден Xray runtime config на соответствующем inbound.
```

Практические правила:

1. Не создавать клиентов прямой SQLite-вставкой только в один слой.
2. После repair/sync проверять оба слоя: normalized binding и `settings.clients`.
3. После restart читать `/usr/local/x-ui/bin/config.json` и проверять `settings.clients` runtime, а не только DB.
4. `subId` в `clients` и `settings.clients` должен совпадать; иначе старые URL начинают отдавать `404`.

## AppleDouble / macOS tar pitfall

При сборке на macOS архивы могут принести AppleDouble files `._*.json`. Если они попадают в Go embed (`internal/web/translation/*`), locale loader может упасть на:

```text
invalid character '\x00' looking for beginning of value
```

Обязательные защиты:

- при архивировании использовать `COPYFILE_DISABLE=1`;
- исключать `._*` и `__MACOSX`;
- locale loader должен игнорировать dot-files;
- перед native deploy проверять старт binary rollback-guard’ом.

## Safe live rollout

Минимальный порядок:

1. Backup `/etc/x-ui/x-ui.db` и `/usr/local/x-ui/x-ui`.
2. Собрать binary с актуальным frontend embed (`frontend npm run build` → Go build).
3. Установить binary с rollback-guard.
4. `systemctl restart x-ui`.
5. Проверить:
   - `systemctl is-active x-ui`;
   - port `2096`;
   - protocol listeners;
   - `/usr/local/x-ui/bin/config.json`;
   - все device subscription URL;
   - VLESS Reality independent datapath smoke;
   - полный protocol matrix, включая AWG2, Mieru и NaiveProxy.

Если Mieru/NaiveProxy отсутствуют в raw или Mihomo YAML — rollout не PASS, даже если VMess/VLESS/Trojan/SS/WG/HY2 зелёные.
