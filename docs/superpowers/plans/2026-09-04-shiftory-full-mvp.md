# Shiftory Full MVP Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Build the complete Shiftory scheduling collaboration MVP described by `docs/需求.md`, including authentication, multi-workspace authorization, shifts, schedules, team calendar, Excel/XLS and image-AI imports, review/conflict/rollback, audit history, themes, and all shared member/admin pages.

**Architecture:** Keep one repository with a Vue SPA and a Go modular monolith. The Go application exposes `/api/v1`, uses MySQL 8.4 for durable state and database-backed jobs, and runs API and worker entrypoints from the same domain modules. All workspace routes carry an explicit workspace ID; access JWTs contain identity only, while current workspace membership is checked against MySQL.

**Tech Stack:** Go 1.27.1, Gin, database/sql, go-sql-driver/mysql, MySQL 8.4, Goose, Excelize, extrame/xls, tRPC-Agent-Go, golang-jwt/jwt/v5, Vue 3.5, TypeScript 6, Vite 8, Pinia, Vue Router, TanStack Vue Query, Element Plus, SCSS, Vitest.

---

Git commit steps are intentionally omitted because the user authorized repository initialization but has not authorized staging or commits.

### Task 1: Normalize the repository and freeze operational configuration

**Files:**
- Move contents: `shiftory-web/shiftory-web/*` to `shiftory-web/*`
- Create: `.gitignore`
- Create: `.env.example`
- Create: `compose.yaml`
- Modify: `docs/需求.md`
- Modify: `.agent/HANDOFF.md`

- [ ] **Step 1: Verify that the nested frontend has no nested Git metadata and move its contents to the existing `shiftory-web` root.**

Run a literal-path PowerShell move after resolving both source and destination under `E:\GoCode\shiftory\shiftory-web`. Preserve every scaffold file and remove only the now-empty inner directory.

- [ ] **Step 2: Add repository ignores and configuration.**

`.gitignore` must contain:

```gitignore
.env
.env.*
!.env.example
.idea/
.vscode/
node_modules/
dist/
coverage/
*.log
shiftory-server/bin/
shiftory-server/uploads/
shiftory-server/.cache/
```

`.env.example` must define non-secret defaults and explicit local overrides:

```dotenv
SHIFTORY_HTTP_ADDR=:8080
SHIFTORY_DATABASE_DSN=root:123456@tcp(127.0.0.1:3306)/shiftory?charset=utf8mb4&parseTime=true&loc=UTC
SHIFTORY_UPLOAD_DIR=./uploads
SHIFTORY_PUBLIC_ORIGIN=http://localhost:5173
SHIFTORY_JWT_ISSUER=shiftory-local
SHIFTORY_JWT_AUDIENCE=shiftory-web
SHIFTORY_JWT_PRIVATE_KEY_FILE=./var/jwt-private.pem
SHIFTORY_JWT_PUBLIC_KEY_FILE=./var/jwt-public.pem
SHIFTORY_AI_MODEL=
SHIFTORY_AI_BASE_URL=
SHIFTORY_AI_API_KEY=
```

- [ ] **Step 3: Update the requirement document with final decisions.**

Keep the existing member schedule visibility. Set MySQL to 8.4 LTS, workspace timezone to an IANA name, historical membership intervals, shift snapshots, JWT/Pinia boundaries, API workspace scoping, job lease/retry/idempotency rules, full-import atomic rollback, field-level AI issues, file limits, and the final repository paths.

### Task 2: Establish backend domain rules with failing tests

**Files:**
- Create: `shiftory-server/internal/schedule/domain.go`
- Create: `shiftory-server/internal/schedule/domain_test.go`
- Create: `shiftory-server/internal/calendar/service.go`
- Create: `shiftory-server/internal/calendar/service_test.go`
- Create: `shiftory-server/internal/importjob/domain.go`
- Create: `shiftory-server/internal/importjob/domain_test.go`

