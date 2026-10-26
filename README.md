# Task Management API

REST API для управления задачами в командах: JWT-аутентификация, разграничение прав, история изменений, Redis-кеш списка задач и SQL-аналитика. Реализовано на Go, MySQL 8.4 и Redis 7; приложение и инфраструктура запускаются через Docker Compose. Спецификация — OpenAPI 3 со Swagger UI.

## Возможности

- Регистрация пользователей и JWT (HS256, 24 часа); пароли хранятся в виде bcrypt-хешей.
- Команды с ролями `owner`, `admin`, `member` и приглашением зарегистрированных пользователей.
- Задачи со статусами `todo`, `in_progress`, `done`, назначением исполнителя, фильтрами и пагинацией.
- Проверка полномочий на уровне бизнес-логики и изоляция данных между командами.
- Оптимистический контроль версий (`expected_version`) и блокировка строки `SELECT ... FOR UPDATE`; создание/обновление задачи и запись истории выполняются атомарно.
- Кеширование списка задач в Redis с TTL 300 секунд и инвалидацией через версию списка команды (`task_revision`).
- SQL-отчёт по команде: задачи по статусам, топ-3 исполнителей по закрытым задачам за 30 дней, среднее время закрытия и количество комментариев.
- JSON-логи (`slog`), health-check и корректное завершение приложения (graceful shutdown).

## Быстрый запуск

Необходимо: Docker Desktop с запущенным Docker Engine и Docker Compose. На Windows рекомендуется Docker Desktop с WSL 2. Отдельно устанавливать MySQL, Redis или Go для запуска в Docker не нужно.

В Windows из корня репозитория:

```powershell
.\start.cmd
```

Или из PowerShell:

```powershell
powershell -NoProfile -ExecutionPolicy Bypass -File .\start.ps1
```

Скрипт создаёт `.env` со случайными секретами при первом запуске, запускает сервисы, ожидает завершения миграций и проверяет HTTP `/health`. Если `.env` уже существует, скрипт не заменяет его и не сбрасывает данные.

Альтернативный ручной запуск:

```powershell
# При первом запуске создайте .env из шаблона и замените все change_me_* значения
Copy-Item .env.example .env
# Откройте .env, задайте уникальные MYSQL_PASSWORD, MYSQL_ROOT_PASSWORD и JWT_SECRET (не менее 32 символов). Укажите тот же пароль пользователя MySQL в MYSQL_DSN; иначе приложение не подключится к базе.

docker compose up --build -d
docker compose ps -a
Invoke-RestMethod http://localhost:8080/health
```

В ручном варианте нельзя оставлять шаблонные пароли, а пароль в `MYSQL_DSN` должен совпадать с `MYSQL_PASSWORD`. Для обычного запуска удобнее `start.cmd`, который генерирует секреты автоматически.

После запуска:

| Ресурс | Адрес |
| --- | --- |
| API | <http://localhost:8080/api/v1> |
| Health-check | <http://localhost:8080/health> |
| Swagger UI | <http://localhost:8080/swagger> |
| OpenAPI YAML | <http://localhost:8080/openapi.yaml> |

Swagger UI загружает JavaScript и CSS из CDN, поэтому для интерфейса необходим доступ к интернету. YAML-спецификация доступна локально в `docs/openapi.yaml`.

### Полезные команды Docker

```powershell
docker compose ps -a                  # контейнеры, в том числе одноразовый migrate
docker compose logs -f app            # журналы API
docker compose logs migrate           # журнал миграций
docker compose down                   # остановить сервисы, сохранив данные
docker compose up --build -d          # пересобрать и запустить
```

Внимание: `docker compose down -v` удаляет volumes MySQL и Redis вместе со всеми данными. Для обычного перезапуска эта команда не нужна.

## Настройки

Шаблон конфигурации: [`.env.example`](.env.example). Файл `.env` хранит секреты локально и исключён из Git.

| Переменная | Назначение |
| --- | --- |
| `HTTP_ADDR` | Адрес прослушивания приложения в контейнере, обычно `:8080` |
| `HOST_PORT` | Порт API на хосте, по умолчанию `8080` |
| `MYSQL_DATABASE` | Имя базы данных |
| `MYSQL_USER`, `MYSQL_PASSWORD` | Учетные данные приложения MySQL |
| `MYSQL_ROOT_PASSWORD` | Пароль администратора MySQL |
| `MYSQL_DSN` | Строка подключения Go-сервиса к MySQL (`parseTime=true`, UTC) |
| `MYSQL_HOST_PORT` | Проброс MySQL на хост, по умолчанию `3306` |
| `REDIS_ADDR`, `REDIS_PASSWORD`, `REDIS_DB` | Подключение к Redis |
| `REDIS_HOST_PORT` | Проброс Redis на хост, по умолчанию `6379` |
| `JWT_SECRET` | Секрет подписи JWT, минимум 32 символа |
| `JWT_ISSUER` | Издатель JWT |

