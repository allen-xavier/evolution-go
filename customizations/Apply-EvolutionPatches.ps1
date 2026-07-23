param(
    [switch]$CheckOnly
)

$ErrorActionPreference = "Stop"
$repoRoot = (Resolve-Path (Join-Path $PSScriptRoot "..")).Path
$patchDirectory = Join-Path $PSScriptRoot "patches"
$patches = @(Get-ChildItem -LiteralPath $patchDirectory -Filter "*.patch" | Sort-Object Name)
$temporarilyApplied = [System.Collections.Generic.List[string]]::new()

function Test-GitPatch {
    param([string[]]$Arguments)
    $previousPreference = $ErrorActionPreference
    try {
        $ErrorActionPreference = "Continue"
        & git @Arguments 2>$null
        return $LASTEXITCODE -eq 0
    }
    finally {
        $ErrorActionPreference = $previousPreference
    }
}

if ($patches.Count -eq 0) {
    throw "No Evolution patches were found in $patchDirectory"
}

Push-Location $repoRoot
try {
    foreach ($patch in $patches) {
        if (Test-GitPatch @("apply", "--reverse", "--check", "--", $patch.FullName)) {
            Write-Host "$($patch.Name): already applied"
            continue
        }

        if (-not (Test-GitPatch @("apply", "--check", "--", $patch.FullName))) {
            throw "$($patch.Name) is not compatible with this Evolution release."
        }

        & git apply -- $patch.FullName
        if ($LASTEXITCODE -ne 0) {
            throw "Failed to apply $($patch.Name)."
        }
        if ($CheckOnly) {
            $temporarilyApplied.Add($patch.FullName)
            Write-Host "$($patch.Name): compatible"
        }
        else {
            Write-Host "$($patch.Name): applied"
        }
    }
}
finally {
    if ($CheckOnly) {
        for ($index = $temporarilyApplied.Count - 1; $index -ge 0; $index--) {
            & git apply --reverse -- $temporarilyApplied[$index]
            if ($LASTEXITCODE -ne 0) {
                Write-Warning "Could not remove temporary patch $($temporarilyApplied[$index])."
            }
        }
    }
    Pop-Location
}
