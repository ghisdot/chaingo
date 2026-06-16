package crypto

import (
	"bytes"
	"testing"
)

// TestViewKeyDeterministic : même seed → même clé de vue (un seul seed à sauver).
func TestViewKeyDeterministic(t *testing.T) {
	seed := make([]byte, Scheme.SeedSize())
	for i := range seed {
		seed[i] = byte(i)
	}
	a := DeriveViewKey(seed)
	b := DeriveViewKey(seed)
	if !bytes.Equal(a.ViewPubBytes(), b.ViewPubBytes()) {
		t.Fatal("dérivation non déterministe de la clé de vue")
	}
	// seed différent → clé différente
	seed[0] ^= 0xff
	if bytes.Equal(a.ViewPubBytes(), DeriveViewKey(seed).ViewPubBytes()) {
		t.Fatal("seeds différents devraient donner des clés de vue différentes")
	}
}

// TestSealOpenRoundTrip : le destinataire déchiffre, l'expéditeur ne fuit rien.
func TestSealOpenRoundTrip(t *testing.T) {
	alice := DeriveViewKey(seedOf(1))
	msg := []byte("note: value=42, rho=deadbeef")
	blob, err := SealTo(alice.ViewPubBytes(), msg)
	if err != nil {
		t.Fatalf("SealTo: %v", err)
	}
	if bytes.Contains(blob, msg) {
		t.Fatal("le clair ne doit jamais apparaître dans le blob chiffré")
	}
	pt, ok := alice.OpenWith(blob)
	if !ok || !bytes.Equal(pt, msg) {
		t.Fatalf("le destinataire doit déchiffrer : ok=%v pt=%q", ok, pt)
	}
}

// TestScanRejectsForeignNotes : c'est LA propriété de scan — une note destinée à
// Alice ne s'ouvre PAS chez Bob (ok=false), donc un wallet sait reconnaître les
// siennes sans index on-chain liant la note à son adresse.
func TestScanRejectsForeignNotes(t *testing.T) {
	alice := DeriveViewKey(seedOf(1))
	bob := DeriveViewKey(seedOf(2))
	blob, _ := SealTo(alice.ViewPubBytes(), []byte("pour Alice"))
	if _, ok := bob.OpenWith(blob); ok {
		t.Fatal("Bob ne doit PAS pouvoir ouvrir une note destinée à Alice")
	}
	if _, ok := alice.OpenWith(blob); !ok {
		t.Fatal("Alice doit pouvoir ouvrir sa note")
	}
	// blob tronqué / corrompu → rejet propre, pas de panique
	if _, ok := alice.OpenWith(blob[:len(blob)-1]); ok {
		t.Fatal("un blob corrompu ne doit pas s'ouvrir")
	}
	if _, ok := alice.OpenWith([]byte{1, 2, 3}); ok {
		t.Fatal("un blob trop court ne doit pas s'ouvrir")
	}
}

func seedOf(b byte) []byte {
	s := make([]byte, Scheme.SeedSize())
	for i := range s {
		s[i] = b
	}
	return s
}
