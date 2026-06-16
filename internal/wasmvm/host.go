package wasmvm

import (
	"context"
	"fmt"

	"github.com/tetratelabs/wazero"
	"github.com/tetratelabs/wazero/api"
)

// host.go : API hôte d'état pour les contrats WASM — toujours en SANDBOX
// (hors-consensus). Un contrat peut désormais lire/écrire son stockage,
// connaître l'appelant et la valeur reçue, journaliser, et demander un
// transfert. Le `Sandbox` fournit un état EN MÉMOIRE : c'est le prototype de
// ce que le moteur consensus-grade branchera sur la vraie machine d'état
// (tranche 4 : tx wasm_deploy/wasm_call + stockage par contrat dans la racine).
//
// ABI mémoire : les chaînes/octets sont passés par (pointeur, longueur) dans la
// mémoire linéaire du module. Les fonctions de lecture écrivent dans un tampon
// fourni par le contrat et renvoient la longueur (ou -1 si trop petit/absent).

// Transfer : un transfert demandé par un contrat (enregistré, pas exécuté en
// sandbox — en consensus, il débiterait le solde du contrat).
type Transfer struct {
	To     string
	Amount int64
}

// Sandbox : contexte d'exécution en mémoire d'un contrat.
type Sandbox struct {
	Caller    string            // adresse appelante
	Value     int64             // CGO envoyés avec l'appel
	Balance   int64             // solde du contrat (pour les transferts)
	Storage   map[string][]byte // stockage clé→valeur du contrat
	Logs      []string          // messages env.log
	Transfers []Transfer        // transferts demandés
}

// NewSandbox crée un contexte vide.
func NewSandbox(caller string, value int64) *Sandbox {
	return &Sandbox{Caller: caller, Value: value, Storage: map[string][]byte{}}
}

// ---- logique d'état (testable sans WASM) ----

func (s *Sandbox) storageWrite(key, val []byte) {
	s.Storage[string(key)] = append([]byte(nil), val...)
}

func (s *Sandbox) storageRead(key []byte) ([]byte, bool) {
	v, ok := s.Storage[string(key)]
	return v, ok
}

func (s *Sandbox) transfer(to string, amount int64) bool {
	if amount < 0 || amount > s.Balance {
		return false
	}
	s.Balance -= amount
	s.Transfers = append(s.Transfers, Transfer{To: to, Amount: amount})
	return true
}

// ---- exécution d'un contrat avec l'API hôte complète ----

// Run instrumente le module (gas déterministe), câble l'API hôte `env` adossée
// à ce sandbox, et appelle `fn`. Borné par `gasLimit`. Utilise le compilateur
// wazero (rapide). Destiné à la sandbox locale (`chaingo wasm run`).
func (s *Sandbox) Run(parent context.Context, wasm []byte, fn string, gasLimit int64, args ...uint64) (*Result, error) {
	return s.run(parent, wasm, fn, gasLimit, false, args...)
}

// RunDeterministic : comme Run, mais force l'INTERPRÉTEUR wazero. L'interpréteur
// suit le même chemin d'exécution sur toutes les architectures (pas de
// génération de code machine spécifique CPU) — c'est le mode utilisé dans le
// CHEMIN DE CONSENSUS, où deux nœuds DOIVENT calculer un résultat bit-à-bit
// identique. Couplé au gas déterministe (meter.go), au jeu d'opcodes restreint
// (validé au déploiement) et aux seuls imports déterministes (module `env`),
// cela rend l'exécution reproductible. (Point d'audit restant : NaN flottants.)
func (s *Sandbox) RunDeterministic(parent context.Context, wasm []byte, fn string, gasLimit int64, args ...uint64) (*Result, error) {
	return s.run(parent, wasm, fn, gasLimit, true, args...)
}

func (s *Sandbox) run(parent context.Context, wasm []byte, fn string, gasLimit int64, interp bool, args ...uint64) (*Result, error) {
	instrumented, err := instrument(wasm, gasLimit, 1)
	if err != nil {
		return nil, err
	}
	var cfg wazero.RuntimeConfig
	if interp {
		cfg = wazero.NewRuntimeConfigInterpreter()
	} else {
		cfg = wazero.NewRuntimeConfig()
	}
	cfg = cfg.WithCloseOnContextDone(true).WithMemoryLimitPages(maxMemoryPages)
	r := wazero.NewRuntimeWithConfig(parent, cfg)
	defer r.Close(parent)

	res := &Result{}
	readMem := func(m api.Module, ptr, ln uint32) []byte {
		b, ok := m.Memory().Read(ptr, ln)
		if !ok {
			return nil
		}
		return append([]byte(nil), b...)
	}
	// writeOut : écrit `data` dans le tampon (ptr, max) ; renvoie la longueur ou -1.
	writeOut := func(m api.Module, ptr, max uint32, data []byte) int32 {
		if uint32(len(data)) > max {
			return -1
		}
		if !m.Memory().Write(ptr, data) {
			return -1
		}
		return int32(len(data))
	}

	b := r.NewHostModuleBuilder("env")
	b.NewFunctionBuilder().WithFunc(func(_ context.Context, m api.Module, ptr, ln uint32) {
		res.Logs = append(res.Logs, string(readMem(m, ptr, ln)))
		s.Logs = res.Logs
	}).Export("log")
	b.NewFunctionBuilder().WithFunc(func(_ context.Context, _ api.Module) int64 {
		return s.Value
	}).Export("value")
	b.NewFunctionBuilder().WithFunc(func(_ context.Context, m api.Module, out, max uint32) int32 {
		return writeOut(m, out, max, []byte(s.Caller))
	}).Export("caller")
	b.NewFunctionBuilder().WithFunc(func(_ context.Context, m api.Module, kptr, klen, vptr, vlen uint32) {
		s.storageWrite(readMem(m, kptr, klen), readMem(m, vptr, vlen))
	}).Export("storage_write")
	b.NewFunctionBuilder().WithFunc(func(_ context.Context, m api.Module, kptr, klen, out, max uint32) int32 {
		v, ok := s.storageRead(readMem(m, kptr, klen))
		if !ok {
			return -1
		}
		return writeOut(m, out, max, v)
	}).Export("storage_read")
	b.NewFunctionBuilder().WithFunc(func(_ context.Context, m api.Module, toPtr, toLen uint32, amount int64) int32 {
		if s.transfer(string(readMem(m, toPtr, toLen)), amount) {
			return 0
		}
		return 1
	}).Export("transfer")
	if _, err := b.Instantiate(parent); err != nil {
		return nil, fmt.Errorf("wasm: host env: %w", err)
	}

	mod, err := r.Instantiate(parent, instrumented)
	if err != nil {
		return nil, fmt.Errorf("wasm: instantiate: %w", err)
	}
	f := mod.ExportedFunction(fn)
	if f == nil {
		return nil, fmt.Errorf("wasm: function %q not exported", fn)
	}
	out, err := f.Call(parent, args...)
	if err != nil {
		return nil, fmt.Errorf("%w (ou trap du contrat) : %v", ErrOutOfGas, err)
	}
	res.Returns = out
	if g := mod.ExportedGlobal(gasExportName); g != nil {
		res.GasLeft = int64(g.Get())
		res.GasUsed = gasLimit - res.GasLeft
	}
	return res, nil
}
