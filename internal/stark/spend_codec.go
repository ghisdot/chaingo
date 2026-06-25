// STARK MAISON — EXPÉRIMENTAL, NON AUDITÉ, hors-consensus.
//
// ÉTAGE 5.1 — Sérialisation binaire de la preuve blindée (transport en tx).
//
// Ce fichier donne un encodage DÉTERMINISTE, longueur-préfixé et BORNÉ de
// l'énoncé public (SpendPublic) et de la preuve (AirProof, qui embarque FriProof
// et les ouvertures AirOpening/QueryStep). C'est l'étage qui rend la preuve
// TRANSPORTABLE : une tx blindée portera ces octets après sa signature (extension
// optionnelle du codec de tx, comme WASM — cf. tx_codec.go).
//
// ---------------------------------------------------------------------------
// Principes de conception
// ---------------------------------------------------------------------------
//
//   - DÉTERMINISME ABSOLU : un même AirProof produit TOUJOURS les mêmes octets
//     (parcours en ordre fixe des champs ; aucune map, aucun time/rand). C'est
//     l'exigence n°1 d'un format de consensus.
//   - Un Felt = 8 octets BIG-ENDIAN (Felt.Bytes / FeltFromBytes : représentation
//     canonique du corps). Un [32]byte (racine/chemin Merkle) = 32 octets bruts.
//   - LONGUEUR-PRÉFIXÉ : chaque slice de taille variable est précédé de sa
//     longueur (uint32 big-endian). Le décodeur sait exactement combien lire ;
//     aucune ambiguïté, aucune dépendance à la taille totale du buffer.
//   - BORNÉ (anti-DoS) : toute longueur lue est comparée à une borne MAXIMALE
//     dérivée des paramètres FIGÉS du circuit (spStark*). Une longueur aberrante
//     (corrompue ou malveillante) est REFUSÉE immédiatement, AVANT toute
//     allocation proportionnelle — on n'alloue jamais un slice géant sur la foi
//     d'un préfixe non vérifié.
//   - ROBUSTESSE : le décodeur ne PANIQUE JAMAIS sur une entrée tronquée ou
//     aberrante. Tout dépassement de tampon, toute longueur hors-borne, tout
//     octet résiduel => erreur Go propre.
//
// Le format n'est PAS auto-décrivant (pas de tags de type) : il est implicitement
// versionné par le schéma du circuit (W = spNumCols colonnes, mcBlowup, etc.). Un
// changement de circuit changerait les bornes et donc l'acceptation — cohérent
// avec le fait que la preuve n'a de sens que pour CE circuit.
package stark

import (
	"encoding/binary"
	"errors"
	"fmt"
)

// ---------------------------------------------------------------------------
// Bornes anti-DoS (dérivées des paramètres FIGÉS du circuit de dépense)
// ---------------------------------------------------------------------------
//
// Toutes les tailles de slice d'une preuve HONNÊTE de dépense sont entièrement
// déterminées par les constantes du moteur (mcBlowup, mcNumQueries) et du circuit
// (spNumCols, spSteps, spMaxDegree). On en déduit des bornes SUPÉRIEURES larges
// mais finies : tout préfixe de longueur au-delà est rejeté sans allocation.
//
// On ne calcule pas les valeurs exactes (le décodeur n'a pas à reconstruire la
// géométrie complète) : des bornes confortables suffisent à fermer la porte au
// DoS tout en acceptant toute preuve bien formée.

const (
	// scMaxColumns borne le nombre de colonnes (ColRoots, OodColZ, ColVals…). Le
	// circuit de dépense a spNumCols colonnes ; on tolère une marge.
	scMaxColumns = spNumCols + 16 // ~77

	// scMaxFriLayers borne le nombre de couches FRI (LayerRoots / QueryStep par
	// requête). On plie de bigN jusqu'à mcBlowup : log2(bigN) couches, soit au plus
	// TwoAdicity() (32). Une marge généreuse couvre tout domaine légitime.
	scMaxFriLayers = 64

	// scMaxQueries borne le nombre de requêtes (Openings / FriProof.Queries). Fixé
	// à mcNumQueries par le moteur ; marge incluse.
	scMaxQueries = mcNumQueries + 32 // 64

	// scMaxMerklePath borne la longueur d'un chemin de Merkle (en hachages [32]byte).
	// Un chemin a log2(domaine) niveaux ; même borne que les couches FRI.
	scMaxMerklePath = 64

	// scMaxFinalCoeffs borne le nombre de coefficients de la couche finale FRI
	// (= mcBlowup pour une preuve honnête). Marge incluse.
	scMaxFinalCoeffs = 256

	// scMaxFelts borne tout slice générique de Felt sans sémantique géométrique
	// (utilisé comme garde-fou ultime ; aucune preuve honnête n'approche ce seuil).
	scMaxFelts = 1 << 16 // 65536

	// scMaxTotalBytes borne la taille TOTALE acceptée d'une preuve sérialisée
	// (garde-fou d'entrée : on refuse de décoder un buffer manifestement aberrant
	// avant même de lire le moindre préfixe). Une preuve de dépense honnête pèse de
	// l'ordre de quelques Mo ; on prévoit large.
	scMaxTotalBytes = 64 << 20 // 64 Mo
)

