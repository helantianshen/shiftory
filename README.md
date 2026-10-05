# Shiftory

Shiftory 是面向小团队的排班协同应用。产品范围包括 JWT 登录、多工作区与角色权限、手动排班、团队日历、XLSX/XLS 导入、图片 AI 异步识别、人工预览纠错、事务提交与全量撤销，以及六套浅色主题；功能完成度以当前源码与实际验收为准。

## 技术结构

- 前端：Vue 3、TypeScript、Vite、Vue Router、Pinia、TanStack Vue Query、Element Plus、SCSS。
- 后端：Go 1.26.8、Gin、`database/sql`、PostgreSQL 18、GORM AutoMigrate、Excelize、Eino、Asynq、Redis。
- 认证：Ed25519 Access JWT + 轮换 Refresh JWT。Access Token 只驻留前端内存；Refresh Token 只在 HttpOnly Cookie 中，刷新和注销另有 CSRF 双提交校验。
- 工程形态：单仓库、前后端分离、模块化单体；API 进程内置图片识别 Worker，迁移命令独立执行。

### 后端分层约定

后端采用按领域模块组织的 MVC 三层边界：`internal/platform/httpserver` 是 Controller/HTTP 适配层，负责路由、权限前置检查、HTTP 状态码和统一 JSON 错误；`internal/importer`、`internal/schedule` 等包负责工作簿规范化和领域规则，不依赖 Gin 或 HTTP；数据库访问和外部存储由平台适配代码提供。导入校验错误在 importer 层使用结构化 `ValidationError`（行号、字段、规则码、修复提示），再由 Controller 映射为 `error.details`，避免领域层泄露传输细节或内部堆栈。

当前历史路由仍按功能文件集中在 `httpserver` 中，新增或重构接口应沿用上述边界；不通过一次性搬迁制造跨模块回归。

项目资料统一维护在 [.agent 项目索引](.agent/PROJECT.md)，包括 [产品需求](.agent/REQUIREMENTS.md) 与 [工程约束](.agent/ENGINEERING.md)；接口契约见 [OpenAPI 3.1](shiftory-server/api/openapi.yaml)。

## 本地启动

前置环境：Go 1.26.8、Node.js 24（含 npm）、PostgreSQL 18。后端使用 **进程环境变量 + YAML 配置文件**，前端继续使用 Vite 原有的 env 文件加载方式，不读取 YAML。

后端 API 和迁移程序接受相同的启动参数：

| 参数 | 作用 |
| --- | --- |
| `--env production` | 生产模式，省略参数时的默认值 |
| `--env development` | 开发模式 |
| `--config /path/to/config.yaml` | 显式指定 YAML 文件；不改变 `--env` 选择的模式 |

后端使用 Go 1.26.8。Linux 构建脚本、PowerShell 开发启动器及 Docker 固定该版本。若系统默认 Go 较新，直接执行 Go 命令前，在当前终端设置 `export GOTOOLCHAIN=go1.26.8`（Bash）或 `$env:GOTOOLCHAIN = "go1.26.8"`（PowerShell）；`go.mod` 的最低版本要求不会自动将较新的系统工具链降级。GoLand 项目 SDK 也应选择 1.26.8。

后端配置优先级为：**非空进程环境变量 > 所选 YAML > 内置默认值**。YAML 使用 `log`、`server`、`postgres`、`jwt`、`ai`、`redis`、`tasks`、`storage` 分组（旧 `worker` 字段仅兼容配置加载），环境变量按 `SHIFTORY_<分组>_<字段>` 命名，例如供应商 `relay` 的 `api_key` 对应 `SHIFTORY_AI_PROVIDERS_RELAY_API_KEY`、`server.port` 对应 `SHIFTORY_SERVER_PORT`。空白环境变量沿用文件值；布尔值 `false` 可正常覆盖 `true`。密钥和数据库密码保留原始空白，YAML 中显式空密码表示无密码。可配置项列在 `config/development.example.yaml` 和 `config/production.example.yaml` 中。

