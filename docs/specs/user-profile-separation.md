# Спецификация: разделение учетной записи, профиля и корпоративной должности

Статус: ready for implementation  
Область: `VerbaTrace/Monolit` и согласованные изменения `VerbaTrace-frontend`
Тип изменения: изменение внутренней архитектуры и схемы данных с контролируемой эволюцией API  

## 1. Цель

Разделить текущую универсальную сущность `models.User` на модели с разными
назначением, жизненным циклом и правилами доступа:

- учетная запись и аутентификация;
- пользовательский профиль;
- публичное представление пользователя;
- пользовательские предпочтения;
- членство и должность пользователя в компании.

После реализации:

1. Код, который показывает пользователя, не получает `password_hash`.
2. Операции профиля не обновляют таблицу учетных данных.
3. Должность в компании принадлежит членству в этой компании.
4. Один пользователь может иметь разные должности в разных компаниях.
5. Существующие пользовательские данные мигрируются без потери и без
   недостоверного автоматического назначения должностей.
6. Backend и frontend проходят свои полные CI-проверки.

## 2. Причина изменения

Сейчас `models.User` одновременно содержит:

- `email` и `password_hash`;
- глобальную системную роль;
- имя, фамилию и `username`;
- телефон и часовой пояс;
- метаданные аватара;
- `post`;
- дату создания.

Из-за общей модели запросы профиля, контактов, аватара и `username` читают или
возвращают внутри backend также `password_hash`, хотя он нужен только
аутентификации и смене пароля.

Поле `users.post` также не имеет однозначной семантики:

- пользователь может редактировать его как часть личного профиля;
- администратор может редактировать его как часть профиля пользователя;
- интерфейс называет его должностью;
- пользователь может состоять в нескольких компаниях.

Глобальная должность пользователя не может корректно представлять должность в
каждой компании.

## 3. Термины и окончательные архитектурные решения

### 3.1. `UserAccount`

Учетная запись. Содержит только данные идентификации, аутентификации и
глобальной системной авторизации.

```go
type UserAccount struct {
    ID           uuid.UUID
    Email        string
    PasswordHash string
    Role         UserRole
    CreatedAt    time.Time
}
```

Глобальная `UserRole` (`user`, `helper`, `admin`, `superadmin`) остается у
учетной записи. Она не заменяет роли членства в компании или отделе.

### 3.2. `UserProfile`

Личный профиль пользователя.

```go
type UserProfile struct {
    UserID          uuid.UUID
    FullName        string
    FullSurname     string
    Username        string
    Headline        *string
    Phone           *string
    Timezone        *string
    AvatarPath      *string
    AvatarMimeType  *string
    AvatarSizeBytes *int64
    AvatarUpdatedAt *time.Time
    UpdatedAt       time.Time
}
```

`Headline` — личное профессиональное описание, например
«Backend-разработчик». Оно не является должностью в конкретной компании.

### 3.3. `CompanyMember.JobTitle`

Должность пользователя в конкретной компании:

```go
type CompanyMember struct {
    CompanyUUID uuid.UUID
    UserUUID    uuid.UUID
    Role        CompanyMemberRole
    Status      MembershipStatus
    JobTitle    *string
    CreatedAt   time.Time
}
```

`JobTitle`:

- принадлежит паре `(company_uuid, user_uuid)`;
- может различаться между компаниями;
- не определяет права;
- не заменяет `CompanyMemberRole`;
- редактируется только менеджером соответствующей компании;
- сохраняется при `suspended`;
- сохраняется при `left`, чтобы история членства не терялась;
- не копируется автоматически в другую компанию или отдел.

Должность не добавляется в `department_members`: в текущей предметной модели
членство в отделе является частью членства в компании. Если в будущем появится
требование иметь разные должности по отделам, это будет отдельное изменение
контракта.

### 3.4. `PublicUser`

Минимальная модель для контактов, поиска и других публичных представлений:

```go
type PublicUser struct {
    ID          uuid.UUID
    FullName    string
    FullSurname string
    Username    string
    Headline    *string
    AvatarURL   *string
}
```

Она никогда не содержит:

- email;
- password hash;
- телефон;
- часовой пояс;
- глобальную системную роль;
- внутренний путь файла аватара.

### 3.5. `CurrentUser`

Агрегат ответа для текущего авторизованного пользователя. Это не таблица:

```go
type CurrentUser struct {
    Account UserAccount
    Profile UserProfile
}
```

Внутри backend учетная запись и профиль остаются разделенными. API-конвертер
формирует разрешенный клиентский DTO.

