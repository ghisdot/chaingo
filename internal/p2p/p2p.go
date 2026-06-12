// Package p2p : gossip TCP minimaliste — handshake (hello), diffusion
// des transactions et des blocs, synchronisation par lots.
package p2p

import (
	"encoding/json"
	"log"
	"net"
	"sync"

	"chaingo/internal/types"
)

type Message struct {
	Type string          `json:"type"` // hello | tx | block | get_blocks | blocks
	Data json.RawMessage `json:"data,omitempty"`
}

type Hello struct {
	ChainID string `json:"chain_id"`
	Height  uint64 `json:"height"`
}

type GetBlocks struct {
	From uint64 `json:"from"`
}

const syncBatch = 200

type Handlers struct {
	// OnTx returns true if the tx was new (=> re-gossip).
	OnTx func(*types.Transaction) bool
	// OnBlock returns (accepted, needSync).
	OnBlock func(*types.Block) (bool, bool)
	Height  func() uint64
	Block   func(h uint64) *types.Block
}

type peer struct {
	conn net.Conn
	enc  *json.Encoder
	mu   sync.Mutex
}

func (p *peer) send(m *Message) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.enc.Encode(m)
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
	log.Printf("[p2p] listening on %s", s.addr)
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

func (s *Server) handle(conn net.Conn) {
	p := &peer{conn: conn, enc: json.NewEncoder(conn)}
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

	hello, _ := json.Marshal(Hello{ChainID: s.chainID, Height: s.h.Height()})
	p.send(&Message{Type: "hello", Data: hello})

	dec := json.NewDecoder(conn)
	for {
		var m Message
		if err := dec.Decode(&m); err != nil {
			return
		}
		switch m.Type {
		case "hello":
			var h Hello
			if json.Unmarshal(m.Data, &h) != nil || h.ChainID != s.chainID {
				log.Printf("[p2p] peer %s on wrong chain, dropping", key)
				return
			}
			if h.Height > s.h.Height() {
				s.requestBlocks(p)
			}
		case "tx":
			var tx types.Transaction
			if json.Unmarshal(m.Data, &tx) != nil {
				continue
			}
			if s.h.OnTx(&tx) {
				s.Broadcast("tx", &tx, p)
			}
		case "block":
			var b types.Block
			if json.Unmarshal(m.Data, &b) != nil {
				continue
			}
			accepted, needSync := s.h.OnBlock(&b)
			if accepted {
				s.Broadcast("block", &b, p)
			} else if needSync {
				s.requestBlocks(p)
			}
		case "get_blocks":
			var g GetBlocks
			if json.Unmarshal(m.Data, &g) != nil {
				continue
			}
			top := s.h.Height()
			for h := g.From; h <= top && h < g.From+syncBatch; h++ {
				b := s.h.Block(h)
				if b == nil {
					break
				}
				data, _ := json.Marshal(b)
				if p.send(&Message{Type: "block", Data: data}) != nil {
					return
				}
			}
		}
	}
}

func (s *Server) requestBlocks(p *peer) {
	g, _ := json.Marshal(GetBlocks{From: s.h.Height() + 1})
	p.send(&Message{Type: "get_blocks", Data: g})
}

// Broadcast sends a payload to every connected peer except `except`.
func (s *Server) Broadcast(typ string, payload any, except *peer) {
	data, err := json.Marshal(payload)
	if err != nil {
		return
	}
	m := &Message{Type: typ, Data: data}
	s.mu.Lock()
	targets := make([]*peer, 0, len(s.peers))
	for _, p := range s.peers {
		if p != except {
			targets = append(targets, p)
		}
	}
	s.mu.Unlock()
	for _, p := range targets {
		p.send(m)
	}
}
