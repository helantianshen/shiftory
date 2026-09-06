# Development Environment Launcher Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox ([34m- [ ][0m) syntax for tracking.

**Goal:** Provide one complete, adjustable local environment file and one PowerShell launcher for starting Shiftory migration, API, and frontend, with optional image AI execution inside the API process.

**Architecture:** Keep the backend existing SHIFTORY_* process-environment contract. The launcher parses a root-level ignored .env.development file, exports values to child processes, runs migration synchronously, and starts the API and frontend in separate PowerShell processes. Image task execution is embedded in the API and is enabled through configuration rather than a separate process.

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
- [ ] Run migrations, then start API and frontend in visible child PowerShell windows.
- [ ] Support EnvFile, SkipMigrate, BackendOnly, FrontendOnly, EnableAI, and ValidateOnly switches.

### Task 3: Document the workflow

**Files:**
- Modify: README.md

- [ ] Document editing .env.development, one-command startup, service URLs, and optional image AI enablement inside the API.

### Task 4: Verify configuration delivery

**Files:**
- Modify: .agent/HANDOFF.md

- [ ] Parse-check the PowerShell script and inspect the generated environment file.
- [ ] Run the launcher in a non-destructive validation mode or invoke its migration/API/frontend commands individually as available.
- [ ] Record validation and remaining AI-key requirements in HANDOFF.
