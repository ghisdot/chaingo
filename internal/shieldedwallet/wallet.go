// Package shielded fournit la logique CÔTÉ WALLET du pool blindé (étage 5) :
// dérivation de la clé de nullifier, construction des notes (engagements), et
// surtout construction du TÉMOIN de dépense (SpendWitness) + du chemin de Merkle
// EXACTEMENT aligné sur l'arbre que la machine d'état recalcule (internal/state,
// poolRoot). C'est ce qui permet à une preuve produite par le wallet de vérifier
// SpendPublic.MerkleRoot == pool.Root côté consensus.
//
// HORS-CONSENSUS : rien ici n'entre dans la racine d'état ni dans le codec des
// blocs. Ce paquet ne fait que PRÉPARER des transactions blindées (lentes à
// prouver) côté client. La vérification, elle, vit dans internal/state.
//
// DÉTERMINISME : la construction de l'arbre reproduit le padding de poolRoot
// (feuilles complétées à EXACTEMENT 2^SpendDepth() par le digest NUL). Aucun
// time/rand dans le chemin de construction de l'arbre : l'aléa des notes (rho,
// nk) est fourni par l'appelant (dérivé de la seed du wallet ou tiré une fois).
//
// NB : paquet DISTINCT de internal/shielded (l'ancien prototype transparent,
// hors-consensus, Phase 3). Ici on s'appuie sur le VRAI circuit zk-STARK
// (internal/stark) câblé en consensus à l'étage 5.
package shieldedwallet

import (
	"crypto/rand"
	"errors"
	"fmt"

	"golang.org/x/crypto/sha3"

	"chaingo/internal/stark"
)

// digestLen : nombre de Felt d'un digest Poseidon (= 4). Aligné sur le circuit.
const digestLen = 4

// commitmentBytes : taille d'un engagement [4]Felt sérialisé (4 × 8 octets BE).
// MÊME format que digestToBytes/cmToDigest dans internal/state.
const commitmentBytes = digestLen * 8

// Note est une NOTE blindée détenue par le wallet : un montant CACHÉ (Value) lié
// à un propriétaire (Nk, la clé de nullifier) via un aléa (Rho). Son engagement
// public (le seul élément qui entre dans l'arbre du pool) est Commitment().
//
// Le détenteur de Nk EST le propriétaire : lui seul peut produire le nullifier
// (SpendNullifier(Nk, cm)) qui autorise la dépense — sans jamais révéler Value.
type Note struct {
	Value uint64                // montant en ucgo (caché on-chain)
	Nk    [digestLen]stark.Felt // clé de nullifier (secret du propriétaire)
	Rho   [digestLen]stark.Felt // aléa du commitment (unicité)
}

// OwnerTag dérive le tag de propriétaire de la note (= SpendOwnerTag(Nk)).
// C'est l'« adresse blindée » : public, mais ne révèle pas Nk.
func (n Note) OwnerTag() [digestLen]stark.Felt {
	return stark.SpendOwnerTag(n.Nk)
}

// Commitment renvoie l'engagement Poseidon de la note (le digest qui figure comme
// feuille dans l'arbre du pool). Pur, déterministe.
func (n Note) Commitment() [digestLen]stark.Felt {
	return stark.SpendCommit(stark.FromUint64(n.Value), n.OwnerTag(), n.Rho)
}

// CommitmentBytes sérialise l'engagement en 32 octets big-endian — le format exact
// d'un Transaction.ShieldCommitment / d'une feuille de pool.Commitments.
func (n Note) CommitmentBytes() []byte {
	return digestBytes(n.Commitment())
}

// NewRandomNote crée une note de montant `value` pour le propriétaire `nk`, avec
// un aléa `rho` tiré aléatoirement (CSPRNG). À utiliser quand le wallet n'a pas de
// schéma de dérivation déterministe imposé. L'aléa garantit l'unicité du
// commitment (et donc du nullifier futur).
func NewRandomNote(value uint64, nk [digestLen]stark.Felt) (Note, error) {
	rho, err := randomDigest()
	if err != nil {
		return Note{}, err
	}
	return Note{Value: value, Nk: nk, Rho: rho}, nil
}

