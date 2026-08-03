#requires -Version 5.1

[CmdletBinding()]
param(
  [Parameter(Mandatory = $true)]
  [string]$DataDir,
  [string]$Destination,
  [Parameter(Mandatory = $true)]
  [switch]$ConfirmServiceStopped
)

$ErrorActionPreference = "Stop"
Import-Module (Join-Path $PSScriptRoot "DataBackup.psm1") -Force
New-DataBackup @PSBoundParameters
