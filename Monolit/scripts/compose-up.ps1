$backendRoot = Split-Path -Parent $PSScriptRoot
$composeFile = Join-Path $backendRoot "deploy/docker-compose.yaml"
$envFile = Join-Path $backendRoot ".env"
$composeArgs = @("-f", $composeFile, "--project-directory", $backendRoot)
if (Test-Path -LiteralPath $envFile) {
    $composeArgs += @("--env-file", $envFile)
}

& docker compose @composeArgs build
if ($LASTEXITCODE -ne 0) {
    Write-Warning "Image build failed. Existing services were not stopped. For registry lookup errors, check Docker Desktop network/DNS and retry task up."
    exit $LASTEXITCODE
}

& docker compose @composeArgs up --no-build --wait
if ($LASTEXITCODE -eq 0) {
    exit 0
}

$exitCode = $LASTEXITCODE
Write-Warning "Docker Compose startup failed. Containers were retained for diagnostics; inspect task ps and task logs."
exit $exitCode
