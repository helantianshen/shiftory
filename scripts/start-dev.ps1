[CmdletBinding()]
param(
    [string]$EnvFile = ".env.development",
    [switch]$SkipMigrate,
    [switch]$BackendOnly,
    [switch]$FrontendOnly,
    [switch]$EnableAI,
    [switch]$ValidateOnly
)

$ErrorActionPreference = "Stop"
$repoRoot = (Resolve-Path (Join-Path $PSScriptRoot "..")).Path
$serverRoot = Join-Path $repoRoot "shiftory-server"
$webRoot = Join-Path $repoRoot "shiftory-web"
. (Join-Path $PSScriptRoot "dev-tooling.ps1")

if (-not [IO.Path]::IsPathRooted($EnvFile)) {
    $EnvFile = Join-Path $repoRoot $EnvFile
}
$EnvFile = (Resolve-Path $EnvFile -ErrorAction Stop).Path

function Import-EnvironmentFile {
    param([string]$Path)

    foreach ($rawLine in Get-Content -LiteralPath $Path) {
        $line = $rawLine.Trim()
        if ([string]::IsNullOrWhiteSpace($line) -or $line.StartsWith("#")) { continue }

        $separator = $line.IndexOf("=")
        if ($separator -lt 1) {
            throw "Invalid environment entry in ${Path}: $rawLine"
        }

        $name = $line.Substring(0, $separator).Trim()
        $value = $line.Substring($separator + 1).Trim()
        if ($name -notmatch '^[A-Za-z_][A-Za-z0-9_]*$') {
            throw "Invalid environment variable name $name in ${Path}"
        }

        if ($value.Length -ge 2) {
            $first = $value[0]
            $last = $value[$value.Length - 1]
            if (($first -eq '"' -and $last -eq '"') -or ($first -eq "'" -and $last -eq "'")) {
                $value = $value.Substring(1, $value.Length - 2)
            }
        }
        [Environment]::SetEnvironmentVariable($name, $value, "Process")
    }
}

function Assert-Directory {
    param([string]$Path, [string]$Label)
    if (-not (Test-Path -LiteralPath $Path -PathType Container)) {
        throw "$Label directory was not found: $Path"
    }
}

function Start-DevProcess {
    param(
        [string]$Title,
        [string]$WorkingDirectory,
        [string]$Command
    )

    $childCommand = '$Host.UI.RawUI.WindowTitle = ' + "'" + $Title + "'" + '; Set-Location -LiteralPath ' + "'" + $WorkingDirectory + "'" + '; ' + $Command
    Start-Process -FilePath "powershell.exe" -WorkingDirectory $WorkingDirectory -ArgumentList @(
        "-NoLogo",
        "-NoExit",
        "-ExecutionPolicy",
        "Bypass",
        "-Command",
        $childCommand
    ) | Out-Null
}

Import-EnvironmentFile -Path $EnvFile
[Environment]::SetEnvironmentVariable("SHIFTORY_ENV_FILE", $EnvFile, "Process")
if ($EnableAI) {
    [Environment]::SetEnvironmentVariable("SHIFTORY_AI_ENABLED", "true", "Process")
}
Assert-Directory -Path $serverRoot -Label "Backend"
Assert-Directory -Path $webRoot -Label "Frontend"

$goCommand = $null
if (-not $FrontendOnly) {
    $goCommand = Get-Command go -ErrorAction SilentlyContinue | Select-Object -First 1
}
if (-not $FrontendOnly -and -not $goCommand) {
    throw "Go was not found in PATH. Install Go 1.27 or open a shell with Go configured."
}

$pnpmInvocation = $null
if (-not $BackendOnly) {
    $webPackage = Get-Content -LiteralPath (Join-Path $webRoot "package.json") -Raw | ConvertFrom-Json
    $pnpmInvocation = Resolve-PnpmInvocation -PackageManager ([string]$webPackage.packageManager)
}

if ($ValidateOnly) {
    Write-Output "Environment loaded: $EnvFile"
    Write-Output "Backend: $serverRoot"
    Write-Output "Frontend: $webRoot"
    Write-Output "API bind: $($env:SHIFTORY_HTTP_ADDR)"
    Write-Output "Web origin: $($env:SHIFTORY_PUBLIC_ORIGIN)"
    if ($goCommand) { Write-Output "Go command: $($goCommand.Source)" }
    if ($pnpmInvocation) { Write-Output "Frontend package runner: $($pnpmInvocation.Provider) ($($pnpmInvocation.FilePath))" }
    Write-Output "Image AI: $(if ($env:SHIFTORY_AI_ENABLED -ne 'true') { 'disabled (SHIFTORY_AI_ENABLED is not true)' } elseif ([string]::IsNullOrWhiteSpace($env:SHIFTORY_AI_API_KEY)) { 'enabled but missing SHIFTORY_AI_API_KEY' } else { 'enabled' })"
    exit 0
}

if (-not $FrontendOnly -and -not $SkipMigrate) {
    Push-Location $serverRoot
    try {
        Write-Host "Running database migrations..." -ForegroundColor Cyan
        & go run ./cmd/migrate
        if ($LASTEXITCODE -ne 0) { throw "Database migration failed with exit code $LASTEXITCODE" }
    } finally {
        Pop-Location
    }
}

if (-not $FrontendOnly) {
    Start-DevProcess -Title "Shiftory API" -WorkingDirectory $serverRoot -Command "go run ./cmd/api"
}

if (-not $BackendOnly) {
    $webCommand = Format-PowerShellInvocation -Invocation $pnpmInvocation -Arguments @("dev")
    Start-DevProcess -Title "Shiftory Web" -WorkingDirectory $webRoot -Command $webCommand
}

Write-Host "Shiftory development services are starting in separate PowerShell windows." -ForegroundColor Green
if (-not $FrontendOnly) { Write-Host "API: http://127.0.0.1:8080" }
if (-not $BackendOnly) { Write-Host "Web: http://localhost:5173" }
