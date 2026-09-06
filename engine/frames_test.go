// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package engine

import "testing"

// TestAmbiguousInstanceTwoFrames pins the one place where the evidence links
// could see less than the old per-instance step log did. Here x is called at
// position 0 from both a and b, and x is internally ambiguous over "aaa" —
// p q splits as ("a", "aa") or ("aa", "a").
//
// The GSS node for x at 0 is shared by both call sites, which carry their
// return slots on its two edges, so x is expanded once and one evidence cell
// per internal descriptor collects every match. That is exactly the pre-link
// rule the walk must reproduce: the competitors for element 1 of x are the
// distinct start positions of every step recorded for that element ending at
// 3, over both call sites — {1, 2}. More than one, so packed counts once, and
// with no #assoc the largest start wins, giving p = "aa" and q = "a".
// top ::= a and top ::= b both derive the whole span, so pick counts packed
// once more and takes the lower production id, the one declared first. Total
// Packed 2. #longest only satisfies the compile-time ambiguity check; nothing
// reads it, so selection is the default rule.
func TestAmbiguousInstanceTwoFrames(t *testing.T) {
	c := mustCompileXBNF(t, `
top -> (a | b) #longest ;
a -> x "!" ;
b -> x "!" ;
x -> p q ;
p -> ("a" | "a" p) #longest ;
q -> ("a" | "a" q) #longest ;
#wrap -> () ;
`)
	res, p := c.run("top", "aaa!")
	if !res.OK {
		t.Fatalf("parse: %s", res.Error)
	}

	nodes, callers := 0, 0
	for _, n := range p.gss {
		if n.nid != c.ntNID["x"] || n.i != 0 {
			continue
		}
		nodes++
		for e := n.ehead; e != 0; e = p.edges[e].next {
			callers++
		}
	}
	if nodes != 1 || callers != 2 {
		t.Fatalf("instance (x, 0) has %d GSS nodes and %d callers, want 1 and 2 — "+
			"the test no longer covers caller merging", nodes, callers)
	}

	if res.Packed != 2 {
		t.Errorf("Packed = %d, want 2: one for top's two productions, one for x's two splits", res.Packed)
	}
	if findNode(res.Tree, "b") != nil {
		t.Error("top derived through b; the production declared first wins")
	}
	if findNode(res.Tree, "a") == nil {
		t.Error("top should derive through a")
	}
	x := findNode(res.Tree, "x")
	if x == nil {
		t.Fatal("no x node in the tree")
	}
	if len(x.Children) != 2 {
		t.Fatalf("x should have p and q, got %d children", len(x.Children))
	}
	if got := x.Children[0].Text; got != "aa" {
		t.Errorf("p spans %q, want \"aa\": the later start must win element 1", got)
	}
	if got := x.Children[1].Text; got != "a" {
		t.Errorf("q spans %q, want \"a\"", got)
	}
}

func findNode(n Node, name string) *Node {
	if n.Name == name {
		return &n
	}
	for _, c := range n.Children {
		if hit := findNode(c, name); hit != nil {
			return hit
		}
	}
	return nil
}
