package mnemonic

import (
	"bytes"
	"encoding/hex"
	"strings"
	"testing"
)

// Round-trip : seed -> phrase -> seed reproduit EXACTEMENT le seed d'origine,
// pour de nombreux seeds déterministes.
func TestRoundTrip(t *testing.T) {
	for i := 0; i < 512; i++ {
		seed := make([]byte, 32)
		for j := range seed {
			seed[j] = byte((i*31 + j*7) & 0xff)
		}
		phrase, err := FromSeed(seed)
		if err != nil {
			t.Fatalf("FromSeed: %v", err)
		}
		if n := len(strings.Fields(phrase)); n != WordCount {
			t.Fatalf("phrase de %d mots, attendu %d", n, WordCount)
		}
		got, err := ToSeed(phrase)
		if err != nil {
			t.Fatalf("ToSeed: %v", err)
		}
		if !bytes.Equal(got, seed) {
			t.Fatalf("seed non préservé\n seed=%x\n got =%x", seed, got)
		}
	}
}

// Vecteur BIP39 standard : l'entropie tout-à-zéro (256 bits) donne la phrase
// « abandon × 23 + art ». Confirme que notre encodage EST du BIP39 conforme.
func TestKnownVectorZero(t *testing.T) {
	seed := make([]byte, 32) // tout à zéro
	phrase, err := FromSeed(seed)
	if err != nil {
		t.Fatal(err)
	}
	want := strings.Repeat("abandon ", 23) + "art"
	if phrase != want {
		t.Fatalf("vecteur zéro:\n got  %q\n want %q", phrase, want)
	}
}

// Vecteur BIP39 standard : entropie 0xff... (256 bits) → phrase connue se
// terminant par « … zoo vote ».
func TestKnownVectorFF(t *testing.T) {
	seed, _ := hex.DecodeString(strings.Repeat("ff", 32))
	phrase, err := FromSeed(seed)
	if err != nil {
		t.Fatal(err)
	}
	// La phrase officielle pour 0xff×32 se termine par "zoo vote".
	if !strings.HasSuffix(phrase, "zoo vote") {
		t.Fatalf("vecteur FF: suffixe inattendu: %q", phrase)
	}
	// Et round-trip.
	got, err := ToSeed(phrase)
	if err != nil || !bytes.Equal(got, seed) {
		t.Fatalf("vecteur FF: round-trip cassé (err=%v)", err)
	}
}

// Un checksum faux (dernier mot changé pour un autre valide) est rejeté.
func TestChecksumRejected(t *testing.T) {
	seed := make([]byte, 32)
	for j := range seed {
		seed[j] = byte(j)
	}
	phrase, _ := FromSeed(seed)
	words := strings.Fields(phrase)
	// Remplace le dernier mot par un autre mot valide (casse quasi-sûrement le checksum).
	last := words[len(words)-1]
	repl := "zoo"
	if last == "zoo" {
		repl = "abandon"
	}
	words[len(words)-1] = repl
	if Valid(strings.Join(words, " ")) {
		t.Fatal("un mnémonique au checksum faux a été accepté")
	}
}

// Entrées malformées : mauvais nombre de mots, mot inconnu.
func TestMalformed(t *testing.T) {
	if _, err := ToSeed("abandon abandon"); err == nil {
		t.Fatal("2 mots acceptés")
	}
	bad := strings.Repeat("abandon ", 23) + "notaword"
	if _, err := ToSeed(bad); err == nil {
		t.Fatal("mot inconnu accepté")
	}
	// Casse et espaces : tolérés.
	seed := make([]byte, 32)
	phrase, _ := FromSeed(seed)
	messy := "  " + strings.ToUpper(phrase) + "  "
	if got, err := ToSeed(messy); err != nil || !bytes.Equal(got, seed) {
		t.Fatalf("phrase en MAJ/espaces refusée: %v", err)
	}
}

// FromSeed exige 32 octets.
func TestFromSeedSize(t *testing.T) {
	if _, err := FromSeed(make([]byte, 16)); err == nil {
		t.Fatal("seed de 16 octets accepté")
	}
}

// La wordlist a bien 2048 mots uniques.
func TestWordlistIntegrity(t *testing.T) {
	if len(wordlist) != 2048 {
		t.Fatalf("wordlist de %d mots", len(wordlist))
	}
	if len(wordIndex) != 2048 {
		t.Fatalf("wordIndex de %d entrées (doublons ?)", len(wordIndex))
	}
	if wordlist[0] != "abandon" || wordlist[2047] != "zoo" {
		t.Fatal("bornes de wordlist inattendues")
	}
}