// Erreurs sentinelles du codec (permettent à l'appelant de discriminer).
var (
	// errSCShort indique un tampon trop court (lecture au-delà de la fin).
	errSCShort = errors.New("stark: spend_codec: données tronquées")
	// errSCBound indique une longueur préfixée hors borne anti-DoS.
	errSCBound = errors.New("stark: spend_codec: longueur hors borne (anti-DoS)")
	// errSCTrailing indique des octets résiduels après décodage complet.
	errSCTrailing = errors.New("stark: spend_codec: octets résiduels en fin de flux")
)

// ---------------------------------------------------------------------------
// Écriture (déterministe)
// ---------------------------------------------------------------------------

// scWriter accumule les octets de sérialisation. Pas d'état caché : l'ordre des
// appels EST le format.
type scWriter struct {
	buf []byte
}

// u32 écrit un entier non signé sur 4 octets big-endian (longueurs, compteurs).
func (w *scWriter) u32(n int) {
	var b [4]byte
	binary.BigEndian.PutUint32(b[:], uint32(n))
	w.buf = append(w.buf, b[:]...)
}

// u64 écrit un entier non signé sur 8 octets big-endian (nonce de grinding).
func (w *scWriter) u64(n uint64) {
	var b [8]byte
	binary.BigEndian.PutUint64(b[:], n)
	w.buf = append(w.buf, b[:]...)
}

// felt écrit un Felt sur 8 octets big-endian (représentation canonique).
func (w *scWriter) felt(f Felt) {
	w.buf = append(w.buf, f.Bytes()...) // 8 octets BE
}

// hash écrit un digest Merkle [32]byte brut (32 octets).
func (w *scWriter) hash(h [32]byte) {
	w.buf = append(w.buf, h[:]...)
}

// felts écrit un slice de Felt : longueur (u32) puis les Felt en ordre.
func (w *scWriter) felts(fs []Felt) {
	w.u32(len(fs))
	for _, f := range fs {
		w.felt(f)
	}
}

// hashes écrit un slice de [32]byte : longueur (u32) puis les digests en ordre.
func (w *scWriter) hashes(hs [][32]byte) {
	w.u32(len(hs))
	for _, h := range hs {
		w.hash(h)
	}
}

// ---------------------------------------------------------------------------
// Lecture (bornée, sans panique)
// ---------------------------------------------------------------------------

// scReader consomme un tampon octet par octet. Toute lecture au-delà de la fin
// renvoie une erreur (jamais de panique). Le champ pos avance de façon monotone.
type scReader struct {
	buf []byte
	pos int
}

// remaining renvoie le nombre d'octets non encore consommés.
func (r *scReader) remaining() int { return len(r.buf) - r.pos }

// take consomme exactement n octets et renvoie la tranche correspondante, ou une
// erreur si le tampon est trop court. n est supposé >= 0 (appelants internes).
func (r *scReader) take(n int) ([]byte, error) {
	if n < 0 || n > r.remaining() {
		return nil, errSCShort
	}
	s := r.buf[r.pos : r.pos+n]
	r.pos += n
	return s, nil
}

// u32 lit un entier non signé 4 octets big-endian.
func (r *scReader) u32() (int, error) {
	s, err := r.take(4)
	if err != nil {
		return 0, err
	}
	return int(binary.BigEndian.Uint32(s)), nil
}

// u64 lit un entier non signé 8 octets big-endian (nonce de grinding).
func (r *scReader) u64() (uint64, error) {
	s, err := r.take(8)
	if err != nil {
		return 0, err
	}
	return binary.BigEndian.Uint64(s), nil
}

// boundedLen lit une longueur préfixée (u32) et la valide contre une borne max
// AVANT toute allocation : c'est le rempart anti-DoS. Renvoie la longueur si elle
// est dans [0, max], sinon errSCBound.
func (r *scReader) boundedLen(max int) (int, error) {
	n, err := r.u32()
	if err != nil {
		return 0, err
	}
	if n < 0 || n > max {
		return 0, fmt.Errorf("%w: %d > %d", errSCBound, n, max)
	}
	return n, nil
}

