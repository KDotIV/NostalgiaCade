[CmdletBinding()]
param(
    [ValidateSet('Check','Export','Import')][string]$Action='Check',
    [string]$ArchivePath='',
    [string]$DestinationRoot=''
)
$ErrorActionPreference='Stop'
$migrationRoot=[IO.Path]::GetFullPath((Join-Path $PSScriptRoot '../internal/database/migrations'))
if ($DestinationRoot) { $migrationRoot=[IO.Path]::GetFullPath($DestinationRoot) }
$manifest=Get-Content -LiteralPath (Join-Path $PSScriptRoot '../build/migrations.lock.json') -Raw | ConvertFrom-Json
$utf8=New-Object Text.UTF8Encoding($false,$true)
Add-Type -AssemblyName System.IO.Compression
function Normalize-Sql([byte[]]$Bytes) { $utf8.GetBytes($utf8.GetString($Bytes).Replace("`r`n","`n")) }
function Get-Digest([byte[]]$Bytes) {
    $hash=[Security.Cryptography.SHA256]::Create()
    try { [BitConverter]::ToString($hash.ComputeHash($Bytes)).Replace('-','').ToLowerInvariant() }
    finally { $hash.Dispose() }
}
$expected=@{}
foreach ($file in $manifest.files) {
    if ($file.path -notmatch '^(postgres|sqlite)/[0-9]{3}_[a-z0-9_]+\.sql$' -or $file.sha256 -notmatch '^[0-9a-f]{64}$' -or $expected.ContainsKey($file.path)) { throw 'Invalid migration manifest.' }
    $expected[$file.path]=$file.sha256
}
if ($expected.Count -eq 0) { throw 'Empty migration manifest.' }
if ($Action -eq 'Import') {
    if ($ArchivePath) { $archiveBytes=[IO.File]::ReadAllBytes([IO.Path]::GetFullPath($ArchivePath)) }
    elseif ($env:NCADE_MIGRATIONS_ZIP_BASE64) {
        try { $archiveBytes=[Convert]::FromBase64String($env:NCADE_MIGRATIONS_ZIP_BASE64) }
        finally { $env:NCADE_MIGRATIONS_ZIP_BASE64=$null }
    } else { throw 'Supply -ArchivePath or the private NCADE_MIGRATIONS_ZIP_BASE64 CI secret.' }
    if ($archiveBytes.Length -gt 20MB) { throw 'Migration archive exceeds size limit.' }
    $stream=[IO.MemoryStream]::new($archiveBytes,$false)
    $zip=[IO.Compression.ZipArchive]::new($stream,[IO.Compression.ZipArchiveMode]::Read)
    $validated=@{}
    try {
        foreach ($entry in $zip.Entries) {
            $name=$entry.FullName
            if (-not $expected.ContainsKey($name) -or $validated.ContainsKey($name) -or $entry.Length -gt 1MB) { throw 'Migration archive contains an unexpected, duplicate, or oversized entry.' }
            $reader=$entry.Open(); $buffer=[IO.MemoryStream]::new()
            try { $reader.CopyTo($buffer); $bytes=Normalize-Sql $buffer.ToArray() }
            finally { $reader.Dispose(); $buffer.Dispose() }
            if ((Get-Digest $bytes) -ne $expected[$name]) { throw "Migration checksum mismatch: $name" }
            $target=Join-Path $migrationRoot $name
            if ((Test-Path -LiteralPath $target) -and (Get-Digest (Normalize-Sql ([IO.File]::ReadAllBytes($target)))) -ne $expected[$name]) { throw "Preserving modified local migration: $name. Resolve it before importing." }
            $validated[$name]=$bytes
        }
        if ($validated.Count -ne $expected.Count) { throw 'Migration archive is incomplete.' }
    } finally { $zip.Dispose(); $stream.Dispose(); $archiveBytes=$null }
    # Every name and checksum is verified before writing any SQL. Never extract
    # ZIP paths directly or overwrite an edited local migration.
    foreach ($name in $validated.Keys) {
        $target=Join-Path $migrationRoot $name
        New-Item -ItemType Directory -Path ([IO.Path]::GetDirectoryName($target)) -Force | Out-Null
        [IO.File]::WriteAllBytes($target,$validated[$name])
    }
}
$contents=@{}
foreach ($name in $expected.Keys) {
    $path=Join-Path $migrationRoot $name
    if (-not (Test-Path -LiteralPath $path)) { throw "Private migration missing: $name. Import the private bundle; see docs/private-migrations.md." }
    $bytes=Normalize-Sql ([IO.File]::ReadAllBytes($path))
    if ((Get-Digest $bytes) -ne $expected[$name]) { throw "Migration checksum mismatch: $name" }
    $contents[$name]=$bytes
}
foreach ($file in Get-ChildItem -LiteralPath $migrationRoot -Filter '*.sql' -Recurse -File) {
    $name=$file.FullName.Substring($migrationRoot.Length+1).Replace('\','/')
    if (-not $expected.ContainsKey($name)) { throw "Migration is absent from the checksum manifest: $name" }
}
if ($Action -eq 'Export') {
    if (-not $ArchivePath) { $ArchivePath=Join-Path $PSScriptRoot '../artifacts/private/ncade-migrations.zip' }
    $archive=[IO.Path]::GetFullPath($ArchivePath)
    New-Item -ItemType Directory -Path ([IO.Path]::GetDirectoryName($archive)) -Force | Out-Null
    $stream=[IO.File]::Open($archive,[IO.FileMode]::CreateNew)
    $zip=[IO.Compression.ZipArchive]::new($stream,[IO.Compression.ZipArchiveMode]::Create)
    try {
        foreach ($name in ($contents.Keys | Sort-Object)) {
            $entry=$zip.CreateEntry($name)
            $writer=$entry.Open()
            try { $writer.Write($contents[$name],0,$contents[$name].Length) }
            finally { $writer.Dispose() }
        }
    } finally { $zip.Dispose(); $stream.Dispose() }
    Write-Output "Private bundle exported: $archive"
}
Write-Output "Verified $($expected.Count) private migrations."
