// Package shielded — PROTOTYPE de pool de transactions blindées (Phase 3,
// « anonymat fort »). Cache le DESTINATAIRE (livraison de note chiffrée + scan)
// et modélise le MONTANT caché (commitments).
//
// ⚠️⚠️⚠️ EXPÉRIMENTAL · HORS-CONSENSUS · NON AUDITÉ · JAMAIS EN MAINNET ⚠️⚠️⚠️
//
//   - La couche de CHIFFREMENT des notes (ML-KEM-768, internal/crypto) est du
//     crypto standardisé → CONFIDENTIALITÉ réelle du contenu.
//   - La preuve de validité de dépense est ici un PLACEHOLDER TRANSPARENT
//     (SpendWitness/VerifyTransparent) qui RÉVÈLE les montants : il vérifie
//     l'accounting (appartenance, propriété, nullifier, conservation de valeur)
//     mais N'EST PAS zero-knowledge. Le système final le remplace par une preuve
//     zk-STARK qui prouve la même chose SANS rien révéler — c'est le cœur
//     recherche+audit, voir docs/design/phase3-privacy.md.
//
// Rien dans ce paquet n'est câblé dans la machine d'état ni la validation des
// blocs. C'est un banc de R&D pour valider l'architecture du pool blindé.
package shielded

import (
	"encoding/binary"
	"errors"

	"chaingo/internal/crypto"
)

// ShieldedKey : identité d'un participant au pool blindé, dérivée du seed du
// wallet (un seul seed pour tout). View = clé ML-KEM qui déchiffre les notes
// reçues ; NK = clé de nullifier qui autorise la dépense — JAMAIS publiée.
type ShieldedKey struct {
	View *crypto.ViewKeyPair
	NK   [32]byte
}

// KeyFromSeed dérive l'identité blindée depuis le seed du wallet.
func KeyFromSeed(seed []byte) *ShieldedKey {
	var nk [32]byte
	copy(nk[:], crypto.Hash([]byte("cg-shield-nk-v1"), seed))
	return &ShieldedKey{View: crypto.DeriveViewKey(seed), NK: nk}
}

// MetaAddress : ce qu'on publie pour recevoir des paiements blindés — la clé de
// vue publique uniquement (le NK reste secret).
func (k *ShieldedKey) MetaAddress() []byte { return k.View.ViewPubBytes() }

// Note : un montant détenu de façon confidentielle, payable au détenteur de la
// clé de vue `Owner`. Rho est l'aléa qui rend le commitment cachant.
type Note struct {
	Value uint64
	Owner []byte   // clé de vue publique du destinataire (ML-KEM)
	Rho   [32]byte // aléa de masquage
}

// Commitment : engagement CACHANT + LIANT sur la note, publié on-chain à la
// place de (montant, destinataire). SHA3-256 → sûr en post-quantique (ROM).
func (n *Note) Commitment() [32]byte {
	var v [8]byte
	binary.LittleEndian.PutUint64(v[:], n.Value)
	var out [32]byte
	copy(out[:], crypto.Hash([]byte("cg-shield-cm-v1"), v[:], crypto.Hash(n.Owner), n.Rho[:]))
	return out
}

// Encrypt : livraison chiffrée de la note à son destinataire (ML-KEM). Le réseau
// publie ce blob ; seul le destinataire l'ouvre par scan.
func (n *Note) Encrypt() ([]byte, error) { return crypto.SealTo(n.Owner, n.serialize()) }

func (n *Note) serialize() []byte {
	var v [8]byte
	binary.LittleEndian.PutUint64(v[:], n.Value)
	b := append([]byte{}, v[:]...)
	b = append(b, n.Rho[:]...)
	return append(b, n.Owner...)
}

func deserializeNote(b []byte) (*Note, error) {
	if len(b) < 40 {
		return nil, errors.New("note tronquée")
	}
	n := &Note{Value: binary.LittleEndian.Uint64(b[:8])}
	copy(n.Rho[:], b[8:40])
	n.Owner = append([]byte{}, b[40:]...)
	return n, nil
}

