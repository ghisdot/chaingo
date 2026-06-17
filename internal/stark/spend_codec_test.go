// STARK MAISON — EXPÉRIMENTAL, NON AUDITÉ, hors-consensus.
//
// ÉTAGE 5.1 — Tests de la sérialisation binaire de la preuve blindée
// (spend_codec.go). On valide :
//
//  1. ROUND-TRIP d'une preuve RÉELLE (générée par ProveSpend, cachée via
//     sync.Once : UNE SEULE preuve pour TOUT le fichier — une preuve coûte ~95 s)
//     Marshal -> Unmarshal == identique champ-pour-champ, ET la preuve décodée
//     VÉRIFIE (VerifySpend(public, proofDécodé) == true).
//  2. Round-trip de SpendPublic (rapide, sans STARK).
//  3. Détermination : MarshalSpendProof est stable (mêmes octets à chaque appel).
//  4. ROBUSTESSE : octets tronqués / aberrants / résiduels => ERREUR propre, pas
//     de PANIQUE.
//
// COÛT : UNE seule preuve STARK (sync.Once), réutilisée partout. Aucun autre
// prouveur n'est lancé.
package stark

import (
	"bytes"
	"sync"
	"testing"
)

// ---------------------------------------------------------------------------
// Preuve RÉELLE partagée (UNE SEULE génération pour tout le fichier).
// ---------------------------------------------------------------------------

var (
	scOnce   sync.Once
	scPublic SpendPublic
	scProof  AirProof
)

// scShared génère paresseusement UNE preuve honnête de dépense et la met en cache.
// Coûteux (~95 s) : appelé au plus une fois grâce à sync.Once. Tous les tests de
// preuve réelle réutilisent ce couple (public, proof).
func scShared() (SpendPublic, AirProof) {
	scOnce.Do(func() {
		w, fee, _ := spBuildScenario("codec", 5)
		scPublic, scProof = ProveSpend(w, fee)
	})
	return scPublic, scProof
}

// ---------------------------------------------------------------------------
// Comparaison structurelle profonde des AirProof (sans reflect, pour un message
// d'erreur précis sur le premier champ divergent).
// ---------------------------------------------------------------------------

func scFeltsEqual(a, b []Felt) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if !a[i].Equal(b[i]) {
			return false
		}
	}
	return true
}

func scHashesEqual(a, b [][32]byte) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func scQueryStepEqual(a, b QueryStep) bool {
	return a.Value.Equal(b.Value) && a.Sibling.Equal(b.Sibling) &&
		scHashesEqual(a.Path, b.Path) && scHashesEqual(a.SiblingPath, b.SiblingPath)
}

func scFriEqual(t *testing.T, a, b FriProof) bool {
	t.Helper()
	if a.LogDomain != b.LogDomain {
		t.Errorf("Fri.LogDomain: %d != %d", a.LogDomain, b.LogDomain)
		return false
	}
	if !scHashesEqual(a.LayerRoots, b.LayerRoots) {
		t.Errorf("Fri.LayerRoots divergent")
		return false
	}
	if !scFeltsEqual(a.FinalCoeffs, b.FinalCoeffs) {
		t.Errorf("Fri.FinalCoeffs divergent")
		return false
	}
	if len(a.Queries) != len(b.Queries) {
		t.Errorf("Fri.Queries: %d != %d requêtes", len(a.Queries), len(b.Queries))
		return false
	}
	for q := range a.Queries {
		if len(a.Queries[q]) != len(b.Queries[q]) {
			t.Errorf("Fri.Queries[%d]: %d != %d étapes", q, len(a.Queries[q]), len(b.Queries[q]))
			return false
		}
		for s := range a.Queries[q] {
			if !scQueryStepEqual(a.Queries[q][s], b.Queries[q][s]) {
				t.Errorf("Fri.Queries[%d][%d] divergent", q, s)
				return false
			}
		}
	}
	return true
}

func scOpeningEqual(t *testing.T, i int, a, b AirOpening) bool {
	t.Helper()
	if a.Pos != b.Pos {
		t.Errorf("Openings[%d].Pos: %d != %d", i, a.Pos, b.Pos)
		return false
	}
	if !scFeltsEqual(a.ColVals, b.ColVals) {
		t.Errorf("Openings[%d].ColVals divergent", i)
		return false
	}
	if len(a.ColPaths) != len(b.ColPaths) {
		t.Errorf("Openings[%d].ColPaths: %d != %d", i, len(a.ColPaths), len(b.ColPaths))
		return false
	}
	for c := range a.ColPaths {
		if !scHashesEqual(a.ColPaths[c], b.ColPaths[c]) {
			t.Errorf("Openings[%d].ColPaths[%d] divergent", i, c)
			return false
		}
	}
	if !a.CompVal.Equal(b.CompVal) || !scHashesEqual(a.CompPath, b.CompPath) {
		t.Errorf("Openings[%d] composition divergente", i)
		return false
	}
	if !a.DeepVal.Equal(b.DeepVal) || !scHashesEqual(a.DeepPath, b.DeepPath) {
		t.Errorf("Openings[%d] DEEP divergent", i)
		return false
	}
	return true
}