// DeriveNk dérive une clé de nullifier DÉTERMINISTE à partir d'un secret de wallet
// (typiquement la seed) et d'un index de note, via SHAKE-256 (domaine séparé).
// Permet à un wallet de regénérer ses Nk sans état persistant supplémentaire.
func DeriveNk(secret []byte, index uint64) [digestLen]stark.Felt {
	return deriveDigest("chaingo/shielded/nk", secret, index)
}

// DeriveRho dérive un aléa de note DÉTERMINISTE (même schéma que DeriveNk, domaine
// distinct) — pour un wallet qui veut des notes reproductibles depuis sa seed.
func DeriveRho(secret []byte, index uint64) [digestLen]stark.Felt {
	return deriveDigest("chaingo/shielded/rho", secret, index)
}

// SpendPlan décrit une dépense 1-entrée → 1-sortie à prouver :
//   - In        : la note dépensée (doit figurer dans Commitments) ;
//   - Out       : la note créée (sa valeur + propriétaire + aléa) ;
//   - Fee       : montant public (brûlé en transfer, rendu en unshield).
//
// Invariant de conservation exigé par le circuit : In.Value == Out.Value + Fee.
type SpendPlan struct {
	In  Note
	Out Note
	Fee uint64
}

// SpendPlanN décrit une dépense M-entrées / N-sorties : plusieurs notes dépensées
// (Ins) vers plusieurs notes créées (Outs), plus un montant public (Fee).
// Conservation exigée par le circuit : Σ Ins.Value == Σ Outs.Value + Fee.
type SpendPlanN struct {
	Ins  []Note
	Outs []Note
	Fee  uint64
}

// BuildWitness construit le témoin du cas 1-entrée/1-sortie (cas particulier M=1,
// N=1 du format canonique). Délègue à BuildWitnessMulti.
func BuildWitness(commitments [][]byte, plan SpendPlan) (stark.SpendNWitness, stark.Felt, error) {
	return BuildWitnessMulti(commitments, SpendPlanN{
		Ins:  []Note{plan.In},
		Outs: []Note{plan.Out},
		Fee:  plan.Fee,
	})
}

// BuildWitnessMulti construit le TÉMOIN de dépense M-entrées/N-sorties (SpendNWitness)
// et les frais (Felt) à passer à stark.ProveSpendN, à partir :
//   - de la liste ORDONNÉE des engagements actuels du pool (commitments, format
//     32 octets — exactement state.ShieldedPool.Commitments) ;
//   - du plan de dépense (notes d'entrée, notes de sortie, frais).
//
// L'arbre est RECONSTRUIT EXACTEMENT comme la machine d'état (poolRoot) : feuilles
// = engagements réels puis digests NULS jusqu'à 2^SpendDepth(). Chaque entrée
// obtient son chemin d'appartenance dans CE même arbre ⇒ toutes les preuves portent
// sur pool.Root et vérifient côté consensus.
//
// Erreurs (jamais de panique) : aucune entrée/sortie, note d'entrée introuvable,
// pool plein, conservation rompue (Σ in != Σ out + fee).
func BuildWitnessMulti(commitments [][]byte, plan SpendPlanN) (stark.SpendNWitness, stark.Felt, error) {
	var zero stark.SpendNWitness
	if len(plan.Ins) == 0 || len(plan.Outs) == 0 {
		return zero, stark.Zero(), errors.New("au moins une entrée et une sortie requises")
	}
	var sumIn, sumOut uint64
	for _, n := range plan.Ins {
		sumIn += n.Value
	}
	for _, n := range plan.Outs {
		sumOut += n.Value
	}
	if sumIn != sumOut+plan.Fee {
		return zero, stark.Zero(), fmt.Errorf("conservation rompue : Σin=%d != Σout=%d + fee=%d", sumIn, sumOut, plan.Fee)
	}

	depth := stark.SpendDepth()
	full := 1 << uint(depth)
	if len(commitments) > full {
		return zero, stark.Zero(), fmt.Errorf("pool plein : %d notes > capacité %d", len(commitments), full)
	}

	// Arbre du pool (padding au digest NUL — MÊME que poolRoot).
	leaves := make([][digestLen]stark.Felt, full)
	for i, cm := range commitments {
		d, err := bytesToDigest(cm)
		if err != nil {
			return zero, stark.Zero(), fmt.Errorf("engagement %d: %w", i, err)
		}
		leaves[i] = d
	}
	_, tree := stark.PoseidonCommit(leaves)

	// Une entrée par note dépensée : localisation + chemin d'appartenance.
	ins := make([]stark.SpendNIn, len(plan.Ins))
	for k, note := range plan.Ins {
		inCmBytes := digestBytes(note.Commitment())
		index := -1
		for i, cm := range commitments {
			if bytesEqual(cm, inCmBytes) {
				index = i
				break
			}
		}
		if index < 0 {
			return zero, stark.Zero(), fmt.Errorf("entrée %d absente du pool (engagement introuvable)", k)
		}
		sibs := stark.PoseidonOpen(tree, index)
		if len(sibs) != depth {
			return zero, stark.Zero(), fmt.Errorf("profondeur d'ouverture %d != %d", len(sibs), depth)
		}
		var path stark.SpendPath
		idx := index
		for i := 0; i < depth; i++ {
			path.Siblings[i] = sibs[i]
			if idx&1 == 0 {
				path.Bits[i] = stark.Zero()
			} else {
				path.Bits[i] = stark.One()
			}
			idx >>= 1
		}
		ins[k] = stark.SpendNIn{
			Value: stark.FromUint64(note.Value),
			Rho:   note.Rho,
			Nk:    note.Nk,
			Path:  path,
		}
	}

	outs := make([]stark.SpendNOut, len(plan.Outs))
	for k, note := range plan.Outs {
		outs[k] = stark.SpendNOut{
			Value:    stark.FromUint64(note.Value),
			OwnerTag: note.OwnerTag(),
			Rho:      note.Rho,
		}
	}

	return stark.SpendNWitness{Ins: ins, Outs: outs}, stark.FromUint64(plan.Fee), nil
}

