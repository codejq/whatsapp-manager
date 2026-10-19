<#
.SYNOPSIS
  Builds WhatsApp Manager: web UI, Go exe, portable zip, Inno Setup installer and (optionally) MSIX.

.EXAMPLE
  ./build.ps1                         # dev build (exe + portable zip)
  ./build.ps1 -Version 1.2.0 -Installer -Msix -UpdateRepo owner/repo
#>
param(
    [string]$Version = '',
    [ValidateSet('github', 'store')][string]$Channel = 'github',
    [string]$UpdateRepo = '',
    [switch]$Installer,
    [switch]$Msix,
    [switch]$SkipUI,
    [switch]$SkipTests
)
$ErrorActionPreference = 'Stop'
$root = Split-Path -Parent $MyInvocation.MyCommand.Path
Set-Location $root

function Step($m) { Write-Host "`n==> $m" -ForegroundColor Cyan }
function Exec([scriptblock]$cmd) { & $cmd; if ($LASTEXITCODE -ne 0) { throw "Command failed ($LASTEXITCODE): $cmd" } }

if (-not $Version) {
    $tag = (git describe --tags --abbrev=0 2>$null)
    $Version = if ($tag) { $tag.TrimStart('v') } else { '0.0.0' }
}
$Version = $Version.TrimStart('v')
if ($Version -notmatch '^\d+\.\d+\.\d+') { throw "Version must be SemVer (got '$Version')" }
$numeric = ($Version -split '[-+]')[0]
$fileVersion = "$numeric.0"
Write-Host "WhatsApp Manager $Version ($Channel)"

$dist = Join-Path $root 'dist'
$stage = Join-Path $dist 'app'
if (Test-Path $dist) { Remove-Item -Recurse -Force $dist }
New-Item -ItemType Directory -Force $stage | Out-Null

if (-not $SkipUI) {
    Step 'Building web UI'
    Push-Location web
    if (-not (Test-Path node_modules)) { Exec { npm ci } }
    Exec { npm run build }
    Pop-Location
}

Step 'Fetching GoWA'
Exec { powershell -NoProfile -ExecutionPolicy Bypass -File build/fetch-gowa.ps1 }

if (-not $SkipTests) {
    Step 'Running tests'
    Exec { go vet ./... }
    Exec { go test ./... }
}

Step 'Embedding Windows resources'
$winres = Join-Path (go env GOPATH) 'bin/go-winres.exe'
if (-not (Test-Path $winres)) { Exec { go install github.com/tc-hib/go-winres@latest } }
Exec { & $winres make --in build/winres/winres.json --out cmd/whatsapp-manager/rsrc --arch amd64 --product-version $fileVersion --file-version $fileVersion }

Step 'Building WhatsAppManager.exe'
$env:CGO_ENABLED = '0'
$env:GOOS = 'windows'
$env:GOARCH = 'amd64'
$ldflags = "-s -w -H windowsgui -X main.version=$Version -X main.channel=$Channel -X main.updateRepo=$UpdateRepo"
Exec { go build -trimpath -ldflags $ldflags -o (Join-Path $stage 'WhatsAppManager.exe') ./cmd/whatsapp-manager }

Step 'Staging files'
New-Item -ItemType Directory -Force (Join-Path $stage 'gowa'), (Join-Path $stage 'LICENSES') | Out-Null
Copy-Item build/gowa/whatsapp.exe (Join-Path $stage 'gowa/whatsapp.exe')
Copy-Item LICENSE (Join-Path $stage 'LICENSE.txt')
Copy-Item THIRD_PARTY_NOTICES.md (Join-Path $stage 'THIRD_PARTY_NOTICES.md')
Copy-Item LICENSES/* (Join-Path $stage 'LICENSES')

Step 'Creating portable zip'
$zip = Join-Path $dist "WhatsAppManager-$Version-portable.zip"
Compress-Archive -Path (Join-Path $stage '*') -DestinationPath $zip

if ($Installer) {
    Step 'Building installer (Inno Setup)'
    $iscc = @(
        (Get-Command ISCC.exe -ErrorAction SilentlyContinue | Select-Object -ExpandProperty Source),
        "$env:LOCALAPPDATA\Programs\Inno Setup 6\ISCC.exe",
        "${env:ProgramFiles(x86)}\Inno Setup 6\ISCC.exe",
        "$env:ProgramFiles\Inno Setup 6\ISCC.exe"
    ) | Where-Object { $_ -and (Test-Path $_) } | Select-Object -First 1
    if (-not $iscc) { throw 'Inno Setup 6 (ISCC.exe) not found. Install it: winget install JRSoftware.InnoSetup' }
    Exec { & $iscc "/DAppVersion=$Version" "/DNumericVersion=$fileVersion" "/DSourceDir=$stage" "/DOutputDir=$dist" build/inno/setup.iss }
}

if ($Msix) {
    Step 'Building MSIX'
    Exec { powershell -NoProfile -ExecutionPolicy Bypass -File build/msix/build-msix.ps1 -Version $fileVersion -SourceDir $stage -OutputDir $dist }
}

Step 'Checksums'
$sums = Get-ChildItem $dist -File | Where-Object { $_.Extension -in '.zip', '.exe', '.msix' } | ForEach-Object {
    "$((Get-FileHash -Algorithm SHA256 $_.FullName).Hash.ToLower())  $($_.Name)"
}
$sums | Set-Content -Encoding ascii (Join-Path $dist 'checksums.txt')
$sums | ForEach-Object { Write-Host $_ }
Write-Host "`nDone. Output: $dist" -ForegroundColor Green
