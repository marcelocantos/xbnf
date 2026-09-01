// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package engine_test

import (
	"strings"
	"testing"

	"github.com/marcelocantos/xbnf/engine"
	"github.com/marcelocantos/xbnf/grammar"
)

// recTwin is S -> A | A with A recursive, so the overlapping alt is not in the regular fragment.
func recTwin() *grammar.Grammar {
	return &grammar.Grammar{Stmts: []grammar.Stmt{
		grammar.Rule{Name: "S", Body: grammar.Alt{Terms: []grammar.Term{
			grammar.Ident{Name: "A"},
			grammar.Ident{Name: "A"},
		}}},
		grammar.Rule{Name: "A", Body: grammar.Alt{Terms: []grammar.Term{
			grammar.Seq{Terms: []grammar.Term{
				grammar.String{Text: "("},
				grammar.Ident{Name: "S"},
				grammar.String{Text: ")"},
			}},
			grammar.String{Text: "x"},
		}}},
	}}
}

func recTwinWith(dir grammar.Directive) *grammar.Grammar {
	g := recTwin()
	g.Stmts[0] = grammar.Rule{Name: "S", Body: grammar.Seq{
		Terms:      []grammar.Term{g.Stmts[0].(grammar.Rule).Body},
		Directives: []grammar.Directive{dir},
	}}
	return g
}

func TestAmbiguousAltCompileError(t *testing.T) {
	t.Parallel()
	_, err := engine.Compile(recTwin())
	if err == nil || !strings.Contains(err.Error(), "ambiguous decision") {
		t.Fatalf("want named ambiguous decision, got %v", err)
	}
	if !strings.Contains(err.Error(), "S") {
		t.Fatalf("want rule name: %v", err)
	}
}

func TestRegularDuplicateStringsCompile(t *testing.T) {
	t.Parallel()
	g := &grammar.Grammar{Stmts: []grammar.Stmt{
		grammar.Rule{Name: "S", Body: grammar.Alt{Terms: []grammar.Term{
			grammar.String{Text: "a"},
			grammar.String{Text: "a"},
		}}},
	}}
	if _, err := engine.Compile(g); err != nil {
		t.Fatal(err)
	}
}

func TestDisjointAltCompiles(t *testing.T) {
	t.Parallel()
	g := &grammar.Grammar{Stmts: []grammar.Stmt{
		grammar.Rule{Name: "S", Body: grammar.Alt{Terms: []grammar.Term{
			grammar.String{Text: "a"},
			grammar.String{Text: "b"},
		}}},
	}}
	if _, err := engine.Compile(g); err != nil {
		t.Fatal(err)
	}
}

