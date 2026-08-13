[CmdletBinding()]
param(
    [Parameter(Mandatory = $true, Position = 0)]
    [string]$ArchivePath,
    [switch]$Confirm
)

$ErrorActionPreference = 'Stop'
if (-not $Confirm) {
    throw 'Restore requires the explicit -Confirm switch.'
}
$archive = (Resolve-Path -LiteralPath $ArchivePath).Path
$scriptDir = Split-Path -Parent $MyInvocation.MyCommand.Path
& (Join-Path $scriptDir 'validate-backup.ps1') -ArchivePath $archive

$running = @(& docker compose ps --status running --services)
if ($LASTEXITCODE -ne 0) { throw 'Could not inspect Compose services' }
if ($running -contains 'server') {
    throw 'Refusing to restore while the Server service is running.'
}

$workDir = Join-Path ([System.IO.Path]::GetTempPath()) ("loomtable-restore-" + [guid]::NewGuid().ToString('N'))
New-Item -ItemType Directory -Path $workDir | Out-Null
try {
    function Invoke-DockerFromFile([string[]]$Arguments, [string]$Source) {
        $process = Start-Process -FilePath 'docker' -ArgumentList $Arguments -NoNewWindow -Wait -PassThru -RedirectStandardInput $Source
        if ($process.ExitCode -ne 0) {
            throw "docker exited with code $($process.ExitCode)"
        }
    }

    & tar -xzf $archive -C $workDir
    if ($LASTEXITCODE -ne 0) { throw 'Could not extract backup archive' }
    Invoke-DockerFromFile @('compose', 'exec', '-T', 'postgres', 'pg_restore', '--clean', '--if-exists', '--no-owner', '--no-privileges', '-U', 'loomtable', '-d', 'loomtable') (Join-Path $workDir 'database.dump')

    & docker compose run --rm --no-deps -T --entrypoint sh server -c 'find /var/lib/loomtable/attachments -mindepth 1 -delete'
    if ($LASTEXITCODE -ne 0) { throw 'Could not clear the attachment volume' }
    Invoke-DockerFromFile @('compose', 'run', '--rm', '--no-deps', '-T', '--entrypoint', 'tar', 'server', '-C', '/var/lib/loomtable/attachments', '-xf', '-') (Join-Path $workDir 'attachments.tar')
    Write-Host 'Restore completed; run migrations and readiness checks before reconnecting clients.'
}
finally {
    Remove-Item -LiteralPath $workDir -Recurse -Force -ErrorAction SilentlyContinue
}
