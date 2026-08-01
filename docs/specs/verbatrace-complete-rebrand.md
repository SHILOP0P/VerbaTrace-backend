# Спецификация: полное переименование CallLens в VerbaTrace без потери Docker-данных

Статус: implemented locally; external repository/folder rename pending separate
authorization
Дата фиксации исходного состояния: 2026-07-30
Область: backend `C:\projects\CallLens\Monolit`, frontend `C:\projects\CallLens-frontend`,
корневой репозиторий, Docker Compose, PostgreSQL, uploads, CI, документация и
локальные пользовательские данные браузера
Тип изменения: сквозной rebrand и техническая миграция идентификаторов с
контролируемой ротацией локальных реквизитов PostgreSQL
Целевой бренд: `VerbaTrace`
Целевой технический slug: `verbatrace`

## 1. Цель

Полностью заменить актуальное имя продукта `CallLens` на `VerbaTrace` во всех
пользовательских и технических поверхностях проекта, не потеряв:

- ни одной строки существующей PostgreSQL-базы;
- ни одного загруженного аудио-, видео- или иного файла;
- учетные записи, компании, звонки, транскрипции, анализы, отчеты и настройки;
- миграционную историю БД;
- возможность откатить развертывание на предыдущую версию;
- пользовательские frontend-настройки, сохраненные в браузере.

Итоговый актуальный код, сборки и runtime-конфигурация не должны содержать
`CallLens`, `calllens`, `call-lens` или `call_lens`, кроме специально
зафиксированных migration-only совместимых ключей, которые удаляются после
оговоренного переходного релиза.

## 2. Что означает «абсолютно все»

В обязательную область переименования входят:

1. Пользовательское имя продукта, тексты интерфейса, email/уведомления,
   accessibility labels, metadata, title, favicon и изображения.
2. Backend-строки, prompts, тестовые данные и документация.
3. Go module path и все Go imports.
4. Имена npm-пакета, бинарников, Docker image, containers, Compose project,
   services/aliases при наличии, системного пользователя внутри image.
5. Имена логической PostgreSQL-базы, тестовой базы и PostgreSQL-роли.
6. Пароль локальной PostgreSQL-роли через `POSTGRES_PASSWORD=verbatrace`.
7. Логические имена Docker volumes в Compose.
8. Имена репозиториев и рабочих каталогов.
9. CI/CD, Taskfile, scripts, mockery, lint-конфигурация и примеры env.
10. Browser storage keys, BroadcastChannel/event names и demo-адреса.
11. README, активные спецификации и другие актуальные документы проекта.
12. Проверка имен файлов и каталогов, а не только их содержимого.

### 2.1. Граница требования

Полное удаление старого имени относится к текущим рабочим деревьям, создаваемым
артефактам и новому runtime. Старое имя неизбежно останется во внешних местах,
которые не принадлежат текущему дереву: Git history, старые commit messages,
старые releases/images, CI logs, чужие ссылки, поисковые кэши и исторический
конкурентный отчет.

Переписывание Git history не входит в обычную реализацию этой спеки: это
деструктивная отдельная операция, меняющая commit SHA и требующая
принудительной публикации всех веток и тегов. Если потребуется удалить старое
имя и из истории, для этого должна быть создана отдельная спецификация и дано
отдельное явное разрешение.

## 3. Зафиксированное исходное состояние

На 2026-07-30 подтверждено:

- Compose project фактически имеет имя `monolit`;
- постоянный PostgreSQL volume:
  `monolit_postgres_call_data`;
- постоянный uploads volume:
  `monolit_call_uploads`;
- оба volume существуют в Docker;
- `monolit_postgres_call_data` создан `2026-06-11T15:12:39Z`;
- `monolit_call_uploads` создан `2026-06-24T08:55:28Z`;
- после запуска пользователем контейнеры `call-db` и `call-api` подтверждены
  healthy;
- `call-db` действительно монтирует `monolit_postgres_call_data` в
  `/var/lib/postgresql/data`;
