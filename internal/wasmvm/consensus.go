package wasmvm

import "fmt"

// consensus.go : garde de DÉPLOIEMENT pour les contrats WASM on-chain.
//
// Avant qu'un bytecode n'atterrisse dans l'état (tx wasm_deploy), il doit passer
// Validate : on l'INSTRUMENTE à blanc. Si l'instrumentation échoue, c'est que le
// module est malformé OU qu'il contient un opcode hors du sous-ensemble supporté
// (SIMD, atomics, exceptions…). On REFUSE alors le déploiement. Conséquence :
// seul du bytecode dont on garantit l'arrêt (gas) et le déterminisme (jeu
// d'opcodes maîtrisé) peut être stocké et appelé. C'est le point de contrôle qui
// rend l'exécution en consensus défendable.

// ErrInvalidCode : le bytecode a été refusé au déploiement (malformé ou opcode
// non supporté).
var ErrInvalidCode = fmt.Errorf("wasm: bytecode refusé")

// Validate vérifie qu'un bytecode est déployable : instrumentable (donc arrêt
// garanti par gas) et restreint au sous-ensemble d'opcodes déterministe. Le
// gasLimit passé ici est indifférent (on ne l'exécute pas) — seule compte la
// réussite de l'instrumentation.
func Validate(code []byte) error {
	if len(code) == 0 {
		return fmt.Errorf("%w: vide", ErrInvalidCode)
	}
	if _, err := instrument(code, 1<<30, 1); err != nil {
		return fmt.Errorf("%w: %v", ErrInvalidCode, err)
	}
	return nil
}
