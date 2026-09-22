# 按所选目标平台构建前端静态文件与后端二进制
[CmdletBinding()]
param(
    [string]$BinDir = "deploy/bin",
    [ValidateSet("linux", "windows", "darwin")]
    [string]$TargetOS = "linux",
    [ValidateSet("amd64", "arm64")]
    [string]$TargetArch = "amd64",
    [switch]$SkipWeb,
    [switch]$SkipServer
)

$ErrorActionPreference = "Stop"
$repoRoot = (Resolve-Path (Join-Path $PSScriptRoot "..")).Path
$serverRoot = Join-Path $repoRoot "shiftory-server"
$webRoot = Join-Path $repoRoot "shiftory-web"
$binPath = if ([IO.Path]::IsPathRooted($BinDir)) { $BinDir } else { Join-Path $repoRoot $BinDir }

# Invoke-Pnpm 执行 pnpm，缺少直接入口时通过 npm 运行固定版本，非零退出码终止构建
function Invoke-Pnpm {
    param([string[]]$Arguments)
    $pnpm = Get-Command pnpm -ErrorAction SilentlyContinue
    if ($pnpm) {
        & $pnpm.Source @Arguments
    } else {
        npm exec --yes --package pnpm@11.19.0 -- pnpm @Arguments
    }
    if ($LASTEXITCODE -ne 0) { throw "pnpm command failed with exit code $LASTEXITCODE" }
}

# 前端使用锁文件安装依赖，构建输出供部署时提供静态页面
if (-not $SkipWeb) {
    if (-not (Test-Path (Join-Path $webRoot "package.json"))) { throw "Frontend package.json was not found: $webRoot" }
    Push-Location $webRoot
    try {
        Invoke-Pnpm @("install", "--frozen-lockfile")
        Invoke-Pnpm @("build-only")
    } finally { Pop-Location }
}

if (-not $SkipServer) {
    New-Item -ItemType Directory -Force -Path $binPath | Out-Null
    Push-Location $serverRoot
    # 交叉编译变量只在本次构建期间生效，finally 恢复调用者环境
    $previousGoOS = $env:GOOS
    $previousGoArch = $env:GOARCH
    $env:GOOS = $TargetOS
    $env:GOARCH = $TargetArch
    try {
        & go build -trimpath -ldflags="-s -w" -o (Join-Path $binPath "shiftory-api") ./cmd/api
        if ($LASTEXITCODE -ne 0) { throw "API build failed with exit code $LASTEXITCODE" }
        & go build -trimpath -ldflags="-s -w" -o (Join-Path $binPath "shiftory-migrate") ./cmd/migrate
        if ($LASTEXITCODE -ne 0) { throw "Migration build failed with exit code $LASTEXITCODE" }
    } finally {
        if ($null -eq $previousGoOS) { Remove-Item Env:GOOS -ErrorAction SilentlyContinue } else { $env:GOOS = $previousGoOS }
        if ($null -eq $previousGoArch) { Remove-Item Env:GOARCH -ErrorAction SilentlyContinue } else { $env:GOARCH = $previousGoArch }
        Pop-Location
    }
}

Write-Output "Production artifacts ready."
if (-not $SkipWeb) { Write-Output "Frontend: $webRoot\dist" }
if (-not $SkipServer) { Write-Output "Backend:  $binPath" }