- [ ] **Step 1: Write schedule validation tests and run them red.**

The tests instantiate the wished-for API below and assert that REST rejects segments, WORKING accepts no segment, time ranges require both endpoints, cross-day is explicit, segments cannot overlap, and a shift segment preserves its snapshot:

```go
day := schedule.Day{
    WorkDate: schedule.MustDate("2026-09-04"),
    Status: schedule.StatusWorking,
    Segments: []schedule.Segment{{
        Type: schedule.SegmentTimeRange,
        StartTime: schedule.MustClock("08:30"),
        EndTime: schedule.MustClock("17:30"),
    }},
}
require.NoError(t, day.Validate())
```

Run: `go test ./internal/schedule -run TestDay -count=1`
Expected: FAIL because the package implementation does not exist.

- [ ] **Step 2: Implement schedule value objects and invariants, then run green.**

Define `Date`, `Clock`, `Status`, `SegmentType`, `Day`, and `Segment`; parsing must reject non-canonical values and `Validate` must return exported sentinel errors usable by handlers and importers.

- [ ] **Step 3: Write team calendar aggregation tests and run them red.**

Use three selected members and assert working/rest/missing counts, zero-member behavior, all-rest only when every selected member has explicit REST, and cross-day segments remain owned by their `work_date`.

Run: `go test ./internal/calendar -run TestAggregate -count=1`
Expected: FAIL because `calendar.Aggregate` does not exist.

- [ ] **Step 4: Implement pure aggregation and run green.**

Expose:

```go
func Aggregate(dates []schedule.Date, memberIDs []uint64, days []schedule.Day) []DaySummary
```

Each `DaySummary` contains `Date`, `Working`, `Rest`, `Missing`, `AllRest`, and per-member details.

- [ ] **Step 5: Write import state transition tests and run them red.**

Assert the permitted transitions `UPLOADED -> PENDING -> PARSING -> NEEDS_REVIEW -> COMMITTING -> COMPLETED -> ROLLED_BACK`, failure/cancel paths, and rejection of transitions from terminal states.

- [ ] **Step 6: Implement import job state rules and run all domain tests green.**

Run: `go test ./internal/schedule ./internal/calendar ./internal/importjob -count=1`
Expected: PASS.

### Task 3: Create the MySQL schema and persistence modules

**Files:**
- Create: `shiftory-server/migrations/00001_initial.sql`
- Create: `shiftory-server/internal/platform/config/config.go`
- Create: `shiftory-server/internal/platform/database/mysql.go`
- Create: `shiftory-server/internal/platform/database/migrate.go`
- Create: `shiftory-server/internal/platform/database/mysql_integration_test.go`
- Modify: `shiftory-server/go.mod`

- [ ] **Step 1: Write an integration test that creates an isolated `shiftory_test` schema and runs migrations.**

The test must skip only when `SHIFTORY_TEST_DATABASE_DSN` is absent. With the supplied local database it must verify all expected tables and the unique `(workspace_id,user_id,work_date)` index.

Run with `SHIFTORY_TEST_DATABASE_DSN=root:123456@tcp(127.0.0.1:3306)/shiftory_test?charset=utf8mb4&parseTime=true&loc=UTC` and expect the first run to fail because migrations do not exist.

- [ ] **Step 2: Implement configuration loading, database connection, and embedded Goose migrations.**

The schema must include `users`, `workspaces`, `workspace_members`, `workspace_invitations`, `shifts`, `shift_aliases`, `schedule_days`, `schedule_segments`, `schedule_revisions`, `import_jobs`, `import_items`, `import_files`, `auth_refresh_tokens`, `audit_logs`, and `user_preferences`, with foreign keys and query indexes.

- [ ] **Step 3: Run migration tests green.**

Run the integration test twice; the second run must prove migration idempotence.

### Task 4: Implement JWT authentication and current-user APIs test-first