### 3.6. `UserPreferences`

Существующая отдельная таблица и модель сохраняются. Тема, диапазон дат и
активная компания не переносятся в профиль.

## 4. Целевая схема данных

### 4.1. Таблица `users`

Название `users` сохраняется, чтобы не переписывать все внешние ключи.

После contract-миграции таблица содержит:

```sql
CREATE TABLE users (
    user_uuid UUID PRIMARY KEY,
    email TEXT NOT NULL,
    password_hash TEXT NOT NULL,
    role TEXT NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
```

Сохраняются:

- уникальный case-insensitive индекс email;
- существующие внешние ключи на `users(user_uuid)`;
- существующие ограничения глобальных ролей;
- административная логика изменения роли и инвалидации сессий.

### 4.2. Таблица `user_profiles`

```sql
CREATE TABLE user_profiles (
    user_uuid UUID PRIMARY KEY
        REFERENCES users(user_uuid) ON DELETE CASCADE,
    full_name TEXT NOT NULL,
    full_surname TEXT NOT NULL,
    username TEXT NOT NULL,
    headline TEXT NULL,
    phone TEXT NULL,
    timezone TEXT NULL,
    avatar_path TEXT NULL,
    avatar_mime_type TEXT NULL,
    avatar_size_bytes BIGINT NULL,
    avatar_updated_at TIMESTAMPTZ NULL,
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    CONSTRAINT chk_user_profiles_full_name_not_blank
        CHECK (btrim(full_name) <> ''),
    CONSTRAINT chk_user_profiles_full_surname_not_blank
        CHECK (btrim(full_surname) <> ''),
    CONSTRAINT chk_user_profiles_username_not_blank
        CHECK (btrim(username) <> ''),
    CONSTRAINT chk_user_profiles_avatar_metadata
        CHECK (
            (avatar_path IS NULL
             AND avatar_mime_type IS NULL
             AND avatar_size_bytes IS NULL
             AND avatar_updated_at IS NULL)
            OR
            (avatar_path IS NOT NULL
             AND avatar_mime_type IS NOT NULL
             AND avatar_size_bytes IS NOT NULL
             AND avatar_size_bytes > 0
             AND avatar_updated_at IS NOT NULL)
        )
);

CREATE UNIQUE INDEX idx_user_profiles_username_lower
    ON user_profiles (lower(username));
```

`timezone` дополнительно проверяется сервисом через `time.LoadLocation`.
Database CHECK со списком временных зон не добавляется, поскольку список IANA
не должен дублироваться в миграции.

### 4.3. Таблица `company_members`

Добавляется:

```sql
ALTER TABLE company_members
    ADD COLUMN job_title TEXT NULL;

ALTER TABLE company_members
    ADD CONSTRAINT chk_company_members_job_title_not_blank
    CHECK (job_title IS NULL OR btrim(job_title) <> '');
```

Максимальная длина `headline` и `job_title` задается одинаково в сервисе и DTO:
200 Unicode code points. Ограничение считается по `[]rune`, а не по байтам.

## 5. Миграционная стратегия

Изменение выполняется через expand → backfill → switch → contract. Нельзя
сразу удалить профильные колонки из `users`.

### 5.1. Миграция A: expand и backfill

Одна транзакционная goose-миграция:

1. Создает `user_profiles`.
2. Добавляет `company_members.job_title`.
3. Снимает `NOT NULL` со старых `users.full_name`,
   `users.full_surname` и `users.username`. Это необходимо, чтобы после switch
   новая регистрация могла вставить account без заполнения legacy profile
   columns. Старый backend остается совместим с migration A до момента switch,
   поскольку все уже существующие строки заполнены.
4. Копирует профильные данные:

```sql
INSERT INTO user_profiles (
    user_uuid,
    full_name,
    full_surname,
    username,
    headline,
    phone,
    timezone,
    avatar_path,
    avatar_mime_type,
    avatar_size_bytes,
    avatar_updated_at,
    updated_at
)
SELECT
    user_uuid,
    full_name,
    full_surname,
    username,
    post,
    phone,
    timezone,
    avatar_path,
    avatar_mime_type,
    avatar_size_bytes,
    avatar_updated_at,
    now()
FROM users;
```

Существующий `post` переносится в `headline`, а не в `job_title`. Это сохраняет
данные и их прежнее глобальное поведение, не назначая одну и ту же должность во
всех компаниях без подтвержденной бизнес-семантики.

