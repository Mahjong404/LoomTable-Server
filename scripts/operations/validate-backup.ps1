[CmdletBinding()]
param(
    [Parameter(Mandatory = $true, Position = 0)]
    [string]$ArchivePath
)

$ErrorActionPreference = 'Stop'
$archive = (Resolve-Path -LiteralPath $ArchivePath).Path
$workDir = Join-Path ([System.IO.Path]::GetTempPath()) ("loomtable-validate-" + [guid]::NewGuid().ToString('N'))
New-Item -ItemType Directory -Path $workDir | Out-Null
try {
    & tar -xzf $archive -C $workDir
    if ($LASTEXITCODE -ne 0) { throw 'Could not extract backup archive' }
    foreach ($name in @('database.dump', 'attachments.tar', 'manifest.json', 'SHA256SUMS')) {
        if (-not (Test-Path -LiteralPath (Join-Path $workDir $name) -PathType Leaf)) {
            throw "Backup is missing $name"
        }
    }
    $manifest = Get-Content -LiteralPath (Join-Path $workDir 'manifest.json') -Raw | ConvertFrom-Json
    if ($manifest.format -ne 'loomtable-backup-v1') {
        throw "Unsupported backup format: $($manifest.format)"
    }
    foreach ($line in Get-Content -LiteralPath (Join-Path $workDir 'SHA256SUMS')) {
        if ($line -notmatch '^([0-9a-fA-F]{64})  (.+)$') { throw "Invalid checksum entry: $line" }
        $expected, $name = $Matches[1].ToLowerInvariant(), $Matches[2]
        $target = Join-Path $workDir $name
        $actual = (Get-FileHash -Algorithm SHA256 -LiteralPath $target).Hash.ToLowerInvariant()
        if ($actual -ne $expected) { throw "Checksum mismatch: $name" }
    }
    Write-Host "Backup is valid: $archive"
}
finally {
    Remove-Item -LiteralPath $workDir -Recurse -Force -ErrorAction SilentlyContinue
}
