package wasmvm

import (
	"context"
	"errors"
	"testing"
	"time"
)

// TestMeteredAddStillWorks : après instrumentation du gas, le module `add`
// donne TOUJOURS le bon résultat — l'instrumentation ne corrompt pas la
// sémantique (si la chirurgie binaire était fausse, wazero rejetterait le
// module ou le résultat serait faux).
func TestMeteredAddStillWorks(t *testing.T) {
	res, err := RunMetered(context.Background(), addWasm, "add", 1_000_000, 20, 22)
	if err != nil {
		t.Fatalf("RunMetered add: %v", err)
	}
	if len(res.Returns) != 1 || res.Returns[0] != 42 {
		t.Fatalf("add(20,22) instrumenté = %v, want [42]", res.Returns)
	}
}

// TestMeteredSpinRunsOutOfGas : LE test clé. Une boucle infinie s'arrête
// désormais par ÉPUISEMENT DU GAS (déterministe), AVANT le filet wall-clock.
// C'est la garantie d'arrêt qui rendrait le WASM exécutable en consensus.
func TestMeteredSpinRunsOutOfGas(t *testing.T) {
	start := time.Now()
	_, err := RunMetered(context.Background(), spinWasm, "spin", 50_000)
	elapsed := time.Since(start)

	if err == nil {
		t.Fatal("une boucle infinie doit s'arrêter (out-of-gas)")
	}
	if errors.Is(err, ErrTimeout) {
		t.Fatal("FAILLE : arrêt par CHRONO (non déterministe) au lieu du GAS — instrumentation trouée")
	}
	if !errors.Is(err, ErrOutOfGas) {
		t.Fatalf("attendu ErrOutOfGas, got %v", err)
	}
	// L'arrêt par gas doit être quasi instantané (50k tours), loin du filet 10 s.
	if elapsed > 2*time.Second {
		t.Fatalf("l'arrêt par gas devrait être rapide, a pris %v", elapsed)
	}
}

// TestMeteredReportsGasUsed : le moteur rapporte le gas CONSOMMÉ (comme ETH
// affiche le « gas used »). add ne fait qu'une charge d'entrée de fonction →
// consommation faible et non nulle, bien en deçà de la limite.
func TestMeteredReportsGasUsed(t *testing.T) {
	const limit = 1_000_000
	res, err := RunMetered(context.Background(), addWasm, "add", limit, 20, 22)
	if err != nil {
		t.Fatalf("RunMetered: %v", err)
	}
	if res.GasUsed <= 0 {
		t.Fatalf("gas consommé doit être > 0, got %d", res.GasUsed)
	}
	if res.GasUsed >= limit {
		t.Fatalf("gas consommé (%d) doit être bien en deçà de la limite (%d)", res.GasUsed, limit)
	}
	if res.GasLeft != int64(limit)-res.GasUsed {
		t.Fatalf("incohérence : left=%d used=%d limit=%d", res.GasLeft, res.GasUsed, limit)
	}
}

// TestMeteredEnoughGasCompletes : avec assez de gas, le module add s'exécute
// normalement (le gas ne bloque pas un travail légitime).
func TestMeteredEnoughGasCompletes(t *testing.T) {
	if _, err := RunMetered(context.Background(), addWasm, "add", 100, 1, 2); err != nil {
		t.Fatalf("add avec 100 de gas devrait passer (peu d'opérations) : %v", err)
	}
}

// TestInstrumentRejectsUnknownOpcode : par sûreté, un module contenant un
// opcode hors du sous-ensemble supporté est REJETÉ (jamais deviné).
func TestInstrumentRejectsUnknownOpcode(t *testing.T) {
	// addWasm dont l'i32.add (0x6a) est remplacé par un préfixe 0xfc (bulk mem,
	// non supporté) suivi d'un sous-opcode — doit être rejeté à l'instrumentation.
	bad := make([]byte, len(addWasm))
	copy(bad, addWasm)
	// l'i32.add est l'avant-dernier octet du corps ; on le passe à 0xfc.
	for i := len(bad) - 1; i >= 0; i-- {
		if bad[i] == 0x6a {
			bad[i] = 0xfc
			break
		}
	}
	if _, err := instrument(bad, 1000, 1); !errors.Is(err, ErrUnsupportedOpcode) && err == nil {
		t.Fatal("un opcode non supporté doit faire rejeter le module")
	}
}

// FuzzInstrument : la propriété de SÛRETÉ critique — instrumenter des octets
// ARBITRAIRES ne doit JAMAIS paniquer (au pire une erreur). Le nœud passe du
// bytecode hostile à `instrument` ; une panique = DoS. On fuzze l'instrumenteur
// SEUL (le code neuf à prouver), pas l'exécution wazero (dépendance éprouvée qui
// n'ajouterait que du bruit de timing). La garantie d'arrêt à l'exécution est
// prouvée par TestMeteredSpinRunsOutOfGas.
func FuzzInstrument(f *testing.F) {
	f.Add(addWasm)
	f.Add(spinWasm)
	f.Add([]byte("\x00asm\x01\x00\x00\x00"))
	f.Add([]byte{})
	f.Fuzz(func(t *testing.T, data []byte) {
		out, err := instrument(data, 10_000, 1)
		if err != nil {
			return // rejet propre : OK
		}
		// Instrumentation réussie → ré-instrumenter le résultat ne doit pas
		// paniquer non plus (robustesse / idempotence de structure).
		_, _ = instrument(out, 10_000, 1)
	})
}