- `call-api` действительно монтирует `monolit_call_uploads` в `/app/uploads`;
- Compose объявляет логические volumes `postgres_call_data` и `call_uploads`;
- фактическая основная БД: `calllens`, фактическая роль: `calllens_user`;
- PostgreSQL: `16.13`, основная БД занимает около `11 MB`;
- роль `calllens_user` имеет `SUPERUSER`, `CREATEDB` и `LOGIN`;
- кроме основной БД существуют `postgres` и 16 integration-test БД с
  префиксом `calllens_test_`;
- в основной БД 35 таблиц схемы `public`;
- контрольный baseline основной БД: 7 пользователей, 1 компания, 20 звонков,
  20 транскрипций, 20 анализов, 0 call report exports, 3 агрегированных
  анализа и 57 processing jobs;
- последняя примененная goose migration: `202607300002`;
- в uploads подтверждено 26 файлов общим объемом около `39 MB`;
- `POSTGRES_PASSWORD` передается через env;
- backend module path: `calllens/monolit`;
- в backend/root найдено 536 файлов со старым именем, включая Go imports;
- во frontend найдено 17 файлов со старым именем;
- в корневом worktree уже существуют посторонние untracked-каталоги
  `.gocache/` и `.golangci-cache/`; они не относятся к rebrand и не должны
  удаляться или коммититься автоматически.

Числа файлов — снимок, а не окончательный whitelist. Реализация обязана
выполнить повторный поиск перед изменениями и финальный поиск после них.

## 4. Главный инвариант сохранности данных

Новый Compose сначала обязан подключиться к существующим физическим volumes,
а не создать пустые volumes с новым project prefix.

В первой безопасной версии Compose логические имена переименовываются, но
physical volume names остаются прежними:

```yaml
services:
  api:
    volumes:
      - verbatrace_uploads:/app/uploads

  db:
    volumes:
      - verbatrace_postgres_data:/var/lib/postgresql/data

volumes:
  verbatrace_postgres_data:
    external: true
    name: monolit_postgres_call_data
  verbatrace_uploads:
    external: true
    name: monolit_call_uploads
```

Это намеренное временное исключение: старые physical names остаются только в
Compose migration mapping, чтобы не потерять данные. Оно не видно пользователю
и не создает новый volume.

После подтвержденной миграции можно сделать отдельную физическую копию в
`verbatrace_postgres_data` и `verbatrace_uploads`, проверить контрольные суммы,
переключить Compose и только затем архивировать старые volumes. Удаление старых
volumes не входит в эту спеку.

Запрещено:

- `docker compose down -v`;
- `docker volume rm`;
- `docker system prune --volumes`;
- изменение Compose project name и немедленный `up` без `external.name`;
- ручное копирование PostgreSQL data directory между несовместимыми версиями;
- удаление старых volumes после одного успешного запуска;
- запуск пустой новой БД и импорт «поверх» нее без утвержденного recovery-плана.

## 5. Важное правило PostgreSQL password

Для уже инициализированного PostgreSQL volume изменение строки
`POSTGRES_PASSWORD` в `.env` **не меняет** пароль существующей роли. Переменные
`POSTGRES_DB`, `POSTGRES_USER` и `POSTGRES_PASSWORD` postgres image применяются
при первичной инициализации пустого data directory.

Поэтому требование `POSTGRES_PASSWORD=verbatrace` выполняется двумя
согласованными действиями:

1. SQL-миграция существующей роли внутри текущей БД.
2. Одновременное изменение `.env`, `.env.example`, Compose defaults, CI и
   runtime-конфигурации.

Для локальной среды целевые значения:

```dotenv
POSTGRES_DB=verbatrace
POSTGRES_TEST_DB=verbatrace_test
POSTGRES_USER=verbatrace_user
POSTGRES_PASSWORD=verbatrace
```

`verbatrace` является слабым предсказуемым паролем. Он допустим только для
локальной разработки по прямому требованию этой спеки. В production/staging
должен использоваться отдельный случайный secret; бренд не должен быть
production-паролем.

