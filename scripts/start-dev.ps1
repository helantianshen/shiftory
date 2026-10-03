# 解析开发启动选项，迁移完成后在独立窗口启动所选服务
[CmdletBinding()]
param(
    [string]$ConfigFile = "config/development.yaml",
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

# 后端配置路径以仓库根目录解析，模式与 YAML 路径作为启动参数传给 Go
$backendArguments = @("--env", "development")
if (-not $FrontendOnly) {
    if (-not [IO.Path]::IsPathRooted($ConfigFile)) {
        $ConfigFile = Join-Path $repoRoot $ConfigFile
    }
    $ConfigFile = (Resolve-Path $ConfigFile -ErrorAction Stop).Path
    $backendArguments += @("--config", $ConfigFile)
}

# Assert-Directory 检查必要目录是否存在，缺失时终止启动
function Assert-Directory {
    param([string]$Path, [string]$Label)
    if (-not (Test-Path -LiteralPath $Path -PathType Container)) {
        throw "$Label directory was not found: $Path"
    }
}

# Start-DevProcess 在独立 Windows PowerShell 窗口中设置工作目录并执行服务命令
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

# AI 开关通过当前进程环境变量覆盖 YAML，子进程继承该值
if ($EnableAI) {
    [Environment]::SetEnvironmentVariable("SHIFTORY_AI_ENABLED", "true", "Process")
}
Assert-Directory -Path $serverRoot -Label "Backend"
Assert-Directory -Path $webRoot -Label "Frontend"

# 后端子进程固定使用 Go 1.26.8，避免较新的系统 Go 自动成为实际工具链
if (-not $FrontendOnly) {
    [Environment]::SetEnvironmentVariable("GOTOOLCHAIN", "go1.26.8", "Process")
}
$goCommand = $null
if (-not $FrontendOnly) {
    $goCommand = Get-Command go -ErrorAction SilentlyContinue | Select-Object -First 1
}
if (-not $FrontendOnly -and -not $goCommand) {
    throw "Go was not found in PATH. Install Go 1.26.8 or open a shell with Go configured."
}

$pnpmInvocation = $null
if (-not $BackendOnly) {
    $webPackage = Get-Content -LiteralPath (Join-Path $webRoot "package.json") -Raw | ConvertFrom-Json
    $pnpmInvocation = Resolve-PnpmInvocation -PackageManager ([string]$webPackage.packageManager)
}

# 仅检查工具与文件路径，YAML 内容校验由后端启动时执行
if ($ValidateOnly) {
    Write-Output "Backend profile: development"
    Write-Output "Backend YAML: $ConfigFile (parsed by the Go process at startup)"
    Write-Output "Backend: $serverRoot"
    Write-Output "Frontend: $webRoot"
    if ($goCommand) { Write-Output "Go command: $($goCommand.Source)" }
    if ($pnpmInvocation) { Write-Output "Frontend package runner: $($pnpmInvocation.Provider) ($($pnpmInvocation.FilePath))" }

    exit 0
}

# 迁移同步执行且失败即停止，防止 API 在错误表结构上启动
if (-not $FrontendOnly -and -not $SkipMigrate) {
    Push-Location $serverRoot
    try {
        Write-Host "Running database migrations..." -ForegroundColor Cyan
        & go run ./cmd/migrate @backendArguments
        if ($LASTEXITCODE -ne 0) { throw "Database migration failed with exit code $LASTEXITCODE" }
    } finally {
        Pop-Location
    }
}

if (-not $FrontendOnly) {
    $apiCommand = Format-PowerShellInvocation -Invocation ([pscustomobject]@{ FilePath = $goCommand.Source; PrefixArguments = @() }) -Arguments (@("run", "./cmd/api") + $backendArguments)
    Start-DevProcess -Title "Shiftory API" -WorkingDirectory $serverRoot -Command $apiCommand
}

if (-not $BackendOnly) {
    $webCommand = Format-PowerShellInvocation -Invocation $pnpmInvocation -Arguments @("dev")
    Start-DevProcess -Title "Shiftory Web" -WorkingDirectory $webRoot -Command $webCommand
}

Write-Host "Shiftory development services are starting in separate PowerShell windows." -ForegroundColor Green
if (-not $FrontendOnly) { Write-Host "API: http://127.0.0.1:8080" }
if (-not $BackendOnly) { Write-Host "Web: http://localhost:5173" }
