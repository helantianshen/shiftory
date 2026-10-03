# PostgreSQL 全面切换

授权：2026-10-02 用户授权全面替换 MySQL，并新建本机全局共享 PostgreSQL Docker 容器；2026-10-03 用户授权将 AI/Eino 重构与 PostgreSQL 切换一起提交并推送。

范围：保留 database/sql、GORM AutoMigrate、Redis/Asynq、事务与 HTTP 合同。保留已有未提交修改，不迁移或删除旧 MySQL 数据。

状态：已完成；2026-10-03 补充浏览器验收与最终运行状态检查。

环境：Linux amd64，命令 Bash；Go 使用进程级 GOTOOLCHAIN=go1.26.8。共享容器 postgres，127.0.0.1:5432，postgres:18，postgres-data 持久卷；管理配置在 /home/helan/.config/shared-services/postgres，凭据不入仓库。Shiftory 独立账号、业务库及白名单测试库。

方案：时间戳 timestamptz；业务 date/time 保持；文本比较区分大小写与重音；触发器维护 updated_at。

实现：仅保留 PostgreSQL 驱动与配置；转换全部 SQL 调用、GORM 模型、初始化 SQL、唯一冲突处理及测试辅助设施。时间戳、序列、约束和更新时间触发器均通过实际 PostgreSQL 验证。共享服务 Compose 位于仓库外；项目 Compose 连接外部数据库网络。

验证：2026-10-02 全量 Go 测试及 race、vet、build、依赖校验通过；实际 PostgreSQL/Redis 集成、并发排班与刷新令牌、真实 AI 文本/图片导入提交回滚链路通过；前端类型检查、28 项测试和生产构建通过。2026-10-03 浏览器登录、保存排班及刷新保留通过；ai-smoke test/vet 与共享容器、数据库权限检查通过。

交付记录：docs/testing/postgresql-switch-2026-10-03.md。业务 shiftory 库已创建 18 张表，未写入默认账号或旧业务数据；浏览器种子与排班仅在 shiftory_test_httpserver 中。临时验收 API 已关闭；共享 postgres 保持运行。

边界：未执行 Git 提交、推送或生产部署；未启动项目 Compose 应用栈；环境无 pwsh，未执行 PowerShell 测试。旧 MySQL 服务及已有未提交修改保留。