// felt lit un Felt (8 octets big-endian, réduit dans [0,P) par FeltFromBytes).
func (r *scReader) felt() (Felt, error) {
	s, err := r.take(8)
	if err != nil {
		return Zero(), err
	}
	return FeltFromBytes(s), nil
}

// hash lit un digest Merkle [32]byte.
func (r *scReader) hash() ([32]byte, error) {
	var h [32]byte
	s, err := r.take(32)
	if err != nil {
		return h, err
	}
	copy(h[:], s)
	return h, nil
}

// felts lit un slice de Felt longueur-préfixé, borné par max.
func (r *scReader) felts(max int) ([]Felt, error) {
	n, err := r.boundedLen(max)
	if err != nil {
		return nil, err
	}
	// Garde-fou avant allocation : n est déjà <= max (borne finie raisonnable),
	// donc l'allocation est sûre. On vérifie en plus qu'il reste assez d'octets
	// (8·n) pour éviter une grande allocation sur un flux tronqué.
	if n*8 > r.remaining() {
		return nil, errSCShort
	}
	out := make([]Felt, n)
	for i := 0; i < n; i++ {
		f, err := r.felt()
		if err != nil {
			return nil, err
		}
		out[i] = f
	}
	return out, nil
}

// hashes lit un slice de [32]byte longueur-préfixé, borné par max.
func (r *scReader) hashes(max int) ([][32]byte, error) {
	n, err := r.boundedLen(max)
	if err != nil {
		return nil, err
	}
	if n*32 > r.remaining() {
		return nil, errSCShort
	}
	out := make([][32]byte, n)
	for i := 0; i < n; i++ {
		h, err := r.hash()
		if err != nil {
			return nil, err
		}
		out[i] = h
	}
	return out, nil
}

// ---------------------------------------------------------------------------
// SpendPublic : énoncé public (MerkleRoot | Nf | OutCm | Fee)
// ---------------------------------------------------------------------------

// MarshalSpendPublic sérialise l'énoncé public d'une dépense en octets
// déterministes : les trois digests [4]Felt (MerkleRoot, Nf, OutCm) puis Fee,
// chacun en big-endian. Taille FIXE (3·4 + 1)·8 = 104 octets : on n'a pas besoin
// de préfixe de longueur (la géométrie est figée par poseidonDigestLen).
func MarshalSpendPublic(p SpendPublic) []byte {
	w := &scWriter{}
	for k := 0; k < poseidonDigestLen; k++ {
		w.felt(p.MerkleRoot[k])
	}
	for k := 0; k < poseidonDigestLen; k++ {
		w.felt(p.Nf[k])
	}
	for k := 0; k < poseidonDigestLen; k++ {
		w.felt(p.OutCm[k])
	}
	w.felt(p.Fee)
	return w.buf
}

// spendPublicBytes est la taille FIXE attendue d'un SpendPublic sérialisé.
const spendPublicBytes = (3*poseidonDigestLen + 1) * 8 // 104

// UnmarshalSpendPublic décode un énoncé public depuis exactement spendPublicBytes
// octets. Refuse proprement un tampon de mauvaise taille (trop court OU avec des
// octets résiduels). Ne panique jamais.
func UnmarshalSpendPublic(b []byte) (SpendPublic, error) {
	var p SpendPublic
	if len(b) != spendPublicBytes {
		return p, fmt.Errorf("stark: spend_codec: taille SpendPublic %d, attendu %d",
			len(b), spendPublicBytes)
	}
	r := &scReader{buf: b}
	read4 := func(dst *[poseidonDigestLen]Felt) error {
		for k := 0; k < poseidonDigestLen; k++ {
			f, err := r.felt()
			if err != nil {
				return err
			}
			dst[k] = f
		}
		return nil
	}
	if err := read4(&p.MerkleRoot); err != nil {
		return SpendPublic{}, err
	}
	if err := read4(&p.Nf); err != nil {
		return SpendPublic{}, err
	}
	if err := read4(&p.OutCm); err != nil {
		return SpendPublic{}, err
	}
	fee, err := r.felt()
	if err != nil {
		return SpendPublic{}, err
	}
	p.Fee = fee
	// Par construction (len vérifié), il ne reste rien ; contrôle défensif.
	if r.remaining() != 0 {
		return SpendPublic{}, errSCTrailing
	}
	return p, nil
}

