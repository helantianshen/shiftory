# Embedded Worker Architecture Cleanup Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Remove every deployable standalone Worker artifact and make scripts, documentation, and automated checks consistently describe the API-embedded image task runner.

**Architecture:** The API binary is the only long-running backend process and conditionally owns the image task `Runner`; the migration binary remains a one-shot deployment command. Internal Worker, lease, polling, and pool concepts remain because they implement durable asynchronous task execution inside the API process, while all independent Worker commands and launch options are removed.

**Tech Stack:** Go 1.27.1, PowerShell, ants/v2, MySQL 8.4, GoLand shared run configurations.

---

### Task 1: Add an executable architecture invariant

**Files:**
- Create: `scripts/tests/embedded-worker-architecture.Tests.ps1`

- [x] **Step 1: Write the failing architecture test**

```powershell
if (Test-Path "$repositoryRoot/shiftory-server/cmd/worker") {
    throw "Standalone Worker command directory must not exist."
}
if ((rg -n "cmd[/\\]worker|WithWorker" $repositoryRoot)) {
    throw "Standalone Worker launch references must not exist."
}
$packages = & go list ./...
if ($packages -contains "shiftory-server/cmd/worker") {
    throw "Standalone Worker package must not be buildable."
}
```

- [x] **Step 2: Run the test and verify the current architecture fails**

Run: `powershell -NoProfile -ExecutionPolicy Bypass -File scripts/tests/embedded-worker-architecture.Tests.ps1`

Expected: FAIL because `shiftory-server/cmd/worker` and `WithWorker` still exist.

### Task 2: Remove the standalone process surface

**Files:**
- Delete: `shiftory-server/cmd/worker/main.go`
- Modify: `scripts/start-dev.ps1`
- Modify: `deploy/shiftory.service`
- Modify: `README.md`

- [x] **Step 1: Delete the independent Worker command**

Remove `shiftory-server/cmd/worker/main.go` so `go list ./...` exposes only `cmd/api` and `cmd/migrate` as backend commands.

- [x] **Step 2: Rename the development override to the behavior it controls**

```powershell
param([switch]$EnableAI)
if ($EnableAI) {
    [Environment]::SetEnvironmentVariable("SHIFTORY_AI_ENABLED", "true", "Process")
}
```

Remove the obsolete warning about not opening a separate Worker window. Update the README command example from `-WithWorker` to `-EnableAI` and describe it as enabling the API's image recognition capability.

- [x] **Step 3: Run the architecture test and verify it passes for executable code**

Run: `powershell -NoProfile -ExecutionPolicy Bypass -File scripts/tests/embedded-worker-architecture.Tests.ps1`

Expected: PASS after all remaining documentation references are aligned in Task 3.

### Task 3: Align design records and verify the full repository

**Files:**
- Modify: `docs/superpowers/plans/2026-09-04-development-environment-launcher.md`
- Modify: `docs/superpowers/plans/2026-09-04-shiftory-full-mvp.md`
- Modify: `docs/superpowers/plans/2026-09-06-config-and-embedded-worker.md`
- Modify: `.agent/HANDOFF.md`
- Modify: `scripts/tests/embedded-worker-architecture.Tests.ps1`

- [x] **Step 1: Replace obsolete independent Worker instructions**

Update the launcher plan to start migration, API, and frontend only and use `EnableAI`. Update the original MVP plan so the durable Worker is wired into `cmd/api/main.go`, and record deletion of the superseded command in the embedded-Worker plan.

- [x] **Step 2: Verify the architecture invariant is green**

Run: `powershell -NoProfile -ExecutionPolicy Bypass -File scripts/tests/embedded-worker-architecture.Tests.ps1`

Expected: `PASS: embedded Worker architecture invariants`.

- [x] **Step 3: Run backend and tooling regression checks**

Run:

```powershell
Set-Location shiftory-server
go test ./... -count=1
go vet ./...
Set-Location ..
powershell -NoProfile -ExecutionPolicy Bypass -File scripts/tests/dev-tooling.Tests.ps1
powershell -NoProfile -ExecutionPolicy Bypass -File scripts/tests/goland-run-config.Tests.ps1
powershell -NoProfile -ExecutionPolicy Bypass -File scripts/start-dev.ps1 -ValidateOnly
git diff --check
```

Expected: all tests and checks pass; only existing Windows line-ending conversion warnings may be printed by Git.

- [x] **Step 4: Inspect the final command and deployment surface**

Run: `go list ./... | Select-String '/cmd/'` from `shiftory-server` and inspect `scripts/start-linux.sh`, `deploy/shiftory.service`, and `.run`.

Expected: the backend exposes only `shiftory-server/cmd/api` and `shiftory-server/cmd/migrate`; deployment and GoLand start the API rather than a separate Worker.

No commit step is included because this task does not authorize staging or committing changes.
