# Shiftory 项目事实

## 文档索引

- [产品需求](REQUIREMENTS.md)：业务概念、权限、导入、日历、页面与验收范围，不作为功能已完成的证明。
- [工程约束](ENGINEERING.md)：代码分层、数据一致性、运行约束与实现边界。
- [运行与部署](../README.md)：开发启动、配置、部署及验证命令。
- [接口契约](../shiftory-server/api/openapi.yaml)：HTTP API 定义。
- 本目录维护有效需求和长期约束，不保存既往决策过程、开发计划或完成记录。

## 架构

- 单仓库，Vue 3 前端与 Go 模块化单体后端。
- 后端长期运行入口为 `shiftory-server/cmd/api`；一次性数据库迁移入口为 `shiftory-server/cmd/migrate`。
- 图片导入任务 Worker 作为 API 进程内的持久化 Runner 运行，不存在独立的 `cmd/worker` 部署入口。
- MySQL 保存业务数据和图片导入任务队列；文件存储保存上传原文件。

## 运行与配置

- 后端以 `--env development|production` 选择模式，默认 production；`--config` 可指定 YAML。非空进程环境变量优先于 YAML；不读取 env 文件或旧 `SHIFTORY_ENV` / `SHIFTORY_ENV_FILE` 选择器。
- 本地配置为忽略的 `config/development.yaml` / `config/production.yaml`，可分发样例为对应 `*.example.yaml`。前端保留 Vite 原有 env 文件读取方式，不读取 YAML。
- `scripts/start-dev.ps1` 负责向后端传递开发模式和 YAML 路径、执行迁移并启动 API 和前端；`-EnableAI` 只控制 API 内图片识别能力。
- Linux 部署使用 `deploy/shiftory.service` 与 `scripts/start-linux.sh`，单机只运行 API 服务。
- GoLand 共享运行配置位于 `.run/`，包含 API、迁移、前端和开发组合配置。
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