Изменение паролей MySQL в `.env` не меняет пароль пользователя в уже созданном томе БД. Не заменяйте `.env` без необходимости.

## Миграции

Миграции расположены в `migrations/` (`*.up.sql` / `*.down.sql`). В `compose.yaml` предусмотрен одноразовый сервис `migrate`, который применяет `up` после готовности MySQL и до запуска API.

```powershell
docker compose logs migrate
# При необходимости повторно применить доступные миграции:
docker compose run --rm migrate
```

Успешно завершённый контейнер миграций может отображаться как `Exited (0)` — это нормально. Миграция `down` удаляет таблицы; выполнять её на БД с нужными данными нельзя.

## Роли и доступ

| Роль | Возможности |
| --- | --- |
| `owner` | Управление участниками и ролями, все задачи команды, история, комментарии, аналитика |
| `admin` | Приглашение участников (`member`), редактирование любых задач, история, комментарии, аналитика; не изменяет `owner` |
| `member` | Просмотр задач, истории и комментариев своей команды, создание задач; изменение всех поддерживаемых полей своей созданной задачи либо только статуса назначенной |

Исполнитель должен состоять в той же команде. Участники команды могут комментировать любую её задачу, даже если не могут её редактировать. Пользователь вне команды не получает данные её задач, истории, комментариев и аналитики.

## Эндпоинты

Все пути API начинаются с `/api/v1`. Для защищённых методов требуется заголовок `Authorization: Bearer <JWT>`.

| Метод | Путь | Назначение |
| --- | --- | --- |
| `POST` | `/register` | Регистрация |
| `POST` | `/login` | Получение JWT |
| `POST` | `/teams` | Создать команду |
| `GET` | `/teams` | Список своих команд |
| `POST` | `/teams/{id}/invite` | Пригласить зарегистрированного пользователя |
| `PUT` | `/teams/{id}/members/{user_id}/role` | Изменить роль (только `owner`) |
| `POST` | `/tasks` | Создать задачу |
| `GET` | `/tasks?team_id=1&status=todo&assignee_id=5&limit=20&offset=0` | Список задач с фильтрами |
| `PUT` | `/tasks/{id}` | Изменить задачу с проверкой версии |
| `GET` | `/tasks/{id}/history` | История изменений |
| `POST` | `/tasks/{id}/comments` | Добавить комментарий |
| `GET` | `/tasks/{id}/comments` | Получить комментарии |
| `GET` | `/teams/{team_id}/stats` | Отчёт по команде (`owner`/`admin`) |

Для `GET /tasks` обязателен `team_id`. По умолчанию `limit=20`, `offset=0`; максимальный `limit=100`. Для `PUT /tasks/{id}` обязателен `expected_version`: при несовпадении версий возвращается `409 Conflict`. Поле `assignee_id` можно передать как `null`, чтобы снять назначение. При переводе задачи в `done` заполняется `closed_at`; при повторном открытии оно очищается.

## Пример использования (PowerShell)

Команды ниже создают отдельного демонстрационного пользователя и команду. Выполняйте их последовательно в одном окне PowerShell.

```powershell
$base = 'http://localhost:8080/api/v1'
$tag = [guid]::NewGuid().ToString('N').Substring(0, 8)
$email = "owner-$tag@example.test"
$password = 'DemoPassword123!'

$user = Invoke-RestMethod -Method POST -Uri "$base/register" -ContentType 'application/json' -Body (@{
    email = $email; password = $password; name = 'Demo Owner'
} | ConvertTo-Json)

$login = Invoke-RestMethod -Method POST -Uri "$base/login" -ContentType 'application/json' -Body (@{
    email = $email; password = $password
} | ConvertTo-Json)
$auth = @{ Authorization = "Bearer $($login.access_token)" }

$team = Invoke-RestMethod -Method POST -Uri "$base/teams" -Headers $auth -ContentType 'application/json' -Body (@{
    name = 'Backend Team'
} | ConvertTo-Json)

$task = Invoke-RestMethod -Method POST -Uri "$base/tasks" -Headers $auth -ContentType 'application/json' -Body (@{
    team_id = $team.id; title = 'Implement JWT'; description = 'Authentication'; status = 'todo'
} | ConvertTo-Json)

$list = Invoke-RestMethod -Method GET -Uri "$base/tasks?team_id=$($team.id)&limit=20&offset=0" -Headers $auth

$updated = Invoke-RestMethod -Method PUT -Uri "$base/tasks/$($task.id)" -Headers $auth -ContentType 'application/json' -Body (@{
    expected_version = $task.version; status = 'done'
} | ConvertTo-Json)

$history = Invoke-RestMethod -Method GET -Uri "$base/tasks/$($task.id)/history" -Headers $auth
$comment = Invoke-RestMethod -Method POST -Uri "$base/tasks/$($task.id)/comments" -Headers $auth -ContentType 'application/json' -Body (@{
    content = 'Reviewed'
} | ConvertTo-Json)
$stats = Invoke-RestMethod -Method GET -Uri "$base/teams/$($team.id)/stats" -Headers $auth
$stats | ConvertTo-Json -Depth 8
```

