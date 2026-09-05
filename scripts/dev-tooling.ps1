function Resolve-PnpmInvocation {
    param(
        [Parameter(Mandatory)]
        [string]$PackageManager,

        [scriptblock]$CommandResolver = {
            param([string]$Name)
            Get-Command $Name -ErrorAction SilentlyContinue | Select-Object -First 1
        }
    )

    if ($PackageManager -notmatch '^pnpm@\d+\.\d+\.\d+(?:[-+][0-9A-Za-z.-]+)?$') {
        throw "The frontend packageManager must pin an exact pnpm version; got '$PackageManager'."
    }

    foreach ($candidate in @(
        @{ Name = "pnpm"; Provider = "pnpm"; PrefixArguments = @() },
        @{ Name = "corepack"; Provider = "corepack"; PrefixArguments = @("pnpm") },
        @{ Name = "npm"; Provider = "npm exec"; PrefixArguments = @("exec", "--yes", "--package=$PackageManager", "--", "pnpm") }
    )) {
        $command = & $CommandResolver $candidate.Name
        if ($null -eq $command) { continue }

        $filePath = if (-not [string]::IsNullOrWhiteSpace([string]$command.Source)) {
            [string]$command.Source
        } elseif (-not [string]::IsNullOrWhiteSpace([string]$command.Path)) {
            [string]$command.Path
        } else {
            $candidate.Name
        }

        return [pscustomobject]@{
            Provider = $candidate.Provider
            FilePath = $filePath
            PrefixArguments = [string[]]$candidate.PrefixArguments
        }
    }

    throw "Node.js with npm was not found in PATH. Install a supported Node.js version (including npm) before starting the frontend."
}

function ConvertTo-PowerShellLiteral {
    param([Parameter(Mandatory)][AllowEmptyString()][string]$Value)
    return "'" + $Value.Replace("'", "''") + "'"
}

function Format-PowerShellInvocation {
    param(
        [Parameter(Mandatory)]$Invocation,
        [string[]]$Arguments = @()
    )

    $parts = @("&", (ConvertTo-PowerShellLiteral $Invocation.FilePath))
    foreach ($argument in @($Invocation.PrefixArguments) + $Arguments) {
        $parts += ConvertTo-PowerShellLiteral ([string]$argument)
    }
    return $parts -join " "
}