`PASSWORD_PEPPER`, `JWT_SECRET` и `REFRESH_TOKEN_SECRET` не должны получать
значение `verbatrace`: это не реквизиты БД. Их необоснованная ротация может
сломать проверку старых паролей или завершить все пользовательские сессии.
Если их текущие значения содержат старый бренд, нужна отдельная совместимая
ротация секретов, но нельзя заменять их на публичное слово `verbatrace`.

## 6. Целевая карта имен

| Поверхность | Было | Должно стать |
|---|---|---|
| Product display name | `CallLens` | `VerbaTrace` |
| Product slug | `calllens` | `verbatrace` |
| Go module | `calllens/monolit` | `verbatrace/monolit` |
| Backend binary | `calllens`, `calllens.exe` | `verbatrace`, `verbatrace.exe` |
| Docker image | `calllens-backend` | `verbatrace-backend` |
| API container | `call-api` | `verbatrace-api` |
| DB container | `call-db` | `verbatrace-db` |
| Image OS user/group | `calllens` | `verbatrace` |
| PostgreSQL DB | `calllens` | `verbatrace` |
| Test DB prefix | `calllens_test_*` | `verbatrace_test_*` |
| PostgreSQL role | `calllens_user` | `verbatrace_user` |
| Local DB password | текущее значение | `verbatrace` через env + SQL |
| Compose logical DB volume | `postgres_call_data` | `verbatrace_postgres_data` |
| Compose logical upload volume | `call_uploads` | `verbatrace_uploads` |
| Physical DB volume, phase 1 | `monolit_postgres_call_data` | временно тот же volume |
| Physical upload volume, phase 1 | `monolit_call_uploads` | временно тот же volume |
| npm package | `calllens-frontend` | `verbatrace-frontend` |
| Demo email domain | `calllens.local` | `verbatrace.local` |
| Repository/root folder | `CallLens` | `VerbaTrace` |
| Frontend folder | `CallLens-frontend` | `VerbaTrace-frontend` |
| GitHub repositories | `CallLens`, `CallLens-frontend` | `VerbaTrace`, `VerbaTrace-frontend` |

Целевой Go module `verbatrace/monolit` сохраняет текущую локальную схему
module path. Переход на канонический URL вида
`github.com/<owner>/verbatrace` — отдельное архитектурное решение; его нельзя
угадывать без подтверждения будущего владельца/организации репозитория.

## 7. Полный инвентарь изменений

### 7.1. Backend и корневой репозиторий

Обязательные точки:

- `README.md`: заголовки, описания, примеры, пути, компании, приглашения;
- `Monolit/go.mod`: module path;
- все `.go`: imports, prompts, тексты уведомлений, fixtures и assertions;
- `Monolit/.mockery.yaml`: package paths;
- `Monolit/.golangci.yml`: local-prefix/module paths;
- `Monolit/Taskfile.yml`: binary/image names и сообщения;
- `Monolit/deploy/Dockerfile`: output binary, user/group, ownership и CMD;
- `Monolit/deploy/docker-compose.yaml`: containers, defaults, volumes;
- `Monolit/.env.example`: DB names/user/password;
- `Monolit/.env`: локальные значения, без публикации секретов;
- `.github/workflows/backend.yml`: service env и тестовые БД;
- `Monolit/scripts/*.ps1`: Compose project/migration commands;
- migrations, SQL fixtures и integration tests;
- актуальные документы в `docs/`;
- имена файлов/каталогов, содержащие варианты старого бренда.

Замена Go module path выполняется механически во всем backend одним атомарным
изменением, затем запускаются `go mod tidy`, mock generation при необходимости,
format/lint/tests. Смешанное состояние двух module paths не допускается.

### 7.2. Frontend

Обязательные точки:

- `package.json` и lockfile;
- `index.html`: title, description, favicon;
- logo/mark assets и их filenames;
- landing/auth/application shell;
- product strings и aria-labels;
- demo email/fixtures;
- CSS asset URLs;
- custom DOM event names;
- `localStorage`/`sessionStorage` keys;
- BroadcastChannel names;
- frontend README/env/deployment metadata;
- Git remote/repository/folder — только на финальной внешней фазе.

