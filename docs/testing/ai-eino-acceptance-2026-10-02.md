# AI Eino 重构验收记录

日期：2026-10-02。范围依据 `docs/specs/ai-import-eino.md`，记录代码实现与实际检查，不代表生产部署验收。

## 已实现

- 正式模块改用 Eino Workflow、Ark／OpenAI 模型适配；移除 tRPC-Agent-Go、旧 Runner 和 ants 池。供应商按唯一正整数 order 升序，字节、中转站、DeepSeek，独立配置文字／图片能力、输出格式、思考、超时、并发。
- 图片原文件保留；GIF 仅首帧、JPEG EXIF 方向校正。模型原文有界保存、严格 JSON 与单完整围栏兼容；业务疑问按日期保留，不用模型切换掩盖业务问题。
- 文字输出 RuleSet，确定性展开星期、连续区间、单日例外和有锚点周期；逐日保留 ruleIds，缺失不当作休息。相对时刻与班次映射在创建时冻结。
- MySQL 创建任务与 Outbox 同事务，Asynq／Redis 唯一调度；代次、随机 token、有效租约、心跳、取消隔离及有限重试。无调用的供应商等待不消耗识别轮次；重复消息短路。失联／归档转失败，可人工重试新代次。
- Redis 共享供应商并发、冷却、半开探测及配置失效；密钥变更更新非敏感配置指纹。明确错误码区分额度和内容拒绝，普通 429 不直接判定额度耗尽；Retry-After 生效。
- 审查版本、人工纠正、SAME 修改、基线刷新、独立新任务再生成、事务提交／整批撤销、零写入计数、审计及权限回归。决策／修正返回新 reviewVersion，前端保存后提交使用返回版本，避免刷新时序问题；修正前保留未保存决策。提交核对所有未跳过可信日期的基线。
- 执行轮次、调用诊断、受权限保护的模型原文、就绪和聚合指标；解散工作区按依赖顺序删除新增 AI 记录。历史规范化 ai_raw_response 以 legacyResponseFormat 标明，不当作模型原文。
- 配置加载器、OpenAPI、初始化 SQL、前端、Compose Redis 持久卷／AOF／noeviction、服务停止期限及运行说明已同步。历史任务只通过显式离线迁移接纳。

## 检查与证据

| 检查 | 结果与范围 |
| --- | --- |
| 全量 Go 测试 | MySQL 8 隔离测试库及 Redis DB 15，`go test ./... -count=1` 通过；模型与初始化 SQL 一致性通过 |
| 并发回归 | 最终 `go test -race ./... -count=1` 全包通过；覆盖 AI、规则、队列、HTTP；共享限流／半开／密钥更新、重复投递、取消、预算、租约与代次隔离通过 |
| 队列故障 | 连接拒绝故障下保留 Outbox，恢复连接后投递；消费器关闭重建后处理保留消息通过；没有重启共享 Redis |
| 历史接纳 | 过期旧任务固定输入、新代次 Outbox、再次执行不重复接纳通过 |
| 正式供应商合同 | doubao／relay／deepseek 各文字、合成图片通过；此前 doubao 跨优先级替代误拒绝已修正并复测 |
| 真实完整链路 | `TestLiveAIImportWorkflow` 最新复测通过，文字 5.46 秒、图片 4.25 秒，总计 11.06 秒；实际 Eino→Asynq→Redis→MySQL 草稿→HTTP 审核／提交→撤销 |
| Go 静态与构建 | `go vet ./...`、API／migrate 构建通过 |
| 前端 | 11 个文件、28 项测试通过；含团队只读、多时间段／跨日、文字无下载入口；vue-tsc 与 Vite 生产构建通过 |
| 配置 | 实际开发配置加载通过，3 个启用供应商同时支持文字与图片；Redis 16379 DB 1；实际配置 0600 且 Git 忽略 |
| 部署静态检查 | `docker compose config --quiet`、`git diff --check` 通过 |

正式联网测试为显式 `SHIFTORY_LIVE_AI=true` 才执行；常规测试不请求真实模型。完整链路只使用合成描述、合成图片和隔离数据库，不修改业务排班。曾发现规则枚举遗漏、错误链丢失及跨级替代校验过严，已分别用合同、错误链回归和真实请求验证修复。

## 配置与运行边界

实际 `config/ai.development.yaml` 的 ai／redis／tasks 已合并进被忽略的 `config/development.yaml`；其他组保持原值。API 使用后者，独立联网测试使用前者，两份不自动合并。后续改模型和优先级时需同步正式开发配置并重启进程。

本地共享 Gantry Redis 的 AOF 关闭，没有调整其持久化策略。Compose 提供专用 Redis 的 AOF everysec、持久卷和 noeviction，但未启动部署验收。未对业务库执行迁移；API 下次启动自动增量同步，历史旧任务须停止旧 Worker 后显式执行 `--migrate-ai-jobs`。

## 未执行及限制

- 浏览器工具初始化超时，真实浏览器交互验收未通过该工具完成；前端组件测试及真实 HTTP 链路已执行
- 本机无 PowerShell，PowerShell 专项脚本未执行；架构入口及 Compose 静态检查不能替代该脚本
- 未强杀实际 API、重启共享 Redis 或验证生产持久卷恢复；已有消费器重建、连接失败和过期 token／租约隔离测试，不把它们等同于这些部署场景
- 真实用户截图准确率、所有供应商限额策略、免费资格及正式部署备份／恢复未验证
- Vite 提示大于 500 kB 的资源块；Go 1.27 下 Sonic 提示兼容范围并回退 encoding/json，测试正常通过

没有 Git 提交、推送或远程操作。

## Go 1.26 工具链复验

用户要求改用 Go 1.26 后，两个模块、Linux／PowerShell 启动器和 Docker 已统一至 1.26.8；本地 GoLand 项目 SDK 也已切换，系统全局默认 Go 未更改。直接 CLI 使用进程级 `GOTOOLCHAIN=go1.26.8`。

JWT 声明改为显式初始化内嵌 RegisteredClaims，修复 Go 1.26 编译不兼容，签发字段不变。冷却测试由 50 毫秒改为一分钟，避免测试执行时间超过窗口；生产冷却配置不变。

- `GOTOOLCHAIN=go1.26.8 go test -race ./... -count=1` 主模块全包通过，MySQL8隔离测试库／Redis DB15参与验收，无 Sonic 兼容警告
- 独立 AI 测试模块 race 通过；两个模块 vet 和 go mod verify 通过
- `scripts/start-linux.sh build` 通过；`go version -m` 确认 API／migrate 均为 go1.26.8，API `--help` 无 Sonic 警告
- Bash语法、Compose配置、GoLand XML及 diff检查通过；PowerShell 和 Docker 镜像完整构建未执行
- 本轮不重新发起真实供应商请求，先前联网验收保留为 Go1.27 环境下的证据