**Files:**
- Create: `shiftory-server/internal/auth/model.go`
- Create: `shiftory-server/internal/auth/password.go`
- Create: `shiftory-server/internal/auth/token.go`
- Create: `shiftory-server/internal/auth/service.go`
- Create: `shiftory-server/internal/auth/repository.go`
- Create: `shiftory-server/internal/auth/handler.go`
- Create: `shiftory-server/internal/auth/service_test.go`
- Create: `shiftory-server/internal/auth/token_test.go`

- [ ] **Step 1: Write red tests for registration, login, token validation, refresh rotation, replay rejection, logout, disabled users, and `/me`.**

Tests use an in-memory repository implementation of the production interfaces. Token tests generate an Ed25519 keypair in test code and require issuer, audience, type, expiry, subject, and valid-method checks.

- [ ] **Step 2: Implement Argon2id password hashing and EdDSA JWT services.**

Use `github.com/golang-jwt/jwt/v5`. Access claims contain `sub`, `jti`, `iss`, `aud`, `iat`, `nbf`, `exp`, and `typ=access`; refresh claims add `family_id` and `typ=refresh`. Hash refresh token bytes with SHA-256 before persistence.

- [ ] **Step 3: Implement auth handlers and middleware, then run green.**

Routes: `/api/v1/auth/register`, `/login`, `/refresh`, `/logout`, `/me`, `/profile`, and `/password`. Refresh/logout enforce allowed Origin and use the restricted cookie path.

### Task 5: Implement workspace, membership, shift, and schedule APIs test-first

**Files:**
- Create module files and tests under:
  - `shiftory-server/internal/workspace/`
  - `shiftory-server/internal/member/`
  - `shiftory-server/internal/shift/`
  - `shiftory-server/internal/schedule/`
- Create: `shiftory-server/internal/platform/httpx/response.go`

- [ ] **Step 1: Write red service tests for workspace ownership and membership permissions.**

Cover create, list, update, invite, accept, disable, remove, OWNER transfer, ADMIN promotion/demotion, forbidden MEMBER writes, and historical membership preservation. ADMIN cannot mutate OWNER or other ADMIN records.

- [ ] **Step 2: Implement workspace/member services, repositories, and handlers; run green.**

Every route is nested under `/api/v1/workspaces/:workspaceId`; middleware loads active membership from MySQL for each request.

- [ ] **Step 3: Write red shift tests.**

Cover unique codes, case-insensitive alias collisions, disabled shift behavior, optional times, cross-day validation, and schedule snapshot preservation after shift edits.

- [ ] **Step 4: Implement shift CRUD and aliases; run green.**

- [ ] **Step 5: Write red schedule tests.**

Cover self-edit, administrator edit, one day per member, optimistic version conflicts, batch upsert, date range limits, source metadata, history revisions, and permission enforcement.

- [ ] **Step 6: Implement schedule CRUD, batch operations, history, and handlers; run green.**

### Task 6: Implement calendar and overview APIs test-first

**Files:**
- Create: `shiftory-server/internal/calendar/repository.go`
- Create: `shiftory-server/internal/calendar/handler.go`
- Create: `shiftory-server/internal/calendar/repository_integration_test.go`
- Create: `shiftory-server/internal/overview/service.go`
- Create: `shiftory-server/internal/overview/handler.go`
- Create: `shiftory-server/internal/overview/service_test.go`

- [ ] **Step 1: Write red MySQL integration tests for selected-member calendar aggregation.**

Cover explicit REST, missing data, all-rest, disabled historical members, empty selection, member search, and ranges spanning month boundaries.

- [ ] **Step 2: Implement indexed aggregate queries and detail hydration; run green.**

Routes: `/calendar`, `/calendar/:date`, and `/overview`. Date ranges are capped at 366 days and member IDs must belong to the workspace during the requested interval.

### Task 7: Implement file storage and spreadsheet import test-first

