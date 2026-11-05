# Docket

Личный планировщик задач. Работает локально в Docker: один контейнер, один Go-процесс, одна SQLite-база.
Задачи ведёт Claude через MCP, владелец смотрит и правит их в веб-интерфейсе. Оба канала идут через один
сервисный слой, поэтому валидация, история изменений и live-обновления у них одинаковые.

Без пользователей, логина и внешних сервисов: интерфейс не обращается ни к каким внешним хостам (шрифты встроены).

## Запуск

```bash
cp .env.example .env
docker compose up -d --build
```

Открыть: <http://docket.localhost:7777> (Chrome сам резолвит `*.localhost`; для других браузеров подойдёт
<http://localhost:7777>). Проверка: `curl localhost:7777/healthz`.

- Порт опубликован только на `127.0.0.1`.
- Запросы с чужим `Host` получают 403, пишущие запросы к `/api` с чужим `Origin` тоже 403 (защита от DNS-rebinding и CSRF).
- База: `./data/docket.db`. Бэкап (`VACUUM INTO`) при старте и раз в сутки в `./data/backups`, хранятся последние 14.
- Seed: если в базе нет ни одного проекта, при старте импортируется `seed/seed.json` (проекты по `slug`, задачи по
  `external_id`, автор `claude`). Повторный запуск дублей не создаёт.

### Конфигурация (`.env`)

| Переменная | По умолчанию | Что делает |
|---|---|---|
| `DOCKET_ADDR` | `:7777` | адрес сервера внутри контейнера |
| `DOCKET_DB` | `/data/docket.db` | файл базы |
| `DOCKET_BACKUP_DIR` | `/data/backups` | куда писать бэкапы |
| `DOCKET_MCP_TOKEN` | пусто | необязательно: если задан, он главнее токена из базы, а перевыпуск в UI отключён |
| `DOCKET_ALLOWED_HOSTS` | `localhost,127.0.0.1,docket.localhost` | разрешённые `Host` и `Origin` |
| `DOCKET_SEED` | `/app/seed/seed.json` | файл начальных данных |
| `TZ` | `Asia/Ho_Chi_Minh` | часовой пояс, в котором считается «сегодня» |

## Что есть в интерфейсе

| Экран | Адрес | Что делает |
|---|---|---|
| Сегодня | `/` | план на день, просроченное, прогресс, активность, статус MCP; кружок статуса циклит todo → doing → done |
| Доска проекта | `/p/:slug` | колонки по статусам проекта, перетаскивание, фильтры, группировка, сортировка, вкладка «Список», сохранённые виды |
| Панель задачи | `?task=TRK-07`, `/t/TRK-07` | свойства, markdown-описание с кликабельными чекбоксами, подзадачи, заметки, история |
| Настройки проекта | `/p/:slug/settings` | иконка, цвет, ключ, статусы, метки, свои поля, виды, экспорт, архив, удаление |
| MCP | `/mcp` | статус и самопроверка, адрес и токен, режим доступа, переключатели инструментов, журнал вызовов |
| Корзина | `/trash` | восстановление задач и проектов, очистка |

Горячие клавиши: `C` новая задача, `⌘K` / `Ctrl+K` поиск, `Esc` закрывает панель, поиск и диалоги.
Тема (светлая, тёмная, как в системе) переключается в футере сайдбара.

Задачи имеют ID вида `TRK-07`: ключ проекта и номер. Номера не переиспользуются. Ключ можно сменить в настройках проекта,
все ID пересчитаются, но старые ссылки с прежним ключом перестанут работать.

## Подключение Claude (MCP)

На первом старте Docket сам создаёт токен `dkt_…`, пишет его в лог один раз и хранит в базе. Дальше он виден
(маской, с кнопкой «Показать») на вкладке **MCP**, там же его можно перевыпустить и скопировать готовый конфиг.

Claude Code:

```bash
claude mcp add --transport http docket http://localhost:7777/mcp --header "Authorization: Bearer <TOKEN>"
```

Claude Desktop (`claude_desktop_config.json`):

```json
{
  "mcpServers": {
    "docket": {
      "command": "npx",
      "args": ["-y", "mcp-remote", "http://localhost:7777/mcp",
               "--header", "Authorization:${AUTH}", "--allow-http"],
      "env": { "AUTH": "Bearer <TOKEN>" }
    }
  }
}
```

Инструменты (12). В ответах задачи называются по ID (`TRK-07`), `project` принимает ключ, slug или id, статус задаётся ключом.

| Чтение | Запись |
|---|---|
| `list_projects`, `get_board`, `get_today`, `get_task`, `search` | `create_project`, `create_tasks` (до 100 за раз, эпик с подзадачами через `ref`/`parent_ref`, идемпотентно по `external_id`), `update_task`, `move_tasks`, `add_note`, `carry_over`, `delete_tasks` |