func TestDisambiguatorsAllowOverlap(t *testing.T) {
	t.Parallel()
	for _, dir := range []grammar.Directive{
		{Name: "prefer"},
		{Name: "avoid"},
		{Name: "assoc", Value: "left"},
		{Name: "priority"},
		{Name: "longest"},
	} {
		dir := dir
		t.Run(dir.Name, func(t *testing.T) {
			t.Parallel()
			if _, err := engine.Compile(recTwinWith(dir)); err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestUnnecessaryDisambiguationWarning(t *testing.T) {
	t.Parallel()
	g := &grammar.Grammar{Stmts: []grammar.Stmt{
		grammar.Rule{Name: "S", Body: grammar.Seq{
			Terms: []grammar.Term{grammar.Alt{Terms: []grammar.Term{
				grammar.String{Text: "a"},
				grammar.String{Text: "b"},
			}}},
			Directives: []grammar.Directive{{Name: "prefer"}},
		}},
	}}
	c, err := engine.Compile(g)
	if err != nil {
		t.Fatal(err)
	}
	if len(c.Warnings) == 0 {
		t.Fatal("want unnecessary disambiguation warning")
	}
	if !strings.Contains(c.Warnings[0], "unnecessary") {
		t.Fatalf("warning: %s", c.Warnings[0])
	}
}

func TestRegularOverlapNoError(t *testing.T) {
	t.Parallel()
	g := &grammar.Grammar{Stmts: []grammar.Stmt{
		grammar.Rule{Name: "A", Body: grammar.Alt{Terms: []grammar.Term{
			grammar.String{Text: "if"},
			grammar.Quant{
				Term: grammar.CharClass{Elems: []grammar.ClassElem{{Lo: "a", Hi: "z"}}},
				Min:  1,
				Max:  grammar.Unbounded,
			},
		}}},
	}}
	if _, err := engine.Compile(g); err != nil {
		t.Fatal(err)
	}
}

func TestOrderedAltNoError(t *testing.T) {
	t.Parallel()
	g := &grammar.Grammar{Stmts: []grammar.Stmt{
		grammar.Rule{Name: "S", Body: grammar.OrderedAlt{Terms: []grammar.Term{
			grammar.String{Text: "if"},
			grammar.String{Text: "if"},
		}}},
	}}
	if _, err := engine.Compile(g); err != nil {
		t.Fatal(err)
	}
}

func TestGroupEmptyLL2(t *testing.T) {
	t.Parallel()
	g := &grammar.Grammar{Stmts: []grammar.Stmt{
		grammar.Rule{Name: "S", Body: grammar.Alt{Terms: []grammar.Term{
			grammar.Seq{Terms: []grammar.Term{
				grammar.String{Text: "("},
				grammar.String{Text: ")"},
			}},
			grammar.Seq{Terms: []grammar.Term{
				grammar.String{Text: "("},
				grammar.String{Text: "x"},
				grammar.String{Text: ")"},
			}},
			grammar.Seq{Terms: []grammar.Term{
				grammar.String{Text: "q"},
				grammar.Ident{Name: "S"},
			}},
		}}},
	}}
	if _, err := engine.Compile(g); err != nil {
		t.Fatal(err)
	}
}

func TestSharedIdentPrefixLL2(t *testing.T) {
	t.Parallel()
	g := &grammar.Grammar{Stmts: []grammar.Stmt{
		grammar.Rule{Name: "S", Body: grammar.Alt{Terms: []grammar.Term{
			grammar.Seq{Terms: []grammar.Term{
				grammar.String{Text: "#"},
				grammar.Ident{Name: "IDENT"},
				grammar.String{Text: "->"},
			}},
			grammar.Seq{Terms: []grammar.Term{
				grammar.String{Text: "#"},
				grammar.Ident{Name: "IDENT"},
				grammar.Ident{Name: "STR"},
			}},
			grammar.Seq{Terms: []grammar.Term{
				grammar.String{Text: "("},
				grammar.Ident{Name: "S"},
				grammar.String{Text: ")"},
			}},
		}}},
		grammar.Rule{Name: "IDENT", Body: grammar.Quant{
			Term: grammar.CharClass{Elems: []grammar.ClassElem{{Lo: "a", Hi: "z"}}},
			Min:  1,
			Max:  grammar.Unbounded,
		}},
		grammar.Rule{Name: "STR", Body: grammar.Seq{Terms: []grammar.Term{
			grammar.String{Text: `"`},
			grammar.String{Text: `"`},
		}}},
	}}
	if _, err := engine.Compile(g); err != nil {
		t.Fatal(err)
	}
}

func TestPrefixAltsInCFGError(t *testing.T) {
	t.Parallel()
	g := &grammar.Grammar{Stmts: []grammar.Stmt{
		grammar.Rule{Name: "S", Body: grammar.Alt{Terms: []grammar.Term{
			grammar.String{Text: "a"},
			grammar.Seq{Terms: []grammar.Term{
				grammar.String{Text: "a"},
				grammar.String{Text: "b"},
			}},
			grammar.Seq{Terms: []grammar.Term{
				grammar.String{Text: "("},
				grammar.Ident{Name: "S"},
				grammar.String{Text: ")"},
			}},
		}}},
	}}
	_, err := engine.Compile(g)
	if err == nil || !strings.Contains(err.Error(), "ambiguous decision") {
		t.Fatalf("want prefix overlap named, got %v", err)
	}
}
