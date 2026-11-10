param([ValidateSet('Debug','Release')][string]$Configuration = 'Debug')
. "$PSScriptRoot/Environment.ps1"
Push-Location $repoRoot
try {
    New-Item -ItemType Directory -Force -Path artifacts/bin | Out-Null
    go build -trimpath -o artifacts/bin/ncade-core.exe ./cmd/ncade-core
    if ($LASTEXITCODE) { throw 'Go core build failed.' }
    go build -trimpath -o artifacts/bin/ncade.exe ./cmd/ncade
    if ($LASTEXITCODE) { throw 'Operator CLI build failed.' }
    go build -trimpath -o artifacts/bin/ncade-migrate.exe ./cmd/ncade-migrate
    if ($LASTEXITCODE) { throw 'Migration tool build failed.' }
    go build -trimpath -o artifacts/bin/ncade-catalog.exe ./cmd/ncade-catalog
    if ($LASTEXITCODE) { throw 'Catalog admin tool build failed.' }
    go build -trimpath -o artifacts/bin/ncade-pilot.exe ./cmd/ncade-pilot
    if ($LASTEXITCODE) { throw 'Pilot E2E operator tool build failed.' }
    dotnet build NostalgiaCade.slnx -c $Configuration
    if ($LASTEXITCODE) { throw '.NET build failed.' }
} finally { Pop-Location }