Подтвержденные текущие ключи, требующие совместимой миграции:

- `calllens.theme.v1`;
- `calllens-upload-mode`;
- `calllens.activeWorkspaceCompanyId`;
- `calllens.auth.refresh`;
- `calllens:session-expired`;
- `calllens:admin-alert`.

Новые ключи:

- `verbatrace.theme.v1`;
- `verbatrace-upload-mode`;
- `verbatrace.activeWorkspaceCompanyId`;
- `verbatrace.auth.refresh`;
- `verbatrace:session-expired`;
- `verbatrace:admin-alert`.

На один переходный релиз frontend должен:

1. Читать новый ключ.
2. Если нового ключа нет — прочитать старый.
3. Валидировать значение.
4. Записать его в новый ключ.
5. Удалить старый ключ только после успешной записи.

Для event/BroadcastChannel нужен короткий dual-listen/dual-notify слой в одном
релизе, если одновременно могут быть открыты вкладки старой и новой версии.
Migration-only литералы старого имени должны находиться в одном явно
обозначенном модуле и иметь дату удаления.

Нельзя терять или сбрасывать тему, режим загрузки, выбранную компанию и
координацию refresh между вкладками.

### 7.3. Пользовательские тексты и генеративные prompts

Заменить бренд в:

- приглашениях;
- email subject/body;
- analyzer system prompts;
- deep analytics prompts;
- validation/error messages;
- mock responses и snapshots;
- OpenAPI/API examples;
- seed/demo data.

Смысл prompts менять нельзя. Rebrand не является разрешением переработать
критерии анализа или формат сохраненных результатов.

### 7.4. Assets

- создать/переименовать `verbatrace-mark.svg`;
- переименовать background assets с `calllens-*` на `verbatrace-*`;
- обновить все imports/URLs;
- обновить favicon и social preview metadata;
- проверить, нет ли старого слова внутри SVG metadata/text paths;
- проверить generated bundles и source maps.

Новый визуальный знак в эту спеку не входит: если дизайн логотипа не утвержден,
используется текущая графика с новым текстовым брендом и новыми filenames.

### 7.5. Репозитории и каталоги

Переименование каталогов и GitHub repositories выполняется последним, после
успешной проверки кода и Docker-миграции:

1. backend/root repository;
2. frontend repository;
3. local folders;
4. git remotes;
5. CI references, badges и ссылки;
6. IDE/workspace/tasks.

Это внешние изменения и требуют отдельной команды пользователя. До этой фазы
код может быть полностью VerbaTrace, оставаясь физически в старом каталоге.

## 8. Безопасная миграция PostgreSQL

### 8.1. Preflight

До любого изменения:

1. Убедиться, что volumes существуют:
   `monolit_postgres_call_data`, `monolit_call_uploads`.
2. Записать результаты `docker volume inspect` в локальный migration log.
3. Поднять только старую БД на старом volume, без API/worker.
4. Проверить PostgreSQL version и `pg_isready`.
5. Зафиксировать:
   - список БД и ролей;
   - размеры БД;
   - количество пользовательских таблиц;
   - row counts для ключевых таблиц;
   - примененные migrations;
   - количество и размер файлов uploads.
6. Убедиться, что есть свободное место минимум для двух резервных копий.

Если текущие реальные имена БД/роли отличаются от ожидаемых, миграция
останавливается; SQL нельзя выполнять по предположению.

### 8.2. Резервные копии

Создать до rebrand:

- `pg_dump` текущей основной БД в custom format;
- `pg_dumpall --globals-only` для ролей/grants;
- файловый архив `monolit_call_uploads`;
- при возможности cold archive обоих Docker volumes после остановки БД;
- SHA-256 каждого backup-файла;
- текстовый manifest с датой, PostgreSQL version, volume names и checksums.

Backup считается готовым только после тестового чтения:

- `pg_restore --list` успешно читает DB dump;
- архив uploads открывается и список файлов доступен;
- checksums повторно совпадают.

### 8.3. Окно миграции

