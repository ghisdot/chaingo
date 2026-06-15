package types

import (
	"fmt"

	"chaingo/internal/codec"
)

// MarshalBinary / UnmarshalBinary pour Block, BlockHeader, Vote et
// DoubleSignEvidence — tranche 2 du codec binaire compact. Format séparé
// de SigningBytes (qui reste JSON canonique pour préserver toutes les
// signatures existantes).
//
// Choix d'encodage : chaque sous-objet (tx, vote, evidence) est
// length-prefixed via WriteBytes — chaque entrée d'une slice est donc
// auto-délimitée. Coût : 1-2 octets de varint par entrée. Bénéfice :
// décodeur trivialement résistant aux entrées malveillantes (chaque
// frame est bornée par MaxBytesLen).

// ---- Vote ----

func (v *Vote) MarshalBinary() ([]byte, error) {
	e := codec.NewEncoder(256)
	e.WriteString(v.ChainID)
	e.WriteUvarint(v.Height)
	e.WriteUvarint(uint64(v.Round))
	e.WriteString(v.Kind)
	e.WriteString(v.BlockHash)
	e.WriteString(v.Voter)
	e.WriteBytes(v.VoterPub)
	e.WriteBytes(v.Signature)
	return e.Bytes(), nil
}

func (v *Vote) UnmarshalBinary(data []byte) error {
	d := codec.NewDecoder(data)
	var err error
	if v.ChainID, err = d.ReadString(); err != nil {
		return fmt.Errorf("vote.chain_id: %w", err)
	}
	if v.Height, err = d.ReadUvarint(); err != nil {
		return fmt.Errorf("vote.height: %w", err)
	}
	r, err := d.ReadUvarint()
	if err != nil {
		return fmt.Errorf("vote.round: %w", err)
	}
	v.Round = uint32(r)
	if v.Kind, err = d.ReadString(); err != nil {
		return fmt.Errorf("vote.kind: %w", err)
	}
	if v.BlockHash, err = d.ReadString(); err != nil {
		return fmt.Errorf("vote.block_hash: %w", err)
	}
	if v.Voter, err = d.ReadString(); err != nil {
		return fmt.Errorf("vote.voter: %w", err)
	}
	if v.VoterPub, err = d.ReadBytes(); err != nil {
		return fmt.Errorf("vote.voter_pub: %w", err)
	}
	if v.Signature, err = d.ReadBytes(); err != nil {
		return fmt.Errorf("vote.signature: %w", err)
	}
	if err := d.MustFinish(); err != nil {
		return fmt.Errorf("vote: %w", err)
	}
	return nil
}

// ---- DoubleSignEvidence ----

func (ev *DoubleSignEvidence) MarshalBinary() ([]byte, error) {
	e := codec.NewEncoder(512)
	e.WriteUvarint(ev.Height)
	e.WriteString(ev.Voter)
	if ev.VoteA == nil || ev.VoteB == nil {
		return nil, fmt.Errorf("evidence: missing vote")
	}
	a, err := ev.VoteA.MarshalBinary()
	if err != nil {
		return nil, fmt.Errorf("evidence.vote_a: %w", err)
	}
	b, err := ev.VoteB.MarshalBinary()
	if err != nil {
		return nil, fmt.Errorf("evidence.vote_b: %w", err)
	}
	e.WriteBytes(a)
	e.WriteBytes(b)
	return e.Bytes(), nil
}

func (ev *DoubleSignEvidence) UnmarshalBinary(data []byte) error {
	d := codec.NewDecoder(data)
	var err error
	if ev.Height, err = d.ReadUvarint(); err != nil {
		return fmt.Errorf("evidence.height: %w", err)
	}
	if ev.Voter, err = d.ReadString(); err != nil {
		return fmt.Errorf("evidence.voter: %w", err)
	}
	a, err := d.ReadBytes()
	if err != nil {
		return fmt.Errorf("evidence.vote_a: %w", err)
	}
	ev.VoteA = &Vote{}
	if err := ev.VoteA.UnmarshalBinary(a); err != nil {
		return fmt.Errorf("evidence.vote_a: %w", err)
	}
	b, err := d.ReadBytes()
	if err != nil {
		return fmt.Errorf("evidence.vote_b: %w", err)
	}
	ev.VoteB = &Vote{}
	if err := ev.VoteB.UnmarshalBinary(b); err != nil {
		return fmt.Errorf("evidence.vote_b: %w", err)
	}
	if err := d.MustFinish(); err != nil {
		return fmt.Errorf("evidence: %w", err)
	}
	return nil
}

// ---- BlockHeader ----

func writeBlockHeader(e *codec.Encoder, h *BlockHeader) {
	e.WriteUvarint(h.Height)
	e.WriteString(h.PrevHash)
	e.WriteVarint(h.Timestamp)
	e.WriteString(h.Proposer)
	e.WriteUvarint(uint64(h.Round))
	e.WriteString(h.TxRoot)
	e.WriteString(h.EvidenceRoot)
	e.WriteString(h.LastCommitRoot)
	e.WriteString(h.StateRoot)
}

