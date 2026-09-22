# 检查共享 GoLand 配置中的启动入口、模式与组合服务
$ErrorActionPreference = "Stop"

$repositoryRoot = (Resolve-Path (Join-Path $PSScriptRoot "..\..")).Path
$runDirectory = Join-Path $repositoryRoot ".run"

# Read-RunConfiguration 读取共享运行配置并检查 XML 中的配置节点是否存在
function Read-RunConfiguration {
    param(
        [Parameter(Mandatory = $true)]
        [string]$FileName
    )

    $path = Join-Path $runDirectory $FileName
    if (-not (Test-Path -LiteralPath $path -PathType Leaf)) {
        throw "Missing GoLand run configuration: $path"
    }

    [xml]$document = Get-Content -LiteralPath $path -Raw
    $configuration = $document.component.configuration
    if ($null -eq $configuration) {
        throw "Run configuration has no component/configuration element: $path"
    }

    return $configuration
}

# Assert-Equal 比较实际值与期望值，不匹配时报告检查失败
function Assert-Equal {
    param(
        [Parameter(Mandatory = $true)]$Actual,
        [Parameter(Mandatory = $true)]$Expected,
        [Parameter(Mandatory = $true)][string]$Message
    )

    if ([string]$Actual -cne [string]$Expected) {
        throw "$Message Expected '$Expected', got '$Actual'."
    }
}

$apiFileName = if (Test-Path -LiteralPath (Join-Path $runDirectory "Shiftory_API.run.xml")) { "Shiftory_API.run.xml" } else { "Shiftory API.run.xml" }
$api = Read-RunConfiguration $apiFileName
Assert-Equal $api.name "Shiftory API" "Unexpected API configuration name."
Assert-Equal $api.type "GoApplicationRunConfiguration" "Unexpected API configuration type."
Assert-Equal $api.working_directory.value '$PROJECT_DIR$/shiftory-server' "Unexpected API working directory."
Assert-Equal $api.package.value "shiftory-server/cmd/api" "Unexpected API package."
Assert-Equal $api.parameters.value '--env development --config "$PROJECT_DIR$/config/development.yaml"' "api must select the development YAML through startup arguments."
if ($null -ne $api.envs) { throw "api must not select a profile through environment variables." }

$migrate = Read-RunConfiguration "Shiftory_Migrate.run.xml"
Assert-Equal $migrate.name "Shiftory Migrate" "Unexpected migration configuration name."
Assert-Equal $migrate.type "GoApplicationRunConfiguration" "Unexpected migration configuration type."
Assert-Equal $migrate.working_directory.value '$PROJECT_DIR$/shiftory-server' "Unexpected migration working directory."
Assert-Equal $migrate.package.value "shiftory-server/cmd/migrate" "Unexpected migration package."
Assert-Equal $migrate.parameters.value '--env development --config "$PROJECT_DIR$/config/development.yaml"' "migrate must select the development YAML through startup arguments."
if ($null -ne $migrate.envs) { throw "migrate must not select a profile through environment variables." }

$webFileName = if (Test-Path -LiteralPath (Join-Path $runDirectory "Shiftory_Web_pnpm.run.xml")) { "Shiftory_Web_pnpm.run.xml" } else { "Shiftory Web (pnpm).run.xml" }
$web = Read-RunConfiguration $webFileName
Assert-Equal $web.name "Shiftory Web (pnpm)" "Unexpected frontend configuration name."
Assert-Equal $web.type "js.build_tools.npm" "Unexpected frontend configuration type."
Assert-Equal $web.'package-json'.value '$PROJECT_DIR$/shiftory-web/package.json' "Unexpected frontend package.json path."
Assert-Equal $web.command.value "run" "Unexpected frontend package command."
Assert-Equal $web.scripts.script.value "dev" "Unexpected frontend package script."
if ([string]$web.'package-manager'.value -notin @("project", "pnpm")) {
    throw "The frontend must use GoLand's project package manager (pnpm)."
}

$development = Read-RunConfiguration "Shiftory_Development.run.xml"
Assert-Equal $development.name "Shiftory Development" "Unexpected compound configuration name."
Assert-Equal $development.type "CompoundRunConfigurationType" "Unexpected compound configuration type."
$targets = @($development.toRun | ForEach-Object { "$($_.name)|$($_.type)" })
if ($targets.Count -ne 2) {
    throw "The development configuration must contain exactly API and Web targets."
}
if ($targets -notcontains "Shiftory API|GoApplicationRunConfiguration") {
    throw "The development configuration does not reference Shiftory API."
}
if ($targets -notcontains "Shiftory Web (pnpm)|js.build_tools.npm") {
    throw "The development configuration does not reference Shiftory Web (pnpm)."
}

Write-Host "GoLand run configuration checks passed." -ForegroundColor Green
