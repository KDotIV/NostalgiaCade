[CmdletBinding()]
param()
. "$PSScriptRoot/Environment.ps1"

$outputDirectory = Join-Path $repoRoot 'artifacts/azure'
$stageDirectory = Join-Path $outputDirectory ([guid]::NewGuid().ToString('N'))
$packagePath = Join-Path $outputDirectory 'ncade-api.zip'
New-Item -ItemType Directory -Path $stageDirectory -Force | Out-Null
$prior = @{}
foreach ($name in @('GOOS', 'GOARCH', 'CGO_ENABLED')) { $prior[$name] = [Environment]::GetEnvironmentVariable($name, 'Process') }
Push-Location $repoRoot
try {
    $env:GOOS = 'windows'; $env:GOARCH = 'amd64'; $env:CGO_ENABLED = '0'
    $binary = Join-Path $stageDirectory 'ncade-api.exe'
    go build -mod=readonly -trimpath -ldflags '-s -w' -o $binary ./cmd/ncade-api
    if ($LASTEXITCODE) { throw 'API build failed.' }
    $configuration = Join-Path $stageDirectory 'web.config'
    Copy-Item -LiteralPath (Join-Path $repoRoot 'deploy/azure/appservice/web.config') -Destination $configuration
    # Explicit files keep private SQL, credentials and desktop assets out.
    Compress-Archive -LiteralPath $binary, $configuration -DestinationPath $packagePath -Force
    [pscustomobject]@{ Package = $packagePath; SHA256 = (Get-FileHash -LiteralPath $packagePath -Algorithm SHA256).Hash.ToLowerInvariant() }
} finally {
    Pop-Location
    foreach ($name in $prior.Keys) { [Environment]::SetEnvironmentVariable($name, $prior[$name], 'Process') }
}