1. Остановить API и worker, чтобы не было новых записей.
2. Оставить запущенной только БД на старом physical volume.
3. Завершить активные подключения к `calllens`, кроме migration session.
4. В одной контролируемой административной сессии:
   - переименовать роль `calllens_user` в `verbatrace_user`;
   - установить роли пароль из безопасно переданной переменной psql;
   - переименовать БД `calllens` в `verbatrace`;
   - выставить владельца БД/схем/объектов `verbatrace`;
   - пакетные БД `calllens_test_*` не переименовывать по одной без
     необходимости: после подтверждения их тестового назначения пересоздать
     их с префиксом `verbatrace_test_*`; до успешного CI старые тестовые БД не
     удалять;
   - проверить grants/default privileges/extensions.
5. Изменить env на целевые значения.
6. Запустить новый `verbatrace-api` с существующими physical volumes.
7. Проверить migrations и readiness.

SQL должен быть оформлен отдельным idempotent migration script с:

- проверками существования старой и новой роли/БД;
- немедленной остановкой при неоднозначном состоянии;
- `ON_ERROR_STOP=1`;
- отсутствием пароля в Git, process arguments и console log;
- post-checks владельцев и подключений.

Пароль `verbatrace` хранится в локальном `.env`, который не коммитится.
В `.env.example` допускается dev-default по прямому требованию, но CI должен
использовать secret/env и не печатать значение.

### 8.4. Проверка данных после миграции

Сравнить pre/post:

- UUID и row counts ключевых таблиц;
- число пользователей, компаний, звонков, транскрипций, analyses/reports;
- migration version;
- размеры БД с объяснимым допуском;
- список uploads, суммарный размер и выборочные SHA-256;
- выборочно открыть существующую запись и получить ее файл через API;
- выполнить login существующего пользователя;
- открыть существующий анализ/отчет во frontend;
- создать новый тестовый звонок и убедиться, что он сохраняется в той же БД;
- перезапустить stack и повторить чтение старых данных.

Один успешный `/health/ready` не подтверждает сохранность данных.

### 8.5. Rollback

До точки записи новых production-like данных новой версией:

1. Остановить новый API/worker.
2. Вернуть старый env.
3. Обратным SQL переименовать БД/роль и восстановить пароль либо восстановить
   backup в отдельный recovery volume.
4. Поднять старый stack на исходных physical volumes.
5. Повторить data checks.

После появления новых записей rollback через простое переименование опасен:
нужно либо откатывать только application code, оставляя новые DB credentials,
либо согласованно переносить новые данные. Поэтому старая версия backend должна
уметь временно получать новые реквизиты только через env.

Исходные volumes не удаляются минимум до отдельного подтверждения после
нескольких успешных restart/backup циклов.

## 9. Физическое переименование Docker volumes

Docker не поддерживает атомарное rename volume. Если требуется, чтобы старое
имя исчезло даже из `docker volume ls`, выполняется отдельная copy-and-verify
фаза:

1. БД полностью остановлена.
2. Созданы новые volumes:
   - `verbatrace_postgres_data`;
   - `verbatrace_uploads`.
3. Данные копируются побайтово через одноразовый trusted helper container.
4. Для PostgreSQL проверяются owner/mode/symlinks.
5. Для обоих деревьев сравниваются manifest и checksums.
6. Новый PostgreSQL запускается только на копии.
7. Выполняются все post-checks из раздела 8.4 и restart test.
8. Compose переключается с `external.name: monolit_*` на `external.name:
   verbatrace_*`.
9. Старые volumes помечаются как rollback-only, но не удаляются.

Предпочтительный и более переносимый вариант — восстановить проверенный
`pg_dump` в новый чистый PostgreSQL volume, а не копировать внутренний PGDATA.
Побайтовая копия допустима только при той же major/minor image и корректной
остановке сервера.

## 10. Порядок реализации

### Этап A. Инвентаризация и migration tooling

- повторный поиск всех вариантов имени в обоих repos;
- inventory filenames, env keys, Docker objects и browser keys;
- backup/restore scripts;
- idempotent PostgreSQL rename script;
- pre/post data verification script;
- dry run на копии volumes.

