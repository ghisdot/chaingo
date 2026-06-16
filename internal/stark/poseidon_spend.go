// STARK MAISON — EXPÉRIMENTAL, NON AUDITÉ, hors-consensus.
//
// ÉTAGE 4.2 — Circuit de DÉPENSE blindée (1 entrée / 1 sortie + frais).
//
// On prouve, SANS révéler le témoin, l'énoncé d'une transaction blindée à une
// entrée et une sortie :
//
//	« Je connais une note d'entrée (inValue, ownerTag, inRho) dont l'engagement
//	  inCm = Commit(inValue, ownerTag, inRho) appartient à l'arbre de racine
//	  PUBLIQUE merkleRoot ; je connais la clé de nullifier nk dont le digest
//	  ownerTag = PoseidonHash(nk) (donc je suis le propriétaire) ; le nullifier
//	  PUBLIC nf = Hash2(nk, inCm) ; une note de sortie d'engagement PUBLIC
//	  outCm = Commit(outValue, outOwnerTag, outRho) ; et la valeur est conservée
//	  inValue = outValue + fee (fee PUBLIC). »
//
// Le circuit est construit AU-DESSUS du moteur multi-colonnes (stark_mc.go,
// ProveAIR/VerifyAIR) et RÉUTILISE l'arithmétisation d'UNE permutation Poseidon
// (poseidon_air_full.go : ARC + S-box pleine/partielle + MDS, constantes/
// sélecteurs ancrés par bord). On EMPILE VERTICALEMENT plusieurs permutations
// Poseidon (« blocs ») de 32 lignes chacun, exactement comme membership_air.go
// empile les niveaux de l'arbre. Entre deux blocs, la transition de la ligne de
// sortie RÉ-ASSEMBLE l'état d'entrée du bloc suivant à partir du digest produit
// (cur.s[0..3]), de colonnes-témoins et de REGISTRES portés constants.
//
// ┌──────────────────────────────────────────────────────────────────────────┐
// │ PORTÉE & HONNÊTETÉ — PROTOTYPE RÉDUIT ASSUMÉ.                               │
// │                                                                            │
// │ CE QUI EST PROUVÉ (5 contraintes liées) :                                  │
// │   (1) inCm     = Commit(inValue, ownerTag, inRho)   [un Poseidon]          │
// │   (1bis) ownerTag = PoseidonHash(nk)                [un Poseidon]          │
// │       => nk détermine ownerTag : seul le détenteur de nk peut produire la  │
// │          paire (ownerTag, nf) cohérente avec inCm.                         │
// │   (2) inCm ∈ arbre Merkle Poseidon -> merkleRoot    [chaîne d niveaux]      │
// │       (appartenance, étage 4.1, réutilisée TELLE QUELLE en arithmétique).  │
// │   (3) nf       = Hash2(nk, inCm)                    [un Poseidon]          │
// │   (4) outCm    = Commit(outValue, outOwnerTag, outRho)  [un Poseidon]      │
// │   (5) inValue  = outValue + fee                     [contrainte linéaire]   │
// │                                                                            │
// │ PUBLIC : merkleRoot, nf, outCm, fee. PRIVÉ (témoin, JAMAIS publié, ni en   │
// │ clair ni comme valeur publique du transcript) : inValue, inRho, nk,        │
// │ ownerTag, le chemin Merkle de inCm ; outValue, outOwnerTag, outRho. Les    │
// │ engagements intermédiaires inCm et ownerTag ne sont PAS publiés non plus.  │
// │                                                                            │
// │ CE QUI N'EST PAS « ZERO-KNOWLEDGE COMPLET » : la trace n'est PAS masquée   │
// │ (pas de randomized LDE) ; le « ZK » se limite à NE PAS PUBLIER le témoin.  │
// │ Les ouvertures FRI exposent des évaluations LDE de colonnes en quelques    │
// │ points hors du domaine de trace. Le masquage zero-knowledge complet reste  │
// │ À FAIRE et À AUDITER.                                                       │
// │                                                                            │
// │ CE QUI EST RÉDUIT / OMIS : profondeur d'arbre FIXE spendDepth=4 (16        │
// │ feuilles) pour garder le prouveur tractable (un seul format) ; UNE entrée  │
// │ et UNE sortie (pas de multi-in/out ni d'agrégation de notes) ; PAS de      │
// │ borne de domaine sur les valeurs (range proof : on ne prouve pas           │
// │ 0 <= value < 2^64, donc un wrap-around du corps de Goldilocks pourrait     │
// │ théoriquement contourner la conservation — c'est un GAP documenté, à       │
// │ corriger par une décomposition binaire avant tout usage adverse).          │
// │                                                                            │
// │ PARAMÈTRES NON AUDITÉS : matrice MDS + constantes de ronde dérivées par    │
// │ NOUS (poseidon.go). Résistance préimages/collisions NON établie. Ne pas    │
// │ utiliser en consensus / production.                                        │
// └──────────────────────────────────────────────────────────────────────────┘
//
// ---------------------------------------------------------------------------
// Définition de la note via commitment POSEIDON (remplace le SHA3 du prototype
// internal/shielded pour la rendre PROUVABLE en circuit)
// ---------------------------------------------------------------------------
//
//	cm = Commit(value, ownerTag, rho) := Permute(état)[:4] avec l'état de 12 Felt
//	      [ ownerTag(4) | rho(4) | value | sepCommit | 0 | 0 ]
//	      (rate = ownerTag||rho, capacité = value, séparateur, 0, 0).
//	ownerTag = PoseidonHash(nk) := Permute(état)[:4] avec
//	      [ nk(4) | 0 0 0 0 | sepOwner | 0 | 0 | 0 ].
//	nf = Hash2(nk, cm) — EXACTEMENT la fonction Hash2 de poseidon.go.
//
// value, sepCommit, sepOwner occupent la CAPACITÉ (cellules 8..11) : ils sont
// donc liés par la permutation (un commitment liant), et la valeur est cachée
// (cm ne révèle pas value sans rho/ownerTag). Ces définitions sont la VERSION
// NATIVE (en clair) ; l'AIR en est la réplique arithmétique exacte (testé).
//
// ---------------------------------------------------------------------------
// Empilement des blocs (chaîne sans fan-out, via registres portés constants)
// ---------------------------------------------------------------------------
//
// Le seul couplage non-local est : nf et la chaîne d'appartenance ont TOUS DEUX
// besoin de inCm, et nf a besoin de nk (produit au bloc ownerTag). On résout par
// DEUX registres-témoins [4]Felt portés CONSTANTS le long de la trace :
//
//	regNk = nk    : chargé à la ligne 0 (entrée du bloc ownerTag) puis tenu
//	                constant ; relu au glue du bloc nf.
//	regCm = inCm  : chargé à la sortie du bloc inCm puis tenu constant ; relu au
//	                glue du bloc d'appartenance (child initial) et du bloc nf.
//
// Un registre est « tenu » par une contrainte de transition de DEGRÉ 1
// (hold·(next.reg - cur.reg) = 0) sur les lignes concernées, et « chargé » par
// une contrainte (load·(cur.reg - source)=0). C'est la technique standard de
// registre en AIR.
//
// Ordre des blocs (chacun = 32 lignes, base = ℓ·32) :
//
//	bloc 0  : ownerTag = Permute(pack(nk)).      out[0..3] = ownerTag.
//	bloc 1  : inCm     = Permute(commit(ownerTag, inRho, inValue)). out = inCm.
//	bloc 2  : nf       = Permute(Hash2(nk, inCm)).  out[0..3] = nf  (PUBLIC).
//	bloc 3..3+d-1 : appartenance — chaîne Hash2 de inCm vers merkleRoot.
//	                out du dernier = merkleRoot (PUBLIC).
//	bloc 3+d : outCm   = Permute(commit(outOwnerTag, outRho, outValue)).
//	                out[0..3] = outCm (PUBLIC).
//
// Hauteur n = (4 + d)·32. Pour d = spendDepth = 4 : n = 8·32 = 256 (puissance de
// 2, même classe de coût que membership_air.go).
//
// Les états d'entrée de chaque bloc proviennent du glue de la ligne de sortie du
// bloc précédent (mode dédié), sauf la ligne 0 (entrée du bloc ownerTag) qui est
// un TÉMOIN libre (pack(nk)). Le détail des modes est dans spendRowStructure.
//
// DÉTERMINISME ABSOLU : aucun time, aucun math/rand. Constantes/sélecteurs issus
// de params (poseidon.go) ; tout l'aléa du protocole vient du transcript Fiat-
// Shamir via ProveAIR/VerifyAIR.
package stark

