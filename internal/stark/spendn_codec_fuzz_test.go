package stark

import "testing"

// FuzzUnmarshalSpendNPublic vérifie que le décodeur d'énoncé public M-in/N-out ne
// PANIQUE JAMAIS sur des octets arbitraires (surface d'attaque : données reçues du
// réseau). Le fuzzer Go échoue automatiquement sur toute panique. On vérifie en
// plus l'invariant aller-retour : tout ce qui décode se ré-encode à l'identique.
func FuzzUnmarshalSpendNPublic(f *testing.F) {
	// Graines : un énoncé valide + des cas dégénérés.
	w, fee := snBuildScenario("fuzz", 2, 2)
	_, public := buildSpendNTrace(w, fee)
	f.Add(MarshalSpendNPublic(public))
	f.Add([]byte{})
	f.Add([]byte{0, 0, 0, 1, 0, 0, 0, 1})

	f.Fuzz(func(t *testing.T, data []byte) {
		p, err := UnmarshalSpendNPublic(data)
		if err != nil {
			return // rejet propre : comportement attendu
		}
		// Décodé sans erreur → doit se ré-encoder exactement (déterminisme du codec).
		re := MarshalSpendNPublic(p)
		if len(re) != len(data) {
			t.Fatalf("aller-retour: longueur %d != %d", len(re), len(data))
		}
		for i := range re {
			if re[i] != data[i] {
				t.Fatalf("aller-retour: octet %d divergent", i)
			}
		}
	})
}
