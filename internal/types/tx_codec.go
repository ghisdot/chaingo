package types

import (
	"fmt"

	"chaingo/internal/codec"
)

// MarshalBinary / UnmarshalBinary : sérialisation BINAIRE COMPACTE de la
// transaction pour le transport P2P et le stockage. Plus compacte que JSON
// (~25 % gagnés sur des tx avec signature ML-DSA-65 de 3,3 Ko).
//
// IMPORTANT : ce format est SÉPARÉ de SigningBytes (qui reste JSON canonique
// pour préserver la validité de toutes les signatures existantes). Le hash
// d'une tx (tx.Hash()) calculé sur SigningBytes reste donc inchangé après un
// round-trip binaire.
//
// Ordre des champs identique à la struct Transaction. Champs optionnels :
//   - string / []byte : "absent" = longueur 0 (pas de tag de présence)
//   - *TokenParams / *ContractParams : 1 byte de présence (0 = nil, 1 = présent)

func (tx *Transaction) MarshalBinary() ([]byte, error) {
	e := codec.NewEncoder(512)
	e.WriteString(tx.ChainID)
	e.WriteString(string(tx.Type))
	e.WriteString(tx.From)
	e.WriteBytes(tx.FromPubKey)
	e.WriteString(tx.To)
	e.WriteString(tx.TokenID)
	e.WriteUvarint(tx.Amount)
	e.WriteUvarint(tx.Nonce)
	e.WriteUvarint(tx.MaxBaseFee)
	e.WriteUvarint(tx.Tip)
	e.WriteBool(tx.Private)
	e.WriteString(tx.Memo)
	if tx.Token == nil {
		e.WriteBool(false)
	} else {
		e.WriteBool(true)
		writeTokenParams(e, tx.Token)
	}
	if tx.Contract == nil {
		e.WriteBool(false)
	} else {
		e.WriteBool(true)
		writeContractParams(e, tx.Contract)
	}
	e.WriteString(tx.ContractID)
	e.WriteString(tx.Action)
	e.WriteUvarint(tx.Proposal)
	e.WriteVarint(tx.Timestamp)
	e.WriteBytes(tx.Signature)
	// Extensions optionnelles écrites APRÈS la signature, et SEULEMENT pour les tx
	// qui en ont besoin. Conséquence : toute tx existante (sans extension) garde un
	// encodage binaire OCTET POUR OCTET identique, et les blocs déjà stockés (sans
	// extension) restent décodables (0 octet en trop).
	//
	// Deux blocs d'extension, en ordre FIXE : (1) WASM (Code/Args/Gas), puis
	// (2) pool blindé (ShieldCommitment/ShieldNote/SpendProof/SpendPublic). Le bloc
	// WASM est écrit dès qu'UNE extension (WASM OU blindée) est présente : ainsi le
	// décodeur, voyant des octets après la signature, lit toujours d'abord le bloc
	// WASM (vide si la tx est purement blindée) puis le bloc blindé. Une tx WASM
	// PURE (sans champ blindé) n'écrit pas le bloc blindé → son encodage reste
	// OCTET-POUR-OCTET identique à avant l'ajout du pool blindé.
	hasShield := len(tx.ShieldCommitment) > 0 || len(tx.ShieldNote) > 0 ||
		len(tx.SpendProof) > 0 || len(tx.SpendPublic) > 0
	hasWasm := len(tx.Code) > 0 || len(tx.Args) > 0 || tx.Gas > 0
	if hasWasm || hasShield {
		e.WriteBytes(tx.Code)
		e.WriteUvarint(uint64(len(tx.Args)))
		for _, a := range tx.Args {
			e.WriteUvarint(a)
		}
		e.WriteUvarint(tx.Gas)
	}
	if hasShield {
		e.WriteBytes(tx.ShieldCommitment)
		e.WriteBytes(tx.ShieldNote)
		e.WriteBytes(tx.SpendProof)
		e.WriteBytes(tx.SpendPublic)
	}
	return e.Bytes(), nil
}

