# Shiftory

Shiftory 是面向小团队的排班协同应用。当前仓库已实现完整 MVP：JWT 登录、多工作区与角色权限、手动排班、团队日历、XLSX/XLS 导入、图片 AI 异步识别、人工预览纠错、事务提交与全量撤销，以及六套浅色主题。

## 技术结构

- 前端：Vue 3、TypeScript、Vite、Vue Router、Pinia、TanStack Vue Query、Element Plus、SCSS。
- 后端：Go 1.27、Gin、`database/sql`、MySQL 8.4、Goose、Excelize、tRPC-Agent-Go。
- 认证：Ed25519 Access JWT + 轮换 Refresh JWT。Access Token 只驻留前端内存；Refresh Token 只在 HttpOnly Cookie 中，刷新和注销另有 CSRF 双提交校验。
- 工程形态：单仓库、前后端分离、模块化单体；API 进程内置图片识别 Worker，迁移命令独立执行。

### 后端分层约定

后端采用按领域模块组织的 MVC 三层边界：`internal/platform/httpserver` 是 Controller/HTTP 适配层，负责路由、权限前置检查、HTTP 状态码和统一 JSON 错误；`internal/importer`、`internal/schedule` 等包负责工作簿规范化和领域规则，不依赖 Gin 或 HTTP；数据库访问和外部存储由平台适配代码提供。导入校验错误在 importer 层使用结构化 `ValidationError`（行号、字段、规则码、修复提示），再由 Controller 映射为 `error.details`，避免领域层泄露传输细节或内部堆栈。

当前历史路由仍按功能文件集中在 `httpserver` 中，新增或重构接口应沿用上述边界；不通过一次性搬迁制造跨模块回归。

详细需求与设计决策见 [需求文档](docs/需求.md)，接口契约见 [OpenAPI 3.1](shiftory-server/api/openapi.yaml)。

## 本地启动

前置环境：Go 1.27.1、Node.js 24（含 npm）、MySQL 8.4。仓库根目录的 `.env.example` 列出全部环境变量。后端优先读取系统环境变量；缺失项会按顺序查找 `SHIFTORY_ENV_FILE`、Linux 的 `/etc/shiftory/shiftory.env`，以及当前目录和上两级目录中的 `.env.development`、`.env.local`、`.env`、`.env.production`。显式指定的 `SHIFTORY_ENV_FILE` 不存在或格式错误时会直接报错。前端优先使用 PATH 中的 pnpm，其次使用 Corepack；两者都不可用时，启动脚本会通过 npm 缓存临时运行项目锁定的 pnpm 版本，不会修改全局 PATH。

### GoLand 启动

仓库的 `.run` 目录包含可共享的 GoLand 运行配置，重新打开项目或执行 **File > Reload All from Disk** 后，可以直接使用：

- `Shiftory Migrate`：执行一次数据库迁移；首次启动或数据库结构变化后运行。
- `Shiftory API`：启动后端 API，图片 AI Worker 按配置在同一进程内启用。
- `Shiftory Web (pnpm)`：通过 GoLand 的 npm 类型运行 `pnpm dev`。JetBrains 将 npm、Yarn 和 pnpm 脚本统一放在这个运行配置类型中，所以无需创建自定义 Shell 配置。
- `Shiftory Development`：同时启动 API 和前端，适合日常开发；不会自动执行迁移。

运行配置默认不会自动显示在 Services 中。按 `Alt+8` 打开 Services，依次选择 **Add Service > Run Configuration**，加入 `Go Application`、`npm` 和 `Compound` 类型；其中 `npm` 节点就是前端 pnpm 服务。

前端配置使用 GoLand 的项目级包管理器。若启动日志实际调用的不是 pnpm，请在 **Settings > Languages & Frameworks > JavaScript Runtime > Package manager** 中选择项目的 pnpm 或 Corepack，然后重新运行。后端从系统环境变量或仓库根目录的 `.env.development` 自动加载配置；要在 API 内启用图片 Worker，请确保配置中有 `SHIFTORY_AI_ENABLED=true`。

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
.\scripts\start-dev.ps1 -EnableAI       # 临时启用 API 内的图片识别能力
.\scripts\start-dev.ps1 -EnvFile .env.local
```

图片 AI Worker 运行在 API 进程内。设置 `SHIFTORY_AI_ENABLED=true`、模型名称和 API Key 后启用；未启用时普通排班和 Excel 导入仍可用。`SHIFTORY_WORKER_MAX_CONCURRENCY` 控制同时处理的图片任务数，`SHIFTORY_WORKER_LEASE` 控制任务处理租约，`SHIFTORY_WORKER_POLL_INTERVAL` 控制无唤醒信号时的数据库扫描周期。

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

### Linux 单机部署

Linux 单机只需要运行 API 二进制，图片 AI Worker 会在同一进程内启动。将配置放到 `/etc/shiftory/shiftory.env`，或通过 `SHIFTORY_ENV_FILE` 指定其他绝对路径；`SHIFTORY_UPLOAD_DIR`、`SHIFTORY_WEB_DIR` 和 JWT 密钥路径也建议使用绝对路径。API 与 Worker 必须共享同一个上传目录。

```bash
./scripts/start-linux.sh build
./scripts/start-linux.sh migrate
sudo install -m 0755 deploy/shiftory.service /etc/systemd/system/shiftory.service
sudo systemctl daemon-reload
sudo systemctl enable --now shiftory
```

`deploy/shiftory.service` 默认使用 `/opt/shiftory/bin/shiftory-api`、`/etc/shiftory/shiftory.env` 和 `shiftory` 系统用户；如果安装目录不同，请同步调整 unit 文件。数据库迁移只在发布时执行，不由 systemd 每次重启重复触发。

## 图片 AI Worker

设置 OpenAI 兼容视觉模型参数后，启动 API 即会同时启动内置 Worker：

```powershell
$env:SHIFTORY_AI_MODEL = "your-vision-model"
$env:SHIFTORY_AI_BASE_URL = "https://your-provider.example/v1"
$env:SHIFTORY_AI_API_KEY = "your-key"
$env:SHIFTORY_AI_ENABLED = "true"
go run ./cmd/api
```

图片原文件以内联多模态消息发送；模型被要求按严格 JSON Schema 输出，温度为 0。内置 Worker 使用 MySQL 租约、心跳、重试延迟和 `SKIP LOCKED` 领取任务，并通过固定容量的 ants 协程池限制并发。模型结果只生成预览，不会直接写入正式排班；不确定项必须由用户人工修正或保持跳过。AI 供应商的在线连通性需要使用实际密钥单独验收。

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

