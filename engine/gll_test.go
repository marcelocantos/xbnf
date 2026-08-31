// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package engine_test

import (
	"testing"

	"github.com/marcelocantos/xbnf/engine"
	"github.com/marcelocantos/xbnf/grammar"
)

func TestGLLLeftRecursiveExpr(t *testing.T) {
	t.Parallel()
	g := &grammar.Grammar{Stmts: []grammar.Stmt{
		grammar.Rule{Name: "E", Body: grammar.Alt{Terms: []grammar.Term{
			grammar.Seq{Terms: []grammar.Term{
				grammar.Ident{Name: "E"},
				grammar.String{Text: "+"},
				grammar.Ident{Name: "T"},
			}},
			grammar.Ident{Name: "T"},
		}}},
		grammar.Rule{Name: "T", Body: grammar.String{Text: "1"}},
	}}
	res := engine.Parse(g, "E", "1+1+1")
	if !res.OK {
		t.Fatalf("left-recursive E: %s", res.Error)
	}
	if res.End != 5 {
		t.Fatalf("end=%d want 5", res.End)
	}
}

func TestGLLDirectRecursion(t *testing.T) {
	t.Parallel()
	g := &grammar.Grammar{Stmts: []grammar.Stmt{
		grammar.Rule{Name: "S", Body: grammar.Alt{Terms: []grammar.Term{
			grammar.Seq{Terms: []grammar.Term{
				grammar.String{Text: "("},
				grammar.Ident{Name: "S"},
				grammar.String{Text: ")"},
			}},
			grammar.String{Text: "x"},
		}}},
	}}
	res := engine.Parse(g, "S", "((x))")
	if !res.OK {
		t.Fatalf("direct recursion: %s", res.Error)
	}
}

func TestGLLUnorderedAlt(t *testing.T) {
	t.Parallel()
	mk := func(first, second string) *grammar.Grammar {
		return &grammar.Grammar{Stmts: []grammar.Stmt{
			grammar.Rule{Name: "S", Body: grammar.Alt{Terms: []grammar.Term{
				grammar.String{Text: first},
				grammar.String{Text: second},
			}}},
		}}
	}
	r1 := engine.Parse(mk("a", "b"), "S", "a")
	r2 := engine.Parse(mk("b", "a"), "S", "a")
	if !r1.OK || !r2.OK {
		t.Fatalf("unordered alt: %s / %s", r1.Error, r2.Error)
	}
	if r1.Tree.Text != r2.Tree.Text || r1.Tree.Text != "a" {
		t.Fatalf("alt order changed parse: %q vs %q", r1.Tree.Text, r2.Tree.Text)
	}
}

func TestOrderedAltFirstMatch(t *testing.T) {
	t.Parallel()
	g := &grammar.Grammar{Stmts: []grammar.Stmt{
		grammar.Rule{Name: "S", Body: grammar.OrderedAlt{Terms: []grammar.Term{
			grammar.String{Text: "if"},
			grammar.Leaf{Term: grammar.Quant{
				Term: grammar.CharClass{Elems: []grammar.ClassElem{{Lo: "a", Hi: "z"}}},
				Min:  1,
				Max:  grammar.Unbounded,
			}},
		}}},
	}}
	if res := engine.Parse(g, "S", "if"); !res.OK {
		t.Fatalf("if: %s", res.Error)
	}
	if engine.Parse(g, "S", "ifx").OK {
		t.Fatal(`"if" |> /[a-z]+/ must not take ident on ifx`)
	}
	if res := engine.Parse(g, "S", "foo"); !res.OK {
		t.Fatalf("foo: %s", res.Error)
	}
	u := &grammar.Grammar{Stmts: []grammar.Stmt{
		grammar.Rule{Name: "S", Body: grammar.Alt{Terms: []grammar.Term{
			grammar.String{Text: "if"},
			grammar.Leaf{Term: grammar.Quant{
				Term: grammar.CharClass{Elems: []grammar.ClassElem{{Lo: "a", Hi: "z"}}},
				Min:  1,
				Max:  grammar.Unbounded,
			}},
		}}},
	}}
	if !engine.Parse(u, "S", "ifx").OK {
		t.Fatal(`unordered | should accept ifx as ident`)
	}
}

func TestGLLPackedForest(t *testing.T) {
	t.Parallel()
	g := &grammar.Grammar{Stmts: []grammar.Stmt{
		grammar.Rule{Name: "S", Body: grammar.Alt{Terms: []grammar.Term{
			grammar.Ident{Name: "A"},
			grammar.Ident{Name: "B"},
		}}},
		grammar.Rule{Name: "A", Body: grammar.Alt{Terms: []grammar.Term{
			grammar.String{Text: "x"},
			grammar.Seq{Terms: []grammar.Term{
				grammar.String{Text: "("},
				grammar.Ident{Name: "S"},
				grammar.String{Text: ")"},
			}},
		}}},
		grammar.Rule{Name: "B", Body: grammar.Alt{Terms: []grammar.Term{
			grammar.String{Text: "x"},
			grammar.Seq{Terms: []grammar.Term{
				grammar.String{Text: "["},
				grammar.Ident{Name: "S"},
				grammar.String{Text: "]"},
			}},
		}}},
	}}
	res := engine.Parse(g, "S", "x")
	if !res.OK {
		t.Fatalf("ambiguous S: %s", res.Error)
	}
	if res.Packed == 0 {
		t.Fatal("expected packed SPPF nodes, got silent first-match")
	}
}
