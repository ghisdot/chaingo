package wasmvm

import (
	"encoding/binary"
	"errors"
	"fmt"
)

// meter.go : instrumentation de gas DÉTERMINISTE par réécriture du bytecode
// WASM. C'est ce qui permettrait d'exécuter du WASM EN CONSENSUS : on injecte un
// compteur de gas (1) en tête de chaque corps de fonction et (2) en tête de
// chaque `loop`. Comme une boucle et la récursion sont les SEULS moyens de
// répéter du travail en WASM, charger du gas à ces points GARANTIT l'arrêt :
// aucun module ne peut s'exécuter au-delà de son gas, et le point d'arrêt est
// le même sur tous les nœuds (déterministe — contrairement à un timeout).
//
// Périmètre v1 : GARANTIE D'ARRÊT (un contrat ne tourne jamais à l'infini). La
// tarification fine (gas proportionnel au travail, par bloc de base) est un
// raffinement ultérieur. Jeu d'opcodes : sous-ensemble MVP de WebAssembly ; tout
// opcode hors de ce sous-ensemble (SIMD, atomics, bulk-memory 0xfc/0xfd/0xfe…)
// fait REJETER le module — on ne devine jamais la longueur d'un immédiat inconnu
// (un mauvais saut désynchroniserait l'instrumentation = faille).
//
// ⚠️ Toujours HORS-CONSENSUS tant que : API hôte d'état, tx deploy/call, audit
// de déterminisme (flottants) et audit externe ne sont pas faits. Voir
// docs/design/wasm-vm.md.

var (
	ErrUnsupportedOpcode = errors.New("wasm: opcode hors du sous-ensemble supporté (rejeté par sûreté)")
	ErrMalformed         = errors.New("wasm: module mal formé")
)

const (
	gasFuncName = "gas" // pour info ; ici on utilise un global, pas un import
)

// instrument réécrit `wasm` pour qu'il décompte du gas. Le module résultat porte
// un global i64 mutable initialisé à `gasLimit` ; quand il passe sous 0, le
// module piège (`unreachable`) → l'exécution s'arrête (out-of-gas), partout
// identiquement. `costPerBlock` est débité à chaque entrée de fonction et tête
// de boucle.
func instrument(wasm []byte, gasLimit, costPerBlock int64) ([]byte, error) {
	if len(wasm) < 8 || string(wasm[:4]) != "\x00asm" {
		return nil, ErrMalformed
	}
	p := &parser{buf: wasm, pos: 8}

	// 1er passage : compter les globals (importés + définis) pour connaître
	// l'index de NOTRE global de gas (on l'ajoute À LA FIN → aucun renumérotage).
	gImported, gDefined, err := p.countGlobals()
	if err != nil {
		return nil, err
	}
	gasGlobal := uint32(gImported + gDefined)

	// 2e passage : recopier les sections, en réécrivant global (6) et code (10).
	out := make([]byte, 0, len(wasm)+len(wasm)/4)
	out = append(out, wasm[:8]...) // magic + version
	pos := 8
	for pos < len(wasm) {
		id := wasm[pos]
		secStart := pos
		pos++
		size, n, err := readUvarint(wasm, pos)
		if err != nil {
			return nil, err
		}
		pos += n
		if size > uint64(len(wasm)-pos) {
			return nil, ErrMalformed
		}
		content := wasm[pos : pos+int(size)]
		pos += int(size)

		switch id {
		case 6: // global section : ajouter notre global de gas
			newContent, err := appendGasGlobal(content, gImported, gasLimit)
			if err != nil {
				return nil, err
			}
			out = appendSection(out, 6, newContent)
		case 7: // export section : exporter notre global de gas (pour lire le gas restant)
			newContent, err := appendGasExport(content, gasGlobal)
			if err != nil {
				return nil, err
			}
			out = appendSection(out, 7, newContent)
		case 10: // code section : instrumenter chaque corps de fonction
			newContent, err := instrumentCode(content, gasGlobal, costPerBlock)
			if err != nil {
				return nil, err
			}
			out = appendSection(out, 10, newContent)
		default:
			out = append(out, wasm[secStart:pos]...) // copie verbatim
		}
	}

	// Sections à créer si absentes, dans l'ordre canonique (global=6, export=7).
	if gDefined == 0 && !hasSection(wasm, 6) {
		out, err = insertGlobalSection(out, gImported, gasLimit)
		if err != nil {
			return nil, err
		}
	}
	if !hasSection(wasm, 7) {
		out, err = insertExportSection(out, gasGlobal)
		if err != nil {
			return nil, err
		}
	}
	return out, nil
}