### Этап B. Кодовый rebrand без внешнего rename

- backend module/imports/texts;
- frontend package/UI/assets;
- configs/CI/Taskfile/Dockerfile;
- browser compatibility migration;
- README/docs;
- локальные env defaults.

### Этап C. Миграция существующих данных

- backup + restore rehearsal;
- maintenance window;
- SQL role/DB rename + password rotation;
- новый code/runtime на старых physical volumes;
- полный post-check и restart.

### Этап D. Физические Docker names

- копия/restore в `verbatrace_*` volumes;
- проверка;
- переключение;
- сохранение старых volumes для rollback.

### Этап E. Внешние имена

- GitHub repositories;
- local folders;
- remotes;
- CI/deployment references;
- публичные ссылки, когда домены будут отдельно выбраны.

### Этап F. Удаление compatibility слоя

Не раньше следующего подтвержденного релиза:

- удалить fallback на старые browser keys/events;
- удалить migration-only literals;
- выполнить финальный zero-match audit.

## 11. Проверки

### 11.1. Поиск остатков старого имени

В обоих текущих рабочих деревьях:

```powershell
rg -n -i --hidden `
  --glob '!.git/**' `
  --glob '!node_modules/**' `
  --glob '!dist/**' `
  --glob '!graphify-out/**' `
  --glob '!.gocache/**' `
  --glob '!.golangci-cache/**' `
  'calllens|call-lens|call_lens'
```

До удаления compatibility слоя разрешены только строки в единственном
migration-модуле и временные `external.name` mappings. Каждое исключение
перечисляется вручную в migration checklist. После финальной фазы ожидается
ноль совпадений в актуальном коде; historical Git не проверяется этим условием.

Также выполнить поиск по именам:

```powershell
Get-ChildItem -Recurse -Force |
  Where-Object {
    $_.FullName -notmatch '\\.git|node_modules|graphify-out|\\.gocache|\\.golangci-cache' -and
    $_.Name -match '(?i)calllens|call-lens|call_lens'
  }