`company_members.job_title` после миграции остается `NULL`. Менеджеры компаний
назначают корректные должности через новый endpoint.

5. Сверяет количество строк:

```sql
DO $$
BEGIN
    IF (SELECT count(*) FROM users) <>
       (SELECT count(*) FROM user_profiles) THEN
        RAISE EXCEPTION 'user profile backfill count mismatch';
    END IF;
END $$;
```

6. Создает новый unique username index на `user_profiles`.

Старый username index на `users` сохраняется до migration B. Он нужен старой
версии между expand и switch и не мешает новым строкам account, поскольку
PostgreSQL unique index допускает несколько `NULL`.

До contract-миграции профильные колонки в `users` остаются на месте, но новый
код после switch больше их не читает и не записывает.

### 5.2. Миграция B: contract

Применяется только после развертывания версии, работающей через
`user_profiles`.

Удаляет из `users`:

- `full_name`;
- `full_surname`;
- `username`;
- `post`;
- `phone`;
- `timezone`;
- `avatar_path`;
- `avatar_mime_type`;
- `avatar_size_bytes`;
- `avatar_updated_at`.

Только на этом шаге удаляется старый username index на `users`. Новый unique
index на `user_profiles` уже существует с migration A, поэтому периода без
ограничения уникальности нет.

Перед удалением миграция повторно проверяет:

- каждому `users.user_uuid` соответствует профиль;
- нет профиля без учетной записи;
- `email` и `username` остаются уникальными без учета регистра;
- количество account и profile совпадает.

Сравнивать значения profile со старыми колонками на этом шаге нельзя: после
switch новая версия намеренно записывает только `user_profiles`, поэтому старые
колонки являются snapshot на момент migration A и могут законно устареть.

### 5.3. Goose Down

Down для миграции A:

- удаляет `company_members.job_title`;
- удаляет новый username index;
- удаляет `user_profiles`;
- перед удалением `user_profiles` возвращает profile значения в legacy columns
  для строк, созданных после switch;
- восстанавливает `NOT NULL` на имени, фамилии и username;
- старый username index не пересоздает, поскольку migration A его не удаляет.

Down для contract-миграции:

1. возвращает профильные колонки в `users`;
2. backfill-ит их из `user_profiles`;
3. восстанавливает NOT NULL и индексы;
4. не удаляет `user_profiles`.

Это делает откат contract-шага восстанавливающим данные. Откат обеих миграций
в обратном порядке возвращает исходную схему.

### 5.4. Ограничение совместимости при rolling deployment

Backend сейчас развертывается одним экземпляром по текущему compose-контуру.
Если появится rolling deployment нескольких версий, между миграциями A и B
нужен период dual-write или запрет одновременной работы старой и новой версии.

Для текущей реализации выбирается запрет смешанных версий:

- migration A совместима со старой версией;
- затем весь backend переключается на новую версию;
- после smoke-проверки применяется migration B;
- старая версия после migration B запускаться не должна.

## 6. Модели и пакеты backend

### 6.1. Файлы моделей

Создать:

- `internal/models/user_account.go`;
- `internal/models/user_profile.go`;
- `internal/models/public_user.go`.

Перенести:

- `UserRole`, auth inputs и account-поля в `user_account.go`;
- profile inputs и avatar inputs в `user_profile.go`;
- публичные модели в `public_user.go`;
- preferences оставить в отдельном `user_preferences.go`.

Удалить `models.User` после перевода всех потребителей. Нельзя оставлять alias
`type User = CurrentUser`: он снова позволит auth-данным проникать в публичные
операции.

### 6.2. Repository models и scanner

Создать независимые:

- `repository/models/user_account.go`;
- `repository/models/user_profile.go`;
- `repository/scaner/user_account.go`;
- `repository/scaner/user_profile.go`;
- `repository/converter/user_account.go`;
- `repository/converter/user_profile.go`.

Сканер account всегда принимает только account-колонки. Сканер profile никогда
не знает о `password_hash`.

Запрещается использовать `SELECT *`.

## 7. Репозитории

### 7.1. `UserAccountRepository`

```go
type UserAccountRepository interface {
    GetByUUID(ctx context.Context, id uuid.UUID) (models.UserAccount, error)
    GetByEmail(ctx context.Context, email string) (models.UserAccount, error)
    Create(ctx context.Context, account models.UserAccount) (models.UserAccount, error)
    UpdatePasswordHash(
        ctx context.Context,
        userID uuid.UUID,
        passwordHash string,
    ) (models.UserAccount, error)
}
```