// ---------------------------------------------------------------------------
// QueryStep (ouverture FRI : valeur + jumeau + chemins)
// ---------------------------------------------------------------------------

// scWriteQueryStep sérialise un QueryStep : Value, Sibling, puis Path et
// SiblingPath (chacun longueur-préfixé).
func scWriteQueryStep(w *scWriter, q QueryStep) {
	w.felt(q.Value)
	w.felt(q.Sibling)
	w.hashes(q.Path)
	w.hashes(q.SiblingPath)
}

// scReadQueryStep décode un QueryStep, chemins bornés par scMaxMerklePath.
func scReadQueryStep(r *scReader) (QueryStep, error) {
	var q QueryStep
	var err error
	if q.Value, err = r.felt(); err != nil {
		return q, err
	}
	if q.Sibling, err = r.felt(); err != nil {
		return q, err
	}
	if q.Path, err = r.hashes(scMaxMerklePath); err != nil {
		return q, err
	}
	if q.SiblingPath, err = r.hashes(scMaxMerklePath); err != nil {
		return q, err
	}
	return q, nil
}

// ---------------------------------------------------------------------------
// FriProof
// ---------------------------------------------------------------------------

// scWriteFri sérialise une FriProof : LogDomain, LayerRoots, FinalCoeffs, puis
// Queries (chaque requête = un slice de QueryStep, longueur-préfixé).
func scWriteFri(w *scWriter, f FriProof) {
	w.u32(int(f.LogDomain))
	w.u64(f.PowNonce) // nonce de grinding (proof-of-work Fiat-Shamir)
	w.hashes(f.LayerRoots)
	w.felts(f.FinalCoeffs)
	w.u32(len(f.Queries))
	for _, steps := range f.Queries {
		w.u32(len(steps))
		for _, st := range steps {
			scWriteQueryStep(w, st)
		}
	}
}

// scReadFri décode une FriProof. Toutes les longueurs sont bornées (anti-DoS).
func scReadFri(r *scReader) (FriProof, error) {
	var f FriProof

	logDom, err := r.u32()
	if err != nil {
		return f, err
	}
	// LogDomain doit rester dans la 2-adicité du corps (sinon RootOfUnity
	// paniquerait côté vérifieur). Borne stricte ici.
	if logDom < 0 || uint32(logDom) > TwoAdicity() {
		return f, fmt.Errorf("%w: LogDomain %d", errSCBound, logDom)
	}
	f.LogDomain = uint32(logDom)

	if f.PowNonce, err = r.u64(); err != nil {
		return f, err
	}

	if f.LayerRoots, err = r.hashes(scMaxFriLayers); err != nil {
		return f, err
	}
	if f.FinalCoeffs, err = r.felts(scMaxFinalCoeffs); err != nil {
		return f, err
	}

	nq, err := r.boundedLen(scMaxQueries)
	if err != nil {
		return f, err
	}
	f.Queries = make([][]QueryStep, nq)
	for i := 0; i < nq; i++ {
		ns, err := r.boundedLen(scMaxFriLayers)
		if err != nil {
			return f, err
		}
		steps := make([]QueryStep, ns)
		for j := 0; j < ns; j++ {
			st, err := scReadQueryStep(r)
			if err != nil {
				return f, err
			}
			steps[j] = st
		}
		f.Queries[i] = steps
	}
	return f, nil
}

// ---------------------------------------------------------------------------
// AirOpening
// ---------------------------------------------------------------------------

// scWriteOpening sérialise une AirOpening : Pos, ColVals, ColPaths (slice de
// chemins), CompVal+CompPath, DeepVal+DeepPath.
func scWriteOpening(w *scWriter, op AirOpening) {
	w.u32(op.Pos)
	w.felts(op.ColVals)
	// ColPaths : un chemin par colonne. Longueur externe préfixée, puis chaque
	// chemin (lui-même longueur-préfixé).
	w.u32(len(op.ColPaths))
	for _, path := range op.ColPaths {
		w.hashes(path)
	}
	w.felt(op.CompVal)
	w.hashes(op.CompPath)
	w.felt(op.DeepVal)
	w.hashes(op.DeepPath)
}

