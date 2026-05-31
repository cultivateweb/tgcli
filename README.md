# tgcli

CLI-клиент Telegram на Go (MTProto, пользовательский аккаунт).

> Работа в процессе. Уже есть авторизация, отправка и проверка статуса через
> [`gotd/td`](https://github.com/gotd/td). В планах — чтение, события, демон и
> TUI (см. `docs/USER_STORIES.md`).

## Структура

```
cmd/tgcli/          точка входа (main)
internal/
  cli/              разбор аргументов и подкоманды
  config/           настройки (~/.config/tgcli/config.json)
  telegram/         MTProto-клиент на gotd/td
```

## Подготовка: api_id / api_hash

MTProto-клиенту нужны `api_id` и `api_hash`. Получите их на
<https://my.telegram.org> → **API development tools** и задайте одним из способов:

```sh
# через окружение (не попадает в файлы, удобно для разовых запусков)
export TGCLI_API_ID=123456
export TGCLI_API_HASH=xxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxx
```

либо в `~/.config/tgcli/config.json` (создаётся с правами `0600`):

```json
{ "api_id": 123456, "api_hash": "xxxx", "phone": "+71234567890" }
```

Окружение имеет приоритет над файлом. Файлы `config.json` и `session.json`
содержат секреты и уже добавлены в `.gitignore`.

## Сборка и запуск

```sh
make build                       # бинарник в ./bin/tgcli
./bin/tgcli help                 # список команд

go run ./cmd/tgcli help          # без сборки
```

## Команды

| Команда   | Назначение                                            |
|-----------|-------------------------------------------------------|
| `auth`    | войти в аккаунт (телефон → код → 2FA) или выйти (`--logout`) |
| `status`  | показать, авторизован ли клиент и под кем             |
| `send`    | отправить текст (`--to`, текст или stdin)             |
| `version` | показать версию                                       |

Глобальные флаги: `-v` (подробный вывод), `-config <путь>`.

```sh
./bin/tgcli auth                          # первый вход, сохранит сессию
./bin/tgcli status                        # кто залогинен
./bin/tgcli send --to me "напоминание"    # себе, в Избранное
echo "сборка готова" | ./bin/tgcli send --to @username
```

## Дальнейшие шаги

См. `docs/USER_STORIES.md` — демон (фоновый онлайн), чтение чатов, стрим
событий, desktop-уведомления и интерактивный TUI.
