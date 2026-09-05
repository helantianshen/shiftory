$ErrorActionPreference = "Stop"

$toolingScript = Join-Path $PSScriptRoot "..\dev-tooling.ps1"
. $toolingScript

function Assert-Equal {
    param(
        $Expected,
        $Actual,
        [string]$Message
    )

    if ($Expected -ne $Actual) {
        throw "$Message Expected '$Expected', got '$Actual'."
    }
}

function Assert-SequenceEqual {
    param(
        [string[]]$Expected,
        [string[]]$Actual,
        [string]$Message
    )

    Assert-Equal -Expected ($Expected -join "|") -Actual ($Actual -join "|") -Message $Message
}

function New-TestResolver {
    param([hashtable]$Commands)

    return {
        param([string]$Name)

        if ($Commands.ContainsKey($Name)) {
            return [pscustomobject]@{ Source = $Commands[$Name] }
        }
        return $null
    }.GetNewClosure()
}

$packageManager = "pnpm@11.19.0"

$direct = Resolve-PnpmInvocation -PackageManager $packageManager -CommandResolver (New-TestResolver @{ pnpm = "C:\tools\pnpm.cmd" })
Assert-Equal "pnpm" $direct.Provider "Direct pnpm provider mismatch."
Assert-Equal "C:\tools\pnpm.cmd" $direct.FilePath "Direct pnpm path mismatch."
Assert-SequenceEqual @() $direct.PrefixArguments "Direct pnpm arguments mismatch."

$corepack = Resolve-PnpmInvocation -PackageManager $packageManager -CommandResolver (New-TestResolver @{ corepack = "C:\node\corepack.cmd" })
Assert-Equal "corepack" $corepack.Provider "Corepack provider mismatch."
Assert-Equal "C:\node\corepack.cmd" $corepack.FilePath "Corepack path mismatch."
Assert-SequenceEqual @("pnpm") $corepack.PrefixArguments "Corepack arguments mismatch."

$npm = Resolve-PnpmInvocation -PackageManager $packageManager -CommandResolver (New-TestResolver @{ npm = "C:\node\npm.cmd" })
Assert-Equal "npm exec" $npm.Provider "npm fallback provider mismatch."
Assert-Equal "C:\node\npm.cmd" $npm.FilePath "npm fallback path mismatch."
Assert-SequenceEqual @("exec", "--yes", "--package=$packageManager", "--", "pnpm") $npm.PrefixArguments "npm fallback arguments mismatch."

$missingError = $null
try {
    Resolve-PnpmInvocation -PackageManager $packageManager -CommandResolver (New-TestResolver @{}) | Out-Null
} catch {
    $missingError = $_.Exception.Message
}
if ($missingError -notmatch "Node.js.*npm") {
    throw "Missing-tool error should explain that Node.js with npm is required. Actual: $missingError"
}

Write-Output "PASS: pnpm command resolution"
