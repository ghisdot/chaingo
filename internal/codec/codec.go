// Package codec : encodage binaire compact pour le transport (P2P) et le
// stockage de la chaîne. Économise ~25 % vs JSON+base64 sur les types qui
// portent des grosses signatures ML-DSA-65 (~3,3 Ko).
//
// IMPORTANT : ce codec ne remplace PAS la sérialisation signée
// (`Transaction.SigningBytes`, `Block.SigningBytes`, `Vote.SigningBytes`)
// qui doit rester en JSON canonique — sinon toutes les signatures
// existantes deviendraient invalides. Le codec binaire sert uniquement à
// transporter les structures déjà signées.
//
// Format primitif :
//   - uvarint        : entiers non signés (style Protocol Buffers, MSB =
//                      continuation bit). Économe pour les petits nombres.
//   - varint         : entiers signés (zigzag encoding).
//   - byte           : 1 octet brut (utilisé pour bool 0/1).
//   - length-prefixed: uvarint(len) + raw bytes. Sert pour string et []byte.
//
// Format composite (chaque type définit son propre ordre de champs) :
//   - struct : suite de primitives, ordre fixé par la définition.
//   - optionnel : pour les champs string / []byte, "absent" = len 0.
//                 Pour les pointeurs (*T), 1 byte de présence puis encodage si présent.
package codec

import (
	"encoding/binary"
	"errors"
	"fmt"
)

// Erreurs typées (utiles côté P2P pour différencier "donnée tronquée" de
// "format invalide").
var (
	ErrTruncated  = errors.New("codec: data truncated")
	ErrOverflow   = errors.New("codec: varint overflows uint64")
	ErrTooLarge   = errors.New("codec: length-prefixed value exceeds limit")
	ErrInvalidTag = errors.New("codec: invalid optional presence tag")
)

// MaxBytesLen : plafond dur sur la taille d'un champ length-prefixed. Garde
// contre les attaques de mémoire (un pair hostile envoyant len = 2^60). Les
// signatures ML-DSA-65 font ~3,3 Ko, le pubkey ~2 Ko, on laisse de la marge
// pour les blocs entiers (qui peuvent atteindre plusieurs Mo si bien
// remplis : 2000 tx × ~3 Ko = ~6 Mo).
const MaxBytesLen = 32 * 1024 * 1024 // 32 Mo

// ---- Encoder ----

// Encoder accumule l'écriture dans un buffer interne.
type Encoder struct {
	buf []byte
}

// NewEncoder retourne un encodeur prêt à écrire. `initialCap` (peut valoir 0)
// est un hint pour pré-allouer.
func NewEncoder(initialCap int) *Encoder {
	return &Encoder{buf: make([]byte, 0, initialCap)}
}

// Bytes renvoie l'encodage final (référence directe — ne pas modifier).
func (e *Encoder) Bytes() []byte { return e.buf }

// Len : nombre d'octets écrits jusqu'ici.
func (e *Encoder) Len() int { return len(e.buf) }

// WriteUvarint écrit un entier non signé en varint LEB128.
func (e *Encoder) WriteUvarint(v uint64) {
	var tmp [binary.MaxVarintLen64]byte
	n := binary.PutUvarint(tmp[:], v)
	e.buf = append(e.buf, tmp[:n]...)
}

// WriteVarint écrit un entier signé en zigzag-varint.
func (e *Encoder) WriteVarint(v int64) {
	var tmp [binary.MaxVarintLen64]byte
	n := binary.PutVarint(tmp[:], v)
	e.buf = append(e.buf, tmp[:n]...)
}

// WriteU8 écrit un octet brut (utilisé pour les petits uint8 fixés). Pour
// les bool, préférer WriteBool. Nom volontairement distinct de io.ByteWriter
// pour ne pas shadowiser l'interface standard.
func (e *Encoder) WriteU8(b byte) { e.buf = append(e.buf, b) }

// WriteBool écrit un bool en 1 octet (0 ou 1).
func (e *Encoder) WriteBool(b bool) {
	if b {
		e.buf = append(e.buf, 1)
	} else {
		e.buf = append(e.buf, 0)
	}
}

// WriteBytes écrit un []byte length-prefixed. nil et len=0 produisent la
// même sortie ("longueur 0, pas de payload") — c'est l'invariant qu'on
// utilise pour "champ absent" sur les []byte optionnels.
func (e *Encoder) WriteBytes(b []byte) {
	e.WriteUvarint(uint64(len(b)))
	e.buf = append(e.buf, b...)
}