// ---------------------------------------------------------------------------
// Constantes de la note Poseidon (séparateurs de domaine de capacité)
// ---------------------------------------------------------------------------

const (
	// spCommitSep est le séparateur de domaine placé en cellule de capacité 9 du
	// commitment de note (distingue Commit d'un Hash/Hash2 quelconque).
	spCommitSep uint64 = 0x436f6d6d69746d74 // "Commitmt" ASCII (8 octets)

	// spOwnerSep est le séparateur de domaine du hash ownerTag = PoseidonHash(nk),
	// placé en cellule de capacité 8.
	spOwnerSep uint64 = 0x4f776e6572546167 // "OwnerTag" ASCII (8 octets)
)

// ---------------------------------------------------------------------------
// Définitions NATIVES (en clair) de la note Poseidon
// ---------------------------------------------------------------------------

// spOwnerTagState construit l'état d'entrée (12 Felt) du hash ownerTag :
// [ nk(4) | 0 0 0 0 | spOwnerSep | 0 | 0 | 0 ].
func spOwnerTagState(nk [poseidonDigestLen]Felt) [pfStateCols]Felt {
	var st [pfStateCols]Felt
	for k := 0; k < poseidonDigestLen; k++ {
		st[k] = nk[k]
	}
	st[poseidonRate] = FromUint64(spOwnerSep) // cellule 8
	return st
}