## Надёжность и реализация

- MySQL / транзакции. Создание команды и добавление `owner` выполняются атомарно. Создание или обновление задачи и запись `task_history` — в одной транзакции.
- Конкурентные обновления. Строка задачи блокируется через `SELECT ... FOR UPDATE`; дополнительно проверяется `expected_version`, а SQL `UPDATE` использует условие по версии.
- Redis. Ключ включает `team_id`, `task_revision` и фильтры/пагинацию. Срок жизни — пять минут. После создания/обновления задачи ревизия увеличивается в той же транзакции. При недоступности Redis список задач читается из MySQL.
- SQL-отчёт. Один запрос с CTE, `JOIN`, `GROUP BY`, агрегатами и вычислениями по датам. Выборка ограничена `team_id` после проверки роли пользователя.
- История. Сохраняются автор, время, старые и новые значения изменённых полей; создание задачи также фиксируется в истории.

Пример ответа отчёта:

```json
{
  "team_id": 1,
  "by_status": {"todo": 3, "in_progress": 1, "done": 2},
  "top_assignees_last_30_days": [
    {"user_id": 5, "name": "Developer", "closed_count": 2}
  ],
  "average_closure_seconds": 3600,
  "comments_count": 5
}
```

## Тестирование

Модульные тесты (Go 1.25+ установлен локально):

```powershell
go test ./...
go vet ./...
```

Интеграционные тесты используют работающие контейнеры MySQL и Redis и применённые миграции. Они проверяют SQL-отчёт, конкурентное обновление, инвалидацию Redis и межкомандную изоляцию. Для Windows PowerShell настройте адреса контейнеров на порты хоста:

```powershell
# Предварительно запустите .\start.cmd
$envSettings = @{}
Get-Content .env | Where-Object { $_ -match '^[A-Z_]+=' } | ForEach-Object {
    $parts = $_ -split '=', 2
    $envSettings[$parts[0]] = $parts[1]
}
$mysqlPort = if ($envSettings['MYSQL_HOST_PORT']) { $envSettings['MYSQL_HOST_PORT'] } else { '3306' }
$redisPort = if ($envSettings['REDIS_HOST_PORT']) { $envSettings['REDIS_HOST_PORT'] } else { '6379' }
$env:TEST_MYSQL_DSN = $envSettings['MYSQL_DSN'].Replace('mysql:3306', "127.0.0.1:$mysqlPort")
$env:TEST_REDIS_ADDR = "127.0.0.1:$redisPort"

go test -tags=integration -count=1 ./...

Remove-Item Env:\TEST_MYSQL_DSN, Env:\TEST_REDIS_ADDR
```

Сквозной smoke-тест запускает основные HTTP-сценарии и создаёт демонстрационные данные в локальной базе:

```powershell
powershell -NoProfile -ExecutionPolicy Bypass -File .\scripts\smoke.ps1
```

## Структура проекта

```text
cmd/api/             точка входа HTTP-сервера
internal/auth/       JWT и тесты
internal/config/     конфигурация из переменных окружения
internal/domain/     структуры и константы доменной модели
internal/httpapi/    маршруты, HTTP-обработчики, Swagger UI
internal/service/    бизнес-логика, MySQL, Redis, отчёт, тесты
migrations/          миграции MySQL
scripts/smoke.ps1    ручной сквозной HTTP-тест
start.cmd            запуск на Windows
start.ps1            запуск и проверка готовности инфраструктуры
docs/openapi.yaml    спецификация OpenAPI
Dockerfile           многоэтапная сборка приложения
compose.yaml         Go API, MySQL, Redis, миграции
.env.example         пример конфигурации без настоящих секретов
```

## Примечания

Проект предназначен для локального запуска и демонстрации требований тестового задания. Для публичного production-развёртывания дополнительно потребуются HTTPS, централизованное хранение секретов, резервное копирование и операционный мониторинг.
