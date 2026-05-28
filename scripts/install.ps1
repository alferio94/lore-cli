#Requires -Version 5.1

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
$GitHubLatestReleaseApiUrl = "https://api.github.com/repos/$RepoSlug/releases/latest"
$GitHubApiHeaders = @{
    'Accept' = 'application/vnd.github+json'
    'User-Agent' = "lore-cli-installer/$DefaultVersion"
}

function Test-IsWindowsPowerShellDesktop {
    return $PSVersionTable.PSEdition -eq 'Desktop'
}

function Enable-Tls12ForLegacyPowerShell {
    if (-not (Test-IsWindowsPowerShellDesktop)) {
        return
    }

    $currentProtocols = [System.Net.ServicePointManager]::SecurityProtocol
    if (($currentProtocols -band [System.Net.SecurityProtocolType]::Tls12) -ne [System.Net.SecurityProtocolType]::Tls12) {
        [System.Net.ServicePointManager]::SecurityProtocol = $currentProtocols -bor [System.Net.SecurityProtocolType]::Tls12
    }
}

function Invoke-WebRequestCompat {
    param(
        [Parameter(Mandatory = $true)]
        [string]$Uri,
        [string]$OutFile = '',
        [hashtable]$Headers,
        [int]$MaximumRedirection = 0
    )

    $invokeParams = @{
        Uri = $Uri
        ErrorAction = 'Stop'
    }
    if ($OutFile) {
        $invokeParams['OutFile'] = $OutFile
    }
    if ($Headers) {
        $invokeParams['Headers'] = $Headers
    }
    if ($MaximumRedirection -gt 0) {
        $invokeParams['MaximumRedirection'] = $MaximumRedirection
    }

    $invokeWebRequestCommand = Get-Command -Name Invoke-WebRequest -CommandType Cmdlet
    if ((Test-IsWindowsPowerShellDesktop) -and $invokeWebRequestCommand.Parameters.ContainsKey('UseBasicParsing')) {
        $invokeParams['UseBasicParsing'] = $true
    }

    return Invoke-WebRequest @invokeParams
}

function Get-AbsoluteUriOrNull([string]$UriValue) {
    if ([string]::IsNullOrWhiteSpace($UriValue)) {
        return $null
    }

    $parsedUri = $null
    if ([System.Uri]::TryCreate($UriValue, [System.UriKind]::Absolute, [ref]$parsedUri)) {
        return $parsedUri
    }

    return $null
}

Enable-Tls12ForLegacyPowerShell

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
        $parsedBaseUrl = Get-AbsoluteUriOrNull -UriValue $BaseUrl
        if (($null -ne $parsedBaseUrl) -and $parsedBaseUrl.Scheme -eq 'file') {
            if ($DefaultVersion.StartsWith('v')) {
                return $DefaultVersion
            }
            Fail 'latest release lookup is unavailable for file:// fixtures without an embedded release tag; rerun with -Version <tag>.'
        }

        try {
            $response = Invoke-WebRequestCompat -Uri $GitHubLatestReleaseApiUrl -Headers $GitHubApiHeaders
            $release = $response.Content | ConvertFrom-Json
        }
        catch {
            Fail "failed to resolve latest release from GitHub API; rerun with -Version <tag>. $_"
        }

        $tag = $release.tag_name
        if ([string]::IsNullOrWhiteSpace($tag) -or (-not $tag.StartsWith('v'))) {
            Fail 'latest release response did not include a valid tag_name; rerun with -Version <tag>.'
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

function Test-IsWindowsHost {
    $isWindowsVariable = Get-Variable -Name IsWindows -ErrorAction SilentlyContinue
    if ($null -ne $isWindowsVariable) {
        return [bool]$isWindowsVariable.Value
    }

    return [System.Environment]::OSVersion.Platform -eq [System.PlatformID]::Win32NT
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

    if (-not (Test-IsWindowsHost)) {
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
    $parsedUri = [System.Uri]$Uri
    if ($parsedUri.Scheme -eq 'file') {
        Copy-Item -Path $parsedUri.LocalPath -Destination $Destination -Force
        return
    }
    Invoke-WebRequestCompat -Uri $Uri -OutFile $Destination
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

function Normalize-PathSegment([string]$PathValue) {
    return $PathValue.Trim().TrimEnd([char[]]@('\', '/'))
}

function Test-PathSegmentPresent([string[]]$Segments, [string]$TargetDir) {
    $normalizedTarget = Normalize-PathSegment -PathValue $TargetDir
    foreach ($segment in $Segments) {
        if ([string]::Equals((Normalize-PathSegment -PathValue $segment), $normalizedTarget, [System.StringComparison]::OrdinalIgnoreCase)) {
            return $true
        }
    }
    return $false
}

function Update-UserPath([string]$TargetDir) {
    $currentUserPath = [Environment]::GetEnvironmentVariable('Path', 'User')
    $segments = @()
    if ($currentUserPath) {
        $segments = $currentUserPath -split ';' | Where-Object { $_ }
    }
    if (Test-PathSegmentPresent -Segments $segments -TargetDir $TargetDir) {
        return 'already-present'
    }
    $newPath = if ($currentUserPath) { "$currentUserPath;$TargetDir" } else { $TargetDir }
    [Environment]::SetEnvironmentVariable('Path', $newPath, 'User')
    return 'added'
}

function Handle-Path([string]$TargetDir, [string]$BinaryPath) {
    $pathSegments = ($env:PATH -split ';') | Where-Object { $_ }
    $currentSessionHasTarget = Test-PathSegmentPresent -Segments $pathSegments -TargetDir $TargetDir

    Write-Host "Run it directly now: $BinaryPath"

    if ($AddToPath) {
        $pathUpdateResult = Update-UserPath -TargetDir $TargetDir
        if ($pathUpdateResult -eq 'added') {
            Write-Host "Added $TargetDir to the user PATH. Open a new terminal/session to run 'lore' by name."
        }
        else {
            Write-Host "$TargetDir is already configured on the user PATH."
            if (-not $currentSessionHasTarget) {
                Write-Host "Open a new terminal/session if 'lore' is not available in this window yet."
            }
        }
        return
    }

    Write-Host "PATH is unchanged by default. Optional PATH opt-in later: rerun install.ps1 -AddToPath or add $TargetDir to your user PATH."
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
    Write-Host "Installed $targetPath"
    Handle-Path -TargetDir $InstallDir -BinaryPath $targetPath
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