PostgreSQL 使用 `postgres.host`、`postgres.port`、`postgres.database`、`postgres.user`、`postgres.password`、`postgres.sslmode`，对应 `SHIFTORY_POSTGRES_*` 环境变量。默认端口 5432，程序编码连接串，会话时区固定 UTC，不接受运行时 DSN。`sslmode` 支持 `disable`、`require`、`verify-ca`、`verify-full`；本机容器用 `disable`，远程可信证书连接用 `verify-full`。旧 `mysql` YAML 分组不兼容，应明确更新配置。`server.port` 为监听所有网卡的端口，端口范围 1–65535。旧平铺 YAML 和旧地址／DSN环境变量不读取。

未指定 `--config` 时，程序依次查找当前工作目录、父目录和祖父目录下的 `config/<模式>.yaml`，只加载首个命中文件，不合并其他文件。未发现文件时允许使用环境变量和内置默认值；显式路径不存在、YAML 格式错误、未知键或非法配置值会直接报错。YAML 中的相对文件路径仍相对于**进程工作目录**，生产部署建议全部使用绝对路径。后端不再读取任何 env 文件，`SHIFTORY_ENV` 和 `SHIFTORY_ENV_FILE` 不再生效。

首次使用时复制样例，然后修改数据库等参数；实际配置文件被 Git 忽略，请勿将密钥写入样例。已有本地 YAML 时不要重复覆盖：

```powershell
Copy-Item config/development.example.yaml config/development.yaml
Copy-Item config/production.example.yaml config/production.yaml
```

前端继续在 `shiftory-web` 下使用 `.env`、`.env.local`、`.env.development`、`.env.production` 等文件，以及原有 `VITE_*` 变量。前端启动优先使用 PATH 中的 pnpm，其次使用 Corepack；两者都不可用时，启动脚本会通过 npm 缓存临时运行项目锁定的 pnpm 版本。

### GoLand 启动

仓库的 `.run` 目录包含可共享的 GoLand 运行配置，重新打开项目或执行 **File > Reload All from Disk** 后，可以直接使用：

- `Shiftory Migrate`：执行一次数据库迁移；可单独执行 GORM 自动迁移；API 启动时也会执行。
- `Shiftory API`：启动后端 API，图片 AI Worker 按配置在同一进程内启用；共享配置传入 `--env development --config <项目目录>/config/development.yaml`。
- `Shiftory Web (pnpm)`：通过 GoLand 的 npm 类型运行 `pnpm dev`。JetBrains 将 npm、Yarn 和 pnpm 脚本统一放在这个运行配置类型中，所以无需创建自定义 Shell 配置。
- `Shiftory Development`：同时启动 API 和前端，适合日常开发；API 会自动同步表结构。

运行配置默认不会自动显示在 Services 中。按 `Alt+8` 打开 Services，依次选择 **Add Service > Run Configuration**，加入 `Go Application`、`npm` 和 `Compound` 类型；其中 `npm` 节点就是前端 pnpm 服务。

前端配置使用 GoLand 的项目级包管理器。若启动日志实际调用的不是 pnpm，请在 **Settings > Languages & Frameworks > JavaScript Runtime > Package manager** 中选择项目的 pnpm 或 Corepack，然后重新运行。开发启动脚本向后端显式传入开发模式和 YAML 路径；要在 API 内启用图片 Worker，请设置 YAML 的 `ai.enabled: true`（在 `ai` 分组中设置 `enabled`），或进程环境变量 `SHIFTORY_AI_ENABLED=true`。

### 一键开发启动

准备好被 Git 忽略的 `config/development.yaml`，按需修改数据库、端口、JWT 文件、上传目录和 AI Worker 参数，然后执行：

```powershell
.\scripts\start-dev.ps1
```

