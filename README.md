# Shiftory

Shiftory 是面向小团队的排班协同应用。产品范围包括 JWT 登录、多工作区与角色权限、手动排班、团队日历、XLSX/XLS 导入、图片 AI 异步识别、人工预览纠错、事务提交与全量撤销，以及六套浅色主题；功能完成度以当前源码与实际验收为准。

## 技术结构

- 前端：Vue 3、TypeScript、Vite、Vue Router、Pinia、TanStack Vue Query、Element Plus、SCSS。
- 后端：Go 1.27、Gin、`database/sql`、MySQL 8、GORM AutoMigrate、Excelize、tRPC-Agent-Go。
- 认证：Ed25519 Access JWT + 轮换 Refresh JWT。Access Token 只驻留前端内存；Refresh Token 只在 HttpOnly Cookie 中，刷新和注销另有 CSRF 双提交校验。
- 工程形态：单仓库、前后端分离、模块化单体；API 进程内置图片识别 Worker，迁移命令独立执行。

### 后端分层约定

后端采用按领域模块组织的 MVC 三层边界：`internal/platform/httpserver` 是 Controller/HTTP 适配层，负责路由、权限前置检查、HTTP 状态码和统一 JSON 错误；`internal/importer`、`internal/schedule` 等包负责工作簿规范化和领域规则，不依赖 Gin 或 HTTP；数据库访问和外部存储由平台适配代码提供。导入校验错误在 importer 层使用结构化 `ValidationError`（行号、字段、规则码、修复提示），再由 Controller 映射为 `error.details`，避免领域层泄露传输细节或内部堆栈。

当前历史路由仍按功能文件集中在 `httpserver` 中，新增或重构接口应沿用上述边界；不通过一次性搬迁制造跨模块回归。

项目资料统一维护在 [.agent 项目索引](.agent/PROJECT.md)，包括 [产品需求](.agent/REQUIREMENTS.md) 与 [工程约束](.agent/ENGINEERING.md)；接口契约见 [OpenAPI 3.1](shiftory-server/api/openapi.yaml)。

## 本地启动

前置环境：Go 1.27.1、Node.js 24（含 npm）、MySQL 8.0.16+（Compose 使用 8.4）。后端使用 **进程环境变量 + YAML 配置文件**，前端继续使用 Vite 原有的 env 文件加载方式，不读取 YAML。

后端 API 和迁移程序接受相同的启动参数：

| 参数 | 作用 |
| --- | --- |
| `--env production` | 生产模式，省略参数时的默认值 |
| `--env development` | 开发模式 |
| `--config /path/to/config.yaml` | 显式指定 YAML 文件；不改变 `--env` 选择的模式 |

后端配置优先级为：**非空进程环境变量 > 所选 YAML > 内置默认值**。YAML 使用 `log`、`server`、`mysql`、`jwt`、`ai`、`storage`、`worker` 分组，环境变量按 `SHIFTORY_<分组>_<字段>` 命名，例如 `ai.model` 对应 `SHIFTORY_AI_MODEL`、`server.port` 对应 `SHIFTORY_SERVER_PORT`。空白环境变量沿用文件值；布尔值 `false` 可正常覆盖 `true`。密钥和数据库密码保留原始空白，YAML 中显式空密码表示无密码。可配置项列在 `config/development.example.yaml` 和 `config/production.example.yaml` 中。

MySQL 使用 `mysql.host`（IP 或主机名）、`mysql.port`、`mysql.database`、`mysql.user`、`mysql.password`，对应 `SHIFTORY_MYSQL_HOST`、`SHIFTORY_MYSQL_PORT`、`SHIFTORY_MYSQL_DATABASE`、`SHIFTORY_MYSQL_USER`、`SHIFTORY_MYSQL_PASSWORD`。程序统一生成连接串，固定 `utf8mb4`、时间解析与 UTC 时区，不接受运行时 DSN、字符集或时区配置。`server.port` 是监听所有网卡的端口；服务与数据库端口范围均为 1–65535。旧平铺 YAML 会报错，旧 `SHIFTORY_HTTP_ADDR`、`SHIFTORY_DATABASE_DSN`、`SHIFTORY_WEB_DIR`、`SHIFTORY_UPLOAD_DIR`、`SHIFTORY_PUBLIC_ORIGIN` 不再读取。

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

图片 AI Worker 运行在 API 进程内。设置 `SHIFTORY_AI_ENABLED=true`、模型名称和 API Key 后启用；未启用时普通排班和 Excel 导入仍可用。`SHIFTORY_WORKER_MAX_CONCURRENCY` 控制同时处理的图片任务数，`SHIFTORY_WORKER_LEASE` 控制任务处理租约，`SHIFTORY_WORKER_POLL_INTERVAL` 控制无唤醒信号时的数据库扫描周期。

如本机没有 MySQL，可在仓库根目录启动容器：

```powershell
$env:COMPOSE_DISABLE_ENV_FILE = "1" # Compose 只使用当前进程环境变量
$env:MYSQL_ROOT_PASSWORD = "123456" # 与开发 YAML 示例一致；生产环境必须自行设置
docker compose up -d mysql
```

默认数据库连接为 `root:123456@127.0.0.1:3306/shiftory`。首次启动前创建数据库（容器配置会自动创建），然后运行：

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