// ---- lecture de primitives ----

func readUvarint(b []byte, pos int) (uint64, int, error) {
	if pos < 0 || pos > len(b) { // garde anti-panique (b[pos:] hors bornes)
		return 0, 0, ErrMalformed
	}
	v, n := binary.Uvarint(b[pos:])
	if n <= 0 {
		return 0, 0, ErrMalformed
	}
	return v, n, nil
}

func readVarint(b []byte, pos int) (int64, int, error) {
	if pos < 0 || pos > len(b) {
		return 0, 0, ErrMalformed
	}
	v, n := binary.Varint(b[pos:])
	if n <= 0 {
		return 0, 0, ErrMalformed
	}
	return v, n, nil
}

type parser struct {
	buf []byte
	pos int
}

// countGlobals : nombre de globals importés (section 2) + définis (section 6).
func (p *parser) countGlobals() (imported, defined int, err error) {
	pos := 8
	for pos < len(p.buf) {
		id := p.buf[pos]
		pos++
		size, n, e := readUvarint(p.buf, pos)
		if e != nil {
			return 0, 0, e
		}
		pos += n
		if size > uint64(len(p.buf)-pos) {
			return 0, 0, ErrMalformed
		}
		content := p.buf[pos : pos+int(size)]
		pos += int(size)
		switch id {
		case 2: // import section : compter les imports de kind global (0x03)
			c, e := countImportedGlobals(content)
			if e != nil {
				return 0, 0, e
			}
			imported = c
		case 6: // global section : 1er uvarint = nombre de globals
			cnt, _, e := readUvarint(content, 0)
			if e != nil {
				return 0, 0, e
			}
			defined = int(cnt)
		}
	}
	return imported, defined, nil
}

func hasSection(wasm []byte, id byte) bool {
	pos := 8
	for pos < len(wasm) {
		sid := wasm[pos]
		pos++
		size, n, err := readUvarint(wasm, pos)
		if err != nil {
			return false
		}
		pos += n + int(size)
		if sid == id {
			return true
		}
	}
	return false
}

func countImportedGlobals(content []byte) (int, error) {
	pos := 0
	cnt, n, err := readUvarint(content, pos)
	if err != nil {
		return 0, err
	}
	pos += n
	globals := 0
	for i := uint64(0); i < cnt; i++ {
		// module name (vec byte), field name (vec byte), kind (1), desc
		for j := 0; j < 2; j++ {
			l, n, err := readUvarint(content, pos)
			if err != nil {
				return 0, err
			}
			pos += n
			if l > uint64(len(content)-pos) {
				return 0, ErrMalformed
			}
			pos += int(l)
		}
		if pos >= len(content) {
			return 0, ErrMalformed
		}
		kind := content[pos]
		pos++
		switch kind {
		case 0x00: // func : typeidx
			_, n, err := readUvarint(content, pos)
			if err != nil {
				return 0, err
			}
			pos += n
		case 0x01: // table : reftype(1) + limits
			pos++
			pos, err = skipLimits(content, pos)
			if err != nil {
				return 0, err
			}
		case 0x02: // mem : limits
			pos, err = skipLimits(content, pos)
			if err != nil {
				return 0, err
			}
		case 0x03: // global : valtype(1) + mutability(1)
			globals++
			pos += 2
		default:
			return 0, ErrMalformed
		}
	}
	return globals, nil
}

func skipLimits(b []byte, pos int) (int, error) {
	if pos >= len(b) {
		return 0, ErrMalformed
	}
	flag := b[pos]
	pos++
	_, n, err := readUvarint(b, pos) // min
	if err != nil {
		return 0, err
	}
	pos += n
	if flag == 0x01 { // a un max
		_, n, err := readUvarint(b, pos)
		if err != nil {
			return 0, err
		}
		pos += n
	}
	return pos, nil
}

// ---- émission ----

func appendSection(out []byte, id byte, content []byte) []byte {
	out = append(out, id)
	out = appendUvarint(out, uint64(len(content)))
	return append(out, content...)
}