func (tx *Transaction) UnmarshalBinary(data []byte) error {
	d := codec.NewDecoder(data)
	var err error
	if tx.ChainID, err = d.ReadString(); err != nil {
		return fmt.Errorf("tx.chain_id: %w", err)
	}
	var s string
	if s, err = d.ReadString(); err != nil {
		return fmt.Errorf("tx.type: %w", err)
	}
	tx.Type = TxType(s)
	if tx.From, err = d.ReadString(); err != nil {
		return fmt.Errorf("tx.from: %w", err)
	}
	if tx.FromPubKey, err = d.ReadBytes(); err != nil {
		return fmt.Errorf("tx.from_pub_key: %w", err)
	}
	if tx.To, err = d.ReadString(); err != nil {
		return fmt.Errorf("tx.to: %w", err)
	}
	if tx.TokenID, err = d.ReadString(); err != nil {
		return fmt.Errorf("tx.token_id: %w", err)
	}
	if tx.Amount, err = d.ReadUvarint(); err != nil {
		return fmt.Errorf("tx.amount: %w", err)
	}
	if tx.Nonce, err = d.ReadUvarint(); err != nil {
		return fmt.Errorf("tx.nonce: %w", err)
	}
	if tx.MaxBaseFee, err = d.ReadUvarint(); err != nil {
		return fmt.Errorf("tx.max_base_fee: %w", err)
	}
	if tx.Tip, err = d.ReadUvarint(); err != nil {
		return fmt.Errorf("tx.tip: %w", err)
	}
	if tx.Private, err = d.ReadBool(); err != nil {
		return fmt.Errorf("tx.private: %w", err)
	}
	if tx.Memo, err = d.ReadString(); err != nil {
		return fmt.Errorf("tx.memo: %w", err)
	}
	hasToken, err := d.ReadBool()
	if err != nil {
		return fmt.Errorf("tx.token present: %w", err)
	}
	if hasToken {
		tp := &TokenParams{}
		if err := readTokenParams(d, tp); err != nil {
			return fmt.Errorf("tx.token: %w", err)
		}
		tx.Token = tp
	} else {
		tx.Token = nil
	}
	hasContract, err := d.ReadBool()
	if err != nil {
		return fmt.Errorf("tx.contract present: %w", err)
	}
	if hasContract {
		cp := &ContractParams{}
		if err := readContractParams(d, cp); err != nil {
			return fmt.Errorf("tx.contract: %w", err)
		}
		tx.Contract = cp
	} else {
		tx.Contract = nil
	}
	if tx.ContractID, err = d.ReadString(); err != nil {
		return fmt.Errorf("tx.contract_id: %w", err)
	}
	if tx.Action, err = d.ReadString(); err != nil {
		return fmt.Errorf("tx.action: %w", err)
	}
	if tx.Proposal, err = d.ReadUvarint(); err != nil {
		return fmt.Errorf("tx.proposal: %w", err)
	}
	if tx.Timestamp, err = d.ReadVarint(); err != nil {
		return fmt.Errorf("tx.timestamp: %w", err)
	}
	if tx.Signature, err = d.ReadBytes(); err != nil {
		return fmt.Errorf("tx.signature: %w", err)
	}
	// Extension WASM optionnelle (cf MarshalBinary) : présente seulement s'il
	// reste des octets. Les blocs anciens (sans extension) tombent dans le else
	// et laissent Code/Args/Gas à zéro.
	if d.Remaining() > 0 {
		if tx.Code, err = d.ReadBytes(); err != nil {
			return fmt.Errorf("tx.code: %w", err)
		}
		n, err := d.ReadLen()
		if err != nil {
			return fmt.Errorf("tx.args count: %w", err)
		}
		if n > 0 {
			tx.Args = make([]uint64, n)
			for i := range tx.Args {
				if tx.Args[i], err = d.ReadUvarint(); err != nil {
					return fmt.Errorf("tx.args[%d]: %w", i, err)
				}
			}
		}
		if tx.Gas, err = d.ReadUvarint(); err != nil {
			return fmt.Errorf("tx.gas: %w", err)
		}
	}
	// Extension POOL BLINDÉ optionnelle : présente seulement s'il reste des octets
	// APRÈS le bloc WASM (qui, pour une tx purement blindée, a été écrit vide). Les
	// tx WASM pures et les tx anciennes laissent ces champs à nil.
	if d.Remaining() > 0 {
		if tx.ShieldCommitment, err = d.ReadBytes(); err != nil {
			return fmt.Errorf("tx.shield_commitment: %w", err)
		}
		if tx.ShieldNote, err = d.ReadBytes(); err != nil {
			return fmt.Errorf("tx.shield_note: %w", err)
		}
		if tx.SpendProof, err = d.ReadBytes(); err != nil {
			return fmt.Errorf("tx.spend_proof: %w", err)
		}
		if tx.SpendPublic, err = d.ReadBytes(); err != nil {
			return fmt.Errorf("tx.spend_public: %w", err)
		}
	}
	if err := d.MustFinish(); err != nil {
		return fmt.Errorf("tx: %w", err)
	}
	return nil
}

