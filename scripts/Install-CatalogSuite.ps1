[CmdletBinding()]
param(
    [Parameter(Mandatory)][string]$Root,
    [Parameter(Mandatory)][ValidateSet('racing','fighting','action-adventure','platform','hack-and-slash')][string]$Genre,
    [string]$Bindings = '',
    [Parameter(Mandatory)][string]$Cabinet,
    [string]$Subject = 'Sengoku-Admin',
    [string]$FirmwareFrom = '',
    [string]$Archives = '',
    [switch]$AvailableOnly,
    [switch]$Apply
)
$ErrorActionPreference = 'Stop'
$tool = Join-Path $PSScriptRoot '../artifacts/bin/ncade-pilot.exe'
if (-not (Test-Path -LiteralPath $tool)) { throw 'Run scripts/Build.ps1 first.' }
$priorDsn = $env:NCADE_POSTGRES_DSN
try {
    if (-not $env:NCADE_POSTGRES_DSN) { $env:NCADE_POSTGRES_DSN = [Environment]::GetEnvironmentVariable('NCADE_POSTGRES_DSN','User') }
    if (-not $env:NCADE_POSTGRES_DSN) { throw 'Set NCADE_POSTGRES_DSN with scripts/Set-CatalogConnection.ps1 first.' }
    $selectionArgs = @('--genre',$Genre)
    if ($Bindings) { $selectionArgs += @('--bindings',[IO.Path]::GetFullPath($Bindings)) }
    if ($AvailableOnly) { $selectionArgs += '--available-only' }
    $planJson = & $tool --action plan @selectionArgs
    $planExit = $LASTEXITCODE
    if ($planJson) {
        $plan = $planJson | ConvertFrom-Json
        [pscustomobject]@{ Genre=$plan.genre; CatalogGames=$plan.catalogEntries; AvailableGames=$plan.availableEntries; Partial=$plan.partial; Missing=$plan.missing }
    }
    if ($planExit) { throw 'Source preflight failed. No game files were removed or installed.' }
    if (-not $Apply) { Write-Output 'Preview only. Add -Apply to install all five emulators, onboard supplied firmware, and switch the selected genre.'; return }
    $installArgs = @('--action','install','--data-dir',[IO.Path]::GetFullPath($Root),'--root',[IO.Path]::GetFullPath($Root),'--cabinet',$Cabinet,'--subject',$Subject,'--allow-draft','--yes') + $selectionArgs
    if ($FirmwareFrom) { $installArgs += @('--firmware-from',[IO.Path]::GetFullPath($FirmwareFrom)) }
    if ($Archives) { $installArgs += @('--archives',[IO.Path]::GetFullPath($Archives)) }
    & $tool @installArgs
    if ($LASTEXITCODE) { throw 'Installation did not complete. Inspect the reported local operation and pilot-runs journal before retrying.' }
} finally { $env:NCADE_POSTGRES_DSN = $priorDsn }
