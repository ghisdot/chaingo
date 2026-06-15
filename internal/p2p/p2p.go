// Package p2p : gossip TCP minimaliste — handshake (hello), diffusion
// des transactions et des blocs, synchronisation par lots.
//
// Protocole de transport (tranche 3 du codec binaire) :
//
//   [1 byte type code][uvarint payload_len][payload bytes]
//
// Plus de wrapper JSON. Les payloads tx/block/vote utilisent leur
// MarshalBinary (tranches 1 et 2) — gain mesuré ~26-27 %. Hello et
// GetBlocks sont triviaux et encodés inline avec les primitives codec.
//
// Limite de frame : 16 MB (assez pour un bloc complet, refuse les
// frames énormes — protection DoS).
package p2p

import (
	"bufio"
	"encoding/binary"
	"errors"
	"io"
	"log"
	"net"
	"sync"

	"chaingo/internal/codec"
	"chaingo/internal/types"
)

// Codes de type — un seul byte. Toute valeur inconnue déclenche le drop
// du peer (anti-prompt-injection P2P) car les versions futures
// négocieront via le hello plutôt que d'introduire silencieusement des
// codes mystères.
const (
	msgHello     byte = 0x01
	msgTx        byte = 0x02
	msgBlock     byte = 0x03
	msgVote      byte = 0x04
	msgGetBlocks byte = 0x05
)

const (
	maxFrameBytes = 16 * 1024 * 1024 // 16 MB : tient un bloc plein largement
	syncBatch     = 200
)

var (
	errFrameTooLarge = errors.New("p2p: frame too large")
	errUnknownMsg    = errors.New("p2p: unknown message type")
)

// Hello et GetBlocks : encodage binaire trivial via codec.

type Hello struct {
	ChainID string
	Height  uint64
}

func (h *Hello) MarshalBinary() ([]byte, error) {
	e := codec.NewEncoder(64)
	e.WriteString(h.ChainID)
	e.WriteUvarint(h.Height)
	return e.Bytes(), nil
}

func (h *Hello) UnmarshalBinary(data []byte) error {
	d := codec.NewDecoder(data)
	var err error
	if h.ChainID, err = d.ReadString(); err != nil {
		return err
	}
	if h.Height, err = d.ReadUvarint(); err != nil {
		return err
	}
	return d.MustFinish()
}

type GetBlocks struct {
	From uint64
}

func (g *GetBlocks) MarshalBinary() ([]byte, error) {
	e := codec.NewEncoder(16)
	e.WriteUvarint(g.From)
	return e.Bytes(), nil
}

func (g *GetBlocks) UnmarshalBinary(data []byte) error {
	d := codec.NewDecoder(data)
	var err error
	if g.From, err = d.ReadUvarint(); err != nil {
		return err
	}
	return d.MustFinish()
}

type Handlers struct {
	// OnTx returns true if the tx was new (=> re-gossip).
	OnTx func(*types.Transaction) bool
	// OnBlock returns (accepted, needSync).
	OnBlock func(*types.Block) (bool, bool)
	// OnVote returns true if the precommit was new (=> re-gossip).
	OnVote func(*types.Vote) bool
	Height func() uint64
	Block  func(h uint64) *types.Block
}

type peer struct {
	conn net.Conn
	mu   sync.Mutex
}

func (p *peer) send(typ byte, payload []byte) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	return writeFrame(p.conn, typ, payload)
}

type Server struct {
	addr     string
	chainID  string
	h        Handlers
	mu       sync.Mutex
	peers    map[string]*peer
	listener net.Listener
}

func NewServer(addr, chainID string, h Handlers) *Server {
	return &Server{addr: addr, chainID: chainID, h: h, peers: map[string]*peer{}}
}

func (s *Server) Start() error {
	if s.addr == "" {
		return nil
	}
	ln, err := net.Listen("tcp", s.addr)
	if err != nil {
		return err
	}
	s.listener = ln
	log.Printf("[p2p] listening on %s (binary protocol v2)", s.addr)
	go func() {
		for {
			conn, err := ln.Accept()
			if err != nil {
				return
			}
			go s.handle(conn)
		}
	}()
	return nil
}

func (s *Server) Stop() {
	if s.listener != nil {
		s.listener.Close()
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, p := range s.peers {
		p.conn.Close()
	}
}

func (s *Server) Connect(addr string) error {
	conn, err := net.Dial("tcp", addr)
	if err != nil {
		return err
	}
	log.Printf("[p2p] connected to %s", addr)
	go s.handle(conn)
	return nil
}

func (s *Server) PeerCount() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.peers)
}

// writeFrame écrit [type][uvarint len][payload] sur w.
// Pas de buffering : le Write TCP est déjà la frontière logique.
func writeFrame(w io.Writer, typ byte, payload []byte) error {
	if len(payload) > maxFrameBytes {
		return errFrameTooLarge
	}
	var hdr [1 + binary.MaxVarintLen64]byte
	hdr[0] = typ
	n := binary.PutUvarint(hdr[1:], uint64(len(payload)))
	if _, err := w.Write(hdr[:1+n]); err != nil {
		return err
	}
	_, err := w.Write(payload)
	return err
}

