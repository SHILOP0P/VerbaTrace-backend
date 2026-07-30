$ErrorActionPreference = "Stop"

$repositoryRoot = Split-Path -Parent $PSScriptRoot
$backendRoot = Join-Path $repositoryRoot "Monolit"

Push-Location $backendRoot
try {
    $env:GOCACHE = Join-Path $repositoryRoot ".gocache"
    $env:GOLANGCI_LINT_CACHE = Join-Path $repositoryRoot ".golangci-cache"

    task verify:int
    if ($LASTEXITCODE -ne 0) {
        exit $LASTEXITCODE
    }
}
finally {
    Pop-Location
}
