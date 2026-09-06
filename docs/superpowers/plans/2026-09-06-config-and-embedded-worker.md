# 配置自动加载与内置 Worker 实现计划

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 让 API/图片识别 Worker 在单机部署中由一个进程统一运行，并支持系统环境变量优先、指定 env 文件兜底以及可配置的 Worker 执行参数。

**Architecture:** Go 进程先读取显式 `SHIFTORY_ENV_FILE`，否则按约定目录搜索 env 文件；同名系统环境变量始终覆盖文件值。API 启动时创建 MySQL、文件存储、AI 客户端和固定容量 ants 协程池，图片任务仍以 MySQL 为可靠队列，Channel 只负责唤醒处理循环。

**Tech Stack:** Go 1.27.1、Gin、MySQL 8.4、`ants/v2`、现有 importjob Worker、PowerShell 开发脚本、systemd/Docker 环境变量文件。

---

### Task 1: 完善 env 文件加载与配置优先级

**Files:**
- Modify: `shiftory-server/internal/platform/config/config.go`
- Test: `shiftory-server/internal/platform/config/config_test.go`
- Modify: `.env.example`
- Modify: `README.md`

- [x] **Step 1: Write the failing tests**
- [x] **Step 2: Run config tests and verify env-file tests fail**
- [x] **Step 3: Implement explicit and automatic env-file loading**
- [x] **Step 4: Run config tests and verify they pass**
- [x] **Step 5: Document lookup order and production path**

### Task 2: Add configurable Worker timing and fixed pool capacity

**Files:**
- Modify: `shiftory-server/internal/platform/config/config.go`
- Test: `shiftory-server/internal/platform/config/config_test.go`
- Modify: `shiftory-server/internal/importjob/worker.go`
- Test: `shiftory-server/internal/importjob/worker_test.go`
- Modify: `shiftory-server/go.mod`
- Modify: `shiftory-server/go.sum`

- [x] **Step 1: Write failing tests for duration and positive integer parsing**
- [x] **Step 2: Run tests and verify missing configuration behavior fails**
- [x] **Step 3: Add `SHIFTORY_WORKER_POLL_INTERVAL`, `SHIFTORY_WORKER_LEASE`, `SHIFTORY_WORKER_MAX_CONCURRENCY`, and AI timeout fields**
- [x] **Step 4: Run config and Worker tests**

### Task 3: Embed the Worker in the API process

**Files:**
- Modify: `shiftory-server/cmd/api/main.go`
- Create: `shiftory-server/internal/importjob/runner.go`
- Test: `shiftory-server/internal/importjob/runner_test.go`
- Modify: `shiftory-server/internal/platform/httpserver/import_routes.go`

- [x] **Step 1: Write failing runner lifecycle and wakeup tests**
- [x] **Step 2: Run tests and verify the embedded runner is absent**
- [x] **Step 3: Implement a bounded ants pool and non-blocking wakeup channel**
- [x] **Step 4: Start the runner from API startup only when AI is enabled**
- [x] **Step 5: Ensure image task creation signals after transaction commit**
- [x] **Step 6: Add graceful shutdown and wait for active jobs**
- [x] **Step 7: Run importjob and HTTP integration tests**
- [x] **Step 8: Remove the superseded standalone Worker command**

### Task 4: Align development and Linux deployment configuration

**Files:**
- Modify: `scripts/start-dev.ps1`
- Modify: `.env.example`
- Modify: `README.md`
- Create: `scripts/start-linux.sh`
- Create: `deploy/shiftory.service`

- [x] **Step 1: Add worker settings to the example env file**
- [x] **Step 2: Make the development script pass the explicit env-file path**
- [x] **Step 3: Add Linux build, migrate, and service startup commands**
- [x] **Step 4: Document absolute paths, shared uploads, JWT key persistence, and service order**
- [x] **Step 5: Run all backend tests and frontend verification**

### Task 5: Final verification

- [x] **Step 1: Run `go test ./... -count=1`**
- [x] **Step 2: Run frontend test, type-check, and build commands**
- [x] **Step 3: Run `git diff --check` and inspect the final diff**
- [ ] **Step 4: Commit the completed implementation locally**
