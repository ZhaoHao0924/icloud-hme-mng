#requires -Version 5.1

[CmdletBinding()]
param(
  [Parameter(Mandatory = $true)]
  [string]$ArchivePath,
  [Parameter(Mandatory = $true)]
  [string]$DataDir,
  [Parameter(Mandatory = $true)]
  [switch]$ConfirmServiceStopped,
  [Parameter(Mandatory = $true)]
  [switch]$ConfirmRestore
)

$ErrorActionPreference = "Stop"
Import-Module (Join-Path $PSScriptRoot "DataBackup.psm1") -Force
Restore-DataBackup @PSBoundParameters
