param([string]$PostgresBin = '')
. "$PSScriptRoot/Environment.ps1"
if (-not $PostgresBin) {
    foreach ($pgVersion in @('18','17','16')) {
        $candidateRoot = Join-Path $env:ProgramFiles "PostgreSQL/$pgVersion"
        if (Test-Path -LiteralPath (Join-Path $candidateRoot 'share/postgres.bki')) {
            $PostgresBin = Join-Path $candidateRoot 'bin'
            break
        }
    }
}
if (-not $PostgresBin) { throw 'No complete PostgreSQL installation found. Pass -PostgresBin for PostgreSQL 16+.' }
$testRoot = [IO.Path]::GetFullPath((Join-Path $repoRoot '.cache/postgres-tests'))
$clusterPath = [IO.Path]::GetFullPath((Join-Path $testRoot ([Guid]::NewGuid().ToString('N'))))
$dataPath = Join-Path $clusterPath 'data'
$serverLog = Join-Path $clusterPath 'postgres.log'
$started = $false
$previousTestDsn = $env:NCADE_TEST_POSTGRES_DSN
$previousCatalogTest = $env:NCADE_TEST_CATALOG
if (-not (Test-Path -LiteralPath (Join-Path $PostgresBin 'initdb.exe'))) { throw 'PostgreSQL binaries not found. Pass -PostgresBin with a PostgreSQL 16+ bin directory.' }
New-Item -ItemType Directory -Force -Path $clusterPath | Out-Null
Push-Location $repoRoot
try {
    # Only synthetic, disposable data; no existing server or user database is used.
    & "$PostgresBin/initdb.exe" -D $dataPath -U ncade_test --auth=trust --encoding=UTF8 --locale=C > (Join-Path $clusterPath 'initdb.log')
    if ($LASTEXITCODE) { throw "initdb failed; inspect $clusterPath" }
    $portProbe = [Net.Sockets.TcpListener]::new([Net.IPAddress]::Loopback, 0)
    $portProbe.Start()
    $testPort = $portProbe.LocalEndpoint.Port
    $portProbe.Stop()
    & "$PostgresBin/pg_ctl.exe" -D $dataPath -l $serverLog -o "-h 127.0.0.1 -p $testPort" -w start
    if ($LASTEXITCODE) { throw "Temporary PostgreSQL failed to start; inspect $serverLog" }
    $started = $true
    & "$PostgresBin/createdb.exe" -h 127.0.0.1 -p $testPort -U ncade_test ncade_test_foundation
    if ($LASTEXITCODE) { throw 'Temporary database creation failed.' }
    $env:NCADE_TEST_POSTGRES_DSN = "postgres://ncade_test@127.0.0.1:$testPort/ncade_test_foundation?sslmode=disable"
    go test ./internal/database -run '^TestPostgresMigrations$' -count=1 -v
    if ($LASTEXITCODE) { throw 'PostgreSQL migration tests failed.' }
    $env:NCADE_TEST_CATALOG = '1'
    go test ./internal/emucatalog -run '^TestPostgresCatalogPublication$' -count=1 -v
    if ($LASTEXITCODE) { throw 'PostgreSQL catalog publication tests failed.' }
} finally {
    if ($started) {
        & "$PostgresBin/pg_ctl.exe" -D $dataPath -m fast -w stop
        if ($LASTEXITCODE) { Write-Warning "Stop failed for test cluster $dataPath; inspect the log." }
    }
    $env:NCADE_TEST_POSTGRES_DSN = $previousTestDsn
    $env:NCADE_TEST_CATALOG = $previousCatalogTest
    Pop-Location
    # Retain isolated cluster/logs for inspection under ignored .cache. This
    # deliberately avoids recursively deleting a potentially still-running DB.
}
