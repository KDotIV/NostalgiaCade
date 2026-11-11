$ErrorActionPreference='Stop'
$helper=Join-Path $PSScriptRoot 'Private-Migrations.ps1'
$testRoot=Join-Path $PSScriptRoot ('../.cache/migration-bundle-tests/'+[guid]::NewGuid().ToString('N'))
New-Item -ItemType Directory -Path $testRoot -Force | Out-Null
$archive=Join-Path $testRoot 'valid.zip'
& $helper -Action Export -ArchivePath $archive | Out-Null
$destination=Join-Path $testRoot 'imported'
& $helper -Action Import -ArchivePath $archive -DestinationRoot $destination | Out-Null
& $helper -Action Check -DestinationRoot $destination | Out-Null
function Expect-Failure([scriptblock]$Action,[string]$Expected) {
    $message=''
    try { & $Action | Out-Null } catch { $message=$_.Exception.Message }
    if ($message -notlike "*$Expected*") { throw "Expected failure '$Expected'; received '$message'." }
}
Expect-Failure { & $helper -Action Check -DestinationRoot (Join-Path $testRoot 'missing') } 'Private migration missing'
$changed=Join-Path $destination 'sqlite/001_local_state.sql'
[IO.File]::AppendAllText($changed,"`n-- local edit")
Expect-Failure { & $helper -Action Import -ArchivePath $archive -DestinationRoot $destination } 'Preserving modified local migration'
if (-not [IO.File]::ReadAllText($changed).EndsWith('-- local edit')) { throw 'Import overwrote a local change.' }
foreach ($scenario in @('checksum','missing','traversal','duplicate')) {
    $badArchive=Join-Path $testRoot ($scenario+'.zip')
    Copy-Item -LiteralPath $archive -Destination $badArchive
    $stream=[IO.File]::Open($badArchive,[IO.FileMode]::Open)
    $zip=[IO.Compression.ZipArchive]::new($stream,[IO.Compression.ZipArchiveMode]::Update)
    try {
        switch ($scenario) {
            'checksum' {
                $entry=$zip.GetEntry('sqlite/001_local_state.sql'); $entry.Delete()
                $writer=[IO.StreamWriter]::new($zip.CreateEntry('sqlite/001_local_state.sql').Open())
                try { $writer.Write('altered SQL') } finally { $writer.Dispose() }
            }
            'missing' { $zip.GetEntry('sqlite/001_local_state.sql').Delete() }
            'traversal' { $null=$zip.CreateEntry('../outside.sql') }
            'duplicate' { $null=$zip.CreateEntry('sqlite/001_local_state.sql') }
        }
    } finally { $zip.Dispose(); $stream.Dispose() }
    $expected=switch($scenario){'checksum'{'checksum mismatch'} 'missing'{'incomplete'} default{'unexpected, duplicate'}}
    $target=Join-Path $testRoot ('rejected-'+$scenario)
    Expect-Failure { & $helper -Action Import -ArchivePath $badArchive -DestinationRoot $target } $expected
    if (Test-Path -LiteralPath $target) { throw "Invalid $scenario archive wrote files." }
}
Write-Output 'Private migration checks passed: round trip, missing assets, edited-local preservation, tampering, incomplete archive, traversal and duplicates.'
