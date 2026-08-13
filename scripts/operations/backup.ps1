[CmdletBinding()]
param(
    [Parameter(Mandatory = $true, Position = 0)]
    [string]$OutputPath
)

$ErrorActionPreference = 'Stop'
$resolvedOutput = [System.IO.Path]::GetFullPath($OutputPath)
if (Test-Path -LiteralPath $resolvedOutput) {
    throw "Refusing to overwrite existing backup: $resolvedOutput"
}

$workDir = Join-Path ([System.IO.Path]::GetTempPath()) ("loomtable-backup-" + [guid]::NewGuid().ToString('N'))
New-Item -ItemType Directory -Path $workDir | Out-Null
try {
    function Invoke-DockerToFile([string[]]$Arguments, [string]$Destination) {
        $process = Start-Process -FilePath 'docker' -ArgumentList $Arguments -NoNewWindow -Wait -PassThru -RedirectStandardOutput $Destination
        if ($process.ExitCode -ne 0) {
            throw "docker exited with code $($process.ExitCode)"
        }
    }

    Invoke-DockerToFile @('compose', 'exec', '-T', 'postgres', 'pg_dump', '-Fc', '-U', 'loomtable', '-d', 'loomtable') (Join-Path $workDir 'database.dump')
    Invoke-DockerToFile @('compose', 'run', '--rm', '--no-deps', '-T', '--entrypoint', 'tar', 'server', '-C', '/var/lib/loomtable/attachments', '-cf', '-', '.') (Join-Path $workDir 'attachments.tar')

    $schemaVersions = (& docker compose exec -T postgres psql -U loomtable -d loomtable -Atc "SELECT COALESCE(string_agg(version, ',' ORDER BY version), '') FROM schema_migrations") -join ''
    if ($LASTEXITCODE -ne 0) { throw 'Could not read schema versions' }
    $serverVersion = if ($env:LOOMTABLE_SERVER_VERSION) { $env:LOOMTABLE_SERVER_VERSION } else { 'unknown' }
    $manifest = [ordered]@{
        format = 'loomtable-backup-v1'
        createdAt = [DateTimeOffset]::UtcNow.ToString('yyyy-MM-ddTHH:mm:ssZ')
        serverVersion = $serverVersion
        schemaVersions = $schemaVersions.Trim()
        files = @('database.dump', 'attachments.tar')
    }
    $manifest | ConvertTo-Json -Depth 4 | Set-Content -LiteralPath (Join-Path $workDir 'manifest.json') -Encoding utf8NoBOM

    $checksumLines = foreach ($name in @('database.dump', 'attachments.tar', 'manifest.json')) {
        $hash = (Get-FileHash -Algorithm SHA256 -LiteralPath (Join-Path $workDir $name)).Hash.ToLowerInvariant()
        "$hash  $name"
    }
    $checksumLines | Set-Content -LiteralPath (Join-Path $workDir 'SHA256SUMS') -Encoding ascii

    & tar -czf $resolvedOutput -C $workDir database.dump attachments.tar manifest.json SHA256SUMS
    if ($LASTEXITCODE -ne 0) { throw 'Could not create backup archive' }
    Write-Host "Backup created: $resolvedOutput"
    Write-Warning 'This unencrypted archive contains Token hashes and business data.'
}
finally {
    Remove-Item -LiteralPath $workDir -Recurse -Force -ErrorAction SilentlyContinue
}
