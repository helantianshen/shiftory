$ErrorActionPreference = "Stop"

$repositoryRoot = (Resolve-Path (Join-Path $PSScriptRoot "..\..")).Path
$serverRoot = Join-Path $repositoryRoot "shiftory-server"
$commandRoot = Join-Path $serverRoot "cmd"

$commandNames = @(Get-ChildItem -LiteralPath $commandRoot -Directory | Select-Object -ExpandProperty Name | Sort-Object)
$expectedCommands = @("api", "migrate")
if (($commandNames -join "|") -cne ($expectedCommands -join "|")) {
    throw "Backend commands must be exactly api and migrate. Actual: $($commandNames -join ', ')"
}

Push-Location $serverRoot
try {
    $packages = @(& go list ./...)
    if ($LASTEXITCODE -ne 0) {
        throw "go list ./... failed with exit code $LASTEXITCODE"
    }
} finally {
    Pop-Location
}
if ($packages -contains "shiftory-server/cmd/worker") {
    throw "Standalone Worker package must not be buildable."
}

$apiSource = Get-Content -LiteralPath (Join-Path $serverRoot "cmd\api\main.go") -Raw
if ($apiSource -notmatch "if\s+cfg\.AIEnabled" -or $apiSource -notmatch "importjob\.NewRunner") {
    throw "The API command must own the conditional embedded image task runner."
}
if ($apiSource -match "database\.Migrate") {
    throw "The long-running API command must not execute deployment migrations."
}

$migrateSource = Get-Content -LiteralPath (Join-Path $serverRoot "cmd\migrate\main.go") -Raw
if ($migrateSource -notmatch "database\.Migrate") {
    throw "The one-shot migrate command must remain the migration owner."
}

$excludedGit = "!.git/**"
$excludedIDE = "!.idea/**"
$excludedHandoff = "!.agent/HANDOFF.md"
$excludedPlan = "!docs/superpowers/plans/2026-09-06-embedded-worker-cleanup.md"
$excludedTest = "!scripts/tests/embedded-worker-architecture.Tests.ps1"
# HANDOFF is an append-only progress record and intentionally names removed artifacts.
$violations = @(& rg --hidden -n "cmd[/\\]worker|WithWorker|go\s+(run|build)\s+\.?[/\\]cmd[/\\]worker" --glob $excludedGit --glob $excludedIDE --glob $excludedHandoff --glob $excludedPlan --glob $excludedTest $repositoryRoot)
if ($LASTEXITCODE -notin @(0, 1)) {
    throw "Architecture residue scan failed with exit code $LASTEXITCODE"
}
if ($violations.Count -gt 0) {
    throw "Standalone Worker architecture references remain:`n$($violations -join [Environment]::NewLine)"
}

Write-Output "PASS: embedded Worker architecture invariants"
