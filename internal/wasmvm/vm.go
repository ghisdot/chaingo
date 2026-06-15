// Package wasmvm : moteur d'exécution WebAssembly — PREVIEW EXPÉRIMENTALE.
//
// ⚠️ HORS-CONSENSUS. Ce moteur N'EST PAS câblé dans la machine d'état ni dans
// la validation des blocs. Il prouve que ChainGO peut charger et exécuter des
// contrats WASM arbitraires (façon ETH/BNB, mais en WASM), et sert de socle au
// futur moteur consensus-grade. Runtime : wazero (Go pur, sans CGO — cohérent
// avec l'invariant de compilation native du projet).
//
// Ce qu'il MANQUE avant de pouvoir l'exécuter DANS un bloc (voir
// docs/design/wasm-vm.md) :
//   - Gas DÉTERMINISTE par instruction (instrumentation du bytecode) — sinon une
//     boucle infinie gèlerait tous les nœuds, et un coût wall-clock divergerait
//     entre nœuds (= split de la chaîne).
//   - Audit de déterminisme (canonicalisation des NaN flottants, interdiction
//     des imports non déterministes).
//   - API hôte d'accès à l'état (storage get/set, appelant, transfert), chacune
//     gas-métrée.
//   - Types de tx wasm_deploy / wasm_call + stockage par contrat.
//   - Fuzzing + audit externe.
//
// Tant que ces points ne sont pas faits, ce paquet reste une SANDBOX de
// démonstration, bornée par un timeout WALL-CLOCK (non déterministe — d'où
// l'interdiction de l'utiliser en consensus).
package wasmvm

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/tetratelabs/wazero"
	"github.com/tetratelabs/wazero/api"
)

// Result : sortie d'une exécution sandbox.
type Result struct {
	Returns []uint64 // valeurs de retour de la fonction
	Logs    []string // messages émis par env.log(ptr, len)
}

// ErrTimeout : l'exécution a dépassé la limite de temps (protection sandbox
// contre les boucles infinies — wall-clock, donc NON déterministe).
var ErrTimeout = errors.New("wasm: execution timed out (sandbox wall-clock limit)")

// ErrOutOfGas : le contrat a épuisé son gas (arrêt DÉTERMINISTE — identique sur
// tous les nœuds, contrairement à ErrTimeout). C'est le mécanisme qui rendrait
// l'exécution consensus-safe.
var ErrOutOfGas = errors.New("wasm: out of gas")

// RunMetered instrumente `wasm` avec un compteur de gas DÉTERMINISTE
// (cf meter.go) puis l'exécute. L'exécution est bornée par `gasLimit`, pas par
// l'horloge : une boucle infinie épuise son gas et s'arrête au MÊME point sur
// tous les nœuds. Un timeout wall-clock généreux reste en filet de sécurité (au
// cas où l'instrumentation aurait un trou — défense en profondeur).
//
// Toujours HORS-CONSENSUS (il manque l'API hôte d'état, les tx, l'audit). Mais
// c'est le cœur du problème — la garantie d'arrêt déterministe — qui est résolu.
func RunMetered(parent context.Context, wasm []byte, fn string, gasLimit int64, args ...uint64) (*Result, error) {
	instrumented, err := instrument(wasm, gasLimit, 1)
	if err != nil {
		return nil, err
	}
	res, err := Run(parent, instrumented, fn, 10*time.Second, args...)
	if err != nil {
		if errors.Is(err, ErrTimeout) {
			return nil, err // le filet wall-clock a sauté AVANT le gas → instrumentation suspecte
		}
		// Un trap (unreachable) sur out-of-gas ressort ici comme une erreur
		// d'exécution. On la mappe sur ErrOutOfGas (best-effort : en v1 on ne
		// distingue pas un trap-gas d'un trap-logique du contrat).
		return nil, fmt.Errorf("%w (ou trap du contrat) : %v", ErrOutOfGas, err)
	}
	return res, nil
}

// maxMemoryPages : 256 pages × 64 Kio = 16 Mio max de mémoire linéaire.
const maxMemoryPages = 256

// Run charge le module `wasm`, fournit l'API hôte minimale (`env.log`), et
// appelle la fonction exportée `fn` avec `args`. Borné par `timeout` (sandbox).
// NE PAS utiliser dans le chemin de consensus (timeout wall-clock).
func Run(parent context.Context, wasm []byte, fn string, timeout time.Duration, args ...uint64) (*Result, error) {
	ctx, cancel := context.WithTimeout(parent, timeout)
	defer cancel()

	// WithCloseOnContextDone : wazero interrompt l'exécution si le contexte
	// expire (sinon une boucle infinie ne rendrait jamais la main).
	cfg := wazero.NewRuntimeConfig().
		WithCloseOnContextDone(true).
		WithMemoryLimitPages(maxMemoryPages)
	r := wazero.NewRuntimeWithConfig(ctx, cfg)
	defer r.Close(ctx)

	res := &Result{}
	// Module hôte "env" : une seule fonction pour l'instant, log(ptr, len) qui
	// lit une chaîne depuis la mémoire linéaire du contrat.
	_, err := r.NewHostModuleBuilder("env").
		NewFunctionBuilder().
		WithFunc(func(_ context.Context, m api.Module, ptr, ln uint32) {
			if b, ok := m.Memory().Read(ptr, ln); ok {
				res.Logs = append(res.Logs, string(b))
			}
		}).
		Export("log").
		Instantiate(ctx)
	if err != nil {
		return nil, fmt.Errorf("wasm: host module: %w", err)
	}

	mod, err := r.Instantiate(ctx, wasm)
	if err != nil {
		return nil, fmt.Errorf("wasm: instantiate: %w", err)
	}
	f := mod.ExportedFunction(fn)
	if f == nil {
		return nil, fmt.Errorf("wasm: function %q not exported", fn)
	}
	out, err := f.Call(ctx, args...)
	if err != nil {
		if ctx.Err() == context.DeadlineExceeded {
			return nil, ErrTimeout
		}
		return nil, fmt.Errorf("wasm: call %q: %w", fn, err)
	}
	res.Returns = out
	return res, nil
}
