$backendRoot = Split-Path -Parent $PSScriptRoot
$composeFile = Join-Path $backendRoot "deploy/docker-compose.yaml"
$envFile = Join-Path $backendRoot ".env"
$composeArgs = @("-f", $composeFile, "--project-directory", $backendRoot)
if (Test-Path -LiteralPath $envFile) {
    $composeArgs += @("--env-file", $envFile)
}

& docker compose @composeArgs down
exit $LASTEXITCODE
