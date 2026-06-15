package codec

import (
	"bytes"
	"errors"
	"math"
	"testing"
)

// TestUvarintRoundtrip : couvre les bornes — 0, 1, 127 (1 octet), 128 (2 octets),
// 16383 (2 octets), 16384 (3 octets), MaxUint64 (10 octets).
func TestUvarintRoundtrip(t *testing.T) {
	cases := []uint64{0, 1, 127, 128, 16383, 16384, 1 << 32, math.MaxUint64}
	for _, v := range cases {
		e := NewEncoder(0)
		e.WriteUvarint(v)
		d := NewDecoder(e.Bytes())
		got, err := d.ReadUvarint()
		if err != nil {
			t.Fatalf("ReadUvarint(%d): %v", v, err)
		}
		if got != v {
			t.Fatalf("uvarint roundtrip: got %d, want %d", got, v)
		}
		if !d.Empty() {
			t.Fatalf("uvarint(%d): octets parasites en fin", v)
		}
	}
}

// TestVarintRoundtrip : signed (zigzag), incluant min/max et zéro.
func TestVarintRoundtrip(t *testing.T) {
	cases := []int64{math.MinInt64, -1 << 32, -1, 0, 1, 1 << 32, math.MaxInt64}
	for _, v := range cases {
		e := NewEncoder(0)
		e.WriteVarint(v)
		d := NewDecoder(e.Bytes())
		got, err := d.ReadVarint()
		if err != nil {
			t.Fatalf("ReadVarint(%d): %v", v, err)
		}
		if got != v {
			t.Fatalf("varint roundtrip: got %d, want %d", got, v)
		}
	}
}

// TestStringAndBytesRoundtrip : strings et []byte, vides et non vides.
// Vérifie l'invariant "len 0 = absent" : nil et []byte{} encodent pareil et
// se relisent en nil.
func TestStringAndBytesRoundtrip(t *testing.T) {
	t.Run("strings", func(t *testing.T) {
		for _, s := range []string{"", "a", "hello world", string(make([]byte, 1024))} {
			e := NewEncoder(0)
			e.WriteString(s)
			d := NewDecoder(e.Bytes())
			got, err := d.ReadString()
			if err != nil {
				t.Fatalf("ReadString(%q): %v", s, err)
			}
			if got != s {
				t.Fatalf("string roundtrip: got %q, want %q", got, s)
			}
		}
	})
	t.Run("bytes_nil_and_empty_are_equivalent", func(t *testing.T) {
		e1, e2 := NewEncoder(0), NewEncoder(0)
		e1.WriteBytes(nil)
		e2.WriteBytes([]byte{})
		if !bytes.Equal(e1.Bytes(), e2.Bytes()) {
			t.Fatalf("nil et []byte{} doivent produire la même sortie (len 0)")
		}
		d := NewDecoder(e1.Bytes())
		got, _ := d.ReadBytes()
		if got != nil {
			t.Fatalf("ReadBytes(len 0) doit renvoyer nil, got %v", got)
		}
	})
	t.Run("bytes_payload", func(t *testing.T) {
		payload := []byte{0x00, 0xff, 0x42, 0x80, 0x7f, 0x01}
		e := NewEncoder(0)
		e.WriteBytes(payload)
		d := NewDecoder(e.Bytes())
		got, err := d.ReadBytes()
		if err != nil {
			t.Fatalf("ReadBytes: %v", err)
		}
		if !bytes.Equal(got, payload) {
			t.Fatalf("bytes roundtrip: %v vs %v", got, payload)
		}
	})
}

// TestBoolRoundtrip + valeur invalide rejetée (≠ 0 et ≠ 1).
func TestBoolRoundtrip(t *testing.T) {
	for _, b := range []bool{true, false} {
		e := NewEncoder(0)
		e.WriteBool(b)
		d := NewDecoder(e.Bytes())
		got, err := d.ReadBool()
		if err != nil || got != b {
			t.Fatalf("bool roundtrip(%v): got=%v err=%v", b, got, err)
		}
	}
	d := NewDecoder([]byte{0x02})
	if _, err := d.ReadBool(); !errors.Is(err, ErrInvalidTag) {
		t.Fatalf("ReadBool(0x02) doit échouer avec ErrInvalidTag, got %v", err)
	}
}

// TestTruncatedInputs : un buffer coupé court doit renvoyer ErrTruncated sur
// chaque primitive — pas de panique, pas de lecture hors limites.
func TestTruncatedInputs(t *testing.T) {
	t.Run("uvarint_vide", func(t *testing.T) {
		d := NewDecoder(nil)
		if _, err := d.ReadUvarint(); !errors.Is(err, ErrTruncated) {
			t.Fatalf("attendu ErrTruncated, got %v", err)
		}
	})
	t.Run("string_payload_manquant", func(t *testing.T) {
		// uvarint = 5 (payload de 5 octets annoncé) mais buffer vide
		d := NewDecoder([]byte{0x05})
		if _, err := d.ReadString(); !errors.Is(err, ErrTruncated) {
			t.Fatalf("attendu ErrTruncated, got %v", err)
		}
	})
	t.Run("bytes_trop_grand", func(t *testing.T) {
		// uvarint annonçant > MaxBytesLen → refus immédiat
		e := NewEncoder(0)
		e.WriteUvarint(uint64(MaxBytesLen + 1))
		d := NewDecoder(e.Bytes())
		if _, err := d.ReadBytes(); !errors.Is(err, ErrTooLarge) {
			t.Fatalf("attendu ErrTooLarge, got %v", err)
		}
	})
	t.Run("u8_vide", func(t *testing.T) {
		d := NewDecoder(nil)
		if _, err := d.ReadU8(); !errors.Is(err, ErrTruncated) {
			t.Fatalf("attendu ErrTruncated, got %v", err)
		}
	})
}

// TestMustFinishCatchesTrailing : si l'appelant attend que tout soit consommé,
// MustFinish signale les octets parasites.
func TestMustFinishCatchesTrailing(t *testing.T) {
	d := NewDecoder([]byte{0x01, 0x99, 0x99})
	if _, err := d.ReadU8(); err != nil {
		t.Fatal(err)
	}
	if err := d.MustFinish(); err == nil {
		t.Fatal("octets parasites doivent être détectés")
	}
}

// TestEncodingCompactness : pour les valeurs typiques de la chaîne, le varint
// est sensiblement plus court qu'un encodage fixe 8 octets.
func TestEncodingCompactness(t *testing.T) {
	cases := []struct {
		v    uint64
		want int
	}{
		{0, 1},
		{100, 1},
		{50_000, 3},    // base fee typique = 100_000 (3 octets)
		{1 << 30, 5},   // 1 milliard ucgo = 1 CGO
		{1 << 60, 9},   // hauteur extrême
	}
	for _, c := range cases {
		e := NewEncoder(0)
		e.WriteUvarint(c.v)
		if got := e.Len(); got != c.want {
			t.Errorf("WriteUvarint(%d) → %d octet(s), attendu %d", c.v, got, c.want)
		}
	}
}
