# Shiftory

Shiftory 是面向小团队的排班协同应用。当前仓库已实现完整 MVP：JWT 登录、多工作区与角色权限、手动排班、团队日历、XLSX/XLS 导入、图片 AI 异步识别、人工预览纠错、事务提交与全量撤销，以及六套浅色主题。

## 技术结构

- 前端：Vue 3、TypeScript、Vite、Vue Router、Pinia、TanStack Vue Query、Element Plus、SCSS。
- 后端：Go 1.27、Gin、`database/sql`、MySQL 8.4、Goose、Excelize、tRPC-Agent-Go。
- 认证：Ed25519 Access JWT + 轮换 Refresh JWT。Access Token 只驻留前端内存；Refresh Token 只在 HttpOnly Cookie 中，刷新和注销另有 CSRF 双提交校验。
- 工程形态：单仓库、前后端分离、模块化单体；API、图片识别 Worker 和迁移命令独立启动。

### 后端分层约定

后端采用按领域模块组织的 MVC 三层边界：`internal/platform/httpserver` 是 Controller/HTTP 适配层，负责路由、权限前置检查、HTTP 状态码和统一 JSON 错误；`internal/importer`、`internal/schedule` 等包负责工作簿规范化和领域规则，不依赖 Gin 或 HTTP；数据库访问和外部存储由平台适配代码提供。导入校验错误在 importer 层使用结构化 `ValidationError`（行号、字段、规则码、修复提示），再由 Controller 映射为 `error.details`，避免领域层泄露传输细节或内部堆栈。

当前历史路由仍按功能文件集中在 `httpserver` 中，新增或重构接口应沿用上述边界；不通过一次性搬迁制造跨模块回归。

详细需求与设计决策见 [需求文档](docs/需求.md)，接口契约见 [OpenAPI 3.1](shiftory-server/api/openapi.yaml)。

## 本地启动

前置环境：Go 1.27.1、Node.js 24（含 npm）、MySQL 8.4。仓库根目录的 `.env.example` 列出全部环境变量；后端直接读取进程环境，不自动加载 `.env` 文件。前端优先使用 PATH 中的 pnpm，其次使用 Corepack；两者都不可用时，启动脚本会通过 npm 缓存临时运行项目锁定的 pnpm 版本，不会修改全局 PATH。
### 一键开发启动

仓库根目录已提供被 Git 忽略的 `.env.development`，其中包含完整的本地开发配置。按需修改数据库、端口、JWT 文件、上传目录和 AI Worker 参数，然后执行：

```powershell
.\scripts\start-dev.ps1
```

脚本会读取 `.env.development`，先执行数据库迁移，再分别打开 API 和前端开发服务器窗口。默认访问地址是 `http://127.0.0.1:8080` 和 `http://localhost:5173`。常用选项：

```powershell
.\scripts\start-dev.ps1 -ValidateOnly   # 检查配置、目录和开发工具，不启动服务
.\scripts\start-dev.ps1 -SkipMigrate    # 跳过迁移
.\scripts\start-dev.ps1 -BackendOnly    # 只启动 API
.\scripts\start-dev.ps1 -FrontendOnly   # 只启动前端
.\scripts\start-dev.ps1 -WithWorker     # 额外启动图片 AI Worker
.\scripts\start-dev.ps1 -EnvFile .env.local
```

图片 AI Worker 只有在 `.env.development` 中填写 `SHIFTORY_AI_API_KEY` 后才适合进行真实供应商验收；空 Key 仍可用于启动和离线开发。

如本机没有 MySQL，可在仓库根目录启动容器：

```powershell
docker compose up -d mysql
```

默认数据库连接为 `root:123456@127.0.0.1:3306/shiftory`。首次启动前创建数据库（容器配置会自动创建），然后运行：

```powershell
cd shiftory-server
go run ./cmd/migrate
go run ./cmd/api
```

另开终端启动前端：

```powershell
cd shiftory-web
pnpm install
pnpm dev
```

开发模式下浏览器访问 `http://localhost:5173`，Vite 会把 `/api` 代理至 `http://127.0.0.1:8080`。执行 `pnpm build-only` 后，后端也可以通过 `SHIFTORY_WEB_DIR=../shiftory-web/dist` 同域提供前端静态资源。

## 图片 AI Worker

设置 OpenAI 兼容视觉模型参数后，单独运行 Worker：

```powershell
$env:SHIFTORY_AI_MODEL = "your-vision-model"
$env:SHIFTORY_AI_BASE_URL = "https://your-provider.example/v1"
$env:SHIFTORY_AI_API_KEY = "your-key"
go run ./cmd/worker
```

图片原文件以内联多模态消息发送；模型被要求按严格 JSON Schema 输出，温度为 0。Worker 使用 MySQL 租约、心跳、重试延迟和 `SKIP LOCKED` 领取任务。模型结果只生成预览，不会直接写入正式排班；不确定项必须由用户人工修正或保持跳过。AI 供应商的在线连通性需要使用实际密钥单独验收。

## 验证

后端测试会自动创建互相隔离的 MySQL 测试库：

```powershell
cd shiftory-server
go test ./... -count=1
```

前端验证：

```powershell
cd shiftory-web
pnpm type-check
pnpm test
pnpm build-only
```

API 启动后，可从仓库根目录运行真实 HTTP 验收：

```powershell
.\scripts\accept-api.ps1
```

脚本覆盖健康检查、注册/登录 JWT、工作区、班次、排班、成员可见完整日历详情、偏好、Refresh 轮换与 CSRF，以及 Excel 模板下载。

## 关键边界

- 工作区 ID 必须显式出现在业务 API 路径中，后端每次请求重新检查数据库成员关系，不信任 Pinia 中的角色。
- 普通成员可以查看同工作区其他成员的完整已确认排班（班次、时间、跨日与备注），但不能修改他人排班，也不能读取他人的原始导入文件或 AI 响应。
- 导入提交与撤销均为全有或全无事务；预览后的版本变化会触发冲突，不进行静默覆盖或部分恢复。
- JWT 私钥、上传文件、构建产物和本地环境文件已排除在 Git 跟踪之外；不要提交真实密钥。

