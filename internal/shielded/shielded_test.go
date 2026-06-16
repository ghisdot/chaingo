package shielded

import (
	"testing"

	"chaingo/internal/crypto"
)

func key(b byte) *ShieldedKey {
	s := make([]byte, crypto.Scheme.SeedSize())
	for i := range s {
		s[i] = b
	}
	return KeyFromSeed(s)
}

func note(val uint64, owner *ShieldedKey, r byte) *Note {
	n := &Note{Value: val, Owner: owner.MetaAddress()}
	for i := range n.Rho {
		n.Rho[i] = r
	}
	return n
}

// TestShieldedFlow : flux complet du prototype — Alice « shield » des fonds dans
// une note pour Bob, la lui livre chiffrée ; Bob la retrouve par SCAN (sans index
// on-chain), puis la dépense vers Carol avec de la monnaie rendue. On vérifie la
// conservation de valeur, le scan, et le refus du double-spend.
func TestShieldedFlow(t *testing.T) {
	bob, carol := key(2), key(3)
	pool := NewPool()

	// 1) Une note de 100 est « shield » pour Bob et publiée (commitment + blob chiffré).
	toBob := note(100, bob, 0xAA)
	pool.Mint(toBob)
	blob, err := toBob.Encrypt()
	if err != nil {
		t.Fatalf("Encrypt: %v", err)
	}

	// 2) Bob scanne les notes publiées et retrouve la sienne (Carol non).
	if found := carol.Scan([][]byte{blob}); len(found) != 0 {
		t.Fatal("Carol ne doit pas retrouver la note de Bob")
	}
	mine := bob.Scan([][]byte{blob})
	if len(mine) != 1 || mine[0].Value != 100 {
		t.Fatalf("Bob doit retrouver sa note de 100, got %+v", mine)
	}

	// 3) Bob dépense sa note de 100 → 70 pour Carol + 29 de monnaie pour lui, fee 1.
	toCarol := note(70, carol, 0xCC)
	change := note(29, bob, 0xBB)
	w := &SpendWitness{NK: bob.NK, Inputs: mine, Outputs: []*Note{toCarol, change}, Fee: 1}
	newCms, err := pool.VerifyTransparent(w)
	if err != nil {
		t.Fatalf("dépense valide refusée: %v", err)
	}
	if len(newCms) != 2 {
		t.Fatalf("2 nouveaux commitments attendus, got %d", len(newCms))
	}

	// 4) Rejouer la même dépense → double-spend rejeté (nullifier déjà vu).
	if _, err := pool.VerifyTransparent(w); err == nil {
		t.Fatal("le double-spend doit être rejeté")
	}

	// 5) Carol retrouve sa note et peut la redépenser (chaînage).
	cblob, _ := toCarol.Encrypt()
	cmine := carol.Scan([][]byte{cblob})
	if len(cmine) != 1 || cmine[0].Value != 70 {
		t.Fatalf("Carol doit retrouver sa note de 70, got %+v", cmine)
	}
}

// TestValueConservation : une dépense qui crée de la valeur (Σsorties+fee >
// Σentrées) doit être refusée, et le pool ne doit pas être modifié.
func TestValueConservation(t *testing.T) {
	alice, bob := key(1), key(2)
	pool := NewPool()
	in := note(50, alice, 0x11)
	pool.Mint(in)

	bad := &SpendWitness{NK: alice.NK, Inputs: []*Note{in}, Outputs: []*Note{note(60, bob, 0x22)}, Fee: 0}
	if _, err := pool.VerifyTransparent(bad); err == nil {
		t.Fatal("création de valeur (60 > 50) doit être refusée")
	}
	// le nullifier ne doit PAS avoir été marqué (rollback atomique) → une dépense
	// correcte de la même note reste possible.
	good := &SpendWitness{NK: alice.NK, Inputs: []*Note{in}, Outputs: []*Note{note(50, bob, 0x33)}, Fee: 0}
	if _, err := pool.VerifyTransparent(good); err != nil {
		t.Fatalf("la dépense correcte après un échec doit passer: %v", err)
	}
}

// TestUnknownInputRejected : on ne peut pas dépenser une note absente du pool.
func TestUnknownInputRejected(t *testing.T) {
	alice, bob := key(1), key(2)
	pool := NewPool()
	ghost := note(10, alice, 0x99) // jamais Mint
	w := &SpendWitness{NK: alice.NK, Inputs: []*Note{ghost}, Outputs: []*Note{note(10, bob, 0x44)}}
	if _, err := pool.VerifyTransparent(w); err == nil {
		t.Fatal("dépenser une note absente du pool doit être refusé")
	}
}
