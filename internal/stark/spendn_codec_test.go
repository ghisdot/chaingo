package stark

import (
	"bytes"
	"testing"
)

// L'énoncé public M-in/N-out fait l'aller-retour octets sans perte, pour divers
// (M, N).
func TestSpendNPublicCodec_Roundtrip(t *testing.T) {
	for _, c := range [][2]int{{1, 1}, {1, 3}, {2, 1}, {3, 4}} {
		w, fee := snBuildScenario("codec", c[0], c[1])
		_, public := buildSpendNTraceForCodec(w, fee)

		b := MarshalSpendNPublic(public)
		got, err := UnmarshalSpendNPublic(b)
		if err != nil {
			t.Fatalf("(%d,%d): unmarshal: %v", c[0], c[1], err)
		}
		if len(got.Nfs) != c[0] || len(got.OutCms) != c[1] {
			t.Fatalf("(%d,%d): dimensions %d/%d", c[0], c[1], len(got.Nfs), len(got.OutCms))
		}
		if !bytes.Equal(MarshalSpendNPublic(got), b) {
			t.Fatalf("(%d,%d): ré-encodage divergent", c[0], c[1])
		}
	}
}

// buildSpendNTraceForCodec : raccourci pour obtenir l'énoncé public sans dérouler
// le prouveur (rapide).
func buildSpendNTraceForCodec(w SpendNWitness, fee Felt) ([][]Felt, SpendNPublic) {
	return buildSpendNTrace(w, fee)
}

// Aller-retour COMPLET (énoncé + preuve) puis vérification : ce qui transite sur le
// fil vérifie réellement.
func TestSpendNCodec_RoundtripVerifie(t *testing.T) {
	skipShort(t)
	w, fee := snBuildScenario("codec-full", 2, 2)
	public, proof := ProveSpendN(w, fee)

	pubBytes := MarshalSpendNPublic(public)
	prBytes := MarshalSpendProof(proof)

	pub2, err := UnmarshalSpendNPublic(pubBytes)
	if err != nil {
		t.Fatalf("unmarshal public: %v", err)
	}
	pr2, err := UnmarshalSpendProof(prBytes)
	if err != nil {
		t.Fatalf("unmarshal proof: %v", err)
	}
	if !VerifySpendN(pub2, pr2) {
		t.Fatal("preuve décodée depuis les octets rejetée")
	}
}

// Décodage robuste : M/N hors borne, tampon tronqué, octets résiduels → erreur
// propre, jamais de panique.
func TestSpendNPublicCodec_Robuste(t *testing.T) {
	w, fee := snBuildScenario("robuste", 2, 2)
	_, public := buildSpendNTraceForCodec(w, fee)
	good := MarshalSpendNPublic(public)

	// Tronqué à chaque longueur < complète : toujours une erreur, jamais de panique.
	for n := 0; n < len(good); n++ {
		if _, err := UnmarshalSpendNPublic(good[:n]); err == nil {
			t.Fatalf("tampon tronqué à %d accepté", n)
		}
	}
	// Octets résiduels.
	if _, err := UnmarshalSpendNPublic(append(append([]byte{}, good...), 0x00)); err == nil {
		t.Fatal("octets résiduels acceptés")
	}
	// M = 0 (numIn nul) : 8 premiers octets = 0,0 → rejet.
	zero := make([]byte, len(good))
	copy(zero, good)
	for i := 0; i < 8; i++ {
		zero[i] = 0
	}
	if _, err := UnmarshalSpendNPublic(zero); err == nil {
		t.Fatal("numIn=0/numOut=0 accepté")
	}
	// M hors borne (numIn énorme).
	huge := make([]byte, len(good))
	copy(huge, good)
	huge[0], huge[1], huge[2], huge[3] = 0x7f, 0xff, 0xff, 0xff
	if _, err := UnmarshalSpendNPublic(huge); err == nil {
		t.Fatal("numIn hors borne accepté")
	}
}
