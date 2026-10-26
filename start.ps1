param(
    [switch]$Smoke,
    [switch]$OpenSwagger
)

$ErrorActionPreference = 'Stop'
Set-Location -LiteralPath $PSScriptRoot

function Fail([string]$Message) {
    Write-Host "ERROR: $Message" -ForegroundColor Red
    exit 1
}
function DockerReady {
    try {
        $version = & docker info --format '{{.ServerVersion}}' 2>$null
        return ($LASTEXITCODE -eq 0 -and -not [string]::IsNullOrWhiteSpace(($version | Out-String)))
    }
    catch { return $false }
}
function CheckExit([string]$Step) {
    if ($LASTEXITCODE -ne 0) { Fail "$Step failed (exit code: $LASTEXITCODE). See: docker compose logs --tail=100" }
}
function GetSetting([string]$Key, [string]$Fallback) {
    foreach ($line in (Get-Content -LiteralPath '.env')) {
        if ($line -match ('^\s*' + [regex]::Escape($Key) + '\s*=\s*(.*)\s*$')) {
            return $Matches[1].Trim().Trim('"').Trim("'")
        }
    }
    return $Fallback
}

Write-Host 'Task Management API - one-click startup' -ForegroundColor Cyan
if (-not (Get-Command docker -ErrorAction SilentlyContinue)) {
    Fail 'Docker CLI is not installed. Install Docker Desktop with WSL 2.'
}

if (-not (Test-Path -LiteralPath '.env')) {
    if (-not (Test-Path -LiteralPath '.env.example')) { Fail '.env.example is missing' }
    $mysqlPassword = [guid]::NewGuid().ToString('N')
    $rootPassword = [guid]::NewGuid().ToString('N')
    $jwtSecret = [guid]::NewGuid().ToString('N') + [guid]::NewGuid().ToString('N')
    $envText = [System.IO.File]::ReadAllText((Join-Path $PSScriptRoot '.env.example'))
    $envText = $envText.Replace('change_me_mysql_password', $mysqlPassword)
    $envText = $envText.Replace('change_me_root_password', $rootPassword)
    $envText = $envText.Replace('replace_with_a_random_secret_of_at_least_32_characters', $jwtSecret)
    [System.IO.File]::WriteAllText((Join-Path $PSScriptRoot '.env'), $envText, (New-Object System.Text.UTF8Encoding($false)))
    Write-Host 'Created local .env with unique secrets (not committed to Git).' -ForegroundColor Yellow
}
else {
    Write-Host 'Using existing .env (preserving database passwords).' -ForegroundColor Gray
}

Write-Host 'Checking Docker Engine...' -ForegroundColor Cyan
if (-not (DockerReady)) {
    $paths = @(
        (Join-Path $env:ProgramFiles 'Docker\Docker\Docker Desktop.exe'),
        (Join-Path $env:LOCALAPPDATA 'Programs\DockerDesktop\Docker Desktop.exe')
    )
    $executable = $paths | Where-Object { Test-Path $_ } | Select-Object -First 1
    if ($executable) {
        Write-Host 'Starting Docker Desktop...' -ForegroundColor Yellow
        Start-Process -FilePath $executable | Out-Null
    }
    else {
        Fail 'Docker Engine is stopped and Docker Desktop executable was not found. Open Docker Desktop manually.'
    }
    $deadline = (Get-Date).AddMinutes(3)
    while ((Get-Date) -lt $deadline -and -not (DockerReady)) { Start-Sleep -Seconds 3 }
    if (-not (DockerReady)) { Fail 'Docker Engine did not become available. Check Docker Desktop and WSL 2.' }
}

Write-Host 'Validating Docker Compose configuration...' -ForegroundColor Cyan
& docker compose config --quiet
CheckExit 'Compose validation'

Write-Host 'Building and starting Go, MySQL, Redis and migrations...' -ForegroundColor Cyan
& docker compose up --build -d
CheckExit 'Container startup'

$migrateIDs = @(& docker compose ps -a -q migrate)
CheckExit 'Reading migration container ID'

if ($migrateIDs.Count -eq 0) {
    Fail 'Migration container not found'
}

$migrateID = $migrateIDs[0]
if ([string]::IsNullOrWhiteSpace($migrateID)) { Fail 'Migration container not found' }
$migrateExit = & docker inspect --format '{{.State.ExitCode}}' $migrateID
CheckExit 'Inspecting migration exit code'
if ($migrateExit.Trim() -ne '0') {
    & docker compose logs --tail=80 migrate
    Fail "Migrations failed (exit $migrateExit). Database volumes have been preserved."
}

$port = GetSetting 'HOST_PORT' '8080'
if ($port -notmatch '^\d{1,5}$' -or [int]$port -lt 1 -or [int]$port -gt 65535) {
    Fail 'HOST_PORT in .env must be between 1 and 65535'
}
$url = "http://127.0.0.1:$port/health"
Write-Host "Waiting for API readiness: $url" -ForegroundColor Cyan
$ready = $false
$deadline = (Get-Date).AddMinutes(2)
while ((Get-Date) -lt $deadline) {
    try {
        $result = Invoke-RestMethod -Uri $url -TimeoutSec 3 -ErrorAction Stop
        if ($result.status -eq 'ok') { $ready = $true; break }
    }
    catch { }
    Start-Sleep -Seconds 2
}
if (-not $ready) {
    & docker compose ps -a
    & docker compose logs --tail=80 app
    Fail 'API did not become ready. Check the logs above.'
}

Write-Host ''
Write-Host 'SUCCESS: Go API, MySQL, Redis and migrations are ready.' -ForegroundColor Green
& docker compose ps -a
Write-Host "API:     http://localhost:$port/api/v1" -ForegroundColor Green
Write-Host "Health:  http://localhost:$port/health" -ForegroundColor Green
Write-Host "Swagger: http://localhost:$port/swagger" -ForegroundColor Green

if ($Smoke) {
    if ($port -ne '8080') { Write-Host "Smoke uses custom API port $port" }
    & powershell.exe -NoProfile -ExecutionPolicy Bypass -File (Join-Path $PSScriptRoot 'scripts\smoke.ps1') -Base "http://127.0.0.1:$port/api/v1"
    CheckExit 'API smoke test'
}
if ($OpenSwagger) { Start-Process "http://localhost:$port/swagger" }