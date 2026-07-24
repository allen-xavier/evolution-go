param(
    [string]$ImageTag = "evolution-go:proxy-patch-check"
)

$ErrorActionPreference = "Stop"
$repoRoot = (Resolve-Path (Join-Path $PSScriptRoot "..")).Path
$patchDirectory = Join-Path $PSScriptRoot "patches"
$patches = @(Get-ChildItem -LiteralPath $patchDirectory -Filter "*.patch" | Sort-Object Name)
$newlyApplied = [System.Collections.Generic.List[string]]::new()

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
        $newlyApplied.Add($patch.FullName)
    }

    $unsafePatterns = @(
        "SetProxy\(nil\)",
        "continuing without proxy",
        "attempting to connect without proxy",
        "Successfully connected without proxy"
    )
    foreach ($pattern in $unsafePatterns) {
        $matches = @(Select-String -Path "pkg/whatsmeow/service/whatsmeow.go" -Pattern $pattern)
        if ($matches.Count -gt 0) {
            throw "Unsafe direct-fallback pattern remains after patch: $pattern"
        }
    }

    $workspaceMount = "${repoRoot}:/workspace"
    & docker run --rm -v $workspaceMount -w /workspace golang:1.25.0-alpine sh -c "apk add --no-cache git build-base libjpeg-turbo-dev libwebp-dev >/dev/null && /usr/local/go/bin/go test ./pkg/config ./pkg/instance/service ./pkg/whatsmeow/service ./pkg/events/webhook ./pkg/events/rabbitmq"
    if ($LASTEXITCODE -ne 0) {
        throw "Evolution proxy policy tests failed."
    }

    & docker build --tag $ImageTag .
    if ($LASTEXITCODE -ne 0) {
        throw "Evolution Docker build failed."
    }

    Write-Host "Patch validated successfully. Local image: $ImageTag"
}
finally {
    for ($index = $newlyApplied.Count - 1; $index -ge 0; $index--) {
        & git apply --reverse -- $newlyApplied[$index]
        if ($LASTEXITCODE -ne 0) {
            Write-Warning "Could not automatically remove temporary patch $($newlyApplied[$index])."
        }
    }
    Pop-Location
}
