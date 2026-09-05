# 当前目标

交付 Shiftory 完整 MVP：JWT 认证、多工作区排班协同、Excel/图片导入预览与安全提交、团队日历和 Vue 3 管理界面。

# 当前状态

完整 MVP 已实现并通过后端全量测试、前端类型检查/测试/生产构建及真实 HTTP API 验收。项目根目录已有唯一的本地 Git 仓库，分支为 `main`；尚未暂存、提交、推送或配置远程。

当前开发配置已补充统一入口：根目录 `.env.development` 提供可调整的本地环境变量，`scripts/start-dev.ps1` 负责读取配置、执行迁移并分别启动 API、前端和可选图片 AI Worker。启动器已兼容普通 Windows PowerShell 中没有全局 pnpm/Corepack 的环境，会通过 npm 缓存临时运行前端锁定的 pnpm 版本。尚未执行提交或远程操作。

# 已完成工作

- 需求文档已升级为可执行基线，并修正 JWT、Pinia、时区、成员生命周期、班次快照、导入并发/撤销、MySQL 8.4 和最终体交付范围。
- 后端采用 Go 1.27、Gin、`database/sql`、MySQL 8.4、Goose、Excelize、XLS 适配器和 tRPC-Agent-Go 图片识别模块。
- Access JWT 仅驻留前端内存；Refresh JWT 仅存 HttpOnly Cookie，支持轮换、令牌族撤销、重放检测、CSRF 双提交、Ed25519 `kid` 和持久化密钥文件。
- 已实现注册、登录、刷新、注销、资料、密码、偏好、多工作区、邀请、角色/成员、所有权转移、删除、班次、手动/批量排班、历史和审计。
- 普通成员可查看其他成员完整已确认排班，包括班次、时间、跨日、来源和备注；修改权限仍由后端独立校验。
- 历史团队日历按成员关系的 `joined_at` / `left_at` 有效日期统计；移除成员不删除历史数据。
- 已实现 XLSX/XLS 固定模板、签名/大小/结构校验、别名映射、缺失/相同/新增/冲突/不确定预览、人工纠错、原文件访问控制、原子提交、陈旧预览检测和全量撤销。
- 未映射 Excel 班次不会阻断整批，而是形成带问题说明的 `UNCERTAIN` 条目，用户可在预览中选择班次或自定义时间。
- 图片导入由持久化 Worker 以租约、心跳、退避重试、过期任务恢复和 `FOR UPDATE SKIP LOCKED` 执行；严格结构化输出只生成预览，不直接写正式排班。
- Vue 3 前端已实现完整路由和页面、角色菜单、响应式布局、团队日历详情、导入修正、成员完整度/最近导入，以及六套浅色主题。
- Pinia 仅管理用户、登录态、工作区摘要、当前工作区、主题和内存 Access Token；服务端业务数据由 Vue Query 管理，任何令牌均不持久化。
- 已补充 OpenAPI 3.1、README、环境变量样例、MySQL Compose 和真实 API 验收脚本。
- 已补充被 Git 忽略的 `.env.development` 和 `scripts/start-dev.ps1`；示例环境文件中的 AI Key 已改为占位符，避免分发疑似真实凭据。
- 前端在 `package.json` 固定使用 `pnpm@11.19.0`；启动器按 pnpm、Corepack、npm exec 顺序解析包管理器，不要求修改系统 PATH 或进行全局安装。
- 修正 HTTP 集成测试中的日期敏感夹具：固定成员加入日为 `2026-09-01`、移除日为 `2026-09-04`，确保测试稳定覆盖成员有效期边界。
- Excel 导入模板的日期列 `A2:A1000` 已设置为 Excel 文本格式 `@`，模板日期以 `yyyy-mm-dd` 字符串保存，避免编辑后按本地格式显示为 `yyyy/m/d`。
- Excel 导入模板已增加工作区启用班次动态下拉、状态/跨日下拉和组合校验；下拉选项写入隐藏的 `模板选项` 工作表，非法输入使用 Stop 错误提示。
- 模板组合校验允许空白新行；休息日禁止班次、时间和跨日；工作日班次与自定义起止时间互斥；自定义时间要求起止同时存在；跨日不能单独出现。

# 关键文件

- `docs/需求.md`
- `docs/superpowers/plans/2026-09-04-shiftory-full-mvp.md`
- `shiftory-server/api/openapi.yaml`
- `shiftory-server/cmd/api/main.go`
- `shiftory-server/cmd/worker/main.go`
- `shiftory-server/internal/platform/httpserver/`
- `shiftory-server/internal/importjob/`
- `shiftory-server/migrations/`
- `shiftory-web/src/stores/session.ts`
- `shiftory-web/src/views/`
- `README.md`
- `scripts/accept-api.ps1`
- `scripts/start-dev.ps1`
- `scripts/dev-tooling.ps1`
- `scripts/tests/dev-tooling.Tests.ps1`

# 验证结果