func appendUvarint(b []byte, v uint64) []byte {
	var tmp [binary.MaxVarintLen64]byte
	n := binary.PutUvarint(tmp[:], v)
	return append(b, tmp[:n]...)
}

// appendSLEB encode `v` en LEB128 SIGNÉ standard (celui de WebAssembly).
// ⚠️ NE PAS utiliser binary.PutVarint : Go fait du zig-zag, incompatible WASM.
func appendSLEB(b []byte, v int64) []byte {
	for {
		c := byte(v & 0x7f)
		v >>= 7 // décalage arithmétique (préserve le signe)
		if (v == 0 && c&0x40 == 0) || (v == -1 && c&0x40 != 0) {
			return append(b, c)
		}
		b = append(b, c|0x80)
	}
}

// gasGlobalEntry : un global i64 mutable initialisé à `limit` (i64.const limit ; end).
func gasGlobalEntry(limit int64) []byte {
	e := []byte{0x7e, 0x01, 0x42} // i64, mutable, i64.const
	e = appendSLEB(e, limit)
	e = append(e, 0x0b) // end
	return e
}

// appendGasGlobal : ajoute notre global à une section global existante.
func appendGasGlobal(content []byte, imported int, limit int64) ([]byte, error) {
	cnt, n, err := readUvarint(content, 0)
	if err != nil {
		return nil, err
	}
	rest := content[n:]
	out := appendUvarint(nil, cnt+1) // un global de plus
	out = append(out, rest...)
	out = append(out, gasGlobalEntry(limit)...)
	_ = imported
	return out, nil
}

// insertGlobalSection : crée une section global (id 6) avec notre seul global,
// insérée juste avant la section export (7) si présente, sinon avant code (10),
// sinon en fin — en respectant l'ordre canonique des sections WASM.
func insertGlobalSection(out []byte, imported int, limit int64) ([]byte, error) {
	sec := appendUvarint(nil, 1) // 1 global
	sec = append(sec, gasGlobalEntry(limit)...)
	globalSection := appendSection(nil, 6, sec)

	// trouver le point d'insertion : avant la 1re section d'id > 6.
	pos := 8
	insertAt := len(out)
	for pos < len(out) {
		id := out[pos]
		start := pos
		pos++
		size, n, err := readUvarint(out, pos)
		if err != nil {
			return nil, err
		}
		pos += n + int(size)
		if id > 6 {
			insertAt = start
			break
		}
	}
	res := make([]byte, 0, len(out)+len(globalSection))
	res = append(res, out[:insertAt]...)
	res = append(res, globalSection...)
	res = append(res, out[insertAt:]...)
	_ = imported
	return res, nil
}

// gasExportName : nom sous lequel on exporte le global de gas, pour relire le
// gas restant après exécution (calcul du gas consommé).
const gasExportName = "chaingo_gas"

// gasExportEntry : une entrée d'export [namelen][name][kind=global 0x03][index].
func gasExportEntry(gasGlobal uint32) []byte {
	e := appendUvarint(nil, uint64(len(gasExportName)))
	e = append(e, gasExportName...)
	e = append(e, 0x03) // kind = global
	e = appendUvarint(e, uint64(gasGlobal))
	return e
}

// appendGasExport : ajoute notre export à une section export existante.
func appendGasExport(content []byte, gasGlobal uint32) ([]byte, error) {
	cnt, n, err := readUvarint(content, 0)
	if err != nil {
		return nil, err
	}
	rest := content[n:]
	out := appendUvarint(nil, cnt+1)
	out = append(out, rest...)
	out = append(out, gasExportEntry(gasGlobal)...)
	return out, nil
}

// insertExportSection : crée une section export (id 7) avec notre seul export,
// insérée avant la 1re section d'id > 7 (ordre canonique WASM).
func insertExportSection(out []byte, gasGlobal uint32) ([]byte, error) {
	sec := appendUvarint(nil, 1)
	sec = append(sec, gasExportEntry(gasGlobal)...)
	exportSection := appendSection(nil, 7, sec)

	pos := 8
	insertAt := len(out)
	for pos < len(out) {
		id := out[pos]
		start := pos
		pos++
		size, n, err := readUvarint(out, pos)
		if err != nil {
			return nil, err
		}
		pos += n + int(size)
		if id > 7 {
			insertAt = start
			break
		}
	}
	res := make([]byte, 0, len(out)+len(exportSection))
	res = append(res, out[:insertAt]...)
	res = append(res, exportSection...)
	res = append(res, out[insertAt:]...)
	return res, nil
}

