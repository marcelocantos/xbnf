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
	tree := res.Tree()
	if tree.Kind != KindRule || tree.Name != "s" || tree.Text(res.Input) != "a" ||
		tree.Start != 0 || tree.End != 1 || len(tree.Children) != 0 {
		t.Fatalf("partial tree was not retained: %+v", tree)
	}
	original := FingerprintResult(res)
	if original.Nodes != 1 {
		t.Fatalf("partial tree fingerprint has %d nodes, want 1", original.Nodes)
	}
	for _, tc := range []struct {
		name   string
		mutate func([]Event) []Event
	}{
		{"kind", func(ev []Event) []Event { ev[0].Kind = KindLeaf; return ev }},
		{"name", func(ev []Event) []Event { ev[0].Name = "other"; return ev }},
		{"text", func(ev []Event) []Event { ev[0].Len = 2; return ev }},
		{"children", func(ev []Event) []Event {
			return []Event{{Op: OpOpen, Kind: KindRule, Name: "s"}, {Op: OpLeaf, Kind: Kind(99), Len: 1}, {Op: OpClose}}
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			changed := *res
			changed.Events = tc.mutate(append([]Event(nil), res.Events...))
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

// The fingerprint is a function of the decoded tree over the input, not of
// the stream's spelling: a Skip inside a parent and the same bytes as a
// longer leaf decode to different trees, but two streams that decode to the
// same tree hash the same.
func TestFingerprintDecodedTree(t *testing.T) {
	c := compileSrc(t, "s -> \"a\" \"b\";\n#wrap -> \\s*;\n")
	res := c.Parse("s", "a b")
	if !res.OK {
		t.Fatal(res.Error)
	}
	original := FingerprintResult(res)
	same := *res
	same.Events = append([]Event(nil), res.Events...)
	if FingerprintResult(&same).SHA256 != original.SHA256 {
		t.Fatal("a copied stream must fingerprint the same")
	}
	moved := *res
	// Move the wrap from before "b" to after it: the tree changes.
	moved.Events = []Event{
		{Op: OpOpen, Kind: KindRule, Name: "s"},
		{Op: OpLeaf, Kind: KindString, Len: 1},
		{Op: OpLeaf, Kind: KindString, Len: 1},
		{Op: OpSkip, Len: 1},
		{Op: OpClose},
	}
	if FingerprintResult(&moved).SHA256 == original.SHA256 {
		t.Fatal("moving wrap changes node text and must change the fingerprint")
	}
}
