$backendRoot = Split-Path -Parent $PSScriptRoot
$composeFile = Join-Path $backendRoot "deploy/docker-compose.yaml"
$envFile = Join-Path $backendRoot ".env"
$composeArgs = @("-f", $composeFile, "--project-directory", $backendRoot)
if (Test-Path -LiteralPath $envFile) {
    $composeArgs += @("--env-file", $envFile)
}

& docker compose @composeArgs up --build --wait
if ($LASTEXITCODE -eq 0) {
    exit 0
}

$exitCode = $LASTEXITCODE
Write-Warning "Docker Compose startup failed; stopping partially started services."
& docker compose @composeArgs down
exit $exitCode
