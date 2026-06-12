// Package store : persistance des blocs et de l'état (bbolt).
package store

import (
	"encoding/binary"
	"encoding/json"

	bolt "go.etcd.io/bbolt"

	"chaingo/internal/types"
)

var (
	bucketBlocks  = []byte("blocks")
	bucketTxIndex = []byte("txindex")
	bucketMeta    = []byte("meta")
	keyHeight     = []byte("height")
	keyState      = []byte("state")
	keyGenesis    = []byte("genesis")
)

type Store struct{ db *bolt.DB }

func Open(path string) (*Store, error) {
	db, err := bolt.Open(path, 0o600, nil)
	if err != nil {
		return nil, err
	}
	err = db.Update(func(tx *bolt.Tx) error {
		for _, b := range [][]byte{bucketBlocks, bucketTxIndex, bucketMeta} {
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

func (s *Store) SaveBlock(b *types.Block) error {
	data, err := json.Marshal(b)
	if err != nil {
		return err
	}
	return s.db.Update(func(tx *bolt.Tx) error {
		if err := tx.Bucket(bucketBlocks).Put(heightKey(b.Header.Height), data); err != nil {
			return err
		}
		idx := tx.Bucket(bucketTxIndex)
		for _, t := range b.Txs {
			if err := idx.Put([]byte(t.Hash()), heightKey(b.Header.Height)); err != nil {
				return err
			}
		}
		return tx.Bucket(bucketMeta).Put(keyHeight, heightKey(b.Header.Height))
	})
}

func (s *Store) GetBlock(h uint64) (*types.Block, error) {
	var b *types.Block
	err := s.db.View(func(tx *bolt.Tx) error {
		data := tx.Bucket(bucketBlocks).Get(heightKey(h))
		if data == nil {
			return nil
		}
		b = &types.Block{}
		return json.Unmarshal(data, b)
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
