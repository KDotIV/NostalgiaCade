param([string]$OutputDirectory = '', [string]$DolphinReleaseURL = '')
. "$PSScriptRoot/Environment.ps1"
if (-not $OutputDirectory) { $OutputDirectory = Join-Path $repoRoot 'artifacts/catalog-review' }
New-Item -ItemType Directory -Force -Path $OutputDirectory | Out-Null
$packages = @()
$reviewedAt = [DateTime]::UtcNow.ToString('yyyy-MM-ddTHH:mm:ssZ')
function Get-GitHubAsset($Repository, $Tag, $NamePattern) {
    $release = Invoke-RestMethod -Uri "https://api.github.com/repos/$Repository/releases/$Tag" -TimeoutSec 30
    $asset = @($release.assets | Where-Object { $_.name -match $NamePattern })
    if ($asset.Count -ne 1) { throw "Expected one Windows package for $Repository" }
    return @{ release=$release; asset=$asset[0] }
}
function Save-Package($Slug, $Name, $Version, $URL, $Format, $Executable, $Official, $Page, $License, $LicenseURL, $Requirements, $ExpectedDigest = '') {
    $archive = Join-Path $OutputDirectory "$Slug.$Format"
    Invoke-WebRequest -Uri $URL -OutFile $archive -TimeoutSec 600
    $hash = (Get-FileHash -LiteralPath $archive -Algorithm SHA256).Hash.ToLowerInvariant()
    if ($ExpectedDigest -and $ExpectedDigest -ne "sha256:$hash") { throw "Upstream checksum mismatch for $Slug" }
    $review = if ($Slug -eq 'duckstation') { 'Noncommercial license: obtain permission before commercial venue deployment.' } else { 'Review license and bundled third-party notices before redistributing a cabinet image.' }
    return [ordered]@{
        slug=$Slug; name=$Name; version=$Version; os='windows'; architecture='x64'; officialUrl=$Official; downloadPage=$Page
        url=$URL; sha256=$hash; bytes=(Get-Item -LiteralPath $archive).Length; format=$Format; executable=$Executable
        allowedHosts=@('github.com','release-assets.githubusercontent.com','dl.dolphin-emu.org','stable.eden-emu.dev')
        license=$License; licenseUrl=$LicenseURL; deploymentReview=$review; verifiedAt=$reviewedAt; requirements=$Requirements
    }
}
$asset = Get-GitHubAsset 'stenzek/duckstation' 'tags/latest' '^duckstation-windows-x64-release.zip$'
$version = 'rolling-' + ([DateTime]$asset.asset.updated_at).ToUniversalTime().ToString('yyyyMMdd-HHmmss')
$packages += Save-Package 'duckstation' 'DuckStation' $version $asset.asset.browser_download_url 'zip' 'duckstation-qt-x64-ReleaseLTCG.exe' 'https://www.duckstation.org/' 'https://www.duckstation.org/windl' 'CC-BY-NC-ND-4.0' 'https://github.com/stenzek/duckstation/blob/master/LICENSE' @{firmware='User-supplied PlayStation BIOS';runtime='Microsoft Visual C++ v14 x64';documentation='https://github.com/stenzek/duckstation'} $asset.asset.digest
$asset = Get-GitHubAsset 'PCSX2/pcsx2' 'latest' '^pcsx2-.*-windows-x64-Qt.7z$'
$packages += Save-Package 'pcsx2' 'PCSX2' $asset.release.tag_name $asset.asset.browser_download_url '7z' 'pcsx2-qt.exe' 'https://pcsx2.net/' 'https://pcsx2.net/downloads/' 'GPL-3.0-or-later' 'https://github.com/PCSX2/pcsx2/blob/master/COPYING.GPLv3' @{firmware='User-supplied PS2 BIOS';runtime='Microsoft Visual C++ v14 x64';documentation='https://pcsx2.net/docs/setup/requirements/'} $asset.asset.digest
if (-not $DolphinReleaseURL) { $DolphinReleaseURL = (Invoke-WebRequest -Uri 'https://dolphin-emu.org/download/' -TimeoutSec 30).Content }
$match = [regex]::Match($DolphinReleaseURL, 'https://dl\.dolphin-emu\.org/releases/([^/]+)/dolphin-[^"\s<>]+-x64\.7z')
if (-not $match.Success) { throw 'Dolphin release link not found' }
$packages += Save-Package 'dolphin' 'Dolphin' $match.Groups[1].Value $match.Value '7z' 'Dolphin-x64/Dolphin.exe' 'https://dolphin-emu.org/' 'https://dolphin-emu.org/download/' 'GPL-2.0-or-later' 'https://github.com/dolphin-emu/dolphin/blob/master/COPYING' @{firmware='HLE / GameCube skip-IPL requires no BIOS in the current adapter';runtime='Microsoft Visual C++ v14 x64';documentation='https://dolphin-emu.org/docs/faq/'}
$asset = Get-GitHubAsset 'RPCS3/rpcs3-binaries-win' 'latest' '^rpcs3-.*_win64_msvc.7z$'
$packages += Save-Package 'rpcs3' 'RPCS3' $asset.release.name $asset.asset.browser_download_url '7z' 'rpcs3.exe' 'https://rpcs3.net/' 'https://rpcs3.net/download' 'GPL-2.0-only' 'https://github.com/RPCS3/rpcs3/blob/master/LICENSE' @{firmware='Install official PS3 firmware through RPCS3';runtime='Microsoft Visual C++ v14 x64';documentation='https://rpcs3.net/quickstart'} $asset.asset.digest
$eden = Invoke-RestMethod -Uri 'https://git.eden-emu.dev/api/v1/repos/eden-emu/eden/releases/latest' -TimeoutSec 30
$edenAsset = @($eden.assets | Where-Object { $_.name -match '^Eden-Windows-.*-amd64-clang-pgo.zip$' })
if ($edenAsset.Count -ne 1) { throw 'Expected one Eden stable x64 PGO package' }
$packages += Save-Package 'eden' 'Eden' $eden.tag_name $edenAsset[0].browser_download_url 'zip' 'eden.exe' 'https://eden-emu.dev/' 'https://eden-emu.dev/downloads/' 'GPL-3.0-or-later' 'https://git.eden-emu.dev/eden-emu/eden/src/branch/master/LICENSE.txt' @{firmware='User-supplied Switch keys and firmware; never distributed in the catalog';graphics='Vulkan 1.1 or OpenGL 4.6 minimum';memory='16 GB recommended';documentation='https://eden-emu.dev/system-requirements/'}
$destination = Join-Path $OutputDirectory 'windows.json'
@{version=1;packages=$packages} | ConvertTo-Json -Depth 8 | Set-Content -LiteralPath $destination -Encoding utf8NoBOM
$packages | ForEach-Object { [pscustomobject]$_ } | Select-Object slug,version,bytes,sha256 | Format-Table
Write-Output "Review generated catalog: $destination. This command does not publish it or modify a cabinet."
