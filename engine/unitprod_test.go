// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package engine

import (
	"strings"
	"testing"

	"github.com/marcelocantos/xbnf/syntax"
)

// sketch writes a tree as name(kid kid …) so a test can pin a shape without
// depending on node text.
func sketch(n Node) string {
	var b strings.Builder
	var walk func(Node)
	walk = func(n Node) {
		name := n.Name
		if name == "" {
			name = n.Kind
		}
		b.WriteString(name)
		if len(n.Children) == 0 {
			return
		}
		b.WriteByte('(')
		for i, k := range n.Children {
			if i > 0 {
				b.WriteByte(' ')
			}
			walk(k)
		}
		b.WriteByte(')')
	}
	walk(n)
	return b.String()
}

// TestUnitProdShortcutShapes covers what the fork/pop shortcut has to get
// right: a chain several links long, a nullable target reached through one,
// an ambiguity resolved below one, and a lookahead over one.
func TestUnitProdShortcutShapes(t *testing.T) {
	const nullable = "s -> a \"z\" ;\na -> b ;\nb -> c* ;\nc -> \"(\" b \")\" ;\n"
	for _, tc := range []struct {
		name, src, start, in, want string
		packed                     int
	}{{
		name:  "chain of four",
		src:   "a -> b ;\nb -> c ;\nc -> d ;\nd -> \"(\" a \")\" | \"x\" ;\n",
		start: "a",
		in:    "(x)",
		want:  "a(b(c(d(string a(b(c(d(string)))) string))))",
	}, {
		name:  "nullable target, empty",
		src:   nullable,
		start: "s",
		in:    "z",
		want:  "s(a(b) string)",
	}, {
		name:  "nullable target, one iteration",
		src:   nullable,
		start: "s",
		in:    "()z",
		want:  "s(a(b(quant(c(string b string)))) string)",
	}, {
		name:   "ambiguity under a chain",
		src:    "s -> m ;\nm -> (a | b) #prefer ;\na -> \"x\" | \"(\" s \")\" ;\nb -> \"x\" | \"[\" s \"]\" ;\n",
		start:  "s",
		in:     "x",
		want:   "s(m(a(string)))",
		packed: 1,
	}, {
		name:  "lookahead over a chain",
		src:   "s -> (?=a) a ;\na -> b ;\nb -> \"(\" b \")\" | \"x\" ;\n",
		start: "s",
		in:    "((x))",
		want:  "s(a(b(string b(string b(string) string) string)))",
	}} {
		t.Run(tc.name, func(t *testing.T) {
			c := compileSrc(t, tc.src)
			res := c.Parse(tc.start, tc.in)
			if !res.OK {
				t.Fatalf("%q rejected: %s", tc.in, res.Error)
			}
			if got := sketch(res.Tree); got != tc.want {
				t.Errorf("tree\n got %s\nwant %s", got, tc.want)
			}
			if res.Packed != tc.packed {
				t.Errorf("Packed = %d, want %d", res.Packed, tc.packed)
			}
			if again := c.Parse(tc.start, tc.in); sketch(again.Tree) != tc.want {
				t.Errorf("second parse on the pooled gll differed: %s", sketch(again.Tree))
			}
		})
	}
}

// TestUnitProdCycleRejected: a cycle of unit productions derives the same
// language at every link, so compile-time disambiguation rejects it before
// the engine can walk it. schedule's mark and pop's mark would terminate one
// anyway; this records why they never have to.
func TestUnitProdCycleRejected(t *testing.T) {
	g, err := syntax.Parse([]byte("a -> b ;\nb -> a | \"(\" a \")\" | \"x\" ;\n"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := Compile(g); err == nil || !strings.Contains(err.Error(), "ambiguous decision") {
		t.Fatalf("unit cycle should be rejected, got %v", err)
	}
}

// TestUnitProdMarked keeps markUnitProds honest: `a ::= b` counts, and a
// right-hand side that resolved to a DFA does not.
func TestUnitProdMarked(t *testing.T) {
	c := compileSrc(t, "a -> b ;\nb -> \"(\" a \")\" | \"x\" ;\nd -> lex ;\nlex -> \"y\"+ ;\n")
	units := 0
	for pid, pr := range c.prods {
		if !c.unitProd[pid] {
			continue
		}
		units++
		if pr.nt != "a" {
			t.Errorf("unexpected unit production %s ::= %v", pr.nt, pr.rhs)
		}
	}
	if units != 1 {
		t.Fatalf("marked %d unit productions, want 1 (a ::= b)", units)
	}
}
