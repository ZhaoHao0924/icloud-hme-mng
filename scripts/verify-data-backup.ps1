#requires -Version 5.1

[CmdletBinding()]
param(
  [Parameter(Mandatory = $true)]
  [string]$ArchivePath
)

$ErrorActionPreference = "Stop"
Import-Module (Join-Path $PSScriptRoot "DataBackup.psm1") -Force
Test-DataBackupArchive @PSBoundParameters