**Files:**
- Create: `shiftory-server/internal/platform/storage/storage.go`
- Create: `shiftory-server/internal/platform/storage/local.go`
- Create: `shiftory-server/internal/importer/workbook.go`
- Create: `shiftory-server/internal/importer/xlsx.go`
- Create: `shiftory-server/internal/importer/xls.go`
- Create: `shiftory-server/internal/importer/normalize.go`
- Create: `shiftory-server/internal/importer/workbook_test.go`
- Create: `shiftory-server/internal/importjob/service.go`
- Create: `shiftory-server/internal/importjob/repository.go`
- Create: `shiftory-server/internal/importjob/handler.go`

- [ ] **Step 1: Write red parser tests using generated XLSX fixtures and repository-owned XLS fixture data.**

Cover required headers, duplicate dates as segments, shift aliases, dates, REST constraints, explicit cross-day, formulas treated as cached values only, MIME/signature mismatch, size/sheet/row/column limits, and malformed workbooks.

- [ ] **Step 2: Implement `WorkbookReader` adapters.**

Use Excelize for OOXML and `github.com/extrame/xls` for OLE XLS. Normalize both to `WorkbookData` before business validation.

- [ ] **Step 3: Write red import workflow tests.**

Cover upload, parse, preview categories, canonical equality, explicit conflict decisions, stale preview conflicts, idempotent commit, atomic commit, and version-safe full rollback.

- [ ] **Step 4: Implement import jobs/items/files, preview, commit, and rollback; run green.**

Original files are accessed through `storage.Store`; database rows store opaque keys, hashes, sizes, media type, and retention metadata rather than physical paths.

### Task 8: Implement durable worker and image-AI adapter test-first

**Files:**
- Create: `shiftory-server/internal/importjob/worker.go`
- Create: `shiftory-server/internal/importjob/worker_test.go`
- Create: `shiftory-server/internal/importer/imageai/client.go`
- Create: `shiftory-server/internal/importer/imageai/trpc.go`
- Create: `shiftory-server/internal/importer/imageai/schema.go`
- Create: `shiftory-server/internal/importer/imageai/schema_test.go`
- Create: `shiftory-server/cmd/worker/main.go`

- [ ] **Step 1: Write red worker tests.**

Cover `FOR UPDATE SKIP LOCKED` semantics through repository contracts, lease acquisition, heartbeat, retry with bounded backoff, abandoned lease recovery, cancellation, and idempotent completion.

- [ ] **Step 2: Implement worker and run green.**

- [ ] **Step 3: Write red image draft schema tests.**

Validate one target member, requested date bounds, canonical dates, status/segments, field-level issues, and rejection of unknown JSON fields.

- [ ] **Step 4: Implement the tRPC-Agent-Go adapter.**

Use an OpenAI-compatible provider configured by environment, a multimodal user message containing the uploaded image, typed strict structured output, zero temperature, timeout, prompt/model/schema version recording, and no database writes from the model layer.

### Task 9: Assemble HTTP server and OpenAPI contract

**Files:**
- Create: `shiftory-server/api/openapi.yaml`
- Create: `shiftory-server/internal/platform/httpserver/router.go`
- Create: `shiftory-server/internal/platform/httpserver/router_test.go`
- Create: `shiftory-server/cmd/api/main.go`
- Create: `shiftory-server/cmd/migrate/main.go`

- [ ] **Step 1: Write red HTTP integration tests for health, auth, workspace, shift, schedule, calendar, import, audit, and preference routes.**

Tests start Gin with real services and a dedicated test schema, execute JSON/multipart requests, and assert status codes plus response envelopes.

- [ ] **Step 2: Define OpenAPI 3.1 operations and schemas for every route.**

All errors use `{code,message,details,requestId}` and list endpoints use `{items,page,pageSize,total}`.

- [ ] **Step 3: Wire API and migration commands, middleware, trusted proxies, request IDs, recovery, body limits, CORS, static SPA hosting, and graceful shutdown.**

- [ ] **Step 4: Run all API tests green.**

Run: `go test ./... -count=1`
Expected: PASS.

