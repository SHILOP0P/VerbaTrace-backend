$backendRoot = Split-Path -Parent $PSScriptRoot
$composeFile = Join-Path $backendRoot "deploy/docker-compose.yaml"

& docker compose -f $composeFile --project-directory $backendRoot up --build --wait
if ($LASTEXITCODE -eq 0) {
    exit 0
}

$exitCode = $LASTEXITCODE
Write-Warning "Docker Compose startup failed; stopping partially started services."
& docker compose -f $composeFile --project-directory $backendRoot down
exit $exitCode
