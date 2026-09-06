// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package engine

import "testing"

// TestAmbiguousInstanceTwoFrames pins the one place where the evidence links
// could see less than the old per-instance step log did. A production
// instance (pid, l) is shared by every caller, but its GSS frames are per
// return slot: the links belong to the frame that completed the span, while
// the step log merged every frame's matches. Here x is called at position 0
// from both a and b, so instance (x, 0) has two frames, and x is internally
// ambiguous over "aaa" — p q splits as ("a", "aa") or ("aa", "a").
//
// The pre-link rule, which the walk must reproduce: the competitors for
// element 1 of x are the distinct start positions of every step recorded for
// that element ending at 3, over both frames — {1, 2}. More than one, so
// packed counts once, and with no #assoc the largest start wins, giving
// p = "aa" and q = "a". top ::= a and top ::= b both derive the whole span,
// so pick counts packed once more and takes the lower production id, the one
// declared first. Total Packed 2. #longest only satisfies the compile-time
// ambiguity check; nothing reads it, so selection is the default rule.
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

	frames := 0
	for _, n := range p.gss {
		if n.sl.pid < 0 || n.i != 0 || n.sl.ip == 0 {
			continue
		}
		pr := c.prods[n.sl.pid]
		if n.sl.ip > len(pr.rhs) {
			continue
		}
		if e := pr.rhs[n.sl.ip-1]; e.kind == ekNT && e.nt == "x" {
			frames++
		}
	}
	if frames != 2 {
		t.Fatalf("instance (x, 0) has %d GSS frames, want 2 — the test no longer covers frame merging", frames)
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