### Task 10: Build the Vue application foundation test-first

**Files:**
- Modify: `shiftory-web/package.json`
- Modify: `shiftory-web/vite.config.ts`
- Modify: `shiftory-web/src/main.ts`
- Modify: `shiftory-web/src/App.vue`
- Create directories/files under `shiftory-web/src/{api,router,stores,layouts,components,views,styles}`
- Create: `shiftory-web/src/stores/auth.test.ts`
- Create: `shiftory-web/src/stores/preferences.test.ts`

- [ ] **Step 1: Install pinned frontend dependencies and add Vitest scripts.**

Dependencies include Vue Router, Pinia, Element Plus, TanStack Vue Query, dayjs, lucide-vue-next, and zod. Dev dependencies include Vitest, Vue Test Utils, happy-dom, Sass, ESLint, and Prettier.

- [ ] **Step 2: Write red Pinia tests.**

Assert profile/workspace/theme state, access token memory-only behavior, `/auth/me` hydration, refresh retry, logout cleanup, and no persisted credential keys.

- [ ] **Step 3: Implement API client, stores, router guards, theme tokens, and application shell; run tests green.**

Pinia owns user/profile/workspace/theme state. TanStack Query owns schedules, members, calendar, shifts, imports, and overview server state.

### Task 11: Implement all user and administrator views

**Files:**
- Create Vue views for login, registration, overview, personal schedule, import upload/review/history, team calendar, profile, schedule administration, members, shifts, all imports, and workspace settings.
- Create reusable calendar, schedule editor, member picker, import conflict table, time-segment editor, shift editor, and workspace switcher components.
- Create component tests for permission rendering and critical forms.

- [ ] **Step 1: Write failing component tests for authentication, dynamic menus, schedule editing, calendar markers, and import conflict decisions.**

- [ ] **Step 2: Implement shared layout and member-facing views; run targeted tests green.**

- [ ] **Step 3: Implement administrator/owner views with shared components; run targeted tests green.**

- [ ] **Step 4: Implement six light themes through CSS variables and persist only the theme preference; run preference tests green.**

- [ ] **Step 5: Run full frontend checks.**

Run: `pnpm test --run && pnpm type-check && pnpm build`
Expected: PASS with no TypeScript errors.

### Task 12: Verify the complete local acceptance system

**Files:**
- Create: `README.md`
- Create: `docs/API验收清单.md`
- Create: `scripts/acceptance.ps1`
- Modify: `.agent/HANDOFF.md`

- [ ] **Step 1: Create the local MySQL databases and apply migrations.**

Use the user-supplied local credentials only in the local shell environment, not committed source files.

- [ ] **Step 2: Start API and worker, then run the API acceptance script.**

The script registers users, creates/switches a workspace, manages members and shifts, writes/batches schedules, verifies calendar counts, uploads both workbook formats, resolves a conflict, commits and rolls back an import, reads audits, updates profile/theme, refreshes JWTs, and logs out.

- [ ] **Step 3: Run backend and frontend verification.**

Run backend tests, frontend unit tests, type checking, and production build. Image-AI live provider execution is excluded only from automated acceptance per the user's instruction; schema and adapter tests remain required.

- [ ] **Step 4: Update HANDOFF with exact commands, results, known environmental requirements, and no hidden remaining product work.**

### Self-review

- [ ] Every feature listed in `docs/需求.md` maps to a backend module, route, and Vue view above.
- [ ] Authorization is checked by the backend; frontend menu hiding is presentation only.
- [ ] REST, MISSING, multiple segments, cross-day ownership, optimistic conflicts, import atomicity, and rollback conflicts have explicit tests.
- [ ] JWT credentials are not persisted in browser storage and workspace roles are not embedded in access claims.
- [ ] `.xlsx`, `.xls`, and image-AI imports converge on one normalized draft and review workflow.
- [ ] Only one `.git` directory exists under the project root.
- [ ] All listed validation commands have real passing output before completion is claimed.