Метода `GetByUsername` здесь нет: username принадлежит профилю.

### 7.2. `UserProfileRepository`

```go
type UserProfileRepository interface {
    GetByUserUUID(ctx context.Context, userID uuid.UUID) (models.UserProfile, error)
    GetByUsername(ctx context.Context, username string) (models.UserProfile, error)
    Update(ctx context.Context, input models.UpdateUserProfileInput) (models.UserProfile, error)
    UpdateUsername(ctx context.Context, input models.UpdateUsernameInput) (models.UserProfile, error)
    UpdateAvatar(ctx context.Context, input models.UserAvatarUpdate) (models.UserProfile, error)
    DeleteAvatar(ctx context.Context, userID uuid.UUID) (models.UserProfile, error)
}
```

### 7.3. Транзакционное создание

Регистрация должна атомарно создать account и profile:

```go
type UserRegistrationRepository interface {
    CreateAccountWithProfile(
        ctx context.Context,
        account models.UserAccount,
        profile models.UserProfile,
    ) (models.CurrentUser, error)
}
```

Обе вставки выполняются в одной database transaction. Ошибка второй вставки не
может оставить account без profile.

Создание стартовой подписки сейчас выполняется отдельным репозиторием после
создания пользователя. В рамках этого изменения оно остается идемпотентным
`UpsertSubscription`, но должен быть добавлен тест повторной регистрации после
ошибки provisioning. Полная атомарность account/profile/subscription требует
общего unit-of-work или outbox и не должна имитироваться компенсационным
удалением пользователя.

### 7.4. Публичный поиск

`ContactRepository.SearchUsers` заменяется на
`SearchPublicUsers(...) ([]models.PublicUser, error)`.

SQL выбирает только:

- `user_uuid`;
- имя;
- фамилию;
- username;
- headline;
- наличие аватара, необходимое для формирования URL.

Email, телефон, timezone, role, `avatar_path` и `password_hash` не выбираются.

### 7.5. Company repository

Все запросы, которые сейчас берут имя и username из `users`, присоединяют
`user_profiles`.

Списки участников дополнительно возвращают `cm.job_title`.

Добавить:

```go
UpdateCompanyMemberJobTitle(
    ctx context.Context,
    companyID uuid.UUID,
    userID uuid.UUID,
    jobTitle *string,
) (models.CompanyMemberListItem, error)
```

Запрос обновляет только существующее членство. Отсутствующий пользователь или
членство возвращает `ErrCompanyMemberNotFound`.

### 7.6. Admin repository

Admin list/get объединяет `users` и `user_profiles`.

- изменение глобальной роли обновляет только `users`;
- изменение имени, фамилии, username, headline, телефона и timezone обновляет
  только `user_profiles`;
- обе операции сохраняют существующий audit log;
- before/after audit должен содержать только изменяемые и разрешенные поля;
- password hash не входит в admin model и audit.

Администратор системы не меняет `company_members.job_title` через глобальный
профильный endpoint. Для этого используется company-scoped endpoint с
company-manager authorization. Возможное отдельное superadmin-разрешение не
входит в эту реализацию.

## 8. Сервисный слой

### 8.1. Auth service

Auth service получает account, profile и registration repositories.

- `Login` ищет только account по email, проверяет password hash, затем
  загружает profile для ответа.
- `Refresh` загружает account для проверки сессии и profile для ответа.
- `Me` агрегирует account и profile.
- `UpdatePassword` работает только с account.
- `UpdateProfile`, username и avatar работают только с profile.
- access token продолжает содержать `user_uuid`, глобальную role и
  `access_version`; профильные поля в token не добавляются.

Если account существует, а profile отсутствует, это нарушение инварианта:

- API возвращает internal error, а не частично заполненного пользователя;
- ошибка логируется с `user_uuid`;
- login/refresh не создают профиль молча;
- repository integration test обязан доказать, что штатная регистрация такого
  состояния не создает.

### 8.2. Нормализация

- email: trim + lower-case;
- username: существующая нормализация;
- имя/фамилия: trim, пустая строка запрещена;
- headline/job title/phone: trim; пустая строка означает `NULL`;
- timezone: trim + `time.LoadLocation`;
- headline/job title: максимум 200 Unicode code points.

PATCH должен различать:

- поле отсутствует — оставить значение;
- поле равно `null` — очистить nullable поле;
- поле равно `""` — после нормализации очистить nullable поле.

Обычный `*string` не различает отсутствующее поле и явный `null`. Для nullable
PATCH-полей нужно использовать явный generic patch type:

