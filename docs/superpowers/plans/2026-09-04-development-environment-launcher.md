# Development Environment Launcher Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox ([34m- [ ][0m) syntax for tracking.

**Goal:** Provide one complete, adjustable local environment file and one PowerShell launcher for starting Shiftory migration, API, frontend, and optional AI worker.

**Architecture:** Keep the backend existing SHIFTORY_* process-environment contract. The launcher parses a root-level ignored .env.development file, exports values to child processes, runs migration synchronously, and starts each long-running service in its own PowerShell process.

**Tech Stack:** PowerShell, Go, Vite, pnpm, MySQL 8.4.

---

### Task 1: Add adjustable development environment

**Files:**
- Create: .env.development
- Modify: .env.example

- [ ] Add all backend, JWT, storage, worker, and frontend-related values with safe local defaults and a blank AI key.
- [ ] Replace the tracked example AI key with a placeholder so no credential is distributed.

### Task 2: Add one-command PowerShell launcher

**Files:**
- Create: scripts/start-dev.ps1

- [ ] Parse KEY=VALUE entries while ignoring comments and blank lines.
- [ ] Load the selected env file into the current process environment.
- [ ] Run migrations, then start API and frontend in visible child PowerShell windows; optionally start the worker.
- [ ] Support EnvFile, SkipMigrate, BackendOnly, FrontendOnly, and WithWorker switches.

### Task 3: Document the workflow

**Files:**
- Modify: README.md

- [ ] Document editing .env.development, one-command startup, service URLs, and optional worker startup.

### Task 4: Verify configuration delivery

**Files:**
- Modify: .agent/HANDOFF.md

- [ ] Parse-check the PowerShell script and inspect the generated environment file.
- [ ] Run the launcher in a non-destructive validation mode or invoke its migration/API/frontend commands individually as available.
- [ ] Record validation and remaining AI-key requirements in HANDOFF.