脚本将 `--env development --config <YAML路径>` 同时传给迁移和 API，先执行数据库迁移，再分别打开 API 和前端开发服务器窗口。默认访问地址是 `http://127.0.0.1:8080` 和 `http://localhost:5173`。常用选项：

```powershell
.\scripts\start-dev.ps1 -ValidateOnly   # 检查配置路径、目录和开发工具，不启动服务；YAML 由后端启动时校验
.\scripts\start-dev.ps1 -SkipMigrate    # 跳过启动脚本的预检查，API 仍会自动迁移
.\scripts\start-dev.ps1 -BackendOnly    # 只启动 API
.\scripts\start-dev.ps1 -FrontendOnly   # 只启动前端
.\scripts\start-dev.ps1 -EnableAI       # 临时启用 API 内的图片识别能力
.\scripts\start-dev.ps1 -ConfigFile config/custom-development.yaml
```

### 环境与日志

启动参数 `--env` 支持 `development` 和 `production`，默认是 `production`。环境选择只影响自动发现的配置文件和日志默认值：开发环境默认 `debug` + 文本日志，生产环境默认 `info` + JSON 日志。可通过 `SHIFTORY_LOG_LEVEL=debug|info|warn|error` 和 `SHIFTORY_LOG_FORMAT=text|json` 单独覆盖。API 启动时会打印配置摘要、数据库、JWT、Worker 和监听地址；每次 HTTP 调用会记录请求 ID、方法、路径、状态和耗时，图片上传还会记录校验、存储和任务创建阶段。日志不会记录 API Key、令牌或图片内容。

控制台日志使用 `log.format: text`，按时间、级别、消息和有序字段展示。DEBUG 为青色、INFO 为绿色、WARN 为黄色、ERROR 为红色，字段使用“标签: 值”，HTTP 请求优先展示方法、路径、状态和耗时，AI 路由优先展示供应商和模型。设置 `NO_COLOR=1` 或 `TERM=dumb` 可关闭颜色；重定向文本日志时可使用 `NO_COLOR=1`。`log.format: json` 继续输出无颜色的结构化日志。

AI 异步处理运行在 API 进程内，Eino 负责模型调用，Asynq 使用 Redis 调度任务，PostgreSQL 保存任务、Outbox、执行记录和预览。`tasks.concurrency` 控制任务并发，各供应商的 `max_concurrency` 通过 Redis 在进程间共享。关闭 `ai.enabled` 时停止新 AI 请求及消费，已有草稿仍可审查。

本机 PostgreSQL 为全局共享容器 `postgres`，监听 `127.0.0.1:5432`，持久卷为 `postgres-data`。管理 Compose 和密码文件在 `~/.config/shared-services/postgres/`，不属于本仓库。各项目使用独立数据库和账号；当前 Shiftory 应用账号 `shiftory` 仅访问 `shiftory` 数据库，实际密码保存在被 Git 忽略的开发 YAML 中。无需另外启动项目数据库容器。

仓库 `compose.yaml` 只部署应用和 Redis，通过外部网络 `${POSTGRES_NETWORK:-shared-postgres_default}` 连接共享 PostgreSQL；运行前提供 `SHIFTORY_POSTGRES_PASSWORD`，可用 `SHIFTORY_POSTGRES_HOST`、`SHIFTORY_POSTGRES_USER`、`SHIFTORY_POSTGRES_DATABASE` 覆盖连接目标。删除项目 Compose 不会删除共享数据库容器和数据卷。

首次启动前由共享实例管理员创建独立 UTF8 数据库和应用账号，然后运行：

```powershell
cd shiftory-server
go run ./cmd/api --env development
```

另开终端启动前端：

```powershell
cd shiftory-web
pnpm install
pnpm dev
```

开发模式下浏览器访问 `http://localhost:5173`，Vite 会把 `/api` 代理至 `http://127.0.0.1:8080`。执行 `pnpm build-only` 后，后端也可以通过 `SHIFTORY_SERVER_WEB_DIR=../shiftory-web/dist` 同域提供前端静态资源。

