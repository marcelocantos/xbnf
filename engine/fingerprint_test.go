// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package engine

import "testing"

func TestFingerprintPartialTree(t *testing.T) {
	c := compileSrc(t, "s -> \"a\";\n#wrap -> ();\n")
	res := c.Parse("s", "ab")
	if res.OK || res.End != 1 || res.Error != "unconsumed input at 1:2 (byte 1)" {
		t.Fatalf("unexpected failed parse: %+v", res)
	}
	if res.Tree.Kind != "rule" || res.Tree.Name != "s" || res.Tree.Text != "a" || len(res.Tree.Children) != 0 {
		t.Fatalf("partial tree was not retained: %+v", res.Tree)
	}
	original := FingerprintResult(res)
	if original.Nodes != 1 {
		t.Fatalf("partial tree fingerprint has %d nodes, want 1", original.Nodes)
	}
	for _, tc := range []struct {
		name   string
		mutate func(*Node)
	}{
		{"kind", func(n *Node) { n.Kind = "literal" }},
		{"name", func(n *Node) { n.Name = "other" }},
		{"text", func(n *Node) { n.Text = "b" }},
		{"children", func(n *Node) { n.Children = []Node{{Kind: "literal", Text: "a"}} }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			changed := *res
			tc.mutate(&changed.Tree)
			got := FingerprintResult(&changed)
			if got.SHA256 == original.SHA256 {
				t.Fatal("partial tree change did not change the fingerprint")
			}
			got.Nodes, got.SHA256 = original.Nodes, original.SHA256
			if got != original {
				t.Fatalf("tree mutation changed result metadata: want %+v, got %+v", original, got)
			}
		})
	}
}

func TestFingerprintFailureWithoutTree(t *testing.T) {
	c := compileSrc(t, "s -> \"a\";\n#wrap -> ();\n")
	res := c.Parse("s", "b")
	if res.OK || res.Tree.Kind != "" || res.Tree.Name != "" || res.Tree.Text != "" || len(res.Tree.Children) != 0 {
		t.Fatalf("expected failure without a partial tree: %+v", res)
	}
	fp := FingerprintResult(res)
	const emptySHA256 = "e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855"
	if fp.Nodes != 0 || fp.SHA256 != emptySHA256 {
		t.Fatalf("absent tree changed its empty fingerprint: %+v", fp)
	}
}
