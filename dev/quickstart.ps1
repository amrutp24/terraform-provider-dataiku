# From nothing to a DSS with a Terraform-made project in it.
#
#   dev\quickstart.ps1             start DSS, make an API key, apply examples\quickstart
#   dev\quickstart.ps1 -NoApply    stop after the key, for writing your own configuration
#
# Needs Docker Desktop, and terraform unless -NoApply. This is quickstart.sh
# for Windows PowerShell and does the same thing.
#
# Safe to run again. It reuses the instance and the key it made last time.
#
# DSS_PORT and DSS_CONTAINER_NAME change the port and the container name, for
# when 10000 is taken. To run a second instance next to the first, set
# COMPOSE_PROJECT_NAME as well so it gets a data volume of its own.
param([switch]$NoApply)

$ErrorActionPreference = 'Stop'

$here = $PSScriptRoot
$repo = Split-Path -Parent $here
$port = if ($env:DSS_PORT) { $env:DSS_PORT } else { '10000' }
$container = if ($env:DSS_CONTAINER_NAME) { $env:DSS_CONTAINER_NAME } else { 'dataiku-dss-dev' }
$dssHost = "http://localhost:$port"
$envFile = Join-Path $here '.quickstart.env.ps1'

if (-not (Get-Command docker -ErrorAction SilentlyContinue)) {
    throw 'docker is not installed or not on PATH'
}
if (-not $NoApply -and -not (Get-Command terraform -ErrorAction SilentlyContinue)) {
    throw 'terraform is not installed or not on PATH. Install it, or pass -NoApply.'
}

# The HTTP status of a call to the public API, or 0 while nothing answers.
function Get-DssStatus([string]$Key) {
    $headers = @{}
    if ($Key) {
        $pair = [Text.Encoding]::ASCII.GetBytes("${Key}:")
        $headers['Authorization'] = 'Basic ' + [Convert]::ToBase64String($pair)
    }
    try {
        $response = Invoke-WebRequest -Uri "$dssHost/public/api/auth/info" -Headers $headers `
            -UseBasicParsing -TimeoutSec 5
        return [int]$response.StatusCode
    } catch {
        if ($_.Exception.Response) { return [int]$_.Exception.Response.StatusCode }
        return 0
    }
}

Write-Host 'Starting DSS (the image is about 4 GB the first time)'
docker compose -f (Join-Path $here 'docker-compose.yml') up -d dss
if ($LASTEXITCODE -ne 0) { throw 'docker compose could not start DSS' }

# 401 is the answer to wait for: the backend is up and wants a key. Before
# that the port refuses, then nginx answers 502 while the backend starts.
Write-Host "Waiting for DSS at $dssHost. A first start takes several minutes."
$waited = 0
while ((Get-DssStatus) -ne 401) {
    if ($waited -ge 900) { throw "DSS did not come up in 15 minutes. See: docker logs $container" }
    Start-Sleep -Seconds 5
    $waited += 5
    if ($waited % 30 -eq 0) { Write-Host "  still starting (${waited}s)" }
}
Write-Host 'DSS is up.'

# Reuse the key from an earlier run if the instance still accepts it.
$key = ''
if (Test-Path $envFile) {
    $saved = Select-String -Path $envFile -Pattern 'dkuaps-[A-Za-z0-9]+' | Select-Object -First 1
    if ($saved) { $key = $saved.Matches[0].Value }
    if ($key -and (Get-DssStatus $key) -ne 200) { $key = '' }
}

if (-not $key) {
    Write-Host 'Creating an admin API key'
    foreach ($attempt in 1..6) {
        # dsscli logs to stderr, and the redirect has to happen inside the
        # container. Redirecting a native command's stderr in Windows
        # PowerShell turns each line into an error record, which stops the
        # script under ErrorActionPreference = Stop.
        $out = docker exec $container bash -lc 'dss/bin/dsscli api-key-create --admin true --label terraform-quickstart --output json 2>/dev/null'
        $match = [regex]::Match(($out -join "`n"), 'dkuaps-[A-Za-z0-9]+')
        if ($match.Success) { $key = $match.Value; break }
        Start-Sleep -Seconds 5
    }
    if (-not $key) {
        throw "Could not create an API key. Try by hand: docker exec $container bash -lc 'dss/bin/dsscli api-key-create --admin true --label terraform --output json'"
    }
    Set-Content -Path $envFile -Encoding ASCII -Value @(
        "`$env:DATAIKU_HOST = '$dssHost'"
        "`$env:DATAIKU_API_KEY = '$key'"
    )
}

$env:DATAIKU_HOST = $dssHost
$env:DATAIKU_API_KEY = $key

if (-not $NoApply) {
    Write-Host 'Applying examples\quickstart'
    $example = Join-Path $repo 'examples\quickstart'
    terraform "-chdir=$example" init -input=false | Out-Null
    if ($LASTEXITCODE -ne 0) { throw 'terraform init failed' }
    terraform "-chdir=$example" apply -input=false -auto-approve
    if ($LASTEXITCODE -ne 0) { throw 'terraform apply failed' }
}

Write-Host ''
Write-Host 'Done.'
Write-Host ''
Write-Host "  DSS:      $dssHost   (sign in as admin / admin)"
Write-Host '  Key:      saved in dev\.quickstart.env.ps1, which git ignores'
Write-Host ''
Write-Host 'The first time you open DSS it asks you to register for the Free Edition or'
Write-Host 'enter a licence. That is DSS, and nothing here needs either.'
Write-Host ''
if (-not $NoApply) {
    Write-Host "  Project:  $dssHost/projects/QUICKSTART/"
    Write-Host ''
}
Write-Host 'To use the provider yourself from this shell:'
Write-Host ''
Write-Host '  . dev\.quickstart.env.ps1'
Write-Host '  terraform -chdir=examples/quickstart plan'
Write-Host ''
Write-Host 'To throw everything away, data included:'
Write-Host ''
Write-Host '  docker compose -f dev/docker-compose.yml down -v'