// WriteString écrit une string length-prefixed (vide = "absent" possible).
func (e *Encoder) WriteString(s string) {
	e.WriteUvarint(uint64(len(s)))
	e.buf = append(e.buf, s...)
}

// ---- Decoder ----

// Decoder lit séquentiellement depuis un buffer.
type Decoder struct {
	buf []byte
	pos int
}

// NewDecoder enveloppe le buffer. Le buffer N'EST PAS recopié : ne pas le
// modifier pendant le décodage.
func NewDecoder(buf []byte) *Decoder { return &Decoder{buf: buf} }

// Remaining : nombre d'octets non encore lus.
func (d *Decoder) Remaining() int { return len(d.buf) - d.pos }

// Empty : true s'il ne reste rien à lire (utile pour valider qu'on n'a pas
// d'octets parasites en fin de payload).
func (d *Decoder) Empty() bool { return d.pos >= len(d.buf) }

func (d *Decoder) ReadUvarint() (uint64, error) {
	v, n := binary.Uvarint(d.buf[d.pos:])
	if n == 0 {
		return 0, fmt.Errorf("%w: uvarint", ErrTruncated)
	}
	if n < 0 {
		return 0, ErrOverflow
	}
	d.pos += n
	return v, nil
}

func (d *Decoder) ReadVarint() (int64, error) {
	v, n := binary.Varint(d.buf[d.pos:])
	if n == 0 {
		return 0, fmt.Errorf("%w: varint", ErrTruncated)
	}
	if n < 0 {
		return 0, ErrOverflow
	}
	d.pos += n
	return v, nil
}

// ReadU8 lit un octet brut. Pour les bool, préférer ReadBool (qui rejette
// les valeurs ≠ 0/1).
func (d *Decoder) ReadU8() (byte, error) {
	if d.pos >= len(d.buf) {
		return 0, fmt.Errorf("%w: u8", ErrTruncated)
	}
	b := d.buf[d.pos]
	d.pos++
	return b, nil
}

func (d *Decoder) ReadBool() (bool, error) {
	b, err := d.ReadU8()
	if err != nil {
		return false, err
	}
	switch b {
	case 0:
		return false, nil
	case 1:
		return true, nil
	default:
		return false, fmt.Errorf("%w: bool=%d", ErrInvalidTag, b)
	}
}

// ReadBytes lit un []byte length-prefixed. Renvoie nil si la longueur est 0
// (cohérent avec WriteBytes(nil) → length 0).
func (d *Decoder) ReadBytes() ([]byte, error) {
	n, err := d.ReadUvarint()
	if err != nil {
		return nil, err
	}
	if n == 0 {
		return nil, nil
	}
	if n > MaxBytesLen {
		return nil, fmt.Errorf("%w: %d > %d", ErrTooLarge, n, MaxBytesLen)
	}
	if d.Remaining() < int(n) {
		return nil, fmt.Errorf("%w: bytes(len=%d, remaining=%d)", ErrTruncated, n, d.Remaining())
	}
	// Copie défensive : le buffer source peut être réutilisé par l'appelant.
	out := make([]byte, n)
	copy(out, d.buf[d.pos:d.pos+int(n)])
	d.pos += int(n)
	return out, nil
}

// ReadString lit une string length-prefixed. Renvoie "" si la longueur est 0.
func (d *Decoder) ReadString() (string, error) {
	n, err := d.ReadUvarint()
	if err != nil {
		return "", err
	}
	if n == 0 {
		return "", nil
	}
	if n > MaxBytesLen {
		return "", fmt.Errorf("%w: %d > %d", ErrTooLarge, n, MaxBytesLen)
	}
	if d.Remaining() < int(n) {
		return "", fmt.Errorf("%w: string(len=%d, remaining=%d)", ErrTruncated, n, d.Remaining())
	}
	s := string(d.buf[d.pos : d.pos+int(n)])
	d.pos += int(n)
	return s, nil
}

// MustFinish vérifie qu'il ne reste pas d'octets non consommés — détecte
// les payloads trop longs (potentiellement malveillants).
func (d *Decoder) MustFinish() error {
	if !d.Empty() {
		return fmt.Errorf("codec: %d trailing bytes after decode", d.Remaining())
	}
	return nil
}

