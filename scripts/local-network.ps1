# local-network.ps1 — lance un mini-réseau ChainGO en local pour expérimenter.
#
# Le 1er nœud crée la genèse (mode --testnet, faucet ouvert). Les nœuds 2..N
# rejoignent la même chaîne via --genesis-url + --peers.
#
# Usage :
#   .\scripts\local-network.ps1 -Nodes 3
#   .\scripts\local-network.ps1 -Nodes 4 -Reset
#   .\scripts\local-network.ps1 -Stop
#
# Après démarrage :
#   - nœud 1 : http://127.0.0.1:8545 (le bootstrap qui a le faucet)
#   - nœud 2 : http://127.0.0.1:8546
#   - nœud N : http://127.0.0.1:8544+N
#
# Pour bombarder le réseau de transactions :
#   .\chaingo.exe loadtest --api http://127.0.0.1:8545

param(
    [int]$Nodes = 3,
    [switch]$Reset,
    [switch]$Stop
)

$ErrorActionPreference = "Stop"
$root = Split-Path $PSScriptRoot -Parent
$exe  = Join-Path $root "chaingo.exe"
$base = Join-Path $root ".localnet"

if (-not (Test-Path $exe)) {
    Write-Error "Binaire $exe introuvable — exécuter d'abord : go build -o chaingo.exe ./cmd/chaingo"
    exit 1
}

# Arrêt propre des nœuds en cours
function StopAll {
    $running = Get-Process -Name "chaingo" -ErrorAction SilentlyContinue
    if ($running) {
        Write-Host "Arrêt de $($running.Count) nœud(s) chaingo en cours…" -ForegroundColor Yellow
        $running | Stop-Process -Force -ErrorAction SilentlyContinue
        Start-Sleep -Seconds 1
    } else {
        Write-Host "Aucun nœud chaingo en cours." -ForegroundColor Gray
    }
}

if ($Stop) { StopAll; exit 0 }
if ($Nodes -lt 1) { Write-Error "-Nodes doit valoir au moins 1"; exit 1 }

StopAll
if ($Reset -and (Test-Path $base)) {
    Write-Host "Nettoyage de $base…" -ForegroundColor Yellow
    Get-ChildItem $base -Recurse | Remove-Item -Recurse -Force -ErrorAction SilentlyContinue
    Remove-Item $base -Force -ErrorAction SilentlyContinue
}
New-Item -ItemType Directory -Force $base | Out-Null

# Lance le nœud N (1-indexé)
function StartNode($n) {
    $dataDir = Join-Path $base "n$n"
    $apiPort = 8544 + $n
    $p2pPort = 9099 + $n
    $logFile = Join-Path $base "n$n.log"

    $args = @(
        "node", "start",
        "--datadir", $dataDir,
        "--api", "127.0.0.1:$apiPort",
        "--p2p", "127.0.0.1:$p2pPort"
    )

    if ($n -eq 1) {
        # Nœud bootstrap : crée la genèse locale
        $args += @("--testnet")
    } else {
        # Nœud suiveur : rejoint le réseau via le nœud 1
        $args += @(
            "--genesis-url", "http://127.0.0.1:8545/v1/genesis",
            "--peers",       "127.0.0.1:9100"
        )
    }

    Start-Process -FilePath $exe `
                  -ArgumentList $args `
                  -WindowStyle Hidden `
                  -RedirectStandardOutput $logFile `
                  -RedirectStandardError  (Join-Path $base "n$n.err") `
                  | Out-Null

    Write-Host ("  nœud {0,2} : http://127.0.0.1:{1}  (P2P :{2}, datadir n{0})" -f $n, $apiPort, $p2pPort)
}

Write-Host ""
Write-Host "Démarrage d'un réseau local à $Nodes nœud(s)…" -ForegroundColor Cyan
StartNode 1
Start-Sleep -Seconds 2  # laisse le bootstrap initialiser la genèse
for ($i = 2; $i -le $Nodes; $i++) {
    StartNode $i
    Start-Sleep -Milliseconds 600
}

Write-Host ""
Write-Host "Attente de la synchronisation initiale…" -ForegroundColor Cyan
Start-Sleep -Seconds 3

# Récap : hauteur de chaque nœud
Write-Host ""
Write-Host "État du réseau :" -ForegroundColor Cyan
for ($i = 1; $i -le $Nodes; $i++) {
    $port = 8544 + $i
    try {
        $s = (Invoke-WebRequest -UseBasicParsing "http://127.0.0.1:$port/v1/status" -TimeoutSec 3).Content | ConvertFrom-Json
        $line = "  nœud {0,2} : H={1,-5} finalized={2,-5} peers={3,-2} chain={4}" -f $i, $s.height, $s.finalized_height, $s.peers, $s.chain_id
        Write-Host $line -ForegroundColor Green
    } catch {
        Write-Host ("  nœud {0,2} : pas encore prêt ({1})" -f $i, $_.Exception.Message) -ForegroundColor Yellow
    }
}

Write-Host ""
Write-Host "Prochaines actions :" -ForegroundColor Cyan
Write-Host "  → Bombarder le réseau :"  -ForegroundColor White
Write-Host "      .\chaingo.exe loadtest --api http://127.0.0.1:8545"
Write-Host "  → Suivre les logs d'un nœud :"
Write-Host "      Get-Content -Wait $base\n1.log"
Write-Host "  → Tout arrêter :"
Write-Host "      .\scripts\local-network.ps1 -Stop"
Write-Host ""
