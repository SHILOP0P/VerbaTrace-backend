& docker compose -f deploy/docker-compose.yaml --project-directory . up --build --wait
if ($LASTEXITCODE -eq 0) {
    exit 0
}

$exitCode = $LASTEXITCODE
Write-Warning "Docker Compose startup failed; stopping partially started services."
& docker compose -f deploy/docker-compose.yaml --project-directory . down
exit $exitCode
