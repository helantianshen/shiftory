# 导入错误分层与 MVC 整理 Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 让排班文件导入在发现非法组合时返回可定位、可修复的结构化错误，并建立清晰的 importer 领域校验层与 HTTP Controller 错误映射边界。

**Architecture:** 保留现有路由文件作为 Controller 入口，导入器只负责工作簿规范化和领域规则，不依赖 Gin/HTTP。导入器返回带行号、字段、错误码和修复提示的结构化验证错误；Controller 统一将其转换为 API `error.details`，不向客户端泄露内部堆栈或 SQL 信息。后续其他路由可复用同一错误映射模式，避免立即进行高风险全量搬迁。

**Tech Stack:** Go 1.27、Gin、Excelize、标准库 `errors`/`fmt`、现有 `go test` 测试体系。

---

### Task 1: 定义 importer 分层验证错误

**Files:**
- Create: `shiftory-server/internal/importer/errors.go`
- Modify: `shiftory-server/internal/importer/normalize.go`
- Test: `shiftory-server/internal/importer/workbook_test.go`

- [x] **Step 1: Write the failing test**

构造“第 2 行休息日仍填了班次”的工作簿，断言错误包含行号、字段、稳定错误码，并且仍可通过 `errors.Is` 识别领域错误 `schedule.ErrRestHasSegments`。

- [x] **Step 2: Run test to verify it fails**

Run: `go test ./internal/importer -run TestNormalizeReturnsStructuredRestDayError -count=1`
Expected: FAIL，因为当前 Normalize 只返回普通 `fmt.Errorf` 字符串。

- [x] **Step 3: Implement minimal structured error**

新增 `ValidationError`，字段包括 `Row`、`Fields`、`Code`、`Message`、`Hint` 和 `Cause`，实现 `Error`/`Unwrap`。Normalize 在行级规则失败时返回该错误，优先覆盖休息日含班次、班次与自定义时间互斥、起止时间不完整和跨日缺少详情等规则。

- [x] **Step 4: Run test to verify it passes**

Run: `go test ./internal/importer -run TestNormalizeReturnsStructuredRestDayError -count=1`
Expected: PASS。

### Task 2: 在 HTTP Controller 统一映射导入错误

**Files:**
- Create: `shiftory-server/internal/platform/httpserver/import_errors.go`
- Modify: `shiftory-server/internal/platform/httpserver/import_routes.go`
- Test: `shiftory-server/internal/platform/httpserver/import_errors_test.go`

- [x] **Step 1: Write the failing test**

使用 `httptest` 调用错误映射函数，断言 JSON 返回 `code=INVALID_WORKBOOK`，`message` 为中文可读说明，`details` 含 `row`、`fields`、`ruleCode`、`hint`，且不包含 Go 内部错误文本。

- [x] **Step 2: Run test to verify it fails**

Run: `go test ./internal/platform/httpserver -run TestWriteImportValidationFailure -count=1`
Expected: FAIL，因为当前没有统一映射函数。

- [x] **Step 3: Implement minimal Controller mapper**

新增 `writeImportValidationFailure`，对 `*importer.ValidationError` 生成结构化 details，对其他错误返回通用 `INVALID_WORKBOOK`。createImport 调用该函数替换直接 `err.Error()` 返回，保持 HTTP 400 和已有 API 外层格式不变。

- [x] **Step 4: Run focused and full tests**

Run: `go test ./internal/platform/httpserver -run 'TestWriteImportValidationFailure|TestSpreadsheetImportPreviewCommitAndRollback' -count=1`
Expected: PASS。

Run: `go test ./... -count=1`
Expected: PASS。

### Task 3: 同步架构说明和交接状态

**Files:**
- Modify: `README.md`
- Modify: `.agent/HANDOFF.md`

- [x] **Step 1: Document the current MVC boundary**

说明 `httpserver` 是 Controller/HTTP 适配层、`importer` 是工作簿规范化与领域校验层，API 错误由 Controller 统一序列化；标注其他历史路由仍待按同样边界逐步迁移，不虚构已完成的全量 MVC 重构。

- [x] **Step 2: Record verification**

将结构化导入错误测试和 `go test ./... -count=1` 结果写入 HANDOFF，记录附件中第 2 行问题可通过 `row=2` 和字段列表直接定位。
