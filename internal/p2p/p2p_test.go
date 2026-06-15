package p2p

import (
	"net"
	"sync/atomic"
	"testing"
	"time"

	"chaingo/internal/codec"
	"chaingo/internal/crypto"
	"chaingo/internal/types"
)

// legacyHelloBytes : un hello à l'ANCIEN format (chainID + height, sans la
// version de protocole) — pour vérifier la rétro-décodage en version 0.
func legacyHelloBytes(chainID string, height uint64) []byte {
	e := codec.NewEncoder(64)
	e.WriteString(chainID)
	e.WriteUvarint(height)
	return e.Bytes()
}

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

// TestHelloVersionRoundtrip : la version de protocole survit l'encodage, et un
// hello legacy (sans le champ) se décode en version 0.
func TestHelloVersionRoundtrip(t *testing.T) {
	h := Hello{ChainID: "c", Height: 7, ProtocolVersion: 2}
	bin, _ := h.MarshalBinary()
	var got Hello
	if err := got.UnmarshalBinary(bin); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if got.ProtocolVersion != 2 || got.Height != 7 || got.ChainID != "c" {
		t.Fatalf("roundtrip: %+v", got)
	}
	// Hello legacy : seulement chainID + height (pas de version) → v0.
	e := legacyHelloBytes("c", 7)
	var legacy Hello
	if err := legacy.UnmarshalBinary(e); err != nil {
		t.Fatalf("decode legacy: %v", err)
	}
	if legacy.ProtocolVersion != 0 {
		t.Fatalf("hello legacy doit donner version 0, got %d", legacy.ProtocolVersion)
	}
}

// TestKicksOutdatedPeer : un pair annonçant une version < MinPeerProtocol est
// déconnecté (kické) ; un pair à jour reste connecté.
func TestKicksOutdatedPeer(t *testing.T) {
	chainID := "kick-test"
	noop := Handlers{
		OnTx: func(*types.Transaction) bool { return false }, OnVote: func(*types.Vote) bool { return false },
		OnBlock: func(*types.Block) (bool, bool) { return true, false },
		Height:  func() uint64 { return 0 }, Block: func(uint64) *types.Block { return nil },
	}
	srv := NewServer("127.0.0.1:0", chainID, noop)
	if err := srv.Start(); err != nil {
		t.Fatalf("start: %v", err)
	}
	defer srv.Stop()

	old := MinPeerProtocol
	MinPeerProtocol = 2
	defer func() { MinPeerProtocol = old }()

	dial := func(ver uint32) {
		c, err := net.Dial("tcp", srv.listener.Addr().String())
		if err != nil {
			t.Fatalf("dial: %v", err)
		}
		defer c.Close()
		h := Hello{ChainID: chainID, Height: 0, ProtocolVersion: ver}
		hb, _ := h.MarshalBinary()
		writeFrame(c, msgHello, hb)
		time.Sleep(60 * time.Millisecond)
	}

	// Pair v1 (trop vieux) → kické : le serveur ne doit pas le garder.
	dial(1)
	waitFor(t, "peer v1 kické", func() bool { return srv.PeerCount() == 0 })

	// Pair v2 (à jour) → accepté (reste au moins brièvement).
	c, _ := net.Dial("tcp", srv.listener.Addr().String())
	defer c.Close()
	h := Hello{ChainID: chainID, Height: 0, ProtocolVersion: 2}
	hb, _ := h.MarshalBinary()
	writeFrame(c, msgHello, hb)
	waitFor(t, "peer v2 accepté", func() bool { return srv.PeerCount() >= 1 })
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
