param([switch]$ConfirmClear)
$ErrorActionPreference = 'Stop'
if (-not $ConfirmClear) { throw 'Use -ConfirmClear to clear this project runtime data.' }
$taskRoot = [IO.Path]::GetFullPath((Join-Path $PSScriptRoot '..'))
$taskCompose = @('compose','-f',(Join-Path $taskRoot 'docker-compose.yml'),'-f',(Join-Path $taskRoot 'deploy/compose.acceptance.yml'))
$taskEvidencePath = Join-Path $taskRoot 'docs/最终三项验收结果.json'
$taskEvidence = Get-Content -LiteralPath $taskEvidencePath -Raw | ConvertFrom-Json
if (-not ($taskEvidence.skills.passed -and $taskEvidence.clickhouse.passed -and $taskEvidence.permissions.passed -and $taskEvidence.agent.passed -and $taskEvidence.performance.passed -and $taskEvidence.performance.logs_persisted -and $taskEvidence.performance.clickhouse_logs_match)) {
    throw 'All acceptance checks must pass before clearing data.'
}
$taskConfigText = & docker @taskCompose config --format json
if ($LASTEXITCODE -ne 0) { throw 'Cannot resolve project configuration.' }
$taskConfig = ($taskConfigText -join "`n") | ConvertFrom-Json
$taskProject = $taskConfig.name
if ($taskProject -ne 'mcp-marketplace') { throw "Unexpected project: $taskProject" }
$taskContainers = @(& docker ps -aq --filter "label=com.docker.compose.project=$taskProject")
if ($LASTEXITCODE -ne 0) { throw 'Cannot list project containers.' }
foreach ($taskID in $taskContainers) {
    $taskInspection = (& docker inspect $taskID | ConvertFrom-Json)[0]
    $taskWorkingDirectory = $taskInspection.Config.Labels.'com.docker.compose.project.working_dir'
    if (-not $taskWorkingDirectory -or [IO.Path]::GetFullPath($taskWorkingDirectory) -ne $taskRoot) {
        throw "Container $taskID is not attached to the intended workspace."
    }
}
$taskVolumes = @(& docker volume ls -q --filter "label=com.docker.compose.project=$taskProject")
if ($LASTEXITCODE -ne 0) { throw 'Cannot list project volumes.' }
foreach ($taskVolume in $taskVolumes) { if (-not $taskVolume.StartsWith($taskProject + '_')) { throw "Unexpected volume: $taskVolume" } }

# 避免下次启动自动重新灌入示例业务数据。
$taskEnvPath = Join-Path $taskRoot '.env'
if (Test-Path -LiteralPath $taskEnvPath) {
    $taskEnvContent = Get-Content -LiteralPath $taskEnvPath -Raw
    if ($taskEnvContent -match '(?m)^SEED_ON_STARTUP=') { $taskEnvContent = [regex]::Replace($taskEnvContent,'(?m)^SEED_ON_STARTUP=.*$','SEED_ON_STARTUP=false') }
    else { $taskEnvContent += "`nSEED_ON_STARTUP=false`n" }
    Set-Content -LiteralPath $taskEnvPath -Value $taskEnvContent -Encoding utf8
}
& docker @taskCompose down --volumes --remove-orphans
if ($LASTEXITCODE -ne 0) { throw 'Project data cleanup did not complete.' }
$taskRemainingContainers = @(& docker ps -aq --filter "label=com.docker.compose.project=$taskProject")
$taskRemainingVolumes = @(& docker volume ls -q --filter "label=com.docker.compose.project=$taskProject")
if ($taskRemainingContainers.Count -ne 0 -or $taskRemainingVolumes.Count -ne 0) { throw 'Project containers or data volumes remain.' }
$taskCleanup = @{
    cleared_at = [DateTime]::UtcNow.ToString('o')
    project = $taskProject
    removed_named_volumes = $taskVolumes
    remaining_containers = $taskRemainingContainers.Count
    remaining_named_volumes = $taskRemainingVolumes.Count
    seed_on_startup = $false
    scope = 'PostgreSQL, ClickHouse, Redis, Jaeger, Prometheus and acceptance reports runtime data'
    source_and_acceptance_documents_preserved = $true
}
$taskCleanup | ConvertTo-Json -Depth 8 | Set-Content -LiteralPath (Join-Path $taskRoot 'docs/数据清理结果.json') -Encoding utf8
Write-Output 'Project services stopped and runtime data volumes removed.'