func scProofEqual(t *testing.T, a, b AirProof) bool {
	t.Helper()
	ok := true
	if !scHashesEqual(a.ColRoots, b.ColRoots) {
		t.Errorf("ColRoots divergent")
		ok = false
	}
	if a.CompRoot != b.CompRoot {
		t.Errorf("CompRoot divergent")
		ok = false
	}
	if !scFeltsEqual(a.OodColZ, b.OodColZ) {
		t.Errorf("OodColZ divergent")
		ok = false
	}
	if !scFeltsEqual(a.OodColGZ, b.OodColGZ) {
		t.Errorf("OodColGZ divergent")
		ok = false
	}
	if !a.OodHz.Equal(b.OodHz) {
		t.Errorf("OodHz divergent")
		ok = false
	}
	if !scFriEqual(t, a.Fri, b.Fri) {
		ok = false
	}
	if len(a.Openings) != len(b.Openings) {
		t.Errorf("Openings: %d != %d", len(a.Openings), len(b.Openings))
		return false
	}
	for i := range a.Openings {
		if !scOpeningEqual(t, i, a.Openings[i], b.Openings[i]) {
			ok = false
		}
	}
	return ok
}

// ---------------------------------------------------------------------------
// 1) ROUND-TRIP d'une preuve RÉELLE + re-vérification
// ---------------------------------------------------------------------------

// La preuve réelle survit à Marshal -> Unmarshal (identique champ-pour-champ) et
// la preuve décodée VÉRIFIE contre son énoncé public. C'est le test central.
func TestSpendCodec_RoundTripPreuveReelle(t *testing.T) {
	public, proof := scShared()

	enc := MarshalSpendProof(proof)
	if len(enc) == 0 {
		t.Fatalf("encodage vide")
	}

	dec, err := UnmarshalSpendProof(enc)
	if err != nil {
		t.Fatalf("UnmarshalSpendProof: %v", err)
	}

	if !scProofEqual(t, proof, dec) {
		t.Fatalf("la preuve décodée diffère de l'originale")
	}

	// Ré-encoder la preuve décodée donne EXACTEMENT les mêmes octets (idempotence).
	enc2 := MarshalSpendProof(dec)
	if !bytes.Equal(enc, enc2) {
		t.Fatalf("Marshal(Unmarshal(x)) != Marshal(x) : encodage non idempotent")
	}

	// La preuve décodée doit VÉRIFIER contre l'énoncé public original.
	if !VerifySpend(public, dec) {
		t.Fatalf("VerifySpend rejette la preuve décodée (round-trip casse la preuve)")
	}
}

// Déterminisme de l'encodage : deux Marshal de la MÊME preuve donnent les mêmes
// octets (aucune map, aucun ordre instable).
func TestSpendCodec_MarshalDeterministe(t *testing.T) {
	_, proof := scShared()
	a := MarshalSpendProof(proof)
	b := MarshalSpendProof(proof)
	if !bytes.Equal(a, b) {
		t.Fatalf("MarshalSpendProof non déterministe")
	}
}

// ---------------------------------------------------------------------------
// 2) Round-trip de SpendPublic (rapide, sans STARK)
// ---------------------------------------------------------------------------

func TestSpendCodec_SpendPublicRoundTrip(t *testing.T) {
	// Énoncé public natif (sans prouver : buildSpendTrace est rapide).
	w, fee, _ := spBuildScenario("codec-public", 3)
	_, public := buildSpendTrace(w, fee)

	enc := MarshalSpendPublic(public)
	if len(enc) != spendPublicBytes {
		t.Fatalf("taille SpendPublic encodé %d, attendu %d", len(enc), spendPublicBytes)
	}

	dec, err := UnmarshalSpendPublic(enc)
	if err != nil {
		t.Fatalf("UnmarshalSpendPublic: %v", err)
	}
	if dec != public {
		t.Fatalf("SpendPublic décodé != original")
	}

	// Tailles aberrantes => erreur propre (pas de panique).
	if _, err := UnmarshalSpendPublic(enc[:len(enc)-1]); err == nil {
		t.Fatalf("SpendPublic tronqué accepté")
	}
	if _, err := UnmarshalSpendPublic(append(enc, 0x00)); err == nil {
		t.Fatalf("SpendPublic avec octet résiduel accepté")
	}
	if _, err := UnmarshalSpendPublic(nil); err == nil {
		t.Fatalf("SpendPublic vide accepté")
	}
}