// ---- Sous-structures ----

func writeTokenParams(e *codec.Encoder, t *TokenParams) {
	e.WriteString(t.Symbol)
	e.WriteString(t.Name)
	e.WriteU8(t.Decimals)
	e.WriteUvarint(t.Supply)
	e.WriteBool(t.Mintable)
	// Champs ajoutés (mêmes que TokenParams, en fin) — ordre figé.
	e.WriteUvarint(t.MaxSupply)
	e.WriteBool(t.Burnable)
	e.WriteString(t.LogoURI)
	e.WriteString(t.Description)
	e.WriteString(t.Website)
}

func readTokenParams(d *codec.Decoder, t *TokenParams) error {
	var err error
	if t.Symbol, err = d.ReadString(); err != nil {
		return err
	}
	if t.Name, err = d.ReadString(); err != nil {
		return err
	}
	if t.Decimals, err = d.ReadU8(); err != nil {
		return err
	}
	if t.Supply, err = d.ReadUvarint(); err != nil {
		return err
	}
	if t.Mintable, err = d.ReadBool(); err != nil {
		return err
	}
	if t.MaxSupply, err = d.ReadUvarint(); err != nil {
		return err
	}
	if t.Burnable, err = d.ReadBool(); err != nil {
		return err
	}
	if t.LogoURI, err = d.ReadString(); err != nil {
		return err
	}
	if t.Description, err = d.ReadString(); err != nil {
		return err
	}
	if t.Website, err = d.ReadString(); err != nil {
		return err
	}
	return nil
}

func writeContractParams(e *codec.Encoder, c *ContractParams) {
	e.WriteString(c.Template)
	e.WriteString(c.TokenID)
	e.WriteUvarint(c.Amount)
	e.WriteString(c.Beneficiary)
	e.WriteVarint(c.StartMs)
	e.WriteVarint(c.EndMs)
	e.WriteString(c.Seller)
	e.WriteString(c.Arbiter)
	e.WriteUvarint(uint64(len(c.Signers)))
	for _, s := range c.Signers {
		e.WriteString(s)
	}
	e.WriteUvarint(c.Threshold)
}

func readContractParams(d *codec.Decoder, c *ContractParams) error {
	var err error
	if c.Template, err = d.ReadString(); err != nil {
		return err
	}
	if c.TokenID, err = d.ReadString(); err != nil {
		return err
	}
	if c.Amount, err = d.ReadUvarint(); err != nil {
		return err
	}
	if c.Beneficiary, err = d.ReadString(); err != nil {
		return err
	}
	if c.StartMs, err = d.ReadVarint(); err != nil {
		return err
	}
	if c.EndMs, err = d.ReadVarint(); err != nil {
		return err
	}
	if c.Seller, err = d.ReadString(); err != nil {
		return err
	}
	if c.Arbiter, err = d.ReadString(); err != nil {
		return err
	}
	n, err := d.ReadLen()
	if err != nil {
		return err
	}
	if n > 0 {
		c.Signers = make([]string, n)
		for i := range c.Signers {
			if c.Signers[i], err = d.ReadString(); err != nil {
				return err
			}
		}
	}
	if c.Threshold, err = d.ReadUvarint(); err != nil {
		return err
	}
	return nil
}
