$ErrorActionPreference = "Stop"

$repositoryRoot = Split-Path -Parent $PSScriptRoot
git -C $repositoryRoot config core.hooksPath .githooks

Write-Host "Git hooks enabled for $repositoryRoot"
