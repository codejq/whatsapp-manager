<#
.SYNOPSIS
  Packs the staged app into an MSIX for the Microsoft Store.

  Identity values come from environment variables (CI secrets) and default to
  development placeholders:
    MSIX_IDENTITY_NAME      e.g. 12345Publisher.WhatsAppManager
    MSIX_PUBLISHER          e.g. CN=XXXXXXXX-XXXX-XXXX-XXXX-XXXXXXXXXXXX
    MSIX_PUBLISHER_DISPLAY  e.g. Your Name

  The Store signs the package on ingestion. For local sideload testing, sign
  it with a self-signed certificate (see docs/RELEASING.md).
#>
param(
    [Parameter(Mandatory)][string]$Version,   # 4-part numeric, last part must be 0 for the Store
    [Parameter(Mandatory)][string]$SourceDir,
    [Parameter(Mandatory)][string]$OutputDir
)
$ErrorActionPreference = 'Stop'
$here = Split-Path -Parent $MyInvocation.MyCommand.Path
$root = Resolve-Path (Join-Path $here '..\..')

function Find-SdkTool($name) {
    $cmd = Get-Command $name -ErrorAction SilentlyContinue
    if ($cmd) { return $cmd.Source }
    $kits = "${env:ProgramFiles(x86)}\Windows Kits\10\bin"
    if (Test-Path $kits) {
        $hit = Get-ChildItem $kits -Recurse -Filter $name -ErrorAction SilentlyContinue |
            Where-Object { $_.FullName -match '\\x64\\' } | Sort-Object FullName -Descending | Select-Object -First 1
        if ($hit) { return $hit.FullName }
    }
    throw "$name not found. Install the Windows 10/11 SDK (winget install Microsoft.WindowsSDK.10.0.26100)."
}

$identity = if ($env:MSIX_IDENTITY_NAME) { $env:MSIX_IDENTITY_NAME } else { 'WhatsAppManager.Dev' }
$publisher = if ($env:MSIX_PUBLISHER) { $env:MSIX_PUBLISHER } else { 'CN=WhatsAppManagerDev' }
$display = if ($env:MSIX_PUBLISHER_DISPLAY) { $env:MSIX_PUBLISHER_DISPLAY } else { 'WhatsApp Manager contributors' }

$layout = Join-Path $OutputDir 'msix-layout'
if (Test-Path $layout) { Remove-Item -Recurse -Force $layout }
New-Item -ItemType Directory -Force $layout, (Join-Path $layout 'Assets') | Out-Null
Copy-Item (Join-Path $SourceDir '*') $layout -Recurse
Copy-Item (Join-Path $root 'assets\msix\*') (Join-Path $layout 'Assets')

$manifest = Get-Content (Join-Path $here 'AppxManifest.xml') -Raw
$manifest = $manifest.Replace('__IDENTITY_NAME__', $identity).Replace('__PUBLISHER__', $publisher).
    Replace('__PUBLISHER_DISPLAY__', [Security.SecurityElement]::Escape($display)).Replace('__VERSION__', $Version)
[IO.File]::WriteAllText((Join-Path $layout 'AppxManifest.xml'), $manifest, (New-Object Text.UTF8Encoding $false))

$makeappx = Find-SdkTool 'makeappx.exe'
$out = Join-Path $OutputDir "WhatsAppManager-$Version.msix"
& $makeappx pack /o /d $layout /p $out
if ($LASTEXITCODE -ne 0) { throw "makeappx failed ($LASTEXITCODE)" }
Remove-Item -Recurse -Force $layout
Write-Host "MSIX: $out"
