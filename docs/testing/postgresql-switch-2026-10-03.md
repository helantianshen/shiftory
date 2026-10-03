# PostgreSQL 切换验收

实现与自动化验收日期：2026-10-02；浏览器和运行状态补充验收日期：2026-10-03。

## 范围与结果

Shiftory 已切换为单一 PostgreSQL 路径：pgx database/sql 驱动、GORM PostgreSQL 驱动、postgres 配置组及环境变量、原生 PostgreSQL 查询与初始化 SQL。旧 mysql 配置会被拒绝。保留原有 HTTP、事务、Redis/Asynq 与权限合同，没有迁移旧 MySQL 业务数据。

## 本机共享服务

- 容器：postgres；镜像：postgres:18；实际服务器：PostgreSQL 18.6
- 地址：127.0.0.1:5432；重启策略：unless-stopped；检查时 healthy
- 数据卷：postgres-data；Docker 网络：shared-postgres_default
- 全局管理文件：/home/helan/.config/shared-services/postgres/compose.yaml
- 管理密码：同目录 password 文件，权限 0600；应用密码在本机私有 YAML 配置中，未写入本文
- 业务数据库与账号：shiftory；测试账号：shiftory_test
- 独立测试数据库：shiftory_test、shiftory_test_httpserver、shiftory_test_importjob

应用和测试账号均无超级用户与 CREATEDB 权限。业务与测试库撤销 PUBLIC CONNECT；实际检查双方不能连接对方数据库。业务库存在 18 张表，users 为 0 行。其他项目应创建自己的账号和数据库。共享服务的生命周期独立于本项目，不使用 down -v 删除共享数据卷。

## 已通过的检查

2026-10-02：

- Go 全量测试、全量 race 测试（启用真实 PostgreSQL 与 Redis）、go vet、API 构建、go mod verify
- 空库与幂等迁移，初始化 SQL 与模型元数据一致，序列前进、UTC、更新时间触发器、外键/唯一/非负 ID/哈希长度约束
- 8 路并发排班创建和更新的冲突控制、8 路刷新令牌消费与重放控制
- 实际 AI 文本和图片导入：模型调用、Redis 队列、PostgreSQL 草稿、复核、提交、回滚；LiveAIImportWorkflow 通过
- 前端类型检查、11 个测试文件共 28 项测试、生产构建
- 项目与共享服务 Compose 配置检查、Bash 启动脚本语法、IDE XML 格式检查

2026-10-03：

- tools/ai-smoke 的 go test ./... 与 go vet ./...
- 浏览器连接临时 API 18080，在 shiftory_test_httpserver 使用开发种子账号登录
- 保存 2026-10-03 的休息排班与验收备注；页面提示保存成功，刷新仍显示休息排班
- 共享服务健康、版本、18 张业务表、业务用户数与账号隔离检查

提交前复测复现了并发首次创建排班的偶发 500：两个事务均读取到记录缺失，随后插入竞争唯一约束。创建 SQL 使用 `ON CONFLICT (user_id, work_date) DO NOTHING RETURNING id`，未插入时映射为既有版本冲突 409，事务回滚。修复后两项 PostgreSQL 并发测试在 race 模式连续运行 20 次通过，全量 Go/race 测试再次通过。前端类型检查和 28 项测试再次通过，Go vet 与 API 构建通过。本轮未启用已停止的共享 Redis 或重复外部 AI 调用。

浏览器截图保存在 /tmp/shiftory-postgresql-browser.png。临时 API 验收完成后关闭，共享 postgres 保持运行。

## 验证边界

没有执行生产部署或启动项目 Compose 应用栈；没有 Git 提交、推送。环境没有 pwsh，PowerShell 测试未执行。前端构建仍存在原有包体积提示。真实 AI/Redis 验收在 10 月 2 日完成；10 月 3 日浏览器验收关闭 AI，未重复外部模型调用。10 月 2 日的 /tmp 测试日志在跨日恢复时已不存在，本文记录当时实际执行结果。

旧 MySQL 数据与容器保留，已有未提交的 AI 等功能修改保留。历史 MySQL 验收文档保留为历史证据，不代表当前数据库路径。