Linux 单机只需要运行 API 二进制，图片 AI Worker 会在同一进程内启动。将生产 YAML 放到 `/etc/shiftory/production.yaml`，通过 `--config` 指定其绝对路径；`SHIFTORY_STORAGE_UPLOAD_DIR`、`SHIFTORY_SERVER_WEB_DIR` 和 JWT 密钥路径也建议使用绝对路径。API 与 Worker 必须共享同一个上传目录。

```bash
./scripts/start-linux.sh build
./scripts/start-linux.sh migrate --config /etc/shiftory/production.yaml
sudo install -m 0755 deploy/shiftory.service /etc/systemd/system/shiftory.service
sudo systemctl daemon-reload
sudo systemctl enable --now shiftory
```

`deploy/shiftory.service` 默认使用 `/opt/shiftory/bin/shiftory-api`、`/etc/shiftory/production.yaml` 和 `shiftory` 系统用户；如果安装目录不同，请同步调整 unit 文件。数据库迁移只在发布时执行，不由 systemd 每次重启重复触发。

### Docker

镜像将公开的生产 YAML 样例安装为 `/app/config/production.yaml`，Compose 默认传入生产模式。可将自己的 YAML 只读挂载到该路径，并用 Compose 的 `environment` 注入数据库密码、AI Key 等覆盖值。修改 `command` 时，启动参数会同时传递给迁移和 API。

```bash
COMPOSE_DISABLE_ENV_FILE=1 docker compose up -d --build
# 仅检查 Compose 配置，不启动容器：
COMPOSE_DISABLE_ENV_FILE=1 docker compose config --quiet
```

Compose 自身具有隐式读取根目录 `.env` 的行为，因此部署命令显式设置 `COMPOSE_DISABLE_ENV_FILE=1`。这不会禁用前端 Vite 读取 `shiftory-web/.env*`。

## 图片 AI Worker

设置 OpenAI 兼容视觉模型参数后，启动 API 即会同时启动内置 Worker：

```powershell
$env:SHIFTORY_AI_MODEL = "your-vision-model"
$env:SHIFTORY_AI_BASE_URL = "https://your-provider.example/v1"
$env:SHIFTORY_AI_API_KEY = "your-key"
$env:SHIFTORY_AI_ENABLED = "true"
go run ./cmd/api --env development
```

图片原文件以内联多模态消息发送；模型被要求按严格 JSON Schema 输出，温度为 0。内置 Worker 使用 MySQL 租约、心跳、重试延迟和 `SKIP LOCKED` 领取任务，并通过固定容量的 ants 协程池限制并发。模型结果只生成预览，不会直接写入正式排班；不确定项必须由用户人工修正或保持跳过。AI 供应商的在线连通性需要使用实际密钥单独验收。

## 验证

后端集成测试需要已有可访问的 MySQL，会创建专用测试库。`SHIFTORY_TEST_DATABASE_DSN` 供全部后端数据库集成测试复用连接凭据与地址；测试只使用 `shiftory_test`、`shiftory_test_httpserver`、`shiftory_test_importjob` 专用库，不使用 DSN 指定的业务库。该变量仅供测试使用，需通过进程环境变量提供，不从运行时 YAML 或 env 文件读取：

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
- JWT 私钥、上传文件、构建产物和本地 YAML 和前端 env 文件已排除在 Git 跟踪之外；不要提交真实密钥。

## 数据库初始化与自动迁移

项目仍处于开发阶段，尚未部署。表结构由 `shiftory-server/internal/platform/database/models.go` 维护，API 在启动 Worker 和监听 HTTP 前执行 GORM `AutoMigrate`；失败则停止启动。`cmd/migrate` 保留为仅同步结构后退出的命令，两者均不自动创建账号。

新建空数据库后，可直接启动 API 获得空表，再通过注册页面创建用户；需要开发初始数据时，在第一次启动 API **之前**导入完整脚本：

```bash
mysql -h 127.0.0.1 -P 13306 -u root -p < shiftory-server/sql/init.sql
```

端口和用户以本地配置为准。脚本包含全部 15 张表、索引、外键、状态约束，以及以下开发数据：

- 登录用户名：`admin`，邮箱：`admin@example.com`，密码：`ShiftoryDev123!`（数据库保存 Argon2id 摘要）
- 工作区：`开发工作区`；初始用户为该工作区所有者，并具有默认主题、当前工作区偏好和早／中／晚三种预设班次
- 项目不存在全局管理员角色；该用户的管理范围限于其所属工作区

脚本会创建并选择 `shiftory` 数据库，仅供空库执行，不自动重置现有数据库，不在服务启动时重复导入。请在使用后修改开发初始密码。初始化 SQL 导入后可直接启动 API，结构同步不会覆盖初始账号。

旧版 Goose SQL 和依赖已移除。AutoMigrate 不会删除废弃字段，也不负责字段重命名或旧约束的数据转换；已有开发库应先确认需保留的数据，再人工处理不兼容结构，不能通过自动清库来解决。模型结构变更时须同步更新初始化 SQL，并运行数据库集成测试验证二者一致。

项目统一使用 `utf8mb4_0900_as_cs`：大小写和重音字符均区分（`A` 与 `a`、`e` 与 `é` 均不相等）。应用连接、建表、初始化脚本和 Compose 默认保持一致。仅修改数据库默认排序规则或 GORM 新表选项不会转换已有文本列；已有库应单独检查并转换表结构，本项目不会在每次启动时重建表。

数据库排序规则不改变应用层规范化：用户名、邮箱和班次别名仍按现有规则转小写、去首尾空白，因此对应业务匹配不会自动变成大小写敏感。
