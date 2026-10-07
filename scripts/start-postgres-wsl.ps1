$ErrorActionPreference = 'Stop'
# The Ubuntu distribution lives on D: on this workstation. No cloud credentials
# are read or changed; only a project-local database password is generated once.
$projectRoot = Split-Path -Parent $PSScriptRoot
$databaseEnvPath = Join-Path $projectRoot '.env.local'
$backendEnvPath = Join-Path $projectRoot 'backend/.env.local'
$runtimePath = Join-Path $projectRoot '.data'
New-Item -ItemType Directory -Force -Path $runtimePath | Out-Null
$keeperFile = Join-Path $runtimePath 'postgres-wsl-keeper.pid'
$keeperRunning = $false
if (Test-Path -LiteralPath $keeperFile) {
    $keeperProcess = Get-Process -Id ([int]([IO.File]::ReadAllText($keeperFile))) -ErrorAction SilentlyContinue
    $keeperRunning = $keeperProcess -and $keeperProcess.ProcessName -eq 'wsl'
}
# WSL may stop the distribution when no Windows-launched Linux process remains,
# even with PostgreSQL managed by systemd. Keep this local development session alive.
if (-not $keeperRunning) {
    $keeper = Start-Process -FilePath 'wsl.exe' -ArgumentList @('-d','Ubuntu-24.04','--exec','/bin/sleep','infinity') -WindowStyle Hidden -PassThru
    [IO.File]::WriteAllText($keeperFile, [string]$keeper.Id)
}
if (Test-Path -LiteralPath $databaseEnvPath) {
    $passwordLine = Get-Content -LiteralPath $databaseEnvPath | Where-Object { $_ -match '^POSTGRES_PASSWORD=' } | Select-Object -First 1
}
if ($passwordLine) { $databasePassword = $passwordLine.Substring('POSTGRES_PASSWORD='.Length) }
else {
    $databasePassword = [Convert]::ToHexString([System.Security.Cryptography.RandomNumberGenerator]::GetBytes(24)).ToLowerInvariant()
    Add-Content -LiteralPath $databaseEnvPath -Value "`nPOSTGRES_PASSWORD=$databasePassword" -Encoding utf8
}
if ($databasePassword -notmatch '^[a-f0-9]{48}$') { throw 'Local bootstrap expects its generated password; configure an existing database manually.' }
& wsl -d Ubuntu-24.04 -u root --exec /usr/sbin/service postgresql start
if ($LASTEXITCODE -ne 0) { throw 'PostgreSQL service did not start.' }
$databaseSQL = @"
SELECT 'CREATE ROLE frame_space LOGIN PASSWORD ''$databasePassword''' WHERE NOT EXISTS (SELECT FROM pg_roles WHERE rolname = 'frame_space')
\gexec
SELECT 'CREATE DATABASE frame_space OWNER frame_space' WHERE NOT EXISTS (SELECT FROM pg_database WHERE datname = 'frame_space')
\gexec
SELECT 'CREATE DATABASE frame_space_test OWNER frame_space' WHERE NOT EXISTS (SELECT FROM pg_database WHERE datname = 'frame_space_test')
\gexec
"@
$databaseSQL | & wsl -d Ubuntu-24.04 -u postgres --exec psql -X -v ON_ERROR_STOP=1 postgres
if ($LASTEXITCODE -ne 0) { throw 'Project database creation failed.' }
$existingConnection = if (Test-Path -LiteralPath $backendEnvPath) { Get-Content -LiteralPath $backendEnvPath | Where-Object { $_ -match '^DATABASE_URL=.+$' } }
if (-not $existingConnection) {
    if (Test-Path -LiteralPath $backendEnvPath) {
        $content = [IO.File]::ReadAllText($backendEnvPath) -replace '(?m)^DATABASE_URL=\s*$', ''
        [IO.File]::WriteAllText($backendEnvPath, $content, [Text.UTF8Encoding]::new($false))
    }
    Add-Content -LiteralPath $backendEnvPath -Value "`nDATABASE_URL=postgres://frame_space:$databasePassword@127.0.0.1:5432/frame_space?sslmode=disable" -Encoding utf8
}
Write-Output 'Project PostgreSQL databases are ready; backend/.env.local connection configured without printing credentials.'
