# Compile le wallet en WebAssembly dans web/wallet/.
# À lancer une fois avant de servir le site localement (chaingo node start),
# ou après toute modif de internal/crypto, internal/types ou cmd/wallet-wasm.
# Usage : .\scripts\build-wasm.ps1
$ErrorActionPreference = "Stop"
$root = Split-Path $PSScriptRoot -Parent
$go = "C:\Program Files\Go\bin\go.exe"

$env:GOOS = "js"; $env:GOARCH = "wasm"
& $go build -trimpath -ldflags="-s -w" -o "$root\web\wallet\chaingo.wasm" "$root\cmd\wallet-wasm"
$env:GOOS = ""; $env:GOARCH = ""

$goroot = (& $go env GOROOT)
Copy-Item "$goroot\lib\wasm\wasm_exec.js" "$root\web\wallet\wasm_exec.js" -Force
Write-Host "WASM du wallet construit dans web\wallet\ (chaingo.wasm + wasm_exec.js)" -ForegroundColor Green
