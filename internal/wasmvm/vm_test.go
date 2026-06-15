package wasmvm

import (
	"context"
	"errors"
	"testing"
	"time"
)

// addWasm : module WASM minimal exportant `add(i32, i32) -> i32` (encodé à la
// main — c'est le « hello world » du format WebAssembly).
var addWasm = []byte{
	0x00, 0x61, 0x73, 0x6d, 0x01, 0x00, 0x00, 0x00, // \0asm + version 1
	0x01, 0x07, 0x01, 0x60, 0x02, 0x7f, 0x7f, 0x01, 0x7f, // type: (i32,i32)->i32
	0x03, 0x02, 0x01, 0x00, // function section : 1 func, type 0
	0x07, 0x07, 0x01, 0x03, 0x61, 0x64, 0x64, 0x00, 0x00, // export "add" -> func 0
	0x0a, 0x09, 0x01, 0x07, 0x00, 0x20, 0x00, 0x20, 0x01, 0x6a, 0x0b, // code: local.get 0,1 ; i32.add ; end
}

// spinWasm : module exportant `spin()` avec une boucle infinie — pour tester la
// protection anti-runaway de la sandbox.
var spinWasm = []byte{
	0x00, 0x61, 0x73, 0x6d, 0x01, 0x00, 0x00, 0x00,
	0x01, 0x04, 0x01, 0x60, 0x00, 0x00, // type: ()->()
	0x03, 0x02, 0x01, 0x00,
	0x07, 0x08, 0x01, 0x04, 0x73, 0x70, 0x69, 0x6e, 0x00, 0x00, // export "spin"
	0x0a, 0x09, 0x01, 0x07, 0x00, 0x03, 0x40, 0x0c, 0x00, 0x0b, 0x0b, // loop { br 0 }
}

// TestRunAddModule : ChainGO charge et EXÉCUTE un vrai module WASM — la preuve
// que le moteur tourne. add(20, 22) doit rendre 42.
func TestRunAddModule(t *testing.T) {
	res, err := Run(context.Background(), addWasm, "add", time.Second, 20, 22)
	if err != nil {
		t.Fatalf("Run add: %v", err)
	}
	if len(res.Returns) != 1 || res.Returns[0] != 42 {
		t.Fatalf("add(20,22) = %v, want [42]", res.Returns)
	}
}

// TestSpinTimesOut : une boucle infinie est INTERROMPUE par la limite de temps
// de la sandbox (preuve que du code hostile ne gèle pas le process).
func TestSpinTimesOut(t *testing.T) {
	_, err := Run(context.Background(), spinWasm, "spin", 150*time.Millisecond)
	if !errors.Is(err, ErrTimeout) {
		t.Fatalf("une boucle infinie doit déclencher ErrTimeout, got %v", err)
	}
}

// TestMissingFunction : appeler une fonction non exportée échoue proprement.
func TestMissingFunction(t *testing.T) {
	if _, err := Run(context.Background(), addWasm, "nope", time.Second); err == nil {
		t.Fatal("appeler une fonction inexistante doit échouer")
	}
}

// TestInvalidWasm : des octets invalides échouent sans paniquer.
func TestInvalidWasm(t *testing.T) {
	if _, err := Run(context.Background(), []byte{0x00, 0x01, 0x02, 0x03}, "x", time.Second); err == nil {
		t.Fatal("un module WASM invalide doit échouer")
	}
}
