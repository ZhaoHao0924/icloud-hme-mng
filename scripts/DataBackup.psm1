#requires -Version 5.1

Set-StrictMode -Version Latest
$ErrorActionPreference = "Stop"

$script:DataBackupManifestName = "backup-manifest.json"
$script:DataBackupManifestFormat = 1

function Initialize-DataBackupZipSupport {
  if ($null -eq ("System.IO.Compression.ZipFile" -as [type])) {
    Add-Type -AssemblyName System.IO.Compression.FileSystem
  }
}

function Resolve-DataBackupFullPath {
  param(
    [Parameter(Mandatory = $true)]
    [string]$Path
  )

  if ([string]::IsNullOrWhiteSpace($Path)) {
    throw "Path is required."
  }

  try {
    return [System.IO.Path]::GetFullPath($Path)
  } catch {
    throw "Path is invalid: $Path"
  }
}

function Remove-DataBackupTrailingSeparators {
  param(
    [Parameter(Mandatory = $true)]
    [string]$Path
  )

  return $Path.TrimEnd(
    [char[]]@([System.IO.Path]::DirectorySeparatorChar, [System.IO.Path]::AltDirectorySeparatorChar)
  )
}

function Assert-DataBackupSafeDirectory {
  param(
    [Parameter(Mandatory = $true)]
    [string]$Path,
    [switch]$MustExist
  )

  $fullPath = Resolve-DataBackupFullPath -Path $Path
  $trimmedPath = Remove-DataBackupTrailingSeparators -Path $fullPath
  $trimmedRoot = Remove-DataBackupTrailingSeparators -Path ([System.IO.Path]::GetPathRoot($fullPath))
  if ($trimmedPath -eq $trimmedRoot) {
    throw "Refusing to use a filesystem root as a data directory: $fullPath"
  }

  if (
    (Test-Path -LiteralPath $fullPath) -and
    -not (Test-Path -LiteralPath $fullPath -PathType Container)
  ) {
    throw "Data directory path exists but is not a directory: $fullPath"
  }

  if ($MustExist) {
    if (-not (Test-Path -LiteralPath $fullPath -PathType Container)) {
      throw "Data directory does not exist: $fullPath"
    }
  }

  return $trimmedPath
}

function Test-DataBackupChildPath {
  param(
    [Parameter(Mandatory = $true)]
    [string]$Path,
    [Parameter(Mandatory = $true)]
    [string]$Parent
  )

  $fullPath = Resolve-DataBackupFullPath -Path $Path
  $fullParent = Remove-DataBackupTrailingSeparators -Path (Resolve-DataBackupFullPath -Path $Parent)
  $prefix = $fullParent + [System.IO.Path]::DirectorySeparatorChar
  return $fullPath.StartsWith($prefix, [System.StringComparison]::OrdinalIgnoreCase)
}

function Assert-DataBackupNoReparsePoints {
  param(
    [Parameter(Mandatory = $true)]
    [string]$DataDir
  )

  $items = @(
    Get-Item -LiteralPath $DataDir -Force
    Get-ChildItem -LiteralPath $DataDir -Force -Recurse
  )
  foreach ($item in $items) {
    if (($item.Attributes -band [System.IO.FileAttributes]::ReparsePoint) -ne 0) {
      throw "Data directory cannot contain symbolic links or other reparse points: $($item.FullName)"
    }
  }
}

function Get-DataBackupFiles {
  param(
    [Parameter(Mandatory = $true)]
    [string]$DataDir
  )

  Assert-DataBackupNoReparsePoints -DataDir $DataDir
  return @(Get-ChildItem -LiteralPath $DataDir -Force -File -Recurse | Sort-Object FullName)
}

