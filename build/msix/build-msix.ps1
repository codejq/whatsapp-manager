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

# Partner Center identity.
# Order: environment variables > store.local.json (gitignored) > store.json (committed).
$local = @{}
foreach ($name in @('store.json', 'store.local.json')) {
    $f = Join-Path $here $name
    if (Test-Path $f) {
        (Get-Content $f -Raw | ConvertFrom-Json).PSObject.Properties | ForEach-Object { if ($_.Value) { $local[$_.Name] = $_.Value } }
        Write-Host "Store identity: $name"
    }
}
function Pick($envName, $key, $default) {
    $v = [Environment]::GetEnvironmentVariable($envName)
    if ($v) { return $v }
    if ($local[$key]) { return $local[$key] }
    return $default
}
$identity = Pick 'MSIX_IDENTITY_NAME' 'identityName' 'WhatsAppManager.Dev'
$publisher = Pick 'MSIX_PUBLISHER' 'publisher' 'CN=WhatsAppManagerDev'
$display = Pick 'MSIX_PUBLISHER_DISPLAY' 'publisherDisplayName' 'WhatsApp Manager contributors'
$appName = Pick 'MSIX_DISPLAY_NAME' 'displayName' 'WhatsApp Manager'
if ($identity -eq 'WhatsAppManager.Dev') { Write-Warning 'Building with DEV identity placeholders: this package cannot be uploaded to the Store.' }

$layout = Join-Path $OutputDir 'msix-layout'
if (Test-Path $layout) { Remove-Item -Recurse -Force $layout }
New-Item -ItemType Directory -Force $layout, (Join-Path $layout 'Assets') | Out-Null
Copy-Item (Join-Path $SourceDir '*') $layout -Recurse
Copy-Item (Join-Path $root 'assets\msix\*') (Join-Path $layout 'Assets')

$manifest = Get-Content (Join-Path $here 'AppxManifest.xml') -Raw
$manifest = $manifest.Replace('__IDENTITY_NAME__', $identity).Replace('__PUBLISHER__', $publisher).
    Replace('__PUBLISHER_DISPLAY__', [Security.SecurityElement]::Escape($display)).Replace('__VERSION__', $Version).
    Replace('__DISPLAY_NAME__', [Security.SecurityElement]::Escape($appName))
[IO.File]::WriteAllText((Join-Path $layout 'AppxManifest.xml'), $manifest, (New-Object Text.UTF8Encoding $false))

$makeappx = Find-SdkTool 'makeappx.exe'
$out = Join-Path $OutputDir "WhatsAppManager-$Version.msix"
& $makeappx pack /o /d $layout /p $out
if ($LASTEXITCODE -ne 0) { throw "makeappx failed ($LASTEXITCODE)" }
Remove-Item -Recurse -Force $layout
Write-Host "MSIX: $out"
