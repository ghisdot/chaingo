// Package store : persistance des blocs et de l'état (bbolt).
package store

import (
	"encoding/binary"
	"encoding/json"
	"fmt"

	bolt "go.etcd.io/bbolt"

	"chaingo/internal/types"
)

// Format de stockage des blocs (tranche 4 du codec binaire).
//
// Les blocs étaient persistés en JSON. On passe au binaire compact
// (Block.MarshalBinary) pour gagner ~26 % de disque et accélérer la
// relecture. Migration PARESSEUSE et rétrocompatible — aucune passe de
// conversion, aucun risque sur une base existante :
//
//   - Les anciens blocs sont du JSON brut, qui commence toujours par '{'
//     (0x7b). À la lecture, si le 1er octet est '{', on décode en JSON.
//   - Les nouveaux blocs sont préfixés d'un octet de version (0x01) suivi
//     du binaire. À la lecture, ce tag route vers UnmarshalBinary.
//   - On choisit un tag (0x01) ≠ '{' pour que la détection soit sans
//     ambiguïté. La genèse et l'état restent JSON (l'état NE DOIT PAS
//     changer : la racine d'état dépend de encoding/json — invariant).
const (
	blockTagLegacyJSON = byte('{')  // ancien format : JSON brut (pas de préfixe)
	blockTagBinaryV1   = byte(0x01) // nouveau format : [0x01][Block.MarshalBinary]
)

var (
	bucketBlocks      = []byte("blocks")
	bucketTxIndex     = []byte("txindex")
	bucketBlockByHash = []byte("blockbyhash") // hash hex -> height (recherche bloc par hash)
	bucketAddrTxs     = []byte("addrtxs")     // <addr>|<height BE>|<txhash> -> 1 (tx par adresse)
	bucketMeta        = []byte("meta")
	keyHeight         = []byte("height")
	keyState          = []byte("state")
	keyGenesis        = []byte("genesis")
)

type Store struct{ db *bolt.DB }