- `delete_tasks` по умолчанию **выключен**; он только отправляет задачи в корзину, окончательно очищает её владелец в UI.
- Режим «Только чтение» оставляет только инструменты чтения; запись возвращает `forbidden`. Выключенный инструмент
  не виден в `tools/list`, а вызов по имени возвращает `tool_disabled`.
- Каждый вызов попадает в журнал (инструмент, краткие аргументы, время, результат), журнал хранится 7 дней.
- Неизвестная область у задачи создаёт метку проекта сама; в `create_tasks` и `update_task` можно передавать значения
  своих полей по имени (`custom`).

## Разработка

```bash
go test ./...                     # бэкенд (тесты HTTP открывают порт на localhost)
cd web && npm ci && npm run build # tsc --noEmit + vite build → internal/web/dist
```

Для фронта с hot reload запустите бэкенд и `cd web && npm run dev` (Vite проксирует `/api` на `:7777`):

```bash
DOCKET_DB=./data/dev.db DOCKET_BACKUP_DIR=./data/backups DOCKET_SEED=./seed/seed.json go run ./cmd/docket
```

```
cmd/docket          конфиг, wiring, graceful shutdown, обслуживание (бэкап и чистка журнала раз в сутки)
internal/store      SQLite, миграции (migrations/*.sql), весь SQL
internal/service    правила домена, валидация, activity, события, MCP-настройки
internal/httpapi    REST, SSE /api/events, host/origin guard, bearer для /mcp, самопроверка MCP
internal/mcpserver  MCP-инструменты → service, политика доступа и журнал вызовов
internal/events     pub/sub для SSE
internal/web        встроенный SPA (dist)
web/                React + TypeScript + Vite
docs/design/        исходные макеты и токены
```

Правило слоёв: `httpapi` и `mcpserver` вызывают только `service`, SQL живёт только в `store`.

### REST

Все запросы выполняются от имени `denis`. Ошибки приходят как `{"error": {"code", "message"}}`
(`validation` 400, `not_found` 404, `status_in_use` 409, `forbidden` 403). Задачу в пути можно указать кодом (`TRK-07`) или id.

| Метод и путь | Назначение |
|---|---|
| `GET/POST /api/projects`, `PATCH/DELETE /api/projects/:id` | проекты; `DELETE` уводит в корзину |
| `POST /api/projects/:id/archive`, `POST /api/projects/:id/restore`, `GET /api/projects/:id/export` | архив, возврат из корзины, JSON-экспорт |
| `GET/POST /api/projects/:id/statuses`, `PATCH/DELETE /api/statuses/:id`, `POST …/statuses/reorder` | статусы; `DELETE ?move_to=<id>` переносит задачи |
| `GET/POST /api/projects/:id/labels`, `PATCH/DELETE /api/labels/:id` | метки областей |
| `GET/POST /api/projects/:id/fields`, `PATCH/DELETE /api/fields/:id` | свои поля (`number`, `url`, `select`) |
| `GET/POST /api/views?project=`, `PATCH/DELETE /api/views/:id` | сохранённые виды |
| `GET /api/today?date=` | задачи на дату, просроченные и последняя активность |
| `GET /api/tasks?project=&status=&planned_for=&area=&created_by=&q=&parent=&include_done=1` | список |
| `POST /api/tasks`, `GET/PATCH/DELETE /api/tasks/:id` | CRUD; `planned_for: ""` очищает дату |
| `POST /api/tasks/:id/move` | `{status, before_id?, after_id?, project_id?}` (перетаскивание) |
| `POST /api/tasks/:id/restore`, `POST /api/tasks/:id/notes`, `POST /api/tasks/carry-over` | восстановление, заметка, перенос с даты на дату |
| `GET /api/search?q=&scope=all\|tasks\|projects` | задачи по названию, по описанию (со сниппетом), проекты |
| `GET /api/trash`, `GET /api/trash/projects`, `DELETE /api/trash` | корзина и её очистка (физическое удаление) |
| `GET /api/mcp/status`, `POST /api/mcp/check`, `GET/PATCH /api/mcp/settings` | статус, самопроверка, режим и переключатели |
| `GET /api/mcp/token`, `POST /api/mcp/token/rotate`, `GET/DELETE /api/mcp/calls` | токен, перевыпуск, журнал |
| `GET /api/events` | SSE: `{type, task_id?, project_id?, actor}` |
| `GET /api/export`, `GET /healthz` | полный дамп, проверка живости |

## Если что-то не работает

- **`/mcp` отвечает 401.** Нужен заголовок `Authorization: Bearer <TOKEN>`; токен смотрите на вкладке MCP. Вкладка **MCP**
  в браузере открывается без токена: для запросов с `Accept: text/html` отдаётся интерфейс.
- **403 `forbidden_host`.** Добавьте имя хоста в `DOCKET_ALLOWED_HOSTS` и перезапустите контейнер.
- **Самопроверка красная.** Каждая строка на вкладке MCP объясняет, что делать; логи: `docker compose logs docket`.
- **Начать с нуля.** Остановите контейнер и удалите `./data/docket.db*`; бэкапы лежат в `./data/backups`.