```go
type Optional[T any] struct {
    Set   bool
    Value *T
}
```

с собственным `UnmarshalJSON`, либо эквивалентный проверенный тип. Это устраняет
текущую проблему `COALESCE`, при которой nullable profile field невозможно
надежно очистить.

### 8.3. Company service

Добавить `UpdateCompanyMemberJobTitle`:

1. валидирует UUID;
2. нормализует значение;
3. проверяет длину;
4. загружает membership вызывающего пользователя;
5. разрешает операцию только активному `company_manager`;
6. не разрешает suspended/left manager;
7. обновляет любое существующее membership целевого пользователя, включая
   suspended/left, если бизнес-правила управления участниками уже это
   допускают;
8. возвращает обновленный member DTO.

Пользователь не может самостоятельно менять company job title через
`/auth/me/profile`.

## 9. HTTP API

### 9.1. Совместимость `UserResponse`

Маршруты login/register/refresh/me могут сохранить плоский JSON, чтобы
разделение хранения не вынуждало frontend знать внутреннюю схему.

Целевой `CurrentUserResponse`:

```json
{
  "id": "uuid",
  "email": "user@example.com",
  "full_name": "Иван",
  "full_surname": "Петров",
  "username": "@ivan",
  "role": "user",
  "headline": "Backend-разработчик",
  "phone": null,
  "timezone": "Europe/Moscow",
  "avatar_url": "/api/v1/auth/me/avatar",
  "created_at": "2026-07-30T10:00:00Z"
}
```

`password_hash` и storage path никогда не сериализуются.

### 9.2. Переход `post` → `headline`

Чтобы backend и frontend можно было выпускать независимо:

**Compatibility release**

- response временно возвращает и `headline`, и deprecated `post` с одинаковым
  значением;
- register/profile/admin request принимает `headline`;
- старый `post` временно принимается как alias;
- если одновременно переданы разные `headline` и `post`, вернуть `400
  invalid_user_input`;
- OpenAPI/типам и коду пометить `post` deprecated.

**Contract release**

- после обновления frontend удалить `post` из response/request DTO;
- удалить alias parsing и связанные тесты;
- выполнить `rg`-проверку, что `post` не используется как поле пользователя.

Новый `job_title` не подставляется в `headline` и наоборот.

### 9.3. Public user response

`GET /api/v1/users/lookup` и contacts endpoints используют:

```json
{
  "id": "uuid",
  "full_name": "Иван",
  "full_surname": "Петров",
  "username": "@ivan",
  "headline": "Backend-разработчик",
  "avatar_url": "/api/v1/users/{uuid}/avatar"
}
```

Текущий `/auth/me/avatar` требует авторизованного владельца и не подходит как
URL чужого аватара. Реализация должна выбрать один из вариантов:

- добавить защищенный `GET /users/{uuid}/avatar` с проверкой видимости
  пользователя;
- либо не возвращать avatar URL в `PublicUser` до появления такого endpoint.

В рамках этой спецификации выбирается первый вариант. Чтобы lookup-состояние не
требовало отдельной server-side сессии разрешений,
`GET /users/{uuid}/avatar` использует ту же модель обнаружимости, что lookup:
любой авторизованный пользователь может получить аватар существующего
пользователя. Endpoint не раскрывает прочие поля, применяет rate limit и
возвращает одинаковый `404` для отсутствующего пользователя и отсутствующего
аватара. Если политика приватности должна быть строже, ее следует определить
до реализации публичных аватаров и применить одинаково к lookup и avatar.

### 9.4. Job title endpoints

Добавить:

```http
PATCH /api/v1/companies/{company_uuid}/members/{user_uuid}/job-title
Content-Type: application/json

{"job_title":"Руководитель отдела продаж"}
```

Очистка:

```json
{"job_title":null}
```

Ответ — полный `CompanyMemberListItemResponse` с `job_title`.

Ошибки:

- `400` — неверный UUID, JSON, пустое/слишком длинное значение;
- `401` — пользователь не аутентифицирован;
- `403` — не активный manager этой компании;
- `404` — компания или membership не найдены в разрешенном scope;
- `409` — membership изменилось конкурентно, если будет добавлена optimistic
  concurrency;
- `500` — неожиданная ошибка.

В первой реализации optimistic version не добавляется: обновление одного
nullable поля атомарно, последнее подтвержденное действие менеджера побеждает.

### 9.5. Company DTO

Добавить `job_title` в:

