$backendRoot = Split-Path -Parent $PSScriptRoot
$composeFile = Join-Path $backendRoot "deploy/docker-compose.yaml"

& docker compose -f $composeFile --project-directory $backendRoot down
exit $LASTEXITCODE
