param(
    [string]$Version = '__DEFAULT_VERSION__',
    [string]$InstallDir = $(Join-Path $env:LOCALAPPDATA 'Programs\Lore'),
    [switch]$AddToPath,
    [switch]$Force,
    [switch]$Help,
    [string]$BaseUrl = $env:LORE_INSTALL_BASE_URL,
    [string]$PlatformArchOverride = ''
)

$ErrorActionPreference = 'Stop'
Set-StrictMode -Version Latest

$RepoSlug = 'alferio94/lore-cli'
$BinaryName = 'lore.exe'
$DefaultVersion = '__DEFAULT_VERSION__'

function Show-Usage {
    @"
Install lore-cli from GitHub Releases.

Usage: install.ps1 [-Version <tag|latest>] [-InstallDir <dir>] [-AddToPath] [-Force] [-Help]

Defaults:
  Version: embedded release tag ($DefaultVersion)
  InstallDir: $InstallDir

Notes:
  - Pinned release asset URLs are the recommended install path.
  - Checksums provide integrity verification only; signing/notarization is out of scope.
  - Config under the user config directory is preserved by reinstall/uninstall.
"@
}

function Fail([string]$Message) {
    throw $Message
}

function Resolve-Version([string]$RequestedVersion) {
    if ([string]::IsNullOrWhiteSpace($RequestedVersion)) {
        $RequestedVersion = $DefaultVersion
    }
    if ($RequestedVersion -eq 'latest') {
        try {
            $response = Invoke-WebRequest -Uri "https://github.com/$RepoSlug/releases/latest" -MaximumRedirection 5
        }
        catch {
            Fail "failed to resolve latest release; rerun with -Version <tag>. $_"
        }
        $resolvedUri = $response.BaseResponse.ResponseUri.AbsoluteUri
        $tag = Split-Path -Path $resolvedUri -Leaf
        if (-not $tag.StartsWith('v')) {
            Fail "latest release resolved ambiguously ($resolvedUri); rerun with -Version <tag>."
        }
        return $tag
    }
    if (-not $RequestedVersion.StartsWith('v')) {
        Fail 'Version must be a release tag like v1.2.3 or the literal latest.'
    }
    return $RequestedVersion
}

function Get-ReleaseBaseUrl([string]$ResolvedVersion) {
    if ($BaseUrl) {
        return ($BaseUrl.TrimEnd('/') + '/' + $ResolvedVersion)
    }
    return "https://github.com/$RepoSlug/releases/download/$ResolvedVersion"
}

function Resolve-Platform {
    if ($PlatformArchOverride) {
        $overrideArch = $PlatformArchOverride.ToLowerInvariant()
        if ($overrideArch -notin @('amd64', 'arm64')) {
            Fail "unsupported architecture override: $PlatformArchOverride"
        }
        return [pscustomobject]@{
            Os = 'windows'
            Arch = $overrideArch
            ArchiveExt = 'zip'
        }
    }

    if (-not $IsWindows) {
        Fail 'install.ps1 only supports Windows targets.'
    }

    $arch = switch ($env:PROCESSOR_ARCHITECTURE.ToUpperInvariant()) {
        'AMD64' { 'amd64'; break }
        'ARM64' { 'arm64'; break }
        default { Fail "unsupported architecture: $env:PROCESSOR_ARCHITECTURE" }
    }

    [pscustomobject]@{
        Os = 'windows'
        Arch = $arch
        ArchiveExt = 'zip'
    }
}

function Download-File([string]$Uri, [string]$Destination) {
    Invoke-WebRequest -Uri $Uri -OutFile $Destination
}

function Verify-Checksum([string]$SumsFile, [string]$ArchiveFile) {
    $archiveName = [System.IO.Path]::GetFileName($ArchiveFile)
    $expectedLine = Select-String -Path $SumsFile -Pattern ([regex]::Escape($archiveName) + '$') | Select-Object -First 1
    if (-not $expectedLine) {
        Fail "missing checksum for $archiveName"
    }
    $expected = ($expectedLine.Line -split '\s+')[0].ToLowerInvariant()
    $actual = (Get-FileHash -Path $ArchiveFile -Algorithm SHA256).Hash.ToLowerInvariant()
    if ($actual -ne $expected) {
        Fail "checksum mismatch for $archiveName"
    }
}