- 后端：`go test ./... -count=1` 通过；包含认证、排班领域、日历、XLSX/XLS、图片结构化输出、Worker、迁移、HTTP/MySQL 集成、JWT 密钥和存储测试。
- OpenAPI：YAML 解析、版本与 35 个 API 路径组覆盖测试通过。
- 前端：`pnpm type-check` 通过。
- 前端：`pnpm test` 通过，4 个测试文件、6 个测试。
- 前端：`pnpm build-only` 通过。Element Plus 主包产生约 890 kB 的压缩前 chunk 警告，不影响构建或运行。
- 真实 API：本机 MySQL `shiftory` 库迁移到 Goose v2，启动 `cmd/api` 后运行 `scripts/accept-api.ps1`，结果 `PASS`；覆盖健康检查、JWT 注册/登录、工作区、班次、排班、完整团队详情、偏好、Refresh 轮换 + CSRF 和 Excel 模板。
- 文件系统检查：只存在根目录一个 `.git`，HEAD 指向 `refs/heads/main`。
- 开发配置验证：3 个 PowerShell 脚本 AST 解析通过；包管理器解析单测通过；移除 Codex 内置 pnpm/Corepack 路径后，`scripts/start-dev.ps1 -ValidateOnly` 正确选择本机 `npm.exe`，并通过 npm exec 实际运行 `pnpm@11.19.0`。
- 前端降级链路验收：在无全局 pnpm/Corepack 的模拟 PATH 中成功启动 Vite `http://127.0.0.1:5173`，随后已终止测试服务且端口无残留监听。
- 前端降级链路回归：经 npm exec 运行 `pnpm test`（4 文件、6 测试）、`pnpm type-check` 和 `pnpm build-only` 均通过；构建仍只有约 890 kB 主 chunk 警告。
- 本次回归：`go test ./... -count=1` 通过，包含 `internal/platform/httpserver` 全量集成测试。
- 模板回归：`TestScheduleImportTemplateKeepsISODateTextFormat` 通过，确认日期单元格保持共享字符串并引用文本格式。
- 模板回归：`TestScheduleImportTemplateAddsDropdownsAndCombinationValidation` 通过，确认状态/班次/跨日下拉、隐藏选项表、Stop 错误提示和组合公式均写入 XLSX。
- 本次模板增强后的后端全量回归：`go test ./... -count=1` 通过。
- 导入校验错误已完成分层：`internal/importer.ValidationError` 负责行号、字段、规则码、修复提示和领域 Cause；`httpserver.writeImportValidationFailure` 负责转换为统一 HTTP `error.details`，不向客户端暴露内部错误文本。
- 附件复现的第 2 行会返回 `INVALID_WORKBOOK`，message 为“休息日不能填写班次、开始时间、结束时间或跨日”，details 包含 `row=2`、字段列表、`ruleCode=REST_HAS_SEGMENTS` 和修复提示。
- README 已补充当前 MVC 三层边界说明：HTTP Controller、导入/排班领域包、平台适配层；历史路由暂不做高风险一次性搬迁。
- 结构化错误回归：`TestNormalizeReturnsStructuredRestDayError`、`TestWriteImportValidationFailure` 通过；修改后再次执行 `go test ./... -count=1` 通过。
- 导入错误分层计划已保存至 `docs/superpowers/plans/2026-09-05-import-error-mvc.md`；新增 `ValidationError` 覆盖日期、状态、跨日、时间互斥、休息日段和排班组合错误，HTTP Controller 统一返回中文 message 与 `details.row/fields/ruleCode/hint`。
- 附件 `D:/下载/shiftory-schedule-template.xlsx` 已确认第 2 行为“休息 + 早班”，第 9、10、16、17 行为“休息 + 08:30-17:30”；现在 API 会分别返回可定位的行号和字段，不再只返回英文 `rest day cannot contain segments`。

# 尚需外部验收

- 按用户安排，真实图片 AI 供应商调用未在开发阶段联网执行。适配器、严格 Schema、错误处理、重试和离线 fake 测试已完成；需要用户提供实际 `SHIFTORY_AI_MODEL`、`SHIFTORY_AI_BASE_URL`、`SHIFTORY_AI_API_KEY` 后启动 `cmd/worker` 验收模型兼容性。

# 本地状态提示

- 本机正式 `shiftory` 数据库中存在 API 验收脚本创建的临时用户与工作区数据。
- 未获得 Commit/Push/PR 授权，因此没有执行任何提交或远程操作。
- 本次只实际拉起并验收了前端降级启动链路；未通过启动器重新拉起 API/Worker。真实 Worker 供应商调用仍需实际 AI 参数和密钥。
- 当前测试夹具不再依赖运行当天的成员加入/移除时间。
- Excel 数据验证只是编辑体验约束；上传时仍由 `internal/importer/normalize.go` 执行最终互斥和状态校验，复制粘贴或外部程序生成的非法 XLSX 不会绕过后端规则。