func Open(path string) (*Store, error) {
	db, err := bolt.Open(path, 0o600, nil)
	if err != nil {
		return nil, err
	}
	err = db.Update(func(tx *bolt.Tx) error {
		for _, b := range [][]byte{bucketBlocks, bucketTxIndex, bucketBlockByHash, bucketAddrTxs, bucketMeta} {
			if _, err := tx.CreateBucketIfNotExists(b); err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		db.Close()
		return nil, err
	}
	return &Store{db: db}, nil
}

func (s *Store) Close() error { return s.db.Close() }

func heightKey(h uint64) []byte {
	var k [8]byte
	binary.BigEndian.PutUint64(k[:], h)
	return k[:]
}

// addrTxKey : clé composite <addr>|<height BE>|<txhash> permettant un
// scan préfixe par adresse, naturellement ordonné par hauteur croissante.
func addrTxKey(addr string, height uint64, txhash string) []byte {
	k := make([]byte, 0, len(addr)+1+8+1+len(txhash))
	k = append(k, addr...)
	k = append(k, '|')
	var h [8]byte
	binary.BigEndian.PutUint64(h[:], height)
	k = append(k, h[:]...)
	k = append(k, '|')
	k = append(k, txhash...)
	return k
}

// txAddrs : adresses impliquées par une transaction (from + to + bénéficiaires
// éventuels des contrats). On indexe les liens visibles pour un explorateur.
func txAddrs(t *types.Transaction) []string {
	seen := map[string]bool{}
	add := func(a string) {
		if a != "" && !seen[a] {
			seen[a] = true
		}
	}
	add(t.From)
	add(t.To)
	if t.Contract != nil {
		add(t.Contract.Beneficiary)
		add(t.Contract.Seller)
		add(t.Contract.Arbiter)
		for _, s := range t.Contract.Signers {
			add(s)
		}
	}
	out := make([]string, 0, len(seen))
	for a := range seen {
		out = append(out, a)
	}
	return out
}

// encodeBlock : sérialise un bloc au format binaire taggé (0x01 + binaire).
func encodeBlock(b *types.Block) ([]byte, error) {
	bin, err := b.MarshalBinary()
	if err != nil {
		return nil, err
	}
	out := make([]byte, 0, 1+len(bin))
	out = append(out, blockTagBinaryV1)
	out = append(out, bin...)
	return out, nil
}

// decodeBlock : relit un bloc en détectant le format au 1er octet.
// '{' → JSON legacy ; 0x01 → binaire v1 ; sinon erreur explicite.
func decodeBlock(data []byte) (*types.Block, error) {
	if len(data) == 0 {
		return nil, nil
	}
	b := &types.Block{}
	switch data[0] {
	case blockTagLegacyJSON:
		if err := json.Unmarshal(data, b); err != nil {
			return nil, fmt.Errorf("store: legacy json block: %w", err)
		}
	case blockTagBinaryV1:
		if err := b.UnmarshalBinary(data[1:]); err != nil {
			return nil, fmt.Errorf("store: binary v1 block: %w", err)
		}
	default:
		return nil, fmt.Errorf("store: unknown block format tag 0x%02x", data[0])
	}
	return b, nil
}

func (s *Store) SaveBlock(b *types.Block) error {
	data, err := encodeBlock(b)
	if err != nil {
		return err
	}
	return s.db.Update(func(tx *bolt.Tx) error {
		if err := tx.Bucket(bucketBlocks).Put(heightKey(b.Header.Height), data); err != nil {
			return err
		}
		// Index bloc-par-hash : permet la recherche universelle dans l'explorateur.
		if err := tx.Bucket(bucketBlockByHash).Put([]byte(b.Hash), heightKey(b.Header.Height)); err != nil {
			return err
		}
		idx := tx.Bucket(bucketTxIndex)
		addrIdx := tx.Bucket(bucketAddrTxs)
		for _, t := range b.Txs {
			h := t.Hash()
			if err := idx.Put([]byte(h), heightKey(b.Header.Height)); err != nil {
				return err
			}
			for _, addr := range txAddrs(t) {
				if err := addrIdx.Put(addrTxKey(addr, b.Header.Height, h), []byte{1}); err != nil {
					return err
				}
			}
		}
		return tx.Bucket(bucketMeta).Put(keyHeight, heightKey(b.Header.Height))
	})
}

// putRawBlock : écrit des octets bruts comme valeur de bloc à une hauteur,
// SANS ré-encoder ni indexer. Sert à reconstituer un état legacy (JSON brut)
// dans les tests, et de brique pour une éventuelle passe de migration.
func (s *Store) putRawBlock(height uint64, raw []byte) error {
	return s.db.Update(func(tx *bolt.Tx) error {
		return tx.Bucket(bucketBlocks).Put(heightKey(height), raw)
	})
}

// BlockByHash : recherche un bloc par son hash. Retourne nil si inconnu.
func (s *Store) BlockByHash(hash string) (*types.Block, error) {
	var height uint64
	found := false
	if err := s.db.View(func(tx *bolt.Tx) error {
		v := tx.Bucket(bucketBlockByHash).Get([]byte(hash))
		if v != nil {
			height = binary.BigEndian.Uint64(v)
			found = true
		}
		return nil
	}); err != nil {
		return nil, err
	}
	if !found {
		return nil, nil
	}
	return s.GetBlock(height)
}

// TxRef : pointeur léger vers une transaction (assez pour un explorateur).
type TxRef struct {
	Hash   string `json:"hash"`
	Height uint64 `json:"height"`
}

// AddressTxs : retourne jusqu'à `limit` tx impliquant `addr`, des plus
// RÉCENTES vers les plus anciennes. `beforeHeight` (0 = sans limite) ne
// renvoie que les tx strictement antérieures à cette hauteur (pagination).
func (s *Store) AddressTxs(addr string, limit int, beforeHeight uint64) ([]TxRef, error) {
	if limit <= 0 || limit > 500 {
		limit = 50
	}
	prefix := append([]byte(addr), '|')
	out := []TxRef{}
	err := s.db.View(func(tx *bolt.Tx) error {
		c := tx.Bucket(bucketAddrTxs).Cursor()
		// On veut un parcours descendant. Seek à la borne supérieure du préfixe,
		// puis on remonte.
		upper := append([]byte{}, prefix...)
		upper[len(upper)-1] = '|' + 1 // caractère juste après '|' → exclut le préfixe
		k, _ := c.Seek(upper)
		if k == nil {
			k, _ = c.Last()
		} else {
			k, _ = c.Prev()
		}
		for ; k != nil && len(out) < limit; k, _ = c.Prev() {
			if !hasPrefix(k, prefix) {
				return nil
			}
			// k = addr|height BE|txhash
			rest := k[len(prefix):]
			if len(rest) < 9 {
				continue
			}
			h := binary.BigEndian.Uint64(rest[:8])
			if beforeHeight > 0 && h >= beforeHeight {
				continue
			}
			txhash := string(rest[9:])
			out = append(out, TxRef{Hash: txhash, Height: h})
		}
		return nil
	})
	return out, err
}

func hasPrefix(b, prefix []byte) bool {
	if len(b) < len(prefix) {
		return false
	}
	for i, c := range prefix {
		if b[i] != c {
			return false
		}
	}
	return true
}

func (s *Store) GetBlock(h uint64) (*types.Block, error) {
	var b *types.Block
	err := s.db.View(func(tx *bolt.Tx) error {
		data := tx.Bucket(bucketBlocks).Get(heightKey(h))
		if data == nil {
			return nil
		}
		// bbolt prête une slice valide seulement pendant la transaction.
		// decodeBlock → UnmarshalBinary recopie déjà tous les []byte/string
		// (cf. codec.ReadBytes/ReadString), donc le bloc ne référence plus le
		// buffer après retour. La copie ci-dessous est une garde peu coûteuse
		// contre une régression future du codec (1 bloc/lecture).
		dup := append([]byte{}, data...)
		var err error
		b, err = decodeBlock(dup)
		return err
	})
	return b, err
}

// TxHeight returns the height of the block containing the tx, or false.
func (s *Store) TxHeight(hash string) (uint64, bool) {
	var h uint64
	found := false
	s.db.View(func(tx *bolt.Tx) error {
		v := tx.Bucket(bucketTxIndex).Get([]byte(hash))
		if v != nil {
			h = binary.BigEndian.Uint64(v)
			found = true
		}
		return nil
	})
	return h, found
}

func (s *Store) LastHeight() (uint64, bool) {
	var h uint64
	found := false
	s.db.View(func(tx *bolt.Tx) error {
		v := tx.Bucket(bucketMeta).Get(keyHeight)
		if v != nil {
			h = binary.BigEndian.Uint64(v)
			found = true
		}
		return nil
	})
	return h, found
}

func (s *Store) SaveState(data []byte) error {
	return s.db.Update(func(tx *bolt.Tx) error {
		return tx.Bucket(bucketMeta).Put(keyState, data)
	})
}

func (s *Store) LoadState() []byte {
	var out []byte
	s.db.View(func(tx *bolt.Tx) error {
		v := tx.Bucket(bucketMeta).Get(keyState)
		if v != nil {
			out = append([]byte{}, v...)
		}
		return nil
	})
	return out
}

func (s *Store) SaveGenesis(data []byte) error {
	return s.db.Update(func(tx *bolt.Tx) error {
		return tx.Bucket(bucketMeta).Put(keyGenesis, data)
	})
}

func (s *Store) LoadGenesis() []byte {
	var out []byte
	s.db.View(func(tx *bolt.Tx) error {
		v := tx.Bucket(bucketMeta).Get(keyGenesis)
		if v != nil {
			out = append([]byte{}, v...)
		}
		return nil
	})
	return out
}