// scReadOpening décode une AirOpening, toutes longueurs bornées.
func scReadOpening(r *scReader) (AirOpening, error) {
	var op AirOpening

	pos, err := r.u32()
	if err != nil {
		return op, err
	}
	// Pos est un indice de domaine LDE : non négatif et borné par le domaine max
	// (2^TwoAdicity). On le vérifie au moins non-négatif ici ; la cohérence fine
	// (pos < bigN) est revérifiée par VerifyAIR.
	if pos < 0 {
		return op, fmt.Errorf("%w: Pos négatif %d", errSCBound, pos)
	}
	op.Pos = pos

	if op.ColVals, err = r.felts(scMaxColumns); err != nil {
		return op, err
	}

	ncp, err := r.boundedLen(scMaxColumns)
	if err != nil {
		return op, err
	}
	op.ColPaths = make([][][32]byte, ncp)
	for c := 0; c < ncp; c++ {
		path, err := r.hashes(scMaxMerklePath)
		if err != nil {
			return op, err
		}
		op.ColPaths[c] = path
	}

	if op.CompVal, err = r.felt(); err != nil {
		return op, err
	}
	if op.CompPath, err = r.hashes(scMaxMerklePath); err != nil {
		return op, err
	}
	if op.DeepVal, err = r.felt(); err != nil {
		return op, err
	}
	if op.DeepPath, err = r.hashes(scMaxMerklePath); err != nil {
		return op, err
	}
	return op, nil
}

// ---------------------------------------------------------------------------
// AirProof (preuve complète)
// ---------------------------------------------------------------------------

// scWriteProof sérialise une AirProof complète dans l'ordre FIGÉ :
// ColRoots, CompRoot, OodColZ, OodColGZ, OodHz, Fri, Openings.
func scWriteProof(w *scWriter, p AirProof) {
	w.hashes(p.ColRoots)
	w.hash(p.CompRoot)
	w.felts(p.OodColZ)
	w.felts(p.OodColGZ)
	w.felt(p.OodHz)
	scWriteFri(w, p.Fri)
	w.u32(len(p.Openings))
	for _, op := range p.Openings {
		scWriteOpening(w, op)
	}
}

// scReadProof décode une AirProof complète. Toutes les longueurs sont bornées.
func scReadProof(r *scReader) (AirProof, error) {
	var p AirProof
	var err error

	if p.ColRoots, err = r.hashes(scMaxColumns); err != nil {
		return p, err
	}
	if p.CompRoot, err = r.hash(); err != nil {
		return p, err
	}
	if p.OodColZ, err = r.felts(scMaxColumns); err != nil {
		return p, err
	}
	if p.OodColGZ, err = r.felts(scMaxColumns); err != nil {
		return p, err
	}
	if p.OodHz, err = r.felt(); err != nil {
		return p, err
	}
	if p.Fri, err = scReadFri(r); err != nil {
		return p, err
	}

	no, err := r.boundedLen(scMaxQueries)
	if err != nil {
		return p, err
	}
	p.Openings = make([]AirOpening, no)
	for i := 0; i < no; i++ {
		op, err := scReadOpening(r)
		if err != nil {
			return p, err
		}
		p.Openings[i] = op
	}
	return p, nil
}

// MarshalSpendProof sérialise une preuve de dépense (AirProof) en octets
// déterministes, longueur-préfixés et bornés. Le format encode TOUS les champs :
// engagements de colonnes (ColRoots), composition (CompRoot), valeurs hors-domaine
// (OodColZ/OodColGZ/OodHz), preuve FRI complète (couches, coefficients finaux,
// ouvertures par requête) et ouvertures AIR (colonnes/composition/DEEP avec leurs
// chemins de Merkle). Un même AirProof produit toujours les mêmes octets.
func MarshalSpendProof(p AirProof) []byte {
	w := &scWriter{}
	scWriteProof(w, p)
	return w.buf
}

// UnmarshalSpendProof reconstruit une AirProof depuis des octets produits par
// MarshalSpendProof. Décodage STRICT et BORNÉ :
//   - refuse un buffer manifestement aberrant (taille totale > scMaxTotalBytes) ;
//   - refuse toute longueur préfixée hors borne anti-DoS (errSCBound) ;
//   - refuse un flux tronqué (errSCShort) ;
//   - refuse des octets résiduels après la preuve (errSCTrailing).
//
// Ne panique JAMAIS sur entrée corrompue : toujours une erreur Go propre. La
// preuve décodée est ensuite vérifiable telle quelle par VerifySpend.
func UnmarshalSpendProof(b []byte) (AirProof, error) {
	if len(b) > scMaxTotalBytes {
		return AirProof{}, fmt.Errorf("%w: taille totale %d octets", errSCBound, len(b))
	}
	r := &scReader{buf: b}
	p, err := scReadProof(r)
	if err != nil {
		return AirProof{}, err
	}
	if r.remaining() != 0 {
		return AirProof{}, errSCTrailing
	}
	return p, nil
}
