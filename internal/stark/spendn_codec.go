// STARK MAISON — EXPÉRIMENTAL, NON AUDITÉ, hors-consensus.
//
// Codec de l'ÉNONCÉ PUBLIC des dépenses blindées M-entrées / N-sorties
// (SpendNPublic). La PREUVE (AirProof) utilise le codec générique déjà existant
// (MarshalSpendProof / UnmarshalSpendProof) : seul l'énoncé public, de taille
// VARIABLE (M nullifiers, N engagements), nécessite son propre format.
//
// Format déterministe (l'ordre des écritures EST le format) :
//
//	u32  numIn (M)              -- borné anti-DoS
//	u32  numOut (N)             -- borné anti-DoS
//	[4]  MerkleRoot             -- 4 Felt big-endian
//	M×[4] Nf_i                  -- un nullifier par entrée
//	N×[4] OutCm_j               -- un engagement par sortie
//	[1]  Fee
//
// Lecture bornée et sans panique (mêmes garanties que spend_codec.go).
package stark

import "fmt"

const (
	// scMaxShieldInputs / scMaxShieldOutputs bornent M et N au décodage (anti-DoS).
	// Largement au-dessus de tout usage réaliste d'un join-split ; au-delà, la
	// preuve elle-même serait gigantesque (M·7+N blocs Poseidon).
	scMaxShieldInputs  = 64
	scMaxShieldOutputs = 64
)

// MarshalSpendNPublic sérialise l'énoncé public M-entrées / N-sorties en octets
// déterministes. Taille variable : (2 + 4·(1+M+N) + 1)·... cf. format ci-dessus.
func MarshalSpendNPublic(p SpendNPublic) []byte {
	w := &scWriter{}
	w.u32(len(p.Nfs))
	w.u32(len(p.OutCms))
	for k := 0; k < poseidonDigestLen; k++ {
		w.felt(p.MerkleRoot[k])
	}
	for i := range p.Nfs {
		for k := 0; k < poseidonDigestLen; k++ {
			w.felt(p.Nfs[i][k])
		}
	}
	for j := range p.OutCms {
		for k := 0; k < poseidonDigestLen; k++ {
			w.felt(p.OutCms[j][k])
		}
	}
	w.felt(p.Fee)
	return w.buf
}

// UnmarshalSpendNPublic décode un énoncé public M-entrées / N-sorties. Refuse
// proprement (jamais de panique) : M/N hors borne, tampon tronqué, octets
// résiduels. M >= 1 et N >= 1 exigés (un join-split a au moins une entrée et une
// sortie).
func UnmarshalSpendNPublic(b []byte) (SpendNPublic, error) {
	var p SpendNPublic
	r := &scReader{buf: b}

	numIn, err := r.u32()
	if err != nil {
		return p, err
	}
	numOut, err := r.u32()
	if err != nil {
		return p, err
	}
	if numIn < 1 || numIn > scMaxShieldInputs {
		return p, fmt.Errorf("%w: numIn %d", errSCBound, numIn)
	}
	if numOut < 1 || numOut > scMaxShieldOutputs {
		return p, fmt.Errorf("%w: numOut %d", errSCBound, numOut)
	}

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
		return SpendNPublic{}, err
	}
	p.Nfs = make([][poseidonDigestLen]Felt, numIn)
	for i := 0; i < numIn; i++ {
		if err := read4(&p.Nfs[i]); err != nil {
			return SpendNPublic{}, err
		}
	}
	p.OutCms = make([][poseidonDigestLen]Felt, numOut)
	for j := 0; j < numOut; j++ {
		if err := read4(&p.OutCms[j]); err != nil {
			return SpendNPublic{}, err
		}
	}
	fee, err := r.felt()
	if err != nil {
		return SpendNPublic{}, err
	}
	p.Fee = fee

	if r.remaining() != 0 {
		return SpendNPublic{}, errSCTrailing
	}
	return p, nil
}