### Linux 单机部署

采用 **后端二进制 + YAML 配置 + 前端构建文件**，手动复制到服务器指定目录；PostgreSQL 和 Redis 通过 Docker 启动，API 由 systemd 管理。完整步骤、配置填写和 service 占位见 [Linux 手动部署文档](docs/deployment.md)。

```bash
# 构建机：输出 bin/、web/、config/ 及部署脚本和 service 模板
./scripts/build-release.sh
# 服务器：复制部署包后拉取、启动并配置基础服务
sudo bash /YOUR/DEPLOY/DIRECTORY/scripts/start-services.sh
```

脚本逐步输出状态。基础服务脚本使用全局容器名 `postgres`、`redis`；已有同名容器输出状态并跳过。部署包配置由公开样例生成，不包含本地密钥，生产样例端口为 `18763`，可直接修改 YAML 的 `server.port` 并重启应用，service 不固定端口或健康检查地址。复制部署包并填写配置后，运行 `sudo bash scripts/install-service.sh` 自动创建 systemd 服务并启动应用。API 每次启动都会执行结构同步，独立迁移命令可在发布前执行。

### Docker

镜像将公开的生产 YAML 样例安装为 `/app/config/production.yaml`，Compose 默认传入生产模式。可将自己的 YAML 只读挂载到该路径，并用 Compose 的 `environment` 注入数据库密码、AI Key 等覆盖值。修改 `command` 时，启动参数会同时传递给迁移和 API。

```bash
COMPOSE_DISABLE_ENV_FILE=1 docker compose up -d --build
# 仅检查 Compose 配置，不启动容器：
COMPOSE_DISABLE_ENV_FILE=1 docker compose config --quiet
```

.env 文件不用于后端配置。Compose 可通过进程变量 `SHIFTORY_CONFIG_FILE` 只读挂载实际生产 YAML（默认挂载无密钥的生产样例）；供应商密钥可用 `SHIFTORY_AI_PROVIDERS_<ID>_API_KEY` 覆盖。

Compose 自身具有隐式读取根目录 `.env` 的行为，因此部署命令显式设置 `COMPOSE_DISABLE_ENV_FILE=1`。这不会禁用前端 Vite 读取 `shiftory-web/.env*`。

后端通过 Viper 在启动时将 YAML、非空进程环境变量和默认值解码为配置结构体；不监听文件或自动重载，配置修改后需重启进程。

## AI 导入与文字排班

在实际 `config/development.yaml` 中配置 `ai`、`redis`、`tasks`，格式见 [开发样例](config/development.example.yaml) 和 [AI Spec](docs/specs/ai-import-eino.md)。本地已填写的供应商配置已合并到忽略的开发配置。正式进程在启动时读取配置，修改供应商或 `order` 后重启生效。

供应商优先级按唯一正整数 `order` 升序，默认字节、中转站、DeepSeek。字节使用 Eino Ark，其余使用 OpenAI Chat Completions。各模型单独配置图片／文字能力、格式、超时及并发；错误自动切换，整轮失败后按有限预算重试。无需另加协程池。

图片和文字请求只产生逐日草稿；文字先提取规则，再按日历确定性展开。没有描述的日期保持缺失，未知班次及冲突供人工修改。提交携带 `expectedReviewVersion`，在事务中核对原排班版本；撤销校验导入后的版本和来源，拒绝覆盖后续修改。详情可读取各轮供应商诊断及受权限保护的原文。

Redis 故障时新任务保存在 PostgreSQL Outbox，连接恢复后继续投递。正常停机等待正在处理的任务；强杀后由 Asynq 与业务租约恢复，可能重复请求模型但不会绕过执行隔离重复写入草稿。Redis 数据彻底丢失时不自动重建队列，失联任务标记失败后可人工重试。Compose 提供 AOF everysec、noeviction、持久卷；本地共享测试 Redis 的 AOF 未开启，没有调整共享容器。