// readFrame lit une frame complète et valide la taille.
func readFrame(r *bufio.Reader) (byte, []byte, error) {
	typ, err := r.ReadByte()
	if err != nil {
		return 0, nil, err
	}
	n, err := binary.ReadUvarint(r)
	if err != nil {
		return 0, nil, err
	}
	if n > maxFrameBytes {
		return 0, nil, errFrameTooLarge
	}
	buf := make([]byte, n)
	if _, err := io.ReadFull(r, buf); err != nil {
		return 0, nil, err
	}
	return typ, buf, nil
}

func (s *Server) handle(conn net.Conn) {
	p := &peer{conn: conn}
	key := conn.RemoteAddr().String()
	s.mu.Lock()
	s.peers[key] = p
	s.mu.Unlock()
	defer func() {
		conn.Close()
		s.mu.Lock()
		delete(s.peers, key)
		s.mu.Unlock()
		log.Printf("[p2p] peer %s disconnected", key)
	}()

	// Envoi du hello dès la connexion.
	hello := Hello{ChainID: s.chainID, Height: s.h.Height()}
	helloBin, _ := hello.MarshalBinary()
	if p.send(msgHello, helloBin) != nil {
		return
	}

	// bufio.Reader : un seul Read syscall pour le header (1 + varint),
	// puis ReadFull pour le payload. Cohérent avec writeFrame.
	r := bufio.NewReaderSize(conn, 64*1024)
	for {
		typ, payload, err := readFrame(r)
		if err != nil {
			if !errors.Is(err, io.EOF) && err != io.ErrUnexpectedEOF {
				log.Printf("[p2p] peer %s: frame error: %v", key, err)
			}
			return
		}
		switch typ {
		case msgHello:
			var h Hello
			if h.UnmarshalBinary(payload) != nil || h.ChainID != s.chainID {
				log.Printf("[p2p] peer %s on wrong chain, dropping", key)
				return
			}
			if h.Height > s.h.Height() {
				s.requestBlocks(p)
			}
		case msgTx:
			tx := &types.Transaction{}
			if tx.UnmarshalBinary(payload) != nil {
				continue
			}
			if s.h.OnTx(tx) {
				s.broadcastBin(typ, payload, p)
			}
		case msgBlock:
			b := &types.Block{}
			if b.UnmarshalBinary(payload) != nil {
				continue
			}
			accepted, needSync := s.h.OnBlock(b)
			if accepted {
				s.broadcastBin(typ, payload, p)
			} else if needSync {
				s.requestBlocks(p)
			}
		case msgVote:
			v := &types.Vote{}
			if v.UnmarshalBinary(payload) != nil {
				continue
			}
			if s.h.OnVote != nil && s.h.OnVote(v) {
				s.broadcastBin(typ, payload, p)
			}
		case msgGetBlocks:
			var g GetBlocks
			if g.UnmarshalBinary(payload) != nil {
				continue
			}
			top := s.h.Height()
			for h := g.From; h <= top && h < g.From+syncBatch; h++ {
				b := s.h.Block(h)
				if b == nil {
					break
				}
				data, err := b.MarshalBinary()
				if err != nil {
					break
				}
				if p.send(msgBlock, data) != nil {
					return
				}
			}
		default:
			log.Printf("[p2p] peer %s: unknown msg type 0x%02x — dropping", key, typ)
			return
		}
	}
}

func (s *Server) requestBlocks(p *peer) {
	g := GetBlocks{From: s.h.Height() + 1}
	data, _ := g.MarshalBinary()
	p.send(msgGetBlocks, data)
}

// Broadcast diffuse un payload (tx, block, vote) à tous les peers sauf
// `except` — l'appelant fait le MarshalBinary une seule fois.
func (s *Server) Broadcast(typ string, payload any, except *peer) {
	var typByte byte
	var data []byte
	var err error
	switch typ {
	case "tx":
		typByte = msgTx
		data, err = payload.(*types.Transaction).MarshalBinary()
	case "block":
		typByte = msgBlock
		data, err = payload.(*types.Block).MarshalBinary()
	case "vote":
		typByte = msgVote
		data, err = payload.(*types.Vote).MarshalBinary()
	default:
		log.Printf("[p2p] Broadcast: type inconnu %q", typ)
		return
	}
	if err != nil {
		log.Printf("[p2p] Broadcast: MarshalBinary %s: %v", typ, err)
		return
	}
	s.broadcastBin(typByte, data, except)
}

// broadcastBin : version interne quand le payload est déjà encodé.
// Évite de re-marshaler le payload qu'on vient de décoder lors d'un
// re-gossip (la frame brute reçue est rediffusée telle quelle).
func (s *Server) broadcastBin(typ byte, payload []byte, except *peer) {
	s.mu.Lock()
	targets := make([]*peer, 0, len(s.peers))
	for _, p := range s.peers {
		if p != except {
			targets = append(targets, p)
		}
	}
	s.mu.Unlock()
	for _, p := range targets {
		p.send(typ, payload)
	}
}