function ConvertTo-DataBackupRelativePath {
  param(
    [Parameter(Mandatory = $true)]
    [string]$DataDir,
    [Parameter(Mandatory = $true)]
    [string]$FilePath
  )

  $root = Remove-DataBackupTrailingSeparators -Path (Resolve-DataBackupFullPath -Path $DataDir)
  $fullFilePath = Resolve-DataBackupFullPath -Path $FilePath
  $prefix = $root + [System.IO.Path]::DirectorySeparatorChar
  if (-not $fullFilePath.StartsWith($prefix, [System.StringComparison]::OrdinalIgnoreCase)) {
    throw "File is outside the data directory: $fullFilePath"
  }

  return $fullFilePath.Substring($prefix.Length).Replace("\", "/")
}

function Assert-DataBackupEntryName {
  param(
    [Parameter(Mandatory = $true)]
    [string]$EntryName
  )

  if ([string]::IsNullOrWhiteSpace($EntryName)) {
    throw "Archive entry name cannot be empty."
  }

  $normalized = $EntryName.Replace("\", "/")
  if (
    $normalized.StartsWith("/") -or
    $normalized -match "^[A-Za-z]:" -or
    $normalized.Contains(":") -or
    $normalized -match "(^|/)\.\.(/|$)" -or
    $normalized -match "(^|/)\.(/|$)"
  ) {
    throw "Archive entry path is unsafe: $EntryName"
  }
}

function Get-DataBackupStreamHash {
  param(
    [Parameter(Mandatory = $true)]
    [System.IO.Stream]$Stream
  )

  $algorithm = [System.Security.Cryptography.SHA256]::Create()
  try {
    $hash = $algorithm.ComputeHash($Stream)
  } finally {
    $algorithm.Dispose()
  }
  return ([System.BitConverter]::ToString($hash)).Replace("-", "").ToLowerInvariant()
}

function Get-DataBackupRequiredProperty {
  param(
    [Parameter(Mandatory = $true)]
    [object]$Object,
    [Parameter(Mandatory = $true)]
    [string]$Name
  )

  $property = $Object.PSObject.Properties[$Name]
  if ($null -eq $property) {
    throw "Backup manifest is missing required property: $Name"
  }
  return $property.Value
}

function ConvertFrom-DataBackupManifest {
  param(
    [Parameter(Mandatory = $true)]
    [string]$Content
  )

  try {
    $manifest = $Content | ConvertFrom-Json
  } catch {
    throw "Backup manifest is not valid JSON."
  }
  if ($null -eq $manifest -or $manifest -is [System.Array]) {
    throw "Backup manifest must be a JSON object."
  }

  $formatVersion = Get-DataBackupRequiredProperty -Object $manifest -Name "format_version"
  if ($formatVersion -isnot [int] -and $formatVersion -isnot [long]) {
    throw "Backup manifest format_version must be an integer."
  }
  if ([int]$formatVersion -ne $script:DataBackupManifestFormat) {
    throw "Unsupported backup manifest format: $formatVersion"
  }

  $fileCount = Get-DataBackupRequiredProperty -Object $manifest -Name "file_count"
  if ($fileCount -isnot [int] -and $fileCount -isnot [long]) {
    throw "Backup manifest file_count must be an integer."
  }
  if ([long]$fileCount -lt 0) {
    throw "Backup manifest file_count cannot be negative."
  }

  $files = @(Get-DataBackupRequiredProperty -Object $manifest -Name "files")
  if ([long]$fileCount -ne $files.Count) {
    throw "Backup manifest file_count does not match files."
  }

  $seenPaths = @{}
  foreach ($file in $files) {
    $relativePath = [string](Get-DataBackupRequiredProperty -Object $file -Name "path")
    Assert-DataBackupEntryName -EntryName $relativePath
    if ($relativePath -eq $script:DataBackupManifestName) {
      throw "Backup manifest cannot list itself as a data file."
    }
    if ($seenPaths.ContainsKey($relativePath)) {
      throw "Backup manifest contains duplicate path: $relativePath"
    }
    $seenPaths[$relativePath] = $true

    $size = Get-DataBackupRequiredProperty -Object $file -Name "size"
    if ($size -isnot [int] -and $size -isnot [long]) {
      throw "Backup manifest file size must be an integer: $relativePath"
    }
    if ([long]$size -lt 0) {
      throw "Backup manifest file size cannot be negative: $relativePath"
    }

    $hash = [string](Get-DataBackupRequiredProperty -Object $file -Name "sha256")
    if ($hash -notmatch "^[a-fA-F0-9]{64}$") {
      throw "Backup manifest SHA-256 is invalid: $relativePath"
    }
  }

  return $manifest
}

function Read-DataBackupManifestFile {
  param(
    [Parameter(Mandatory = $true)]
    [string]$ManifestPath
  )

  if (-not (Test-Path -LiteralPath $ManifestPath -PathType Leaf)) {
    throw "Backup manifest is missing: $ManifestPath"
  }
  return ConvertFrom-DataBackupManifest -Content ([System.IO.File]::ReadAllText($ManifestPath))
}

function Test-DataBackupDirectoryContents {
  param(
    [Parameter(Mandatory = $true)]
    [string]$DataDir
  )

  $manifestPath = Join-Path $DataDir $script:DataBackupManifestName
  $manifest = Read-DataBackupManifestFile -ManifestPath $manifestPath
  $actualFiles = @{}
  foreach ($file in Get-DataBackupFiles -DataDir $DataDir) {
    $relativePath = ConvertTo-DataBackupRelativePath -DataDir $DataDir -FilePath $file.FullName
    if ($relativePath -eq $script:DataBackupManifestName) {
      continue
    }
    $actualFiles[$relativePath] = $file
  }

  $expectedFiles = @(Get-DataBackupRequiredProperty -Object $manifest -Name "files")
  if ($actualFiles.Count -ne $expectedFiles.Count) {
    throw "Restored data contains an unexpected number of files."
  }

  foreach ($expected in $expectedFiles) {
    $relativePath = [string](Get-DataBackupRequiredProperty -Object $expected -Name "path")
    if (-not $actualFiles.ContainsKey($relativePath)) {
      throw "Restored data is missing file: $relativePath"
    }
    $actual = $actualFiles[$relativePath]
    if ([long]$actual.Length -ne [long](Get-DataBackupRequiredProperty -Object $expected -Name "size")) {
      throw "Restored data file size does not match: $relativePath"
    }
    $actualHash = (Get-FileHash -LiteralPath $actual.FullName -Algorithm SHA256).Hash.ToLowerInvariant()
    $expectedHash = ([string](Get-DataBackupRequiredProperty -Object $expected -Name "sha256")).ToLowerInvariant()
    if ($actualHash -ne $expectedHash) {
      throw "Restored data file hash does not match: $relativePath"
    }
  }

  return [pscustomobject]@{
    FileCount = $expectedFiles.Count
    Manifest = $manifest
  }
}

function New-DataBackup {
  [CmdletBinding()]
  param(
    [Parameter(Mandatory = $true)]
    [string]$DataDir,
    [string]$Destination,
    [Parameter(Mandatory = $true)]
    [switch]$ConfirmServiceStopped
  )

  if (-not $ConfirmServiceStopped) {
    throw "Pass -ConfirmServiceStopped only after the service has been stopped."
  }

  Initialize-DataBackupZipSupport
  $sourceDir = Assert-DataBackupSafeDirectory -Path $DataDir -MustExist
  if (Test-Path -LiteralPath (Join-Path $sourceDir $script:DataBackupManifestName)) {
    throw "Data directory contains reserved file $script:DataBackupManifestName. Move it before creating a backup."
  }

  if ([string]::IsNullOrWhiteSpace($Destination)) {
    $Destination = Join-Path (Split-Path -Parent $sourceDir) "backups"
  }
  $destinationDir = Assert-DataBackupSafeDirectory -Path $Destination
  if (-not (Test-Path -LiteralPath $destinationDir)) {
    $null = New-Item -ItemType Directory -Path $destinationDir -Force
  }
  if (
    $destinationDir.Equals($sourceDir, [System.StringComparison]::OrdinalIgnoreCase) -or
    (Test-DataBackupChildPath -Path $destinationDir -Parent $sourceDir)
  ) {
    throw "Backup destination must be outside the data directory."
  }

  $timestamp = (Get-Date).ToUniversalTime().ToString("yyyyMMddTHHmmssZ")
  $archivePath = Join-Path $destinationDir "icloud-hme-data-$timestamp.zip"
  if (Test-Path -LiteralPath $archivePath) {
    throw "Backup archive already exists: $archivePath"
  }
  $stagingRoot = Join-Path $destinationDir ".icloud-hme-backup-staging"
  $stagingDir = Join-Path $stagingRoot ([guid]::NewGuid().ToString("N"))
  $archiveCreated = $false

  try {
    $null = New-Item -ItemType Directory -Path $stagingDir -Force
    foreach ($file in Get-DataBackupFiles -DataDir $sourceDir) {
      $relativePath = ConvertTo-DataBackupRelativePath -DataDir $sourceDir -FilePath $file.FullName
      $targetPath = Join-Path $stagingDir ($relativePath.Replace("/", [System.IO.Path]::DirectorySeparatorChar))
      $null = New-Item -ItemType Directory -Path (Split-Path -Parent $targetPath) -Force
      [System.IO.File]::Copy($file.FullName, $targetPath, $true)
    }

    $manifestFiles = @()
    foreach ($file in Get-DataBackupFiles -DataDir $stagingDir) {
      $manifestFiles += [ordered]@{
        path = ConvertTo-DataBackupRelativePath -DataDir $stagingDir -FilePath $file.FullName
        sha256 = (Get-FileHash -LiteralPath $file.FullName -Algorithm SHA256).Hash.ToLowerInvariant()
        size = [long]$file.Length
      }
    }
    $manifest = [ordered]@{
      created_at = (Get-Date).ToUniversalTime().ToString("o")
      file_count = $manifestFiles.Count
      files = $manifestFiles
      format_version = $script:DataBackupManifestFormat
    }
    $utf8 = New-Object System.Text.UTF8Encoding($false)
    [System.IO.File]::WriteAllText(
      (Join-Path $stagingDir $script:DataBackupManifestName),
      ($manifest | ConvertTo-Json -Depth 8),
      $utf8
    )

    [System.IO.Compression.ZipFile]::CreateFromDirectory(
      $stagingDir,
      $archivePath,
      [System.IO.Compression.CompressionLevel]::Optimal,
      $false
    )
    $archiveCreated = $true
    $verification = Test-DataBackupArchive -ArchivePath $archivePath
    return [pscustomobject]@{
      ArchivePath = $verification.ArchivePath
      ArchiveSha256 = $verification.ArchiveSha256
      CreatedAt = $verification.CreatedAt
      FileCount = $verification.FileCount
    }
  } catch {
    if ($archiveCreated -and (Test-Path -LiteralPath $archivePath -PathType Leaf)) {
      Remove-Item -LiteralPath $archivePath -Force
    }
    throw
  } finally {
    if (Test-Path -LiteralPath $stagingDir) {
      Remove-Item -LiteralPath $stagingDir -Recurse -Force
    }
    if ((Test-Path -LiteralPath $stagingRoot) -and -not (Get-ChildItem -LiteralPath $stagingRoot -Force | Select-Object -First 1)) {
      Remove-Item -LiteralPath $stagingRoot -Force
    }
  }
}

function Test-DataBackupArchive {
  [CmdletBinding()]
  param(
    [Parameter(Mandatory = $true)]
    [string]$ArchivePath
  )

  Initialize-DataBackupZipSupport
  $fullArchivePath = Resolve-DataBackupFullPath -Path $ArchivePath
  if (-not (Test-Path -LiteralPath $fullArchivePath -PathType Leaf)) {
    throw "Backup archive does not exist: $fullArchivePath"
  }

  $archive = [System.IO.Compression.ZipFile]::Open(
    $fullArchivePath,
    [System.IO.Compression.ZipArchiveMode]::Read
  )
  try {
    $entriesByName = @{}
    foreach ($entry in $archive.Entries) {
      if ([string]::IsNullOrWhiteSpace($entry.FullName)) {
        continue
      }
      $entryName = $entry.FullName.Replace("\", "/")
      Assert-DataBackupEntryName -EntryName $entryName
      if ([string]::IsNullOrWhiteSpace($entry.Name)) {
        continue
      }
      if ($entriesByName.ContainsKey($entryName)) {
        throw "Backup archive contains duplicate entry: $entryName"
      }
      $entriesByName[$entryName] = $entry
    }
    if (-not $entriesByName.ContainsKey($script:DataBackupManifestName)) {
      throw "Backup archive does not contain $script:DataBackupManifestName."
    }

    $manifestStream = $entriesByName[$script:DataBackupManifestName].Open()
    $reader = New-Object System.IO.StreamReader($manifestStream, [System.Text.Encoding]::UTF8, $true)
    try {
      $manifest = ConvertFrom-DataBackupManifest -Content $reader.ReadToEnd()
    } finally {
      $reader.Dispose()
      $manifestStream.Dispose()
    }

    $expectedFiles = @(Get-DataBackupRequiredProperty -Object $manifest -Name "files")
    if ($entriesByName.Count -ne ($expectedFiles.Count + 1)) {
      throw "Backup archive contains unexpected files."
    }
    foreach ($expected in $expectedFiles) {
      $relativePath = [string](Get-DataBackupRequiredProperty -Object $expected -Name "path")
      if (-not $entriesByName.ContainsKey($relativePath)) {
        throw "Backup archive is missing file: $relativePath"
      }
      $entry = $entriesByName[$relativePath]
      if ([long]$entry.Length -ne [long](Get-DataBackupRequiredProperty -Object $expected -Name "size")) {
        throw "Backup archive file size does not match: $relativePath"
      }
      $entryStream = $entry.Open()
      try {
        $actualHash = Get-DataBackupStreamHash -Stream $entryStream
      } finally {
        $entryStream.Dispose()
      }
      $expectedHash = ([string](Get-DataBackupRequiredProperty -Object $expected -Name "sha256")).ToLowerInvariant()
      if ($actualHash -ne $expectedHash) {
        throw "Backup archive file hash does not match: $relativePath"
      }
    }

    return [pscustomobject]@{
      ArchivePath = $fullArchivePath
      ArchiveSha256 = (Get-FileHash -LiteralPath $fullArchivePath -Algorithm SHA256).Hash.ToLowerInvariant()
      CreatedAt = [string](Get-DataBackupRequiredProperty -Object $manifest -Name "created_at")
      FileCount = $expectedFiles.Count
      Valid = $true
    }
  } finally {
    $archive.Dispose()
  }
}

function Restore-DataBackup {
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

  if (-not $ConfirmServiceStopped) {
    throw "Pass -ConfirmServiceStopped only after the service has been stopped."
  }
  if (-not $ConfirmRestore) {
    throw "Pass -ConfirmRestore to replace the target data directory."
  }

  Initialize-DataBackupZipSupport
  $fullArchivePath = Resolve-DataBackupFullPath -Path $ArchivePath
  $targetDir = Assert-DataBackupSafeDirectory -Path $DataDir
  if (-not (Test-Path -LiteralPath (Split-Path -Parent $targetDir) -PathType Container)) {
    throw "Parent directory for restore target does not exist: $(Split-Path -Parent $targetDir)"
  }
  if (Test-DataBackupChildPath -Path $fullArchivePath -Parent $targetDir) {
    throw "Backup archive must be stored outside the target data directory."
  }

  $verification = Test-DataBackupArchive -ArchivePath $fullArchivePath
  $targetParent = Split-Path -Parent $targetDir
  $timestamp = (Get-Date).ToUniversalTime().ToString("yyyyMMddTHHmmssZ")
  $stagingDir = Join-Path $targetParent ".icloud-hme-restore-stage-$([guid]::NewGuid().ToString("N"))"
  $rollbackDir = "$targetDir.restore-before-$timestamp"
  if (Test-Path -LiteralPath $stagingDir) {
    throw "Restore staging directory already exists: $stagingDir"
  }
  if (Test-Path -LiteralPath $rollbackDir) {
    throw "Restore rollback directory already exists: $rollbackDir"
  }

  $previousMoved = $false
  try {
    $null = New-Item -ItemType Directory -Path $stagingDir -Force
    [System.IO.Compression.ZipFile]::ExtractToDirectory($fullArchivePath, $stagingDir)
    $stagedVerification = Test-DataBackupDirectoryContents -DataDir $stagingDir
    Remove-Item -LiteralPath (Join-Path $stagingDir $script:DataBackupManifestName) -Force

    if (Test-Path -LiteralPath $targetDir) {
      Move-Item -LiteralPath $targetDir -Destination $rollbackDir
      $previousMoved = $true
    }
    Move-Item -LiteralPath $stagingDir -Destination $targetDir
    $stagingDir = $null

    return [pscustomobject]@{
      ArchivePath = $verification.ArchivePath
      ArchiveSha256 = $verification.ArchiveSha256
      FileCount = $stagedVerification.FileCount
      RestoredDataDir = $targetDir
      RollbackPath = if ($previousMoved) { $rollbackDir } else { "" }
    }
  } catch {
    if ($previousMoved -and -not (Test-Path -LiteralPath $targetDir) -and (Test-Path -LiteralPath $rollbackDir)) {
      Move-Item -LiteralPath $rollbackDir -Destination $targetDir
    }
    throw
  } finally {
    if ($null -ne $stagingDir -and (Test-Path -LiteralPath $stagingDir)) {
      Remove-Item -LiteralPath $stagingDir -Recurse -Force
    }
  }
}

Export-ModuleMember -Function New-DataBackup, Restore-DataBackup, Test-DataBackupArchive
