// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package engine

import (
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"hash"
)

// Fingerprint summarises a Result for the golden ratchet (🎯T25.1). Two
// results with equal fingerprints have the same acceptance, end position,
// packed count, error text and, node for node, the same public tree.
type Fingerprint struct {
	OK     bool   `json:"ok"`
	End    int    `json:"end"`
	Packed int    `json:"packed"`
	Error  string `json:"error,omitempty"`
	Nodes  int    `json:"nodes"`
	SHA256 string `json:"sha256"`
}

// FingerprintResult hashes the tree in preorder: kind, name and text are
// NUL-terminated, the child count is a uvarint. The encoding is injective for
// strings without NUL, which grammar text cannot contain.
func FingerprintResult(res *Result) Fingerprint {
	h := sha256.New()
	nodes := 0
	if res.OK {
		nodes = hashNode(h, &res.Tree)
	}
	return Fingerprint{
		OK:     res.OK,
		End:    res.End,
		Packed: res.Packed,
		Error:  res.Error,
		Nodes:  nodes,
		SHA256: hex.EncodeToString(h.Sum(nil)),
	}
}

func hashNode(h hash.Hash, n *Node) int {
	var buf [binary.MaxVarintLen64]byte
	h.Write([]byte(n.Kind))
	h.Write(buf[:1])
	h.Write([]byte(n.Name))
	h.Write(buf[:1])
	h.Write([]byte(n.Text))
	h.Write(buf[:1])
	k := binary.PutUvarint(buf[:], uint64(len(n.Children)))
	h.Write(buf[:k])
	count := 1
	for i := range n.Children {
		count += hashNode(h, &n.Children[i])
	}
	return count
}
