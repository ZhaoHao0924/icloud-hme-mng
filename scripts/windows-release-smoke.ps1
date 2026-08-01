[CmdletBinding()]
param(
  [string]$Version = "windows-release-smoke",
  [ValidateRange(1024, 65535)]
  [int]$Port = 18082,
  [switch]$SkipNpmCi
)

$ErrorActionPreference = "Stop"

$rootDir = Split-Path -Parent $PSScriptRoot
$buildScript = Join-Path $rootDir "build.ps1"
$tempRoot = [System.IO.Path]::GetFullPath([System.IO.Path]::GetTempPath())
$tempDir = Join-Path $tempRoot ("icloud-hme-release-smoke-" + [guid]::NewGuid().ToString("N"))
$dataDir = Join-Path $tempDir "data"
$nativeBinary = Join-Path $tempDir "icloud-hme-smoke.exe"
$baseUrl = "http://127.0.0.1:$Port"
$apiTokenBytes = New-Object byte[] 32
$apiTokenGenerator = [Security.Cryptography.RandomNumberGenerator]::Create()
try {
  $apiTokenGenerator.GetBytes($apiTokenBytes)
} finally {
  $apiTokenGenerator.Dispose()
}
$apiToken = -join ($apiTokenBytes | ForEach-Object { $_.ToString("x2") })
$authHeaders = @{ Authorization = "Bearer $apiToken" }
$invalidAuthHeaders = @{ Authorization = "Bearer ${apiToken}.invalid" }
$script:server = $null

function Assert-That {
  param(
    [bool]$Condition,
    [string]$Message
  )

  if (-not $Condition) {
    throw $Message
  }
}

function Get-HttpStatusCode {
  param(
    [string]$Uri,
    [hashtable]$Headers = @{}
  )

  try {
    return [int](Invoke-WebRequest -UseBasicParsing -Uri $Uri -Headers $Headers -ErrorAction Stop).StatusCode
  } catch {
    $response = $_.Exception.Response
    if ($null -eq $response) {
      throw
    }
    return [int]$response.StatusCode
  }
}

function Get-HttpResponse {
  param(
    [string]$Uri,
    [hashtable]$Headers = @{}
  )

  try {
    $response = Invoke-WebRequest -UseBasicParsing -Uri $Uri -Headers $Headers -ErrorAction Stop
    return [PSCustomObject]@{
      StatusCode = [int]$response.StatusCode
      Content = [string]$response.Content
    }
  } catch {
    $response = $_.Exception.Response
    if ($null -eq $response) {
      throw
    }

    $statusCode = [int]$response.StatusCode
    $content = $_.ErrorDetails.Message
    try {
      if ([string]::IsNullOrWhiteSpace($content)) {
        $reader = New-Object System.IO.StreamReader($response.GetResponseStream())
        try {
          $content = $reader.ReadToEnd()
        } finally {
          $reader.Dispose()
        }
      }
    } finally {
      $response.Dispose()
    }

    return [PSCustomObject]@{
      StatusCode = $statusCode
      Content = $content
    }
  }
}

function Assert-APITokenFailure {
  param(
    [object]$Response,
    [string]$Description
  )

  Assert-That ($Response.StatusCode -eq 401) "$Description health check was not rejected."
  $body = $Response.Content | ConvertFrom-Json
  Assert-That ($body.success -eq $false -and $body.code -eq "api_token_invalid") "$Description health response did not expose the expected API Token error code (code=$($body.code); success=$($body.success))."
}

function Test-PortAvailable {
  $listeners = [System.Net.NetworkInformation.IPGlobalProperties]::GetIPGlobalProperties().GetActiveTcpListeners()
  return -not ($listeners | Where-Object { $_.Port -eq $Port })
}

function Wait-ForServer {
  $lastError = $null

  for ($attempt = 0; $attempt -lt 40; $attempt++) {
    if ($script:server.HasExited) {
      throw "Smoke server exited early with code $($script:server.ExitCode)."
    }

    try {
      $response = Invoke-WebRequest -UseBasicParsing -Uri "$baseUrl/api/health" -Headers $authHeaders -ErrorAction Stop
      if ($response.StatusCode -eq 200) {
        return
      }
    } catch {
      $lastError = $_
    }

    Start-Sleep -Milliseconds 250
  }

  throw "Smoke server did not become healthy: $lastError"
}

function Start-SmokeServer {
  $previousApiToken = [Environment]::GetEnvironmentVariable("ICLOUD_HME_API_TOKEN", "Process")
  try {
    [Environment]::SetEnvironmentVariable("ICLOUD_HME_API_TOKEN", $apiToken, "Process")
    $arguments = "-addr 127.0.0.1:$Port -data `"$dataDir`""
    $script:server = Start-Process -FilePath $nativeBinary -ArgumentList $arguments -PassThru -WindowStyle Hidden
  } finally {
    [Environment]::SetEnvironmentVariable("ICLOUD_HME_API_TOKEN", $previousApiToken, "Process")
  }

  Wait-ForServer
}

function Stop-SmokeServer {
  if ($null -eq $script:server) {
    return
  }

  $script:server.Refresh()
  if (-not $script:server.HasExited) {
    Stop-Process -Id $script:server.Id -Force
    $null = $script:server.WaitForExit(10000)
  }
  $script:server = $null
}

function Build-NativeSmokeBinary {
  $previousCgoEnabled = $env:CGO_ENABLED
  $previousGoos = $env:GOOS
  $previousGoarch = $env:GOARCH
  try {
    $env:CGO_ENABLED = "0"
    $env:GOOS = "windows"
    $env:GOARCH = "amd64"
    & go build `
      "-buildvcs=false" `
      "-trimpath" `
      "-ldflags=-s -w -buildid= -X main.version=$Version" `
      "-gcflags=-l=4" `
      "-o" `
      $nativeBinary `
      "."
    if ($LASTEXITCODE -ne 0) {
      throw "go build failed with exit code $LASTEXITCODE"
    }
  } finally {
    $env:CGO_ENABLED = $previousCgoEnabled
    $env:GOOS = $previousGoos
    $env:GOARCH = $previousGoarch
  }
}