- `CompanyMemberResponse`;
- `CompanyMemberListItemResponse`;
- overview manager/employees;
- ответы add member, update role/status и leave, если они возвращают member.

Department member DTO получает `job_title`, вычисленный через соответствующее
company membership, но не хранит его в department membership.

### 9.6. Admin API

`AdminUserResponse.post` заменяется на `headline` через тот же compatibility
период.

Global admin profile endpoint не принимает `job_title`.

## 10. Аватары

- Бинарные файлы остаются в существующем `AvatarStorage`.
- Метаданные переезжают в `user_profiles`.
- Upload сохраняет файл, затем обновляет profile metadata.
- Если DB update не удался, новый файл удаляется best-effort, как сейчас.
- При замене аватара старый файл должен удаляться только после успешного DB
  update; ошибка удаления старого файла логируется и не отменяет успешный
  ответ.
- Delete очищает все avatar metadata атомарно.
- Profile deletion через cascade не удаляет файл автоматически; если в будущем
  будет удаление account, нужен явный cleanup/outbox. Account deletion не
  входит в текущий scope.
- Cache key продолжает использовать `avatar_updated_at`.

## 11. Конкурентность и целостность

- Уникальность email обеспечивается БД и нормализуется в сервисе.
- Уникальность username обеспечивается новым индексом profile.
- Предварительная проверка существования не заменяет обработку unique
  violation.
- Одновременная регистрация одинакового email возвращает контролируемый
  `ErrUserAlreadyExists`.
- Одновременная смена на одинаковый username возвращает контролируемый
  `ErrUserAlreadyExists`.
- Account/profile creation выполняется в одной транзакции.
- Profile update не блокирует password/role update, поскольку они находятся в
  разных строках таблиц.
- Admin audit и admin profile update выполняются в одной транзакции.
- Job title update не меняет company/department role или status.

## 12. Ошибки и наблюдаемость

Добавить/сохранить доменные ошибки:

- `ErrUserAccountNotFound`;
- `ErrUserProfileNotFound`;
- внешний API по-прежнему может отображать их как `user_not_found`;
- `ErrUserAlreadyExists`;
- `ErrCompanyMemberNotFound`;
- `ErrCompanyPermissionDenied`;
- `ErrInvalidUserInput`.

Логи не содержат:

- password hash;
- пароль;
- refresh/access tokens;
- телефон или email без необходимости диагностики.

Для нарушения account/profile invariant логируются error code и `user_uuid`.
Метрика или structured log event:

```text
user_profile_invariant_violation
```

## 13. Изменения frontend

Обновить `VerbaTrace-frontend`:

1. `UserResponse.post` → `headline`.
2. `UpdateProfileRequest.post` → `headline`.
3. `AdminUserResponse` и admin form используют `headline` и подпись
   «Профессиональное описание», а не «Должность».
4. Profile page показывает `headline`.
5. Contacts page показывает `headline`.
6. Company member types получают `job_title`.
7. В company member card/detail менеджер получает контрол редактирования
   должности.
8. Не-manager видит `job_title`, но не видит edit action.
9. Profile page не редактирует company job title.
10. Если пользователь состоит в нескольких компаниях, каждая company view
    показывает должность из своего membership.
11. Mock data и тестовые fixtures используют `headline` и `job_title` с
    правильной семантикой.
12. После contract release удалить legacy `post` из TypeScript типов.

Обновление session state после profile PATCH должно заменить разрешенные поля
текущего пользователя, не затрагивая role/email/id.

## 14. Матрица тестирования backend

### 14.1. Migration integration tests

- пустая БД проходит все migrations up;
- существующий user получает ровно один profile;
- все профильные поля совпадают после backfill;
- `post` попадает в `headline`;
- `job_title` не заполняется автоматически;
- несколько компаний пользователя не влияют на headline backfill;
- null profile fields остаются null;
- avatar metadata сохраняется;
- duplicate case-insensitive username блокирует миграцию, а не теряет строку;
- migration A down работает;
- migration B up/down восстанавливает данные;
- foreign keys продолжают ссылаться на `users`;
- удаление account каскадно удаляет profile/preferences;
- удаление profile отдельно не удаляет account.

### 14.2. Account repository

- create/get by email/get by UUID;
- email unique violation;
- update password возвращает account без profile;
- account queries выбирают только account columns;
- not found mapping.

### 14.3. Profile repository

