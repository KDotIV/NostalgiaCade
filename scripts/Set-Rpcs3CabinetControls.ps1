[CmdletBinding(SupportsShouldProcess)]
param(
    [Parameter(Mandatory)][string]$DataDirectory,
    [string]$ProfileId = 'rpcs3'
)
$ErrorActionPreference = 'Stop'
$cli = Join-Path $PSScriptRoot '../artifacts/bin/ncade.exe'
$sessionJson = & $cli --data-dir $DataDirectory session
if ($LASTEXITCODE) { throw 'Cannot check the cabinet session.' }
$session = $sessionJson | ConvertFrom-Json
if ($session.state -in @('starting','running','interrupted')) { throw 'Close the tracked emulator before changing cabinet controls.' }
if (Get-Process -Name rpcs3 -ErrorAction SilentlyContinue) { throw 'Close all RPCS3 windows before changing cabinet controls.' }
$profileJson = & $cli --data-dir $DataDirectory emulator status --id $ProfileId
if ($LASTEXITCODE) { throw 'Cannot read the emulator profile.' }
$profile = $profileJson | ConvertFrom-Json
if ($profile.adapter -ne 'rpcs3' -or -not $profile.prepared) { throw 'Select a prepared RPCS3 profile.' }
$settingsRoot = [IO.Path]::GetFullPath($profile.settings)
$configPath = [IO.Path]::GetFullPath((Join-Path $settingsRoot 'GuiConfigs/CurrentSettings.ini'))
if (-not $configPath.StartsWith($settingsRoot.TrimEnd('\') + '\', [StringComparison]::OrdinalIgnoreCase)) { throw 'Configuration must remain inside this profile.' }
$ancestor = Get-Item -LiteralPath $configPath
while ($null -ne $ancestor) {
    if ($ancestor.Attributes -band [IO.FileAttributes]::ReparsePoint) { throw 'Linked settings paths are not supported.' }
    $ancestor = if ($ancestor -is [IO.FileInfo]) { $ancestor.Directory } else { $ancestor.Parent }
}
$original = [IO.File]::ReadAllText($configPath)
$newline = if ($original.Contains("`r`n")) { "`r`n" } else { "`n" }
$sectionPattern = '(?ms)^\[GSFrame\]\r?\n(?<body>.*?)(?=^\[|\z)'
$sections = [regex]::Matches($original, $sectionPattern)
if ($sections.Count -gt 1) { throw 'Duplicate GSFrame section; review native settings first.' }
$body = if ($sections.Count -eq 1) { $sections[0].Groups['body'].Value } else { '' }
foreach ($key in @('disableKbHotkeys','disableMouse')) {
    $pattern = '(?m)^' + [regex]::Escape($key) + '=.*$'
    if ([regex]::Matches($body, $pattern).Count -gt 1) { throw "Duplicate $key; review native settings first." }
    if ([regex]::IsMatch($body, $pattern)) { $body = [regex]::Replace($body, $pattern, "$key=true") }
    else { $body = $body.TrimEnd("`r","`n") + $newline + "$key=true" + $newline }
}
$replacement = '[GSFrame]' + $newline + $body
if ($sections.Count -eq 1) {
    $match = $sections[0]
    $updated = $original.Substring(0,$match.Index) + $replacement + $original.Substring($match.Index+$match.Length)
} else { $updated = $original.TrimEnd("`r","`n") + $newline + $newline + $replacement }
if ($updated -eq $original) { Write-Output 'RPCS3 cabinet fullscreen controls are already enabled.'; return }
if ($PSCmdlet.ShouldProcess($configPath, 'Back up settings and disable emulator keyboard/double-click fullscreen shortcuts')) {
    $backup = $configPath + '.ncade-backup-' + (Get-Date -Format 'yyyyMMdd-HHmmssfff')
    [IO.File]::Copy($configPath, $backup, $false)
    $temporary = $configPath + '.ncade-' + [guid]::NewGuid().ToString('N') + '.tmp'
    [IO.File]::WriteAllText($temporary, $updated, [Text.UTF8Encoding]::new($false))
    [IO.File]::Move($temporary, $configPath, $true)
    Write-Output "RPCS3 cabinet fullscreen controls enabled. Original settings: $backup"
}