```

### 11.2. Backend

- `go mod tidy`;
- format check;
- lint;
- unit tests;
- integration tests на `verbatrace_test`;
- vet;
- local build с именем `verbatrace.exe`;
- Docker image build;
- API route/contract smoke;
- migrations на копии существующей БД;
- clean GitHub Actions result.

### 11.3. Frontend

- dependency install без неожиданных lockfile upgrades;
- lint, если script существует;
- build;
- browser smoke для landing/auth/app shell;
- проверка title/favicon/logo;
- миграция всех сохраненных browser keys;
- refresh coordination между двумя вкладками;
- открытие существующего звонка, файла, транскрипции и анализа;
- отсутствие запросов/asset URLs со старым именем.

### 11.4. Docker и данные

- Compose resolved config указывает на ожидаемые physical volumes;
- `docker inspect` подтверждает mounts до первого запуска API;
- `down` не удаляет volumes;
- два последовательных restart сохраняют данные;
- backup восстановлен в отдельный temporary recovery stack;
- row/file manifests совпадают;
- старые volumes остаются доступны для rollback.

## 12. Definition of Done

Работа завершена, только если одновременно выполнено:

1. Весь актуальный UI и пользовательские сообщения используют `VerbaTrace`.
2. Backend module/imports, frontend package и build artifacts переименованы.
3. CI, Docker, Taskfile и env используют целевые технические имена.
4. Существующая БД доступна как `verbatrace`, роль — `verbatrace_user`.
5. Локальный env содержит `POSTGRES_PASSWORD=verbatrace`, а реальный пароль
   существующей PostgreSQL-роли также изменен SQL-командой.
6. Все старые DB rows и uploads подтверждены pre/post manifest.
7. Restore из backup реально проверен, а не только создан.
8. Существующие пользователи входят; старые звонки, файлы и анализы открываются.
9. Browser settings мигрированы без сброса.
10. Backend и frontend проходят полные проверки, включая Docker/integration и
    видимый browser smoke.
11. `graphify update .` выполнен после кодовых изменений.
12. Финальный поиск не находит старый бренд вне явно перечисленного временного
    migration compatibility слоя.
13. Старые Docker volumes не удалены.
14. В Git не попали `.env`, dumps, archives, пароли или другие secrets.
15. Переименование каталогов/repositories выполнено только после отдельного
    разрешения пользователя.

## 13. Операционный чек-лист исполнителя

- [ ] Повторно зафиксирован `git status` обоих repos; чужие изменения сохранены.
- [ ] Зафиксированы реальные Docker volume names и mounts.
- [ ] Проверены фактические имена текущей БД и роли.
- [ ] Созданы и проверены DB/globals/uploads backups.
- [ ] Выполнен restore rehearsal в отдельном recovery stack.
- [ ] Rebrand реализован атомарно в backend.
- [ ] Rebrand и compatibility migration реализованы во frontend.
- [ ] Обновлены Dockerfile, Compose, env examples, Taskfile и CI.
- [ ] SQL rename/password rotation прошли на копии.
- [ ] API/worker остановлены на время реальной DB-миграции.
- [ ] Реальная DB-миграция прошла без ошибок.
- [ ] Pre/post row counts, UUID samples и upload checksums совпадают.
- [ ] Старый и новый пользовательский сценарий проверены в браузере.
- [ ] Выполнены backend/frontend CI и Docker restart checks.
- [ ] Выполнен `graphify update .`.
- [ ] Проведен content- и filename-аудит старого бренда.
- [ ] Старые volumes сохранены как rollback assets.
- [ ] Dumps/env/archives отсутствуют в Git.
- [ ] External repo/folder rename выполнен только по отдельной команде.

## 14. Решения, которые эта спека намеренно не принимает

- покупка домена, резервирование social handles и trademark clearance;
- новый дизайн логотипа;
- production-пароль `verbatrace` — он запрещен как небезопасный;
- ротация password pepper/JWT/refresh secrets без отдельной auth-миграции;
- удаление старых Docker volumes;
- переписывание Git history;
- удаление или переписывание исторического конкурентного отчета;
- изменение API-контрактов, бизнес-логики анализа или схемы критериев под видом
  rebrand.

Эти действия требуют отдельных решений и не должны незаметно попадать в
реализацию переименования.

## 15. Результат реализации 2026-07-30

Выполнено:

- backend module и imports переведены на `verbatrace/monolit`;
- frontend, package metadata, UI, assets и пользовательские тексты
  переименованы в `VerbaTrace`;
- добавлен однорелизный compatibility-слой browser storage/events;
- Docker containers/image/binary/OS user переименованы;
- основная БД переименована в `verbatrace`;
- роль переименована в `verbatrace_user`, локальный DB password изменен на
  `verbatrace`;
- неизмененные auth-секреты переведены в base64 env-представление с
  декодированием при загрузке, поэтому password hashes и активные сессии не
  инвалидированы;
- существующие physical volumes скопированы в `verbatrace_postgres_data` и
  `verbatrace_uploads`;
- SHA-256 manifests исходных и новых volume-копий совпали;
- старые `monolit_*` volumes сохранены для rollback;
- удалены только старые одноразовые `calllens_test_*` БД после успешного
  integration-набора;
- backend unit/integration, format, lint, vet, binary/image build прошли;
- frontend production build и browser smoke прошли;
- graphify обновлен и отвечает по новому бренду.

Подтвержденный post-migration baseline основной БД:

- 7 пользователей;
- 1 компания;
- 20 звонков;
- 20 транскрипций;
- 20 анализов;
- 3 агрегированных анализа;
- 57 processing jobs;
- goose version `202607300002`;
- 26 upload-файлов, около `39 MB`.

Не выполнено намеренно:

- GitHub repositories, git remotes и локальные корневые каталоги не
  переименованы: раздел 7.5 требует отдельной внешней команды;
- Git history не переписывалась;
- migration-only frontend literals старого browser namespace остаются на один
  переходный релиз, чтобы не потерять локальные настройки и межвкладочную
  координацию;
- исторические названия одноименных сторонних конкурентов не переписывались.
