param(
    [Parameter(Mandatory)][string]$Server,
    [Parameter(Mandatory)][string]$Database,
    [Parameter(Mandatory)][string]$Username,
    [switch]$PersistForUser
)
$ErrorActionPreference = 'Stop'
$catalogPassword = Read-Host 'PostgreSQL password (hidden)' -AsSecureString
$catalogPointer = [Runtime.InteropServices.Marshal]::SecureStringToBSTR($catalogPassword)
try {
    $catalogPlaintext = [Runtime.InteropServices.Marshal]::PtrToStringBSTR($catalogPointer)
    $catalogConnection = 'postgresql://{0}:{1}@{2}:5432/{3}?sslmode=verify-full&connect_timeout=15' -f [Uri]::EscapeDataString($Username), [Uri]::EscapeDataString($catalogPlaintext), $Server, [Uri]::EscapeDataString($Database)
    $env:NCADE_POSTGRES_DSN = $catalogConnection
    if ($PersistForUser) { [Environment]::SetEnvironmentVariable('NCADE_POSTGRES_DSN', $catalogConnection, 'User') }
    Write-Output 'NCADE_POSTGRES_DSN is configured. No connection string was written to the repository or console.'
} finally {
    [Runtime.InteropServices.Marshal]::ZeroFreeBSTR($catalogPointer)
    $catalogPassword.Dispose()
    $catalogPlaintext = $null
    $catalogConnection = $null
}
