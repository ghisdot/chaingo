# Compile ChainGO pour toutes les plateformes dans dist/.
# Usage : .\scripts\build-all.ps1
$ErrorActionPreference = "Stop"
$root = Split-Path $PSScriptRoot -Parent
New-Item -ItemType Directory -Force (Join-Path $root "dist") | Out-Null

$targets = @(
    @("windows", "amd64", ".exe"),
    @("linux",   "amd64", ""),
    @("linux",   "arm64", ""),
    @("darwin",  "amd64", ""),
    @("darwin",  "arm64", "")
)

foreach ($t in $targets) {
    $os, $arch, $ext = $t
    $out = Join-Path $root "dist\chaingo-$os-$arch$ext"
    Write-Host "→ $os/$arch"
    $env:GOOS = $os; $env:GOARCH = $arch; $env:CGO_ENABLED = "0"
    go build -trimpath -ldflags="-s -w" -o $out "$root\cmd\chaingo"
}
$env:GOOS = ""; $env:GOARCH = ""; $env:CGO_ENABLED = ""
Write-Host "`nBinaire(s) dans dist\ :" -ForegroundColor Green
Get-ChildItem (Join-Path $root "dist") | Format-Table Name, @{n="Taille (Mo)";e={[math]::Round($_.Length/1MB,1)}}