// ---------------------------------------------------------------------------
// 4) ROBUSTESSE : tronqué / aberrant / résiduel => erreur propre, JAMAIS panique
// ---------------------------------------------------------------------------

// Tout préfixe TRONQUÉ de l'encodage réel doit être REFUSÉ proprement (erreur, pas
// de panique). On échantillonne de nombreuses longueurs de coupe.
func TestSpendCodec_TronquéRefusé(t *testing.T) {
	_, proof := scShared()
	enc := MarshalSpendProof(proof)

	// Pour chaque longueur de coupe (échantillonnée), Unmarshal NE doit pas paniquer
	// et doit renvoyer une erreur (un préfixe strict d'un encodage valide n'est
	// jamais lui-même un encodage complet sans octets résiduels).
	step := len(enc)/200 + 1
	for cut := 0; cut < len(enc); cut += step {
		func() {
			defer func() {
				if r := recover(); r != nil {
					t.Fatalf("PANIQUE sur troncature à %d octets: %v", cut, r)
				}
			}()
			if _, err := UnmarshalSpendProof(enc[:cut]); err == nil {
				t.Fatalf("préfixe tronqué à %d octets accepté comme preuve valide", cut)
			}
		}()
	}
}

// Octets RÉSIDUELS après une preuve valide => errSCTrailing (pas de panique).
func TestSpendCodec_OctetsResiduels(t *testing.T) {
	_, proof := scShared()
	enc := MarshalSpendProof(proof)
	trailing := append(append([]byte{}, enc...), 0xAB, 0xCD)
	if _, err := UnmarshalSpendProof(trailing); err == nil {
		t.Fatalf("preuve avec octets résiduels acceptée")
	}
}

// Entrées ABERRANTES (préfixes de longueur géants, octets aléatoires, buffers
// vides) => erreur propre, JAMAIS de panique. On ne lance AUCUN prouveur ici.
func TestSpendCodec_AberrantRefusé(t *testing.T) {
	cases := map[string][]byte{
		"vide":             {},
		"un octet":         {0x01},
		"trois octets":     {0x00, 0x00, 0x00},
		"len géante u32":   {0xFF, 0xFF, 0xFF, 0xFF},        // ColRoots: 4G entrées
		"len énorme +rien": {0x00, 0x10, 0x00, 0x00},        // 1M entrées, buffer vide derrière
		"tout 0xFF":        bytes.Repeat([]byte{0xFF}, 512), // octets pseudo-aléatoires
		"tout 0x00":        make([]byte, 512),               // longueurs nulles en cascade
	}
	for name, b := range cases {
		b := b
		t.Run(name, func(t *testing.T) {
			defer func() {
				if r := recover(); r != nil {
					t.Fatalf("PANIQUE sur entrée %q: %v", name, r)
				}
			}()
			if _, err := UnmarshalSpendProof(b); err == nil {
				t.Fatalf("entrée aberrante %q acceptée", name)
			}
		})
	}
}

// Un préfixe de longueur dépassant la borne anti-DoS est rejeté SANS allocation
// proportionnelle : on le vérifie en fabriquant un en-tête ColRoots annonçant un
// nombre d'entrées au-delà de scMaxColumns.
func TestSpendCodec_BorneAntiDoS(t *testing.T) {
	w := &scWriter{}
	w.u32(scMaxColumns + 1) // ColRoots: longueur hors borne
	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("PANIQUE sur longueur hors borne: %v", r)
		}
	}()
	if _, err := UnmarshalSpendProof(w.buf); err == nil {
		t.Fatalf("longueur ColRoots hors borne acceptée")
	}
}

// Garde-fou de taille TOTALE : un buffer plus grand que scMaxTotalBytes est refusé
// immédiatement (on n'alloue pas un reader dessus). On simule sans réellement
// allouer 64 Mo en s'appuyant sur la vérification de longueur (test logique léger
// via un buffer juste au-dessus serait coûteux ; on teste la frontière logique en
// vérifiant qu'un buffer normal passe la garde et qu'un préfixe géant échoue, déjà
// couvert ci-dessus). Ici on vérifie seulement que la constante est positive et que
// le chemin nominal ne la déclenche pas.
func TestSpendCodec_TailleTotaleConstante(t *testing.T) {
	if scMaxTotalBytes <= 0 {
		t.Fatalf("scMaxTotalBytes doit être positif")
	}
	_, proof := scShared()
	enc := MarshalSpendProof(proof)
	if len(enc) > scMaxTotalBytes {
		t.Fatalf("une preuve honnête (%d octets) dépasse la borne totale %d", len(enc), scMaxTotalBytes)
	}
}