旧 Worker 必须停止后，才可显式接纳尚未投递的历史图片任务：

```bash
cd shiftory-server
go run ./cmd/migrate --env development --config ../config/development.yaml --migrate-ai-jobs
```

该命令不自动迁移正在持有有效租约的任务，不改变已审核草稿。历史 `ai_raw_response` 仍保存旧兼容预览，新模型原文保存在 `import_provider_calls.raw_text`。`/health/ai` 与 `/metrics/ai` 提供队列就绪及无内容的聚合指标。

## 验证

仓库不保留自动化测试代码。后端执行编译和静态检查：

```bash
cd shiftory-server
GOTOOLCHAIN=go1.26.8 go build ./...
GOTOOLCHAIN=go1.26.8 go vet ./...
```

前端执行类型检查和生产构建：

```powershell
cd shiftory-web
pnpm build
```

编译通过不代表业务流程已完成运行验收，实际功能需在对应运行环境中手工验证。

## 关键边界

- 工作区 ID 必须显式出现在业务 API 路径中，后端每次请求重新检查数据库成员关系，不信任 Pinia 中的角色。
- 普通成员可以查看同工作区其他成员的完整已确认排班（班次、时间、跨日与备注），但不能修改他人排班，也不能读取他人的原始导入文件或 AI 响应。
- 导入提交与撤销均为全有或全无事务；预览后的版本变化会触发冲突，不进行静默覆盖或部分恢复。
- JWT 私钥、上传文件、构建产物和本地 YAML 和前端 env 文件已排除在 Git 跟踪之外；不要提交真实密钥。

## 数据库初始化与自动迁移

项目仍处于开发阶段，尚未部署。表结构由 `shiftory-server/internal/platform/database/models.go` 维护，API 在启动 Worker 和监听 HTTP 前执行 GORM `AutoMigrate`；失败则停止启动。`cmd/migrate` 保留为仅同步结构后退出的命令，两者均不自动创建账号。

新建空数据库后，可直接启动 API 获得空表，再通过注册页面创建用户；需要开发初始数据时，在第一次启动 API **之前**导入完整脚本：

```bash
psql -h 127.0.0.1 -p 5432 -U shiftory -d shiftory -v ON_ERROR_STOP=1 -f shiftory-server/sql/init.sql
```

端口和用户以本地配置为准。脚本包含全部 18 张表、索引、外键、状态约束，以及以下开发数据：

- 登录用户名：`admin`，邮箱：`admin@example.com`，密码：`ShiftoryDev123!`（数据库保存 Argon2id 摘要）
- 工作区：`开发工作区`；初始用户为该工作区所有者，并具有默认主题、当前工作区偏好和早／中／晚三种预设班次
- 项目不存在全局管理员角色；该用户的管理范围限于其所属工作区

数据库由实例管理员预先创建，脚本在指定数据库中创建结构并初始化自增序列，仅供空库执行，不自动重置现有数据库，不在服务启动时重复导入。请在使用后修改开发初始密码。初始化 SQL 导入后可直接启动 API，结构同步不会覆盖初始账号。

旧版 Goose SQL 和依赖已移除。AutoMigrate 不会删除废弃字段，也不负责字段重命名或旧约束的数据转换；已有开发库应先确认需保留的数据，再人工处理不兼容结构，不能通过自动清库来解决。模型结构变更时须同步更新初始化 SQL，并在隔离数据库中验证二者一致。

数据库排序规则不改变应用层规范化：用户名、邮箱和班次别名仍按现有规则转小写、去首尾空白，因此对应业务匹配不会自动变成大小写敏感。

PostgreSQL 使用 UTF8 与区分大小写、重音的字符串比较；数据库实例使用 C locale。绝对时间使用 timestamptz，排班 date／time 保留业务含义。GORM 同步后安装 updated_at 触发器，原始 SQL 更新同样维护更新时间。项目不支持 MySQL 或双数据库运行。
