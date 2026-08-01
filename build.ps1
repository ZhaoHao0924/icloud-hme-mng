[CmdletBinding()]
param(
  [string]$Version,
  [switch]$SkipNpmCi,
  [switch]$SkipUpx
)

$ErrorActionPreference = "Stop"

if ([string]::IsNullOrWhiteSpace($Version)) {
  $Version = if ([string]::IsNullOrWhiteSpace($env:VERSION)) { "dev" } else { $env:VERSION.Trim() }
}

$rootDir = Split-Path -Parent $PSCommandPath
$outputDir = Join-Path $rootDir "build"
$binaryPath = Join-Path $outputDir "icloud-hme"
$webDir = Join-Path $rootDir "web"
$webDistDir = Join-Path $webDir "dist"
$embedDistDir = Join-Path $rootDir "internal\webui\dist"

function Invoke-NativeCommand {
  param(
    [Parameter(Mandatory = $true)]
    [string]$Command,
    [Parameter(ValueFromRemainingArguments = $true)]
    [string[]]$Arguments
  )

  & $Command @Arguments
  if ($LASTEXITCODE -ne 0) {
    throw "$Command failed with exit code $LASTEXITCODE"
  }
}

Push-Location $rootDir
try {
  Write-Host "==> Cleaning previous build output"
  if (Test-Path -LiteralPath $outputDir) {
    Remove-Item -LiteralPath $outputDir -Recurse -Force
  }
  New-Item -ItemType Directory -Path $outputDir | Out-Null

  if (-not $SkipNpmCi) {
    Write-Host "==> Installing frontend dependencies"
    Invoke-NativeCommand -Command "npm" -Arguments @("--prefix", $webDir, "ci")
  }

  Write-Host "==> Building frontend assets"
  Invoke-NativeCommand -Command "npm" -Arguments @("--prefix", $webDir, "run", "build")

  Write-Host "==> Preparing embedded frontend assets"
  $embeddedAssetsDir = Join-Path $embedDistDir "assets"
  $embeddedIndex = Join-Path $embedDistDir "index.html"
  if (Test-Path -LiteralPath $embeddedAssetsDir) {
    Remove-Item -LiteralPath $embeddedAssetsDir -Recurse -Force
  }
  if (Test-Path -LiteralPath $embeddedIndex) {
    Remove-Item -LiteralPath $embeddedIndex -Force
  }
  Copy-Item -Path (Join-Path $webDistDir "*") -Destination $embedDistDir -Recurse -Force

  Write-Host "==> Building Linux amd64 binary (version: $Version)"
  $previousCgoEnabled = $env:CGO_ENABLED
  $previousGoos = $env:GOOS
  $previousGoarch = $env:GOARCH
  try {
    $env:CGO_ENABLED = "0"
    $env:GOOS = "linux"
    $env:GOARCH = "amd64"
    Invoke-NativeCommand -Command "go" -Arguments @(
      "build",
      "-buildvcs=false",
      "-trimpath",
      "-ldflags=-s -w -buildid= -X main.version=$Version",
      "-gcflags=-l=4",
      "-o",
      $binaryPath,
      "."
    )
  } finally {
    $env:CGO_ENABLED = $previousCgoEnabled
    $env:GOOS = $previousGoos
    $env:GOARCH = $previousGoarch
  }

  $upx = Get-Command upx -ErrorAction SilentlyContinue
  if ($null -ne $upx -and -not $SkipUpx) {
    Write-Host "==> Compressing binary with upx"
    & $upx.Source --best --lzma $binaryPath
    if ($LASTEXITCODE -ne 0) {
      Write-Warning "upx failed; keeping the uncompressed binary"
    }
  } else {
    Write-Host "    (upx unavailable or skipped)"
  }

  $binary = Get-Item -LiteralPath $binaryPath
  Write-Host ""
  Write-Host "==> Build complete"
  Write-Host "    File: $($binary.FullName)"
  Write-Host "    Size: $([Math]::Round($binary.Length / 1MB, 2)) MiB"
} finally {
  Pop-Location
}
