# Shiftory 项目事实

## 文档索引

- [产品需求](REQUIREMENTS.md)：业务概念、权限、导入、日历、页面与验收范围，不作为功能已完成的证明。
- [模块数据范围](DATA_SCOPES.md)：个人共享数据与工作区隔离模块的接口、缓存和界面边界。
- [工程约束](ENGINEERING.md)：代码分层、数据一致性、运行约束与实现边界。
- [运行与部署](../README.md)：开发启动、配置、部署及验证命令。
- [接口契约](../shiftory-server/api/openapi.yaml)：HTTP API 定义。
- 本目录维护有效需求和长期约束，不保存既往决策过程、开发计划或完成记录。

## 架构

- 项目处于开发阶段，尚未部署。空库初始化脚本为 `shiftory-server/sql/init.sql`，包含完整结构与开发初始账号。

- 单仓库，Vue 3 前端与 Go 模块化单体后端。
- 后端长期运行入口为 `shiftory-server/cmd/api`；API 启动时通过 GORM AutoMigrate 同步表结构，一次性结构同步入口为 `shiftory-server/cmd/migrate`。
- AI 导入任务由 API 进程内的 Asynq 消费器运行，不存在独立的 `cmd/worker` 部署入口。
- PostgreSQL 保存业务数据、导入任务与事务 Outbox；Redis 保存 Asynq 消息及供应商共享调度状态；文件存储保存上传原文件。

## 运行与配置

- 后端和 AI 接入工具统一 Go 1.26.8；启动器／Docker 固定版本，直接 CLI 使用进程级 GOTOOLCHAIN=go1.26.8，GoLand 项目 SDK 使用相同版本

- 后端以 `--env development|production` 选择模式，默认 production；`--config` 可指定 YAML。非空进程环境变量优先于 YAML；不读取 env 文件或旧 `SHIFTORY_ENV` / `SHIFTORY_ENV_FILE` 选择器。
- 后端 YAML 按 log、server、postgres、jwt、ai、redis、tasks、storage 分组；worker 仅保留旧字段兼容，环境变量采用 SHIFTORY_<分组>_<字段>。server.port 指定监听端口；postgres 使用 host、port、database、user、password、sslmode，连接串统一编码，会话时区固定 UTC。不接受运行时 DSN 或平铺 YAML 键。
- 本地配置为忽略的 `config/development.yaml` / `config/production.yaml`，可分发样例为对应 `*.example.yaml`。前端保留 Vite 原有 env 文件读取方式，不读取 YAML。
- `scripts/start-dev.ps1` 负责向后端传递开发模式和 YAML 路径、执行迁移并启动 API 和前端；`-EnableAI` 只控制 API 内 AI 识别能力。
- Linux 手动部署使用 `scripts/build-release.sh` 打包二进制、前端与公开配置样例，`scripts/start-services.sh` 提供全局 PostgreSQL／Redis Docker 启动与配置，`scripts/install-service.sh` 自动准备运行环境、填写 `deploy/shiftory.service` 路径并安装启动服务，步骤见 `docs/deployment.md`；单机只运行 API 服务，`scripts/start-linux.sh` 保留单独构建、迁移和运行入口。
- 前端固定使用 `pnpm@11.19.0`；启动器支持在没有全局 pnpm/Corepack 时通过 npm 缓存降级运行。

## 验证基线

- 后端验证：`go test ./... -count=1`、`go vet ./...`。
- 架构验证：`scripts/tests/embedded-worker-architecture.Tests.ps1`。
- 开发工具与 IDE 配置验证：`scripts/tests/dev-tooling.Tests.ps1`、`scripts/tests/goland-run-config.Tests.ps1`。
- 启动器验证：`scripts/start-dev.ps1 -ValidateOnly`。
- 修改后应运行与变更范围匹配的测试，并执行 `git diff --check`。

## Git 边界

- 项目仓库使用本地 `main` 分支和 `origin` 远程。
- 提交、推送、PR 等操作必须以用户当前指令为准，不能从代码修改授权中推断出来。

- 本机 PostgreSQL 为全局共享 Docker 容器 postgres，127.0.0.1:5432；Shiftory 使用独立 shiftory 数据库及应用账号，测试使用另一个账号和三个白名单测试库。管理配置在 /home/helan/.config/shared-services/postgres；仓库 Compose 连接外部共享数据库网络，不管理数据库容器。