- get by UUID/username;
- case-insensitive username lookup и uniqueness;
- update каждого поля;
- отсутствующее PATCH-поле не изменяет значение;
- null и empty очищают nullable значение;
- имя/фамилию/username нельзя очистить;
- timezone validation;
- avatar update/delete;
- profile queries не выбирают password hash.

### 14.4. Registration/auth

- регистрация создает account и profile;
- ошибка profile insert откатывает account;
- повторный email/username;
- login success/failure;
- refresh загружает profile;
- account без profile дает invariant error;
- password update не изменяет profile;
- profile update не изменяет password/role/email;
- role change продолжает инвалидировать access versions;
- auth responses не содержат `password_hash` и storage path.

### 14.5. Contacts/public user

- lookup исключает текущего пользователя;
- минимальная длина username сохраняется;
- response не содержит email/phone/timezone/role;
- contact list работает после разделения;
- public avatar access требует authentication;
- неизвестный avatar/user возвращает 404 без утечки существования закрытых
  данных;
- rate-limit behavior тестируется на handler/middleware уровне.

### 14.6. Company job title

- manager назначает, меняет и очищает job title;
- employee получает 403;
- suspended/left manager получает 403;
- member в двух компаниях имеет независимые job titles;
- изменение company role не меняет job title;
- изменение status не стирает job title;
- department views возвращают company job title;
- member без job title возвращает JSON `null`;
- Unicode length boundary 200/201;
- whitespace-only нормализуется в null;
- отсутствующая company/member;
- SQL и service error mapping;
- API DTO и frontend contract.

### 14.7. Admin

- list/get join account + profile;
- filters/search продолжают работать по email, имени, фамилии и username;
- pagination/count не дублируются из-за join;
- profile edit изменяет только profile;
- role edit изменяет только account;
- audit before/after корректен;
- concurrent username conflict;
- superadmin protections не изменены;
- admin response не содержит password hash.

### 14.8. Regression

Проверить все места, создающие users в integration fixtures:

- call repository;
- analysis/instruction/report repositories;
- billing;
- companies/departments/invitations;
- admin bootstrap;
- contacts/favorites;
- processing/monitoring.

Общий test helper должен создавать account + profile. Прямые SQL INSERT в
`users`, которым нужны profile joins, должны быть обновлены.

## 15. Матрица тестирования frontend

- login/register/refresh/me parsing;
- profile view/edit;
- headline clear/update;
- avatar upload/delete/cache refresh;
- contact lookup/list;
- admin user list/detail/edit;
- company members list/detail;
- manager job title edit;
- employee read-only state;
- multiple companies with different titles;
- loading, empty, validation, 403, 404, 409 и 500 states;
- отсутствие обращения к legacy `post` после contract release.

Build не считается достаточной проверкой. Нужен browser smoke:

1. зарегистрировать пользователя;
2. изменить profile headline;
3. добавить его в компанию;
4. manager назначает company job title;
5. profile показывает headline;
6. company member показывает job title;
7. во второй компании назначается другое значение;
8. первая компания сохраняет свое значение;
9. контакт не получает приватные поля;
10. logout/login сохраняет оба значения через соответствующие API.

## 16. Порядок реализации

### PR/коммит 1: backend expand

- migration A;
- новые модели/repositories/scanners/converters;
- transactional account + profile creation;
- auth/service switch;
- public user model;
- company/admin joins;
- `job_title` endpoint;
- compatibility `headline` + deprecated `post`;
- mocks regeneration;
- unit и integration tests.

Этот коммит не удаляет профильные колонки из `users`.

### PR/коммит 2: frontend compatibility

- читать `headline` с fallback на `post` только на время перехода;
- отправлять `headline`;
- company job title UI;
- типы и тесты;
- browser smoke.

### PR/коммит 3: contract cleanup

- migration B;
- удалить backend legacy `post`;
- удалить frontend fallback и legacy types;
- повторить полный backend/frontend CI и smoke.

Если требуется строго один атомарный commit в каждом репозитории, изменения
можно squash-ить только после проверки всех трех стадий. История локальной
разработки все равно должна сохранять возможность диагностировать expand и
contract отдельно до squash.

## 17. Генерация и mocks

После изменения repository/service/API interfaces:

- запустить mockery по существующей `.mockery.yaml`;
- проверить, что generated mocks соответствуют новым интерфейсам;
- не редактировать generated mocks вручную;
- выполнить `git diff --check`;
- проверить отсутствие неожиданных массовых изменений generated files.

Если версия mockery не зафиксирована в Taskfile/CI, перед реализацией следует
зафиксировать используемую версию или запускать ту же версию, которой были
созданы существующие mocks.