function Install-Binary([string]$ExtractedBinary, [string]$TargetDir) {
    New-Item -ItemType Directory -Force -Path $TargetDir | Out-Null
    $targetPath = Join-Path $TargetDir $BinaryName
    $tempTarget = "$targetPath.tmp"
    Copy-Item -Path $ExtractedBinary -Destination $tempTarget -Force
    Move-Item -Path $tempTarget -Destination $targetPath -Force
    return $targetPath
}

function Verify-Install([string]$BinaryPath) {
    & $BinaryPath version | Out-Null
    if ($LASTEXITCODE -ne 0) {
        Fail 'installed binary failed version check'
    }
}

function Update-UserPath([string]$TargetDir) {
    $currentUserPath = [Environment]::GetEnvironmentVariable('Path', 'User')
    $segments = @()
    if ($currentUserPath) {
        $segments = $currentUserPath -split ';' | Where-Object { $_ }
    }
    if ($segments -contains $TargetDir) {
        Write-Host "$TargetDir is already on the user PATH."
        return
    }
    $newPath = if ($currentUserPath) { "$currentUserPath;$TargetDir" } else { $TargetDir }
    [Environment]::SetEnvironmentVariable('Path', $newPath, 'User')
    Write-Host "Added $TargetDir to the user PATH. Open a new terminal to pick up the change."
}

function Handle-Path([string]$TargetDir) {
    $pathSegments = ($env:PATH -split ';') | Where-Object { $_ }
    if ($pathSegments -contains $TargetDir) {
        Write-Host "$TargetDir is already on PATH."
        return
    }
    if ($AddToPath) {
        Update-UserPath -TargetDir $TargetDir
    }
    else {
        Write-Host "Add $TargetDir to PATH to run 'lore' without a full path:"
        Write-Host "  [Environment]::SetEnvironmentVariable('Path', [Environment]::GetEnvironmentVariable('Path', 'User') + ';$TargetDir', 'User')"
    }
}

if ($Help) {
    Show-Usage
    exit 0
}

$ResolvedVersion = Resolve-Version -RequestedVersion $Version
$platform = Resolve-Platform
$archiveName = "lore-cli_${ResolvedVersion}_$($platform.Os)_$($platform.Arch).$($platform.ArchiveExt)"
$releaseBaseUrl = Get-ReleaseBaseUrl -ResolvedVersion $ResolvedVersion
$tempDir = Join-Path ([System.IO.Path]::GetTempPath()) ([System.Guid]::NewGuid().ToString('N'))
New-Item -ItemType Directory -Force -Path $tempDir | Out-Null

try {
    $archivePath = Join-Path $tempDir $archiveName
    $sumsPath = Join-Path $tempDir 'SHA256SUMS'
    Write-Host "Installing lore $ResolvedVersion for $($platform.Os)/$($platform.Arch)"
    Download-File -Uri "$releaseBaseUrl/$archiveName" -Destination $archivePath
    Download-File -Uri "$releaseBaseUrl/SHA256SUMS" -Destination $sumsPath
    Verify-Checksum -SumsFile $sumsPath -ArchiveFile $archivePath
    Expand-Archive -Path $archivePath -DestinationPath $tempDir -Force
    $extractedBinary = Join-Path $tempDir $BinaryName
    if (-not (Test-Path -Path $extractedBinary)) {
        Fail "$archiveName did not contain $BinaryName"
    }
    $targetPath = Install-Binary -ExtractedBinary $extractedBinary -TargetDir $InstallDir
    Verify-Install -BinaryPath $targetPath
    Handle-Path -TargetDir $InstallDir
    Write-Host "Installed $targetPath"
    Write-Host "Uninstall: delete $targetPath; config under your user config directory is preserved by default."
    if ($Force) {
        Write-Verbose 'Force flag accepted; installs already replace existing binaries idempotently.'
    }
}
finally {
    if (Test-Path -Path $tempDir) {
        Remove-Item -Path $tempDir -Recurse -Force
    }
}