// ---- helpers de (dé)sérialisation digest <-> octets ----

// digestBytes sérialise un digest [4]Felt en 32 octets big-endian (= digestToBytes
// dans internal/state). Format d'un engagement / nullifier on-chain.
func digestBytes(d [digestLen]stark.Felt) []byte {
	out := make([]byte, 0, commitmentBytes)
	for k := 0; k < digestLen; k++ {
		out = append(out, d[k].Bytes()...)
	}
	return out
}

// bytesToDigest décode 32 octets (4 Felt big-endian) en digest [4]Felt. Erreur si
// la taille est mauvaise (refus propre, jamais de panique).
func bytesToDigest(b []byte) ([digestLen]stark.Felt, error) {
	var d [digestLen]stark.Felt
	if len(b) != commitmentBytes {
		return d, fmt.Errorf("digest de taille %d, attendu %d", len(b), commitmentBytes)
	}
	for k := 0; k < digestLen; k++ {
		d[k] = stark.FeltFromBytes(b[k*8 : k*8+8])
	}
	return d, nil
}

func bytesEqual(a, b []byte) bool {
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

// randomDigest tire un digest [4]Felt aléatoire (CSPRNG), chaque Felt réduit dans
// le corps. Sert d'aléa de note quand aucune dérivation déterministe n'est imposée.
func randomDigest() ([digestLen]stark.Felt, error) {
	var d [digestLen]stark.Felt
	var buf [8]byte
	for k := 0; k < digestLen; k++ {
		if _, err := rand.Read(buf[:]); err != nil {
			return d, err
		}
		d[k] = stark.FeltFromBytes(buf[:])
	}
	return d, nil
}

// deriveDigest : SHAKE-256(domaine || secret || index) -> [4]Felt. Domaine séparé
// pour éviter toute corrélation entre nk et rho dérivés du même secret.
func deriveDigest(domain string, secret []byte, index uint64) [digestLen]stark.Felt {
	h := sha3.NewShake256()
	h.Write([]byte(domain))
	h.Write(secret)
	var ib [8]byte
	for i := 0; i < 8; i++ {
		ib[i] = byte(index >> (8 * (7 - i)))
	}
	h.Write(ib[:])
	var d [digestLen]stark.Felt
	var buf [8]byte
	for k := 0; k < digestLen; k++ {
		h.Read(buf[:])
		d[k] = stark.FeltFromBytes(buf[:])
	}
	return d
}
