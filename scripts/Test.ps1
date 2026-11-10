param([switch]$Postgres)
. "$PSScriptRoot/Build.ps1"
Push-Location $repoRoot
try {
    go vet ./...
    if ($LASTEXITCODE) { throw 'Go vet failed.' }
    go test ./...
    if ($LASTEXITCODE) { throw 'Go tests failed.' }
    go build -ldflags '-H=windowsgui' -o artifacts/bin/ncade-test-emulator.exe ./tests/fixtures/emulator
    if ($LASTEXITCODE) { throw 'Emulator test fixture build failed.' }
    $env:NCADE_TEST_CORE_PATH = Join-Path $repoRoot 'artifacts/bin/ncade-core.exe'
    $env:NCADE_TEST_SCREENSHOTS = Join-Path $repoRoot 'artifacts/preview'
    dotnet test NostalgiaCade.slnx --no-build
    if ($LASTEXITCODE) { throw '.NET / IPC integration tests failed.' }
    if ($Postgres) { & "$PSScriptRoot/Test-Postgres.ps1" }
} finally { Pop-Location }