// ---- instrumentation du code ----

// instrumentCode réécrit la section code : pour chaque corps de fonction, on
// préfixe la charge de gas et on en insère une après chaque `loop`.
func instrumentCode(content []byte, gasGlobal uint32, cost int64) ([]byte, error) {
	pos := 0
	nFuncs, n, err := readUvarint(content, pos)
	if err != nil {
		return nil, err
	}
	pos += n
	out := appendUvarint(nil, nFuncs)
	for i := uint64(0); i < nFuncs; i++ {
		bodySize, n, err := readUvarint(content, pos)
		if err != nil {
			return nil, err
		}
		pos += n
		if bodySize > uint64(len(content)-pos) {
			return nil, ErrMalformed
		}
		body := content[pos : pos+int(bodySize)]
		pos += int(bodySize)

		newBody, err := instrumentBody(body, gasGlobal, cost)
		if err != nil {
			return nil, err
		}
		out = appendUvarint(out, uint64(len(newBody)))
		out = append(out, newBody...)
	}
	return out, nil
}

// charge : la séquence d'opcodes qui débite `cost` du gas et piège si < 0.
//   global.get G ; i64.const cost ; i64.sub ; global.set G ;
//   global.get G ; i64.const 0 ; i64.lt_s ; if void ; unreachable ; end
func charge(gasGlobal uint32, cost int64) []byte {
	b := []byte{0x23}            // global.get
	b = appendUvarint(b, uint64(gasGlobal))
	b = append(b, 0x42)          // i64.const
	b = appendSLEB(b, cost)
	b = append(b, 0x7d)          // i64.sub
	b = append(b, 0x24)          // global.set
	b = appendUvarint(b, uint64(gasGlobal))
	b = append(b, 0x23)          // global.get
	b = appendUvarint(b, uint64(gasGlobal))
	b = append(b, 0x42, 0x00)    // i64.const 0
	b = append(b, 0x53)          // i64.lt_s
	b = append(b, 0x04, 0x40)    // if (void)
	b = append(b, 0x00)          // unreachable
	b = append(b, 0x0b)          // end
	return b
}

// instrumentBody : préfixe la charge au corps (après les déclarations de
// locals) et insère une charge après chaque `loop`.
func instrumentBody(body []byte, gasGlobal uint32, cost int64) ([]byte, error) {
	// 1) sauter les déclarations de locals : vec de (count uvarint, valtype byte).
	pos := 0
	nLocals, n, err := readUvarint(body, pos)
	if err != nil {
		return nil, err
	}
	pos += n
	for i := uint64(0); i < nLocals; i++ {
		_, n, err := readUvarint(body, pos)
		if err != nil {
			return nil, err
		}
		pos += n
		if pos >= len(body) { // place pour l'octet valtype
			return nil, ErrMalformed
		}
		pos++ // valtype
	}
	localsHeader := body[:pos]
	code := body[pos:]

	ch := charge(gasGlobal, cost)
	out := make([]byte, 0, len(body)+len(ch)*2)
	out = append(out, localsHeader...)
	out = append(out, ch...) // charge d'entrée de fonction

	// 2) parcourir les instructions ; après chaque `loop`, insérer une charge.
	ip := 0
	for ip < len(code) {
		op := code[ip]
		start := ip
		ip++
		// sauter l'immédiat de l'opcode
		nip, err := skipImmediate(code, ip, op)
		if err != nil {
			return nil, err
		}
		out = append(out, code[start:nip]...)
		if op == 0x03 { // loop : charge en tête de boucle
			out = append(out, ch...)
		}
		ip = nip
	}
	return out, nil
}

// needBytes : avance de `k` octets en vérifiant les bornes (anti-panique sur
// bytecode tronqué).
func needBytes(code []byte, pos, k int) (int, error) {
	if pos+k > len(code) {
		return 0, ErrMalformed
	}
	return pos + k, nil
}

