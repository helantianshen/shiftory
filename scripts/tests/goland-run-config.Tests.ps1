$ErrorActionPreference = "Stop"

$repositoryRoot = (Resolve-Path (Join-Path $PSScriptRoot "..\..")).Path
$runDirectory = Join-Path $repositoryRoot ".run"

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

$api = Read-RunConfiguration "Shiftory_API.run.xml"
Assert-Equal $api.name "Shiftory API" "Unexpected API configuration name."
Assert-Equal $api.type "GoApplicationRunConfiguration" "Unexpected API configuration type."
Assert-Equal $api.working_directory.value '$PROJECT_DIR$/shiftory-server' "Unexpected API working directory."
Assert-Equal $api.package.value "shiftory-server/cmd/api" "Unexpected API package."

$migrate = Read-RunConfiguration "Shiftory_Migrate.run.xml"
Assert-Equal $migrate.name "Shiftory Migrate" "Unexpected migration configuration name."
Assert-Equal $migrate.type "GoApplicationRunConfiguration" "Unexpected migration configuration type."
Assert-Equal $migrate.working_directory.value '$PROJECT_DIR$/shiftory-server' "Unexpected migration working directory."
Assert-Equal $migrate.package.value "shiftory-server/cmd/migrate" "Unexpected migration package."

$web = Read-RunConfiguration "Shiftory_Web_pnpm.run.xml"
Assert-Equal $web.name "Shiftory Web (pnpm)" "Unexpected frontend configuration name."
Assert-Equal $web.type "js.build_tools.npm" "Unexpected frontend configuration type."
Assert-Equal $web.'package-json'.value '$PROJECT_DIR$/shiftory-web/package.json' "Unexpected frontend package.json path."
Assert-Equal $web.command.value "run" "Unexpected frontend package command."
Assert-Equal $web.scripts.script.value "dev" "Unexpected frontend package script."
Assert-Equal $web.'package-manager'.value "project" "The frontend must use GoLand's project package manager (pnpm)."

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