// Nullifier : marqueur révélé à la dépense pour empêcher le double-spend. Dérivé
// de la clé de nullifier du propriétaire : lui seul peut le calculer, et il est
// non-liable au commitment sans NK.
func Nullifier(cm, nk [32]byte) [32]byte {
	var out [32]byte
	copy(out[:], crypto.Hash([]byte("cg-shield-nf-v1"), nk[:], cm[:]))
	return out
}

// Scan : parmi des blobs de notes publiés, récupère ceux destinés à cette clé —
// sans aucun index on-chain liant la note à l'adresse (c'est ça, cacher le
// destinataire). Essaie d'ouvrir chaque blob ; ok = « pour moi ».
func (k *ShieldedKey) Scan(blobs [][]byte) []*Note {
	var mine []*Note
	for _, blob := range blobs {
		if pt, ok := k.View.OpenWith(blob); ok {
			if n, err := deserializeNote(pt); err == nil {
				mine = append(mine, n)
			}
		}
	}
	return mine
}

// Pool : état du pool blindé — commitments existants + nullifiers déjà dépensés.
// (En production, les commitments vivent dans un ARBRE de Merkle pour permettre
// une preuve d'APPARTENANCE en zero-knowledge ; ici un set suffit au prototype.)
type Pool struct {
	commitments map[[32]byte]bool
	nullifiers  map[[32]byte]bool
}

func NewPool() *Pool {
	return &Pool{commitments: map[[32]byte]bool{}, nullifiers: map[[32]byte]bool{}}
}

// Mint ajoute un commitment au pool (entrée de fonds « shield » depuis le solde
// public, dans le système complet). Renvoie le commitment créé.
func (p *Pool) Mint(n *Note) [32]byte {
	cm := n.Commitment()
	p.commitments[cm] = true
	return cm
}

func (p *Pool) HasCommitment(cm [32]byte) bool { return p.commitments[cm] }
func (p *Pool) NullifierSpent(nf [32]byte) bool { return p.nullifiers[nf] }

// SpendWitness : ⚠️ TÉMOIN TRANSPARENT (PAS zero-knowledge). Révèle les notes en
// clair pour que VerifyTransparent vérifie l'accounting. Remplacé par une preuve
// zk-STARK dans le système final (mêmes contraintes, zéro révélation).
type SpendWitness struct {
	NK      [32]byte // clé de nullifier du dépensier (révélée ici ; masquée par le ZK)
	Inputs  []*Note  // notes dépensées (doivent exister dans le pool)
	Outputs []*Note  // notes créées
	Fee     uint64   // frais publics (en clair même dans le système final)
}

// VerifyTransparent applique un témoin : vérifie que chaque entrée existe et
// n'est pas dépensée, que la valeur est conservée (Σentrées = Σsorties + frais),
// puis marque les nullifiers et ajoute les nouveaux commitments. Atomique : en
// cas d'erreur, le pool n'est pas modifié. NON-PRIVÉ (placeholder du circuit ZK).
func (p *Pool) VerifyTransparent(w *SpendWitness) (newCommitments [][32]byte, err error) {
	var inSum, outSum uint64
	seenNf := map[[32]byte]bool{}
	for _, in := range w.Inputs {
		cm := in.Commitment()
		if !p.commitments[cm] {
			return nil, errors.New("entrée inconnue du pool")
		}
		nf := Nullifier(cm, w.NK)
		if p.nullifiers[nf] || seenNf[nf] {
			return nil, errors.New("double-spend : nullifier déjà dépensé")
		}
		seenNf[nf] = true
		inSum += in.Value
	}
	for _, out := range w.Outputs {
		outSum += out.Value
	}
	if inSum != outSum+w.Fee {
		return nil, errors.New("valeur non conservée : Σentrées ≠ Σsorties + frais")
	}
	// Tout est valide → on applique (aucune mutation avant ce point).
	for nf := range seenNf {
		p.nullifiers[nf] = true
	}
	for _, out := range w.Outputs {
		cm := out.Commitment()
		p.commitments[cm] = true
		newCommitments = append(newCommitments, cm)
	}
	return newCommitments, nil
}
