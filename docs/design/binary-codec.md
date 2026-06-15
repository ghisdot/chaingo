# Codec binaire compact

> Statut : livré (tranches 1 à 5). Issue [#8](https://github.com/ghisdot/chaingo/issues/8).

ChainGO sérialise transactions, blocs et votes en **binaire compact** pour
le transport P2P et le stockage, là où le JSON était utilisé auparavant.
Objectif : réduire la bande passante et l'empreinte disque, et accélérer
l'encodage/décodage — sans jamais toucher au format qui couvre les
signatures.

## Invariant fondateur : `SigningBytes` reste JSON canonique

Le codec binaire est **strictement séparé** de ce que couvre une signature.

- `Transaction.SigningBytes()`, `Vote.SigningBytes()` et
  `Block.SigningBytes()` (header) restent du **JSON canonique** : l'ordre
  des champs de la struct EST le format signé.
- Conséquence : toute signature ML-DSA-65 produite avant l'arrivée du
  codec reste valide après n'importe quel aller-retour binaire. Le hash
  d'une tx ou d'un bloc (calculé sur `SigningBytes`) est inchangé.

Cet invariant est vérifié par des tests dédiés : après `MarshalBinary` →
`UnmarshalBinary`, on revérifie la signature du proposeur, des tx, des
votes et des preuves d'équivocation, et on compare les hashes.

## Primitives — `internal/codec`

Encodeur/décodeur sans réflexion, avec protections anti-DoS :

| Primitive | Encodage |
|---|---|
| entier non signé | `uvarint` (LEB128) — 1 octet pour les petites valeurs |
| entier signé | `varint` zigzag |
| `bool` | 1 octet (0/1, toute autre valeur rejetée) |
| `uint8` | 1 octet |
| `string` / `[]byte` | `uvarint` longueur + octets ; **longueur 0 = absent** |

Protections : `MaxBytesLen` (32 MB) sur toute longueur lue, `ErrTruncated`
sur buffer court, `ErrTooLarge`, `ErrInvalidTag`, et `MustFinish()` qui
détecte les octets parasites en fin (anti-injection sur les frames P2P).

## Format de transport P2P (tranche 3)

Le gossip TCP utilise des **frames** :

```
[1 octet : type][uvarint : longueur du payload][payload]
```

| Code | Type | Payload |
|---|---|---|
| `0x01` | hello | `chain_id` + hauteur |
| `0x02` | tx | `Transaction.MarshalBinary` |
| `0x03` | block | `Block.MarshalBinary` |
| `0x04` | vote | `Vote.MarshalBinary` |
| `0x05` | get_blocks | hauteur de départ |

- Tout type inconnu **déconnecte le peer** (les évolutions futures se
  négocieront via le `hello`, pas par des codes silencieux).
- Limite : **16 MB par frame** (tient un bloc plein, refuse les frames
  forgées énormes).
- Re-gossip optimisé : une frame valide reçue est rediffusée **telle
  quelle**, sans re-marshaler le payload.

## Format de stockage (tranche 4)

Les blocs sont persistés en binaire dans bbolt (bucket `blocks`), avec
**migration paresseuse rétrocompatible** :

```
ancien : [JSON brut]                  → 1er octet '{' (0x7b)
nouveau : [0x01][Block.MarshalBinary] → 1er octet 0x01
```

À la lecture, le 1er octet route le décodage. Aucune passe de conversion :
les anciens blocs JSON restent lisibles indéfiniment, les nouveaux sont
écrits en binaire, et une base mixte (post-upgrade) se relit sans
problème. **L'état et la genèse restent en JSON** — la racine d'état
dépend de `encoding/json` (tri des clés de map), c'est un invariant de
déterminisme entre nœuds.

## Gains mesurés

### Taille (sur octets réellement transmis/stockés)

| Objet | JSON | Binaire | Gain |
|---|---|---|---|
| Transaction signée | 7 410 o | 5 425 o | **27 %** |
| Bloc complet (header + 2 tx + 2 précommits) | 36 597 o | 26 999 o | **26 %** |

### Vitesse encode + décode (`go test -bench=Codec -benchmem`)

| Objet | Binaire | JSON | Accélération |
|---|---|---|---|
| Transaction | 3,9 µs/op | 91,9 µs/op | **~23×** |
| Bloc complet | 48 µs/op | 329 µs/op | **~6,8×** |

Le JSON était dominé par l'encodage base64 des signatures ML-DSA-65
(~3,3 Ko chacune) ; le binaire les écrit telles quelles.

## Reproduire les mesures

```bash
# Tailles (logguées par les tests de compacité)
go test ./internal/types/ -run Compact -v

# Débit + allocations
go test ./internal/types/ -bench=Codec -benchmem -run='^$'
```

## Compatibilité

Le protocole P2P binaire n'est **pas** compatible avec l'ancien protocole
JSON : tous les nœuds d'un réseau doivent tourner sur la même version. Le
stockage, lui, est rétrocompatible (lecture des anciennes bases). Au
stade testnet, un redéploiement coordonné de tous les nœuds suffit.