## 18. CI и критерии завершения

### 18.1. Backend local

Из `Monolit`:

```powershell
task fmt:check
task lint
task test
task vet
task test:int
task image
```

Эквивалентный агрегированный запуск:

```powershell
task verify:int
```

На Windows при ошибке доступа к общему Go cache использовать project-local
`GOCACHE` и `GOLANGCI_LINT_CACHE`. Такая ошибка сама по себе не доказывает
ошибку кода.

### 18.2. Frontend local

В текущем `VerbaTrace-frontend/package.json` определены только `dev`, `build` и
`preview`; отдельных lint/test scripts нет. Поэтому обязательны:

```powershell
npm.cmd ci
npm.cmd run build
git diff --check
```

`npm.cmd run build` включает `tsc --noEmit` и production Vite build.
Дополнительно обязателен browser smoke из раздела 15.

Нельзя указывать lint/tests как успешно пройденные, пока соответствующие scripts
и конфигурация действительно не добавлены в frontend.

На Windows использовать `npm.cmd`, если PowerShell блокирует `npm.ps1`.

### 18.3. Remote CI

В текущем backend существует `.github/workflows/backend.yml`: pull request
запускает Unit checks, а Integration checks и Docker image выполняются на
`main`. В текущем frontend отдельный GitHub Actions workflow не обнаружен.

В рамках полной реализации добавить во frontend workflow, который на pull
request и push:

1. checkout;
2. устанавливает зафиксированную подходящую Node.js версию;
3. выполняет `npm ci`;
4. выполняет `npm run build`.

Изменение завершено только когда:

- backend Unit checks зеленые;
- backend Integration checks зеленые на `main`;
- backend Docker image job зеленый;
- добавленный frontend build CI зеленый;
- migrations проверены на чистой БД и на snapshot с существующими users;
- remote commit SHA совпадает с проверенным локальным commit;
- в обоих worktree отсутствуют случайно включенные cache/upload/generated
  артефакты.

Локальная компиляция не заменяет remote integration CI с PostgreSQL.

## 19. Definition of Done

- [ ] В коде отсутствует универсальный `models.User`.
- [ ] Account и profile имеют разные models/repositories/scanners.
- [ ] Ни один public/profile/contact query не выбирает `password_hash`.
- [ ] Регистрация атомарно создает account + profile.
- [ ] `users` содержит только account-поля после contract migration.
- [ ] Каждый account имеет ровно один profile.
- [ ] `UserPreferences` остается отдельной сущностью.
- [ ] Legacy `post` без потери перенесен в `headline`.
- [ ] Company membership имеет независимый `job_title`.
- [ ] Job title изменяет только активный company manager.
- [ ] Public DTO не раскрывает приватные/account поля.
- [ ] Admin role/profile операции и audit работают после разделения.
- [ ] Все прямые SQL fixtures обновлены.
- [ ] Mocks перегенерированы.
- [ ] Backend unit/lint/fmt/vet проходят.
- [ ] Backend integration migrations проходят на PostgreSQL.
- [ ] Backend Docker image собирается.
- [ ] Frontend lint/test/build проходят.
- [ ] Browser smoke подтверждает headline и разные company job titles.
- [ ] Remote CI зеленый на фактическом commit SHA.
- [ ] После изменения выполнен `graphify update .`.

## 20. Вне scope

- выделение отдельного user/profile микросервиса;
- смена auth provider или внешний identity provider;
- удаление учетной записи и GDPR workflow;
- должность на уровне отдела;
- изменение глобальной RBAC-модели;
- перенос preferences в profile;
- полная атомарность provisioning подписки через outbox;
- публичная выдача приватного email или телефона;
- автоматическое угадывание company job title из legacy `post`.

## 21. Риски и меры

| Риск | Мера |
|---|---|
| Потеря profile данных | expand/backfill/count/value checks и восстанавливающий down |
| Старый backend после contract migration | запрет смешанных версий и порядок deployment |
| Username race | unique index + mapping unique violation |
| Account без profile | одна transaction + invariant error |
| Неверный перенос должности | legacy `post` → `headline`; `job_title` не угадывается |
| Утечка password hash через широкую модель | отдельные scanners и удаление `models.User` |
| Поломка contacts/admin/company | отдельные contract и integration tests |
| Невозможность очистить nullable PATCH | explicit Optional patch type |
| Расхождение backend/frontend | compatibility release и contract cleanup |
| Зеленый build при сломанном поведении | PostgreSQL integration + browser smoke + remote CI |