// skipImmediate avance `pos` au-delà de l'immédiat de l'opcode `op`. Rejette
// tout opcode hors du sous-ensemble MVP supporté (sûreté : pas de devinette).
func skipImmediate(code []byte, pos int, op byte) (int, error) {
	switch {
	// pas d'immédiat
	case op == 0x00 || op == 0x01 || op == 0x05 || op == 0x0b || op == 0x0f ||
		op == 0x1a || op == 0x1b || op == 0xd1 ||
		(op >= 0x45 && op <= 0xc4): // comparaisons, arithmétique, conversions, sign-ext
		return pos, nil
	// blocktype (sleb) : block, loop, if
	case op == 0x02 || op == 0x03 || op == 0x04:
		_, n, err := readVarint(code, pos)
		if err != nil {
			return 0, err
		}
		return pos + n, nil
	// un uleb : br, br_if, call, local/global.get/set/tee, table.get/set, ref.func
	case op == 0x0c || op == 0x0d || op == 0x10 ||
		(op >= 0x20 && op <= 0x26) || op == 0xd2:
		_, n, err := readUvarint(code, pos)
		if err != nil {
			return 0, err
		}
		return pos + n, nil
	// br_table : vec uleb + default uleb
	case op == 0x0e:
		cnt, n, err := readUvarint(code, pos)
		if err != nil {
			return 0, err
		}
		pos += n
		for i := uint64(0); i <= cnt; i++ { // cnt labels + 1 default
			_, n, err := readUvarint(code, pos)
			if err != nil {
				return 0, err
			}
			pos += n
		}
		return pos, nil
	// call_indirect : typeidx uleb + tableidx uleb
	case op == 0x11:
		for i := 0; i < 2; i++ {
			_, n, err := readUvarint(code, pos)
			if err != nil {
				return 0, err
			}
			pos += n
		}
		return pos, nil
	// memarg (align + offset) : tous les load/store
	case op >= 0x28 && op <= 0x3e:
		for i := 0; i < 2; i++ {
			_, n, err := readUvarint(code, pos)
			if err != nil {
				return 0, err
			}
			pos += n
		}
		return pos, nil
	// memory.size / memory.grow : 1 octet réservé
	case op == 0x3f || op == 0x40:
		return needBytes(code, pos, 1)
	// i32.const / i64.const : sleb
	case op == 0x41 || op == 0x42:
		_, n, err := readVarint(code, pos)
		if err != nil {
			return 0, err
		}
		return pos + n, nil
	// f32.const : 4 octets ; f64.const : 8 octets
	case op == 0x43:
		return needBytes(code, pos, 4)
	case op == 0x44:
		return needBytes(code, pos, 8)
	// ref.null : 1 octet reftype
	case op == 0xd0:
		return needBytes(code, pos, 1)
	// select typé (select t*) : vec de valtypes
	case op == 0x1c:
		cnt, n, err := readUvarint(code, pos)
		if err != nil {
			return 0, err
		}
		return needBytes(code, pos+n, int(cnt)) // cnt octets de valtype
	// préfixe 0xfc : trunc saturante + bulk-memory/table (émis par Rust/LLVM)
	case op == 0xfc:
		sub, n, err := readUvarint(code, pos)
		if err != nil {
			return 0, err
		}
		pos += n
		switch sub {
		case 0, 1, 2, 3, 4, 5, 6, 7: // i32/i64.trunc_sat_f* : pas d'immédiat
			return pos, nil
		case 8: // memory.init : dataidx uleb + 1 octet réservé
			_, n, err := readUvarint(code, pos)
			if err != nil {
				return 0, err
			}
			return needBytes(code, pos+n, 1)
		case 9, 13, 15, 16, 17: // data.drop / elem.drop / table.grow|size|fill : 1 uleb
			_, n, err := readUvarint(code, pos)
			if err != nil {
				return 0, err
			}
			return pos + n, nil
		case 10: // memory.copy : 2 octets réservés
			return needBytes(code, pos, 2)
		case 11: // memory.fill : 1 octet réservé
			return needBytes(code, pos, 1)
		case 12, 14: // table.init / table.copy : 2 uleb
			for i := 0; i < 2; i++ {
				_, n, err := readUvarint(code, pos)
				if err != nil {
					return 0, err
				}
				pos += n
			}
			return pos, nil
		default:
			return 0, fmt.Errorf("%w: 0xfc %d", ErrUnsupportedOpcode, sub)
		}
	default:
		return 0, fmt.Errorf("%w: 0x%02x", ErrUnsupportedOpcode, op)
	}
}
