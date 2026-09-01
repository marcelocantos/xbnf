// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package engine_test

import (
	"strings"
	"testing"

	"github.com/marcelocantos/xbnf/engine"
	"github.com/marcelocantos/xbnf/grammar"
)

func TestPromotionDFAAndGLL(t *testing.T) {
	t.Parallel()
	g := &grammar.Grammar{Stmts: []grammar.Stmt{
		grammar.Rule{Name: "S", Body: grammar.Alt{Terms: []grammar.Term{
			grammar.Seq{Terms: []grammar.Term{
				grammar.String{Text: "("},
				grammar.Ident{Name: "S"},
				grammar.String{Text: ")"},
			}},
			grammar.Ident{Name: "A"},
		}}},
		grammar.Rule{Name: "A", Body: grammar.Quant{
			Term: grammar.CharClass{Elems: []grammar.ClassElem{{Lo: "a", Hi: "z"}}},
			Min:  1,
			Max:  grammar.Unbounded,
		}},
	}}
	c, err := engine.Compile(g)
	if err != nil {
		t.Fatal(err)
	}
	p := c.Promotion()
	if !contains(p.DFA, "A") {
		t.Fatalf("DFA missing A: %v", p.DFA)
	}
	if !contains(p.GLL, "S") {
		t.Fatalf("GLL missing S: %v", p.GLL)
	}
	s := p.String()
	if !strings.Contains(s, "DFA") || !strings.Contains(s, "GLL") {
		t.Fatalf("explain text: %s", s)
	}
	if !strings.Contains(s, "runtime forking") {
		t.Fatalf("want GLL forking: %s", s)
	}
}

func TestParseErrorPositionAndExpected(t *testing.T) {
	t.Parallel()
	g := &grammar.Grammar{Stmts: []grammar.Stmt{
		grammar.Rule{Name: "S", Body: grammar.Alt{Terms: []grammar.Term{
			grammar.String{Text: "x"},
			grammar.String{Text: "("},
		}}},
	}}
	res := engine.Parse(g, "S", "y")
	if res.OK {
		t.Fatal("want fail")
	}
	if !strings.Contains(res.Error, "1:1") {
		t.Fatalf("want position: %s", res.Error)
	}
	if !strings.Contains(res.Error, `"x"`) && !strings.Contains(res.Error, "S") {
		t.Fatalf("want expected terminal or rule: %s", res.Error)
	}
}

func contains(xs []string, w string) bool {
	for _, x := range xs {
		if x == w {
			return true
		}
	}
	return false
}