function Remove-SmokeDirectory {
  if (-not (Test-Path -LiteralPath $tempDir)) {
    return
  }

  $resolvedTempDir = [System.IO.Path]::GetFullPath($tempDir)
  if (-not $resolvedTempDir.StartsWith($tempRoot, [StringComparison]::OrdinalIgnoreCase)) {
    throw "Refusing to remove a directory outside the system temp directory: $resolvedTempDir"
  }

  Remove-Item -LiteralPath $resolvedTempDir -Recurse -Force
}

Push-Location $rootDir
try {
  Assert-That (Test-Path -LiteralPath $buildScript) "Missing release build script: $buildScript"
  Assert-That (Test-PortAvailable) "Port $Port is already in use. Choose another value with -Port."

  $releaseBuildArguments = @{ Version = $Version; SkipUpx = $true }
  if ($SkipNpmCi) {
    $releaseBuildArguments.SkipNpmCi = $true
  }
  & $buildScript @releaseBuildArguments

  New-Item -ItemType Directory -Path $dataDir -Force | Out-Null
  Build-NativeSmokeBinary
  Start-SmokeServer

  Assert-APITokenFailure -Response (Get-HttpResponse -Uri "$baseUrl/api/health") -Description "Missing API Token"
  Assert-APITokenFailure -Response (Get-HttpResponse -Uri "$baseUrl/api/health" -Headers $invalidAuthHeaders) -Description "Invalid API Token"

  $rootResponse = Invoke-WebRequest -UseBasicParsing -Uri "$baseUrl/" -Headers @{ Accept = "text/html" } -ErrorAction Stop
  Assert-That ($rootResponse.StatusCode -eq 200) "Root page did not return HTTP 200."
  Assert-That ($rootResponse.Content -match '<div id="root"></div>') "Root page did not serve the SPA document."
  Assert-That ($rootResponse.Headers["Cache-Control"] -match "no-cache") "Root page cache policy is not no-cache."
  Assert-That ($rootResponse.Headers["Content-Security-Policy"] -match "frame-ancestors") "Root page is missing CSP frame protection."
  Assert-That ($rootResponse.Headers["X-Content-Type-Options"] -eq "nosniff") "Root page is missing nosniff."

  $assetMatch = [regex]::Match($rootResponse.Content, '(?:src|href)="(?<path>/assets/[^"]+)"')
  Assert-That $assetMatch.Success "Root page did not reference a hashed asset."
  $assetResponse = Invoke-WebRequest -UseBasicParsing -Uri "$baseUrl$($assetMatch.Groups["path"].Value)" -ErrorAction Stop
  Assert-That ($assetResponse.StatusCode -eq 200) "Referenced asset did not return HTTP 200."
  Assert-That ($assetResponse.Headers["Cache-Control"] -match "max-age=31536000, immutable") "Asset cache policy is not immutable."

  $deepLinkResponse = Invoke-WebRequest -UseBasicParsing -Uri "$baseUrl/accounts/smoke/aliases" -Headers @{ Accept = "text/html" } -ErrorAction Stop
  Assert-That ($deepLinkResponse.StatusCode -eq 200 -and $deepLinkResponse.Content -match '<div id="root"></div>') "SPA deep link did not return the application document."
  Assert-That ((Get-HttpStatusCode -Uri "$baseUrl/assets/missing.js") -eq 404) "Unknown asset did not return HTTP 404."
  Assert-That ((Get-HttpStatusCode -Uri "$baseUrl/api/missing" -Headers $authHeaders) -eq 404) "Unknown API route did not return HTTP 404."

  $healthResponse = Invoke-WebRequest -UseBasicParsing -Uri "$baseUrl/api/health" -Headers $authHeaders -ErrorAction Stop
  $health = $healthResponse.Content | ConvertFrom-Json
  Assert-That ($health.success -eq $true -and $health.data.service -eq "icloud-hme" -and $health.data.version -eq $Version) "Authenticated health response did not expose the expected safe metadata."

  $accountPayload = @{ host = "icloud.com"; icloud_email = "windows-smoke@icloud.com"; name = "Windows smoke account" } | ConvertTo-Json -Compress
  $createResponse = Invoke-WebRequest -UseBasicParsing -Uri "$baseUrl/api/accounts" -Method Post -Headers $authHeaders -ContentType "application/json" -Body $accountPayload -ErrorAction Stop
  $createdAccount = $createResponse.Content | ConvertFrom-Json
  Assert-That ($createdAccount.success -eq $true -and $createdAccount.data.name -eq "Windows smoke account") "Smoke account was not created."

  Stop-SmokeServer
  Start-SmokeServer

  $accountsResponse = Invoke-WebRequest -UseBasicParsing -Uri "$baseUrl/api/accounts" -Headers $authHeaders -ErrorAction Stop
  $accounts = $accountsResponse.Content | ConvertFrom-Json
  Assert-That ($accounts.success -eq $true -and $accounts.data.Count -eq 1 -and $accounts.data[0].icloud_email -eq "windows-smoke@icloud.com") "Account data did not persist across the server restart."

  Write-Host "Windows release smoke passed for version $Version."
} finally {
  try {
    Stop-SmokeServer
  } finally {
    try {
      Remove-SmokeDirectory
    } finally {
      Pop-Location
    }
  }
}
