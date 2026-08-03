#requires -Version 5.1

[CmdletBinding()]
param()

$ErrorActionPreference = "Stop"

function Assert-That {
  param(
    [Parameter(Mandatory = $true)]
    [bool]$Condition,
    [Parameter(Mandatory = $true)]
    [string]$Message
  )

  if (-not $Condition) {
    throw $Message
  }
}

$tempRoot = [System.IO.Path]::GetFullPath([System.IO.Path]::GetTempPath())
$tempDir = Join-Path $tempRoot ("icloud-hme-backup-smoke-" + [guid]::NewGuid().ToString("N"))
$sourceDir = Join-Path $tempDir "source-data"
$backupDir = Join-Path $tempDir "backups"
$targetDir = Join-Path $tempDir "restore-target"

Import-Module (Join-Path $PSScriptRoot "DataBackup.psm1") -Force
try {
  $null = New-Item -ItemType Directory -Path (Join-Path $sourceDir "nested") -Force
  [System.IO.File]::WriteAllText(
    (Join-Path $sourceDir "accounts.json"),
    '{"accounts":[],"updated_at":"2026-08-02T00:00:00Z"}'
  )
  [System.IO.File]::WriteAllText(
    (Join-Path $sourceDir "platform-auth.json"),
    '{"configured":true,"username":"smoke-admin"}'
  )
  [System.IO.File]::WriteAllText(
    (Join-Path $sourceDir "nested\operation-log.json"),
    '{"entries":[]}'
  )

  $backup = New-DataBackup -DataDir $sourceDir -Destination $backupDir -ConfirmServiceStopped
  $verification = Test-DataBackupArchive -ArchivePath $backup.ArchivePath
  Assert-That ($verification.Valid -and $verification.FileCount -eq 3) "Backup archive verification failed."

  $null = New-Item -ItemType Directory -Path $targetDir -Force
  [System.IO.File]::WriteAllText((Join-Path $targetDir "stale.txt"), "stale")
  $restore = Restore-DataBackup `
    -ArchivePath $backup.ArchivePath `
    -DataDir $targetDir `
    -ConfirmServiceStopped `
    -ConfirmRestore

  Assert-That (Test-Path -LiteralPath (Join-Path $targetDir "accounts.json") -PathType Leaf) "accounts.json was not restored."
  Assert-That (Test-Path -LiteralPath (Join-Path $targetDir "nested\operation-log.json") -PathType Leaf) "Nested data was not restored."
  Assert-That (-not (Test-Path -LiteralPath (Join-Path $targetDir "stale.txt"))) "Stale target data was not replaced."
  Assert-That (Test-Path -LiteralPath $restore.RollbackPath -PathType Container) "Rollback directory was not retained."
  Assert-That (Test-Path -LiteralPath (Join-Path $restore.RollbackPath "stale.txt") -PathType Leaf) "Rollback data was not retained."

  $sourceHash = (Get-FileHash -LiteralPath (Join-Path $sourceDir "accounts.json") -Algorithm SHA256).Hash
  $restoredHash = (Get-FileHash -LiteralPath (Join-Path $targetDir "accounts.json") -Algorithm SHA256).Hash
  Assert-That ($sourceHash -eq $restoredHash) "Restored data does not match the backup source."

  Write-Host "Backup and restore smoke passed."
} finally {
  if (Test-Path -LiteralPath $tempDir) {
    $resolvedTempDir = [System.IO.Path]::GetFullPath($tempDir)
    if (-not $resolvedTempDir.StartsWith($tempRoot, [System.StringComparison]::OrdinalIgnoreCase)) {
      throw "Refusing to remove a directory outside the system temp directory: $resolvedTempDir"
    }
    Remove-Item -LiteralPath $resolvedTempDir -Recurse -Force
  }
}
