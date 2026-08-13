[CmdletBinding()]
param()

$ErrorActionPreference = 'Stop'
$scriptDir = Split-Path -Parent $MyInvocation.MyCommand.Path
$repoDir = (Resolve-Path (Join-Path $scriptDir '..\..')).Path
$workDir = Join-Path ([System.IO.Path]::GetTempPath()) ("loomtable-acceptance-" + [guid]::NewGuid().ToString('N'))
$archive = Join-Path $workDir 'acceptance-backup.tar.gz'
$savedEnvironment = @{
    COMPOSE_PROJECT_NAME = $env:COMPOSE_PROJECT_NAME
    LOOMTABLE_POSTGRES_PASSWORD = $env:LOOMTABLE_POSTGRES_PASSWORD
    LOOMTABLE_HOST_ADDR = $env:LOOMTABLE_HOST_ADDR
    LOOMTABLE_HOST_PORT = $env:LOOMTABLE_HOST_PORT
}
$env:COMPOSE_PROJECT_NAME = 'loomtable_acceptance_' + [guid]::NewGuid().ToString('N').Substring(0, 12)
$env:LOOMTABLE_POSTGRES_PASSWORD = 'acceptance-' + [guid]::NewGuid().ToString('N')
$env:LOOMTABLE_HOST_ADDR = '127.0.0.1'
$env:LOOMTABLE_HOST_PORT = [string](Get-Random -Minimum 32000 -Maximum 32999)
New-Item -ItemType Directory -Path $workDir | Out-Null

function Invoke-Docker([string[]]$Arguments) {
    & docker @Arguments
    if ($LASTEXITCODE -ne 0) {
        throw "docker $($Arguments -join ' ') exited with code $LASTEXITCODE"
    }
}

function Wait-LoomTableReady {
    $uri = "http://127.0.0.1:$($env:LOOMTABLE_HOST_PORT)/readyz"
    for ($attempt = 0; $attempt -lt 60; $attempt++) {
        try {
            Invoke-RestMethod -Uri $uri -Method Get -TimeoutSec 2 | Out-Null
            return
        }
        catch {
            Start-Sleep -Seconds 1
        }
    }
    Invoke-Docker @('compose', 'logs', 'server')
    throw 'Server did not become ready.'
}

function Wait-PostgresReady {
    for ($attempt = 0; $attempt -lt 60; $attempt++) {
        & docker compose exec -T postgres pg_isready -U loomtable -d loomtable 2>$null | Out-Null
        if ($LASTEXITCODE -eq 0) {
            return
        }
        Start-Sleep -Seconds 1
    }
    Invoke-Docker @('compose', 'logs', 'postgres')
    throw 'PostgreSQL did not become ready.'
}

try {
    Push-Location $repoDir
    Invoke-Docker @('compose', 'up', '-d', 'postgres')
    Wait-PostgresReady
    Invoke-Docker @('compose', '--profile', 'ops', 'run', '--rm', '--build', 'migrate', '-dir', '/app/migrations')
    $bootstrapLines = @(& docker compose --progress quiet --profile ops run --rm admin auth bootstrap --name Acceptance 2>$null | ForEach-Object { [string]$_ })
    if ($LASTEXITCODE -ne 0) { throw 'Authentication bootstrap failed.' }
    $jsonStart = -1
    $jsonEnd = -1
    for ($index = 0; $index -lt $bootstrapLines.Count; $index++) {
        if ($bootstrapLines[$index].Trim() -eq '{') {
            $jsonStart = $index
            break
        }
    }
    for ($index = $bootstrapLines.Count - 1; $index -ge 0; $index--) {
        if ($bootstrapLines[$index].Trim() -eq '}') {
            $jsonEnd = $index
            break
        }
    }
    if ($jsonStart -lt 0 -or $jsonEnd -lt $jsonStart) { throw 'Authentication bootstrap did not return JSON.' }
    $bootstrap = ($bootstrapLines[$jsonStart..$jsonEnd] -join [Environment]::NewLine) | ConvertFrom-Json
    $token = $bootstrap.token.secret
    if (-not $token) { throw 'Bootstrap did not return a Token Secret.' }

    Invoke-Docker @('compose', 'up', '-d', 'server')
    Wait-LoomTableReady
    $headers = @{
        Authorization = "Bearer $token"
        'Idempotency-Key' = 'mut_00000000000000000000000000'
    }
	$workspaceRequest = @{
		Uri = "http://127.0.0.1:$($env:LOOMTABLE_HOST_PORT)/v1/workspaces"
		Method = 'Post'
		Headers = $headers
		ContentType = 'application/json'
		Body = '{"name":"Acceptance workspace"}'
	}
	$workspace = Invoke-RestMethod @workspaceRequest
    if (-not $workspace.id) { throw 'Workspace creation did not return an ID.' }

    & (Join-Path $scriptDir 'backup.ps1') -OutputPath $archive
    & (Join-Path $scriptDir 'validate-backup.ps1') -ArchivePath $archive
    Invoke-Docker @('compose', 'down', '-v', '--remove-orphans')
    Invoke-Docker @('compose', 'up', '-d', 'postgres')
    Wait-PostgresReady
    & (Join-Path $scriptDir 'restore.ps1') -ArchivePath $archive -Confirm
    Invoke-Docker @('compose', 'up', '-d', 'server')
    Wait-LoomTableReady

    $listHeaders = @{ Authorization = "Bearer $token" }
    $restored = Invoke-RestMethod -Uri "http://127.0.0.1:$($env:LOOMTABLE_HOST_PORT)/v1/workspaces" -Method Get -Headers $listHeaders
    if ($restored.items.id -notcontains $workspace.id) {
        throw 'Restored Server did not return the acceptance Workspace.'
    }
    Write-Host 'LoomTable Compose backup/restore acceptance passed.'
}
finally {
    if ((Get-Location).Path -eq $repoDir) { Pop-Location }
    Push-Location $repoDir
    try { & docker compose down -v --remove-orphans 2>$null | Out-Null } catch {}
    Pop-Location
    Remove-Item -LiteralPath $workDir -Recurse -Force -ErrorAction SilentlyContinue
    foreach ($name in $savedEnvironment.Keys) {
        [Environment]::SetEnvironmentVariable($name, $savedEnvironment[$name], 'Process')
    }
}