func readBlockHeader(d *codec.Decoder, h *BlockHeader) error {
	var err error
	if h.Height, err = d.ReadUvarint(); err != nil {
		return fmt.Errorf("header.height: %w", err)
	}
	if h.PrevHash, err = d.ReadString(); err != nil {
		return fmt.Errorf("header.prev_hash: %w", err)
	}
	if h.Timestamp, err = d.ReadVarint(); err != nil {
		return fmt.Errorf("header.timestamp: %w", err)
	}
	if h.Proposer, err = d.ReadString(); err != nil {
		return fmt.Errorf("header.proposer: %w", err)
	}
	r, err := d.ReadUvarint()
	if err != nil {
		return fmt.Errorf("header.round: %w", err)
	}
	h.Round = uint32(r)
	if h.TxRoot, err = d.ReadString(); err != nil {
		return fmt.Errorf("header.tx_root: %w", err)
	}
	if h.EvidenceRoot, err = d.ReadString(); err != nil {
		return fmt.Errorf("header.evidence_root: %w", err)
	}
	if h.LastCommitRoot, err = d.ReadString(); err != nil {
		return fmt.Errorf("header.last_commit_root: %w", err)
	}
	if h.StateRoot, err = d.ReadString(); err != nil {
		return fmt.Errorf("header.state_root: %w", err)
	}
	return nil
}

// ---- Block ----

func (b *Block) MarshalBinary() ([]byte, error) {
	e := codec.NewEncoder(4096)
	writeBlockHeader(e, &b.Header)
	e.WriteUvarint(uint64(len(b.Txs)))
	for i, tx := range b.Txs {
		raw, err := tx.MarshalBinary()
		if err != nil {
			return nil, fmt.Errorf("block.txs[%d]: %w", i, err)
		}
		e.WriteBytes(raw)
	}
	e.WriteUvarint(uint64(len(b.Evidence)))
	for i, ev := range b.Evidence {
		raw, err := ev.MarshalBinary()
		if err != nil {
			return nil, fmt.Errorf("block.evidence[%d]: %w", i, err)
		}
		e.WriteBytes(raw)
	}
	e.WriteUvarint(uint64(len(b.LastCommit)))
	for i, v := range b.LastCommit {
		raw, err := v.MarshalBinary()
		if err != nil {
			return nil, fmt.Errorf("block.last_commit[%d]: %w", i, err)
		}
		e.WriteBytes(raw)
	}
	e.WriteString(b.Hash)
	e.WriteBytes(b.ProposerPubKey)
	e.WriteBytes(b.ProposerSignature)
	return e.Bytes(), nil
}

func (b *Block) UnmarshalBinary(data []byte) error {
	d := codec.NewDecoder(data)
	if err := readBlockHeader(d, &b.Header); err != nil {
		return err
	}
	nTx, err := d.ReadLen()
	if err != nil {
		return fmt.Errorf("block.txs len: %w", err)
	}
	if nTx > 0 {
		b.Txs = make([]*Transaction, nTx)
		for i := range b.Txs {
			raw, err := d.ReadBytes()
			if err != nil {
				return fmt.Errorf("block.txs[%d]: %w", i, err)
			}
			tx := &Transaction{}
			if err := tx.UnmarshalBinary(raw); err != nil {
				return fmt.Errorf("block.txs[%d]: %w", i, err)
			}
			b.Txs[i] = tx
		}
	}
	nEv, err := d.ReadLen()
	if err != nil {
		return fmt.Errorf("block.evidence len: %w", err)
	}
	if nEv > 0 {
		b.Evidence = make([]*DoubleSignEvidence, nEv)
		for i := range b.Evidence {
			raw, err := d.ReadBytes()
			if err != nil {
				return fmt.Errorf("block.evidence[%d]: %w", i, err)
			}
			ev := &DoubleSignEvidence{}
			if err := ev.UnmarshalBinary(raw); err != nil {
				return fmt.Errorf("block.evidence[%d]: %w", i, err)
			}
			b.Evidence[i] = ev
		}
	}
	nVotes, err := d.ReadLen()
	if err != nil {
		return fmt.Errorf("block.last_commit len: %w", err)
	}
	if nVotes > 0 {
		b.LastCommit = make([]*Vote, nVotes)
		for i := range b.LastCommit {
			raw, err := d.ReadBytes()
			if err != nil {
				return fmt.Errorf("block.last_commit[%d]: %w", i, err)
			}
			v := &Vote{}
			if err := v.UnmarshalBinary(raw); err != nil {
				return fmt.Errorf("block.last_commit[%d]: %w", i, err)
			}
			b.LastCommit[i] = v
		}
	}
	if b.Hash, err = d.ReadString(); err != nil {
		return fmt.Errorf("block.hash: %w", err)
	}
	if b.ProposerPubKey, err = d.ReadBytes(); err != nil {
		return fmt.Errorf("block.proposer_pub_key: %w", err)
	}
	if b.ProposerSignature, err = d.ReadBytes(); err != nil {
		return fmt.Errorf("block.proposer_signature: %w", err)
	}
	if err := d.MustFinish(); err != nil {
		return fmt.Errorf("block: %w", err)
	}
	return nil
}
