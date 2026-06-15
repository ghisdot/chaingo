package p2p

import (
	"bufio"
	"bytes"
	"testing"
)

// Fuzzing du lecteur de frames P2P — un peer hostile envoie des octets
// arbitraires. readFrame ne doit jamais paniquer ni allouer une mémoire
// démesurée (la limite maxFrameBytes est censée le garantir).
//
//   go test ./internal/p2p/ -run=^$ -fuzz=FuzzReadFrame -fuzztime=20s

func FuzzReadFrame(f *testing.F) {
	// Graines : une frame hello valide, des en-têtes tronqués, une frame
	// annonçant une taille énorme.
	f.Add([]byte{msgHello, 0x02, 0x00, 0x00})
	f.Add([]byte{})
	f.Add([]byte{msgTx})                                            // type sans longueur
	f.Add([]byte{msgBlock, 0xff, 0xff, 0xff, 0xff, 0x0f})           // longueur énorme, pas de payload
	f.Add([]byte{0x99, 0x01, 0x41})                                 // type inconnu
	f.Fuzz(func(t *testing.T, data []byte) {
		r := bufio.NewReader(bytes.NewReader(data))
		// On boucle : un flux peut contenir plusieurs frames. On s'arrête
		// à la première erreur (EOF, truncated, too large) — l'important
		// est qu'aucun appel ne panique.
		for i := 0; i < 64; i++ {
			_, payload, err := readFrame(r)
			if err != nil {
				return
			}
			// Garde-fou : si une frame est acceptée, sa taille respecte la limite.
			if len(payload) > maxFrameBytes {
				t.Fatalf("readFrame a accepté un payload de %d > %d", len(payload), maxFrameBytes)
			}
		}
	})
}
