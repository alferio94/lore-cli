param(
    [string]$FixtureVersion = 'v9.9.9'
)

$ErrorActionPreference = 'Stop'
Set-StrictMode -Version Latest

$RootDir = Split-Path -Path $PSScriptRoot -Parent
$WorkDir = Join-Path ([System.IO.Path]::GetTempPath()) ("lore-installer-smoke-" + [System.Guid]::NewGuid().ToString('N'))

function Remove-WorkDir {
    if (Test-Path -Path $WorkDir) {
        Remove-Item -Path $WorkDir -Recurse -Force
    }
}

try {
    $releaseDir = Join-Path $WorkDir (Join-Path 'releases' $FixtureVersion)
    $buildDir = Join-Path $WorkDir 'build'
    $localAppData = Join-Path $WorkDir 'LocalAppData'
    New-Item -ItemType Directory -Force -Path $releaseDir, $buildDir, $localAppData | Out-Null

    Push-Location $RootDir
    try {
        $env:GOFLAGS = ''
        go build -trimpath `
            -ldflags "-X github.com/alferio94/lore-cli/internal/version.Version=$FixtureVersion -X github.com/alferio94/lore-cli/internal/version.Commit=test -X github.com/alferio94/lore-cli/internal/version.BuildDate=test" `
            -o (Join-Path $buildDir 'lore.exe') ./cmd/lore
    }
    finally {
        Pop-Location
    }

    $hostArch = switch ($env:PROCESSOR_ARCHITECTURE.ToUpperInvariant()) {
        'AMD64' { 'amd64'; break }
        'ARM64' { 'arm64'; break }
        default { throw "unsupported Windows test host architecture: $env:PROCESSOR_ARCHITECTURE" }
    }
    $archiveName = "lore-cli_${FixtureVersion}_windows_${hostArch}.zip"
    $releaseExe = Join-Path $releaseDir 'lore.exe'
    Copy-Item -Path (Join-Path $buildDir 'lore.exe') -Destination $releaseExe -Force
    Compress-Archive -Path $releaseExe -DestinationPath (Join-Path $releaseDir $archiveName) -Force

    $checksum = (Get-FileHash -Path (Join-Path $releaseDir $archiveName) -Algorithm SHA256).Hash.ToLowerInvariant()
    Set-Content -Path (Join-Path $releaseDir 'SHA256SUMS') -Value "$checksum  $archiveName"

    $renderedInstaller = Join-Path $WorkDir 'install.ps1'
    (Get-Content -Path (Join-Path $RootDir 'scripts/install.ps1') -Raw).Replace('__DEFAULT_VERSION__', $FixtureVersion) | Set-Content -Path $renderedInstaller

    $env:LOCALAPPDATA = $localAppData
    $env:LORE_INSTALL_BASE_URL = ([System.Uri]::new((Join-Path $WorkDir 'releases'))).AbsoluteUri.TrimEnd('/')
    $output = (Get-Content -Path $renderedInstaller -Raw | Invoke-Expression 2>&1 | Out-String)

    $installedBinary = Join-Path $env:LOCALAPPDATA 'Programs\Lore\lore.exe'
    if (-not (Test-Path -Path $installedBinary)) {
        throw "installer did not produce $installedBinary"
    }

    $versionOutput = (& $installedBinary version | Out-String)
    if ($versionOutput -notmatch [regex]::Escape($FixtureVersion)) {
        throw "installed binary version output did not include $FixtureVersion. Output: $versionOutput"
    }
    if ($output -notmatch [regex]::Escape("Run it directly now: $installedBinary")) {
        throw 'installer output did not include direct-run guidance'
    }

    Write-Host 'windows installer smoke tests passed'
}
finally {
    Remove-WorkDir
}
