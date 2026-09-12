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

// FingerprintResult hashes the decoded tree in preorder: kind, name and text
// are NUL-terminated, the child count is a uvarint. The encoding is injective
// for strings without NUL, which grammar text cannot contain. Text is
// input[Start:End], so the hash is the one the tree carried before the event
// stream (docs/tree-stream.md): a stream that decodes to the same tree over
// the same input has the same fingerprint. A failed parse's partial tree is
// included; a result with no events retains the empty fingerprint.
func FingerprintResult(res *Result) Fingerprint {
	h := sha256.New()
	nodes := 0
	if len(res.Events) != 0 {
		tree := res.Tree()
		nodes = hashNode(h, &tree, res.Input)
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

func hashNode(h hash.Hash, n *Node, input string) int {
	var buf [binary.MaxVarintLen64]byte
	h.Write([]byte(n.Kind.String()))
	h.Write(buf[:1])
	h.Write([]byte(n.Name))
	h.Write(buf[:1])
	h.Write([]byte(n.Text(input)))
	h.Write(buf[:1])
	k := binary.PutUvarint(buf[:], uint64(len(n.Children)))
	h.Write(buf[:k])
	count := 1
	for i := range n.Children {
		count += hashNode(h, &n.Children[i], input)
	}
	return count
}
