. "$PSScriptRoot/Build.ps1"
& "$repoRoot/artifacts/bin/ncade.exe" setup --if-needed
if ($LASTEXITCODE) { throw 'Storage setup must finish before starting the desktop.' }
dotnet run --project "$repoRoot/src/NostalgiaCade.Desktop" --no-build
if ($LASTEXITCODE) { throw 'Desktop exited with an error.' }
