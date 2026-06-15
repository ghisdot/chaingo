package p2p

import (
	"sync/atomic"
	"testing"
	"time"

	"chaingo/internal/crypto"
	"chaingo/internal/types"
)

// TestBinaryProtocolHelloAndGossip : smoke test du protocole binaire
// (tranche 3 du codec). Deux serveurs se connectent, doivent :
//   1. Échanger le hello et accepter (même chain_id),
//   2. Propager une tx envoyée par l'un à l'autre,
//   3. Propager un vote signé.
// Si le format binaire est cassé, OnTx/OnVote ne sera jamais appelé
// côté receveur et le test timeout.
func TestBinaryProtocolHelloAndGossip(t *testing.T) {
	chainID := "chaingo-p2p-test"

	var (
		aGotTx, bGotTx     atomic.Int32
		aGotVote, bGotVote atomic.Int32
	)

	mkHandlers := func(gotTx, gotVote *atomic.Int32, height func() uint64) Handlers {
		return Handlers{
			OnTx:    func(tx *types.Transaction) bool { gotTx.Add(1); return true },
			OnVote:  func(v *types.Vote) bool { gotVote.Add(1); return true },
			OnBlock: func(*types.Block) (bool, bool) { return true, false },
			Height:  height,
			Block:   func(h uint64) *types.Block { return nil },
		}
	}

	const stableHeight = uint64(0)
	hA := func() uint64 { return stableHeight }
	hB := func() uint64 { return stableHeight }

	a := NewServer("127.0.0.1:0", chainID, mkHandlers(&aGotTx, &aGotVote, hA))
	b := NewServer("127.0.0.1:0", chainID, mkHandlers(&bGotTx, &bGotVote, hB))
	if err := a.Start(); err != nil {
		t.Fatalf("a.Start: %v", err)
	}
	defer a.Stop()
	if err := b.Start(); err != nil {
		t.Fatalf("b.Start: %v", err)
	}
	defer b.Stop()

	// B se connecte à A.
	if err := b.Connect(a.listener.Addr().String()); err != nil {
		t.Fatalf("b.Connect: %v", err)
	}

	// Attendre que les deux peers se voient.
	waitFor(t, "peer count", func() bool {
		return a.PeerCount() >= 1 && b.PeerCount() >= 1
	})

	// A diffuse une tx signée → B doit recevoir.
	kp, _ := crypto.GenerateKeyPair()
	tx := &types.Transaction{
		ChainID:    chainID,
		Type:       types.TxTransfer,
		To:         "cg1111111111111111111111111111111111111111",
		TokenID:    types.NativeToken,
		Amount:     42,
		Nonce:      0,
		MaxBaseFee: 1_000_000,
		Tip:        50_000,
		Timestamp:  1_700_000_000_000,
	}
	tx.SignWith(kp)
	a.Broadcast("tx", tx, nil)
	waitFor(t, "tx received by B", func() bool { return bGotTx.Load() >= 1 })

	// B diffuse un vote signé → A doit recevoir.
	v := &types.Vote{
		ChainID:   chainID,
		Height:    1,
		Kind:      types.PrecommitKind,
		BlockHash: "deadbeef",
	}
	v.SignWith(kp)
	b.Broadcast("vote", v, nil)
	waitFor(t, "vote received by A", func() bool { return aGotVote.Load() >= 1 })
}

// TestFrameTooLargeRejected : un frame qui annonce une taille au-delà de
// maxFrameBytes est rejetée — anti-DoS.
func TestFrameTooLargeRejected(t *testing.T) {
	// On vérifie surtout que writeFrame refuse un payload trop grand
	// (côté lecteur, c'est testé indirectement via maxFrameBytes dans
	// readFrame — un attaquant qui forge l'en-tête est arrêté).
	hugePayload := make([]byte, maxFrameBytes+1)
	err := writeFrame(&nullWriter{}, msgTx, hugePayload)
	if err != errFrameTooLarge {
		t.Fatalf("writeFrame doit rejeter > %d octets, got err=%v", maxFrameBytes, err)
	}
}

type nullWriter struct{}

func (*nullWriter) Write(p []byte) (int, error) { return len(p), nil }

// waitFor : attend qu'une condition devienne vraie, jusqu'à 3 s.
// Évite les sleeps fixes — le test passe dès que c'est prêt.
func waitFor(t *testing.T, what string, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("timeout waiting for: %s", what)
}
