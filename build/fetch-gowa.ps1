# Downloads the pinned GoWA Windows binary, verifies its SHA-256 and
# extracts it to build/gowa/whatsapp.exe. Safe to re-run (cached).
$ErrorActionPreference = 'Stop'
$root = Split-Path -Parent $MyInvocation.MyCommand.Path
$cfg = @{}
Get-Content (Join-Path $root 'gowa.version') | Where-Object { $_ -match '^\s*([A-Z0-9_]+)=(.+)$' } | ForEach-Object {
    $cfg[$Matches[1]] = $Matches[2].Trim()
}
$out = Join-Path $root 'gowa'
$exe = Join-Path $out 'whatsapp.exe'
$stamp = Join-Path $out 'VERSION'
if ((Test-Path $exe) -and (Test-Path $stamp) -and ((Get-Content $stamp -Raw).Trim() -eq $cfg.SHA256)) {
    Write-Host "GoWA $($cfg.VERSION) already present"
    exit 0
}
New-Item -ItemType Directory -Force $out | Out-Null
$zip = Join-Path $out $cfg.ASSET
Write-Host "Downloading $($cfg.URL)"
[Net.ServicePointManager]::SecurityProtocol = [Net.SecurityProtocolType]::Tls12
Invoke-WebRequest -Uri $cfg.URL -OutFile $zip -UseBasicParsing
$hash = (Get-FileHash -Algorithm SHA256 $zip).Hash.ToLower()
if ($hash -ne $cfg.SHA256.ToLower()) {
    Remove-Item $zip -Force
    throw "SHA-256 mismatch for $($cfg.ASSET): got $hash, expected $($cfg.SHA256)"
}
$tmp = Join-Path $out 'extract'
if (Test-Path $tmp) { Remove-Item -Recurse -Force $tmp }
Expand-Archive -Path $zip -DestinationPath $tmp
$bin = Get-ChildItem -Path $tmp -Filter '*.exe' -Recurse | Select-Object -First 1
if (-not $bin) { throw "No .exe found in $($cfg.ASSET)" }
Copy-Item $bin.FullName $exe -Force
Remove-Item -Recurse -Force $tmp
Remove-Item $zip -Force
Set-Content -Path $stamp -Value $cfg.SHA256 -Encoding ascii
Write-Host "GoWA $($cfg.VERSION) ready at $exe"
