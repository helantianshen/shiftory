# AI 导入 Eino 重开发

更新时间：2026-10-02。

## 授权与状态

用户已明确授权“现在可以开始正式执行 spec 了”。正式代码实现及自动化验收已完成，部署验收边界见末节；不是仅接入测试阶段。范围为图片／文字完整 AI 导入、错误与重试、Asynq／Redis、审查／修改／提交／撤销。2026-10-03 用户授权将 AI/Eino 重构与 PostgreSQL 切换一起提交并推送。

权威合同：`docs/specs/ai-import-eino.md` v0.4。实际验收记录：`docs/testing/ai-eino-acceptance-2026-10-02.md`。旧接入证据保留在 `docs/testing/ai-provider-smoke-2026-10-02.md`，其简化合同不能替代正式验收。

## 已实现与文件

- `internal/ai`：Eino Workflow、Ark／OpenAI、有界响应、严格草稿／规则、升序供应商切换、共享 Redis 限流／熔断／半开、明确额度／拒绝分类、Retry-After、无隐藏 SDK 重试
- `internal/importer/imageai`：请求合同、严格 Schema、规则展开、GIF 首帧／EXIF 方向；旧 tRPC 客户端已移除
- `internal/importjob`：Asynq、事务 Outbox、代次与 token 租约、心跳／取消、有限重试、故障对账、输入冻结、预览及诊断；旧 Runner／Worker 池已移除，不再使用 ants
- 配置加载器支持 ai.providers、ai.routing、redis、tasks；旧 scalar／worker 字段仅兼容加载，正式路由不使用
- GORM 增量结构、TEXT_AI 类型、新 Outbox／attempts／provider_calls、初始化 SQL同步；API text/retry/refresh/attempts/calls/raw、审查版本、事务审计、基线与领域校验、权限及工作区删除清理
- 前端文字入口、幂等请求、规则与全时间段、跨日、人工跳过／纠正、版本冲突、刷新基线、关联新任务、诊断与原文；团队只读保持
- Compose Redis AOF／noeviction／卷、Node24、360秒停止期，systemd 停止期及 README／项目规则同步

## 配置与环境

以下环境与验收记录描述 2026-10-02 AI 重构完成时的状态；后续数据库已切换至 PostgreSQL，当前状态见 `postgresql-switch.md` 和 `docs/testing/postgresql-switch-2026-10-03.md`。

- 实际 `config/development.yaml` 已合并 AI 实例和 Redis／tasks，原其他组保持；实际 `config/ai.development.yaml` 仍为独立联网工具与测试配置。两份均 0600 且被 Git 忽略，样例无真实密钥。两份不会自动合并，后续修改应同步
- 三类供应商 doubao order10、relay order20、deepseek order40，StepFun 按用户要求已移除。Ark thinking disabled；relay OpenAI Chat Completions `/v1`，允许单完整围栏再严格校验
- 本地 API 配置 MySQL 13306；Redis 使用已获授权的 gantry-redis-1，127.0.0.1:16379 DB1。共享 Redis AOF off，未修改或重启共享服务
- 测试 MySQL8容器 gantry-mysql-1，13306；测试只用隔离 shiftory_test*，Redis DB15 独立测试队列，未触碰业务排班
- 系统默认 Go1.27.1；项目已按用户要求固定 Go1.26.8（模块、启动器、Docker、本地 GoLand SDK），直接 CLI 使用 GOTOOLCHAIN=go1.26.8。默认 Node shim18不适合构建，实际使用 `/home/helan/.cache/codex-runtimes/codex-primary-runtime/dependencies/node/bin/node` v24运行 vue-tsc/Vite/Vitest。没有 pwsh

## 验证及后续边界

- 全量 Go 与初始化 SQL一致性通过；并发路径 race通过。真实正式六个供应商合同通过，完整文字／图片生成、审核、提交、撤销最新总计11.06秒通过
- 前端11文件28项测试、类型检查和生产构建通过；Go vet/build、Compose config及diff检查通过
- 消费器重建、Redis连接拒绝保留Outbox、无调用不耗轮次、重复投递、取消、预算、过期token／代次、历史接纳测试通过
- 最终 `go test -race ./... -count=1` 全包通过，包含新增工作区原文／执行记录清理及事务终态；没有 race 报告
- 未对业务库执行迁移或重启正在运行的 API；下次启动自动增量同步。历史旧任务仅通过显式离线 `cmd/migrate --migrate-ai-jobs` 接纳，不能与旧调度器并行
- 未执行实际API强杀、共享Redis重启、生产持久卷恢复、PowerShell和真实浏览器验收。浏览器工具初始化超时；不能把单元／HTTP验证写成这些部署场景通过
- 不实现 Redis 极端数据丢失自动队列重建，失联任务失败后可人工新代次重试

## 工具链切换结果

2026-10-02 按用户要求改用 Go1.26，选用已可用的 Go1.26.8。主模块全量 race（含MySQL8／Redis）、独立AI模块race、两模块vet／mod verify通过。Linux构建产物版本确认1.26.8，启动帮助无Sonic警告。JWT仅改显式内嵌字段初始化，冷却测试延长测试窗口；不更改签发字段或生产冷却配置。未改系统默认Go、未重启业务服务、未重复联网AI请求。验收详情见现有记录的Go1.26复验节。

## GoLand 本地运行配置复核

2026-10-02 用户启动日志仍为Go1.27和production。当前临时API运行项没有参数；开发配置只读Ping到13306成功，生产配置到3306复现1045。新增共享 `.run/Shiftory_API.run.xml`，显式development配置与GOTOOLCHAIN=go1.26.8；本地临时API及默认Go运行／测试模板同步固定工具链。以 `/usr/lib/go/bin/go` 和旧GOROOT启动并设置GOTOOLCHAIN，实际Go1.26.8可运行连接检查且无Sonic警告。XML检查通过；未在IDE中实际点击重跑，未启动API或迁移业务库。共享运行检查的API断言已允许唯一GOTOOLCHAIN，PowerShell未执行，其他共享运行配置目前缺失。
