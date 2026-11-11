[CmdletBinding()]
param(
    [ValidateSet('Configure','Start','Stop','Restart','Status','CopyToken','RotateToken')][string]$Action='Status',
    [ValidateRange(1024,65535)][int]$Port=8090,
    [string]$Subject='Sengoku-Admin'
)
$ErrorActionPreference='Stop'
if ($env:OS -ne 'Windows_NT') { throw 'This optional local credential helper requires Windows. The Go API itself runs on Windows or Linux using environment variables.' }
$runtimeRoot=Join-Path ([Environment]::GetFolderPath('LocalApplicationData')) 'NCade\CatalogApi'
$secretPath=Join-Path $runtimeRoot 'secrets.clixml'
$settingsPath=Join-Path $runtimeRoot 'settings.json'
$statePath=Join-Path $runtimeRoot 'process.json'
$executable=Join-Path $runtimeRoot 'ncade-api.exe'
function Read-Secret([Security.SecureString]$Value) {
    $pointer=[Runtime.InteropServices.Marshal]::SecureStringToBSTR($Value)
    try { [Runtime.InteropServices.Marshal]::PtrToStringBSTR($pointer) }
    finally { [Runtime.InteropServices.Marshal]::ZeroFreeBSTR($pointer) }
}
function New-AdminToken {
    $bytes=New-Object byte[] 32
    $random=[Security.Cryptography.RandomNumberGenerator]::Create()
    try { $random.GetBytes($bytes); [BitConverter]::ToString($bytes).Replace('-','').ToLowerInvariant() }
    finally { $random.Dispose(); [Array]::Clear($bytes,0,$bytes.Length) }
}
function Save-Secrets($Secrets) {
    $Secrets | Export-Clixml -LiteralPath ($secretPath+'.tmp')
    Move-Item -LiteralPath ($secretPath+'.tmp') -Destination $secretPath -Force
}
function Find-Api {
    if (-not (Test-Path -LiteralPath $statePath)) { return }
    $state=Get-Content -LiteralPath $statePath -Raw | ConvertFrom-Json
    if (-not $state.childPid -or -not $state.childStartedAt) { return }
    $process=Get-Process -Id $state.childPid -ErrorAction SilentlyContinue
    # PowerShell 7 can deserialize ISO timestamps as DateTime; Windows
    # PowerShell keeps strings. Compare instants rather than display formats.
    $startedAt=([datetime]$state.childStartedAt).ToUniversalTime()
    if ($process -and $process.Path -eq $executable -and $process.StartTime.ToUniversalTime().Ticks -eq $startedAt.Ticks) { $process }
}
function Stop-Api {
    $process=Find-Api
    if ($process) { Stop-Process -Id $process.Id -Force; $process.WaitForExit() }
}
function Test-Api {
    if (-not (Test-Path -LiteralPath $secretPath)) { return $false }
    $settings=Get-Content -LiteralPath $settingsPath -Raw | ConvertFrom-Json
    $secrets=Import-Clixml -LiteralPath $secretPath
    $token=Read-Secret $secrets.AdminToken
    try {
        $result=Invoke-WebRequest -Uri "http://127.0.0.1:$($settings.port)/v1/catalog/genres" -Headers @{Authorization="Bearer $token"} -UseBasicParsing -TimeoutSec 3
        return $result.StatusCode -eq 200
    } catch { return $false } finally { $token=$null; $secrets=$null }
}
function Start-Api {
    if (-not (Test-Path -LiteralPath $secretPath)) { throw 'Run -Action Configure first.' }
    if (Find-Api) { return }
    $settings=Get-Content -LiteralPath $settingsPath -Raw | ConvertFrom-Json
    $probe=New-Object Net.Sockets.TcpListener([Net.IPAddress]::Loopback,[int]$settings.port)
    try { $probe.Start() } catch { throw 'The API port is already in use; no unrelated process was stopped.' } finally { $probe.Stop() }
    $secrets=Import-Clixml -LiteralPath $secretPath
    $names=@('NCADE_POSTGRES_DSN','NCADE_ADMIN_TOKEN','NCADE_ADMIN_SUBJECT','NCADE_API_LISTEN','NCADE_API_BEHIND_TLS_PROXY','NCADE_API_TLS_CERT','NCADE_API_TLS_KEY')
    $prior=@{}
    foreach ($name in $names) { $prior[$name]=[Environment]::GetEnvironmentVariable($name,'Process') }
    try {
        $env:NCADE_POSTGRES_DSN=Read-Secret $secrets.DatabaseConnection
        $env:NCADE_ADMIN_TOKEN=Read-Secret $secrets.AdminToken
        $env:NCADE_ADMIN_SUBJECT=$settings.subject
        $env:NCADE_API_LISTEN="127.0.0.1:$($settings.port)"
        $env:NCADE_API_BEHIND_TLS_PROXY='false'; $env:NCADE_API_TLS_CERT=$null; $env:NCADE_API_TLS_KEY=$null
        foreach ($name in @('stdout.log','stderr.log')) {
            $path=Join-Path $runtimeRoot $name
            if (Test-Path -LiteralPath $path) { Move-Item -LiteralPath $path -Destination ($path+'.previous') -Force }
        }
        $process=Start-Process -FilePath $executable -WorkingDirectory $runtimeRoot -WindowStyle Hidden -PassThru -RedirectStandardOutput (Join-Path $runtimeRoot 'stdout.log') -RedirectStandardError (Join-Path $runtimeRoot 'stderr.log')
        [ordered]@{ childPid=$process.Id; childStartedAt=$process.StartTime.ToUniversalTime().ToString('o') } | ConvertTo-Json | Set-Content -LiteralPath $statePath -Encoding UTF8
    } finally {
        foreach ($name in $names) { [Environment]::SetEnvironmentVariable($name,$prior[$name],'Process') }
        $secrets=$null
    }
    for ($attempt=0; $attempt -lt 30; $attempt++) {
        if ($process.HasExited) { throw 'API exited; inspect the local runtime logs.' }
        if (Test-Api) { return }
        Start-Sleep -Milliseconds 500
    }
    throw 'API did not become ready; inspect the local runtime logs.'
}
function Show-Status {
    if (-not (Test-Path -LiteralPath $settingsPath)) { [pscustomobject]@{Configured=$false}; return }
    $settings=Get-Content -LiteralPath $settingsPath -Raw | ConvertFrom-Json
    $process=Find-Api
    [pscustomobject]@{Configured=$true;URL="http://127.0.0.1:$($settings.port)";AuthenticatedRequestSucceeded=(Test-Api);ApiPID=if($process){$process.Id}else{$null};RuntimeDirectory=$runtimeRoot;CredentialStorage='Windows DPAPI, current user';AutomaticStartup=$false}
}
switch ($Action) {
    'Configure' {
        $sourceExe=[IO.Path]::GetFullPath((Join-Path $PSScriptRoot '../artifacts/bin/ncade-api.exe'))
        if (-not (Test-Path -LiteralPath $sourceExe)) { throw 'Build the API first with scripts/Build.ps1.' }
        $dsn=[Environment]::GetEnvironmentVariable('NCADE_POSTGRES_DSN','User')
        if (-not $dsn) { $dsn=$env:NCADE_POSTGRES_DSN }
        if (-not $dsn) { throw 'Configure NCADE_POSTGRES_DSN using Set-CatalogConnection.ps1 first.' }
        if ([string]::IsNullOrWhiteSpace($Subject)) { throw 'Administrator subject is required.' }
        Stop-Api
        New-Item -ItemType Directory -Path $runtimeRoot -Force | Out-Null
        $sid=[Security.Principal.WindowsIdentity]::GetCurrent().User
        # Change only the DACL. Set-Acl can also try to write audit metadata,
        # which requires privileges an ordinary local developer should not need.
        & "$env:SystemRoot\System32\icacls.exe" $runtimeRoot /inheritance:r /grant:r "*$($sid.Value):(OI)(CI)F" '*S-1-5-18:(OI)(CI)F' | Out-Null
        if ($LASTEXITCODE) { throw 'Could not restrict local credential-folder permissions.' }
        $token=if(Test-Path -LiteralPath $secretPath){Read-Secret (Import-Clixml -LiteralPath $secretPath).AdminToken}else{New-AdminToken}
        Save-Secrets ([pscustomobject]@{DatabaseConnection=(ConvertTo-SecureString $dsn -AsPlainText -Force);AdminToken=(ConvertTo-SecureString $token -AsPlainText -Force)})
        $dsn=$null; $token=$null
        [ordered]@{port=$Port;subject=$Subject} | ConvertTo-Json | Set-Content -LiteralPath $settingsPath -Encoding UTF8
        Copy-Item -LiteralPath $sourceExe -Destination $executable -Force
        Write-Output 'Local API configured. Start it explicitly with -Action Start. No startup entry, scheduled task, or service is installed.'
    }
    'Start' { Start-Api; Show-Status }
    'Stop' { Stop-Api; Write-Output 'API stopped.' }
    'Restart' { Stop-Api; Start-Api; Show-Status }
    'Status' { Show-Status }
    'CopyToken' {
        if (-not (Test-Path -LiteralPath $secretPath)) { throw 'Run -Action Configure first.' }
        $token=Read-Secret (Import-Clixml -LiteralPath $secretPath).AdminToken
        try { Set-Clipboard -Value $token; Write-Output 'Token copied. Paste it into Postman Local Vault as ncade-admin-token, then clear your clipboard.' }
        finally { $token=$null }
    }
    'RotateToken' {
        if (-not (Test-Path -LiteralPath $secretPath)) { throw 'Run -Action Configure first.' }
        $wasRunning=[bool](Find-Api)
        $secrets=Import-Clixml -LiteralPath $secretPath
        Stop-Api
        $token=New-AdminToken
        $secrets.AdminToken=ConvertTo-SecureString $token -AsPlainText -Force
        Save-Secrets $secrets
        $token=$null; $secrets=$null
        if ($wasRunning) { Start-Api }
        Write-Output 'Token rotated. Use CopyToken to replace ncade-admin-token in Postman Local Vault.'
        Show-Status
    }
}