// SpendOwnerTag calcule ownerTag = PoseidonHash(nk) (version native, en clair).
// C'est l'identité publique du propriétaire : seul le détenteur de nk peut la
// dériver, donc seul lui peut produire un nullifier cohérent.
func SpendOwnerTag(nk [poseidonDigestLen]Felt) [poseidonDigestLen]Felt {
	out := Permute(spOwnerTagState(nk))
	var d [poseidonDigestLen]Felt
	copy(d[:], out[:poseidonDigestLen])
	return d
}

// spCommitState construit l'état d'entrée (12 Felt) du commitment de note :
// [ ownerTag(4) | rho(4) | value | spCommitSep | 0 | 0 ].
func spCommitState(value Felt, ownerTag, rho [poseidonDigestLen]Felt) [pfStateCols]Felt {
	var st [pfStateCols]Felt
	for k := 0; k < poseidonDigestLen; k++ {
		st[k] = ownerTag[k]                 // cellules 0..3
		st[poseidonDigestLen+k] = rho[k]    // cellules 4..7
	}
	st[poseidonRate] = value                  // cellule 8 (capacité) = valeur
	st[poseidonRate+1] = FromUint64(spCommitSep) // cellule 9 = séparateur
	return st
}

// SpendCommit calcule cm = Commit(value, ownerTag, rho) (version native). Un seul
// appel Poseidon. Remplace le commitment SHA3 du prototype internal/shielded pour
// le rendre arithmétisable.
func SpendCommit(value Felt, ownerTag, rho [poseidonDigestLen]Felt) [poseidonDigestLen]Felt {
	out := Permute(spCommitState(value, ownerTag, rho))
	var d [poseidonDigestLen]Felt
	copy(d[:], out[:poseidonDigestLen])
	return d
}

// SpendNullifier calcule nf = Hash2(nk, cm) (version native). EXACTEMENT la
// fonction Hash2 de poseidon.go (nk à gauche, cm à droite).
func SpendNullifier(nk, cm [poseidonDigestLen]Felt) [poseidonDigestLen]Felt {
	return Hash2(nk, cm)
}
