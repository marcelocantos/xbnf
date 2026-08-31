// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package engine_test

import (
	"strings"
	"testing"

	"github.com/marcelocantos/xbnf/engine"
	"github.com/marcelocantos/xbnf/grammar"
)

func digitPlus() grammar.Term {
	return grammar.Quant{
		Term: grammar.CharClass{Elems: []grammar.ClassElem{{Lo: "0", Hi: "9"}}},
		Min:  1,
		Max:  grammar.Unbounded,
	}
}

func TestDFARegularPromoted(t *testing.T) {
	t.Parallel()
	g := &grammar.Grammar{Stmts: []grammar.Stmt{
		grammar.Rule{Name: "N", Body: digitPlus()},
	}}
	c, err := engine.Compile(g)
	if err != nil {
		t.Fatal(err)
	}
	if !c.IsDFA("N") {
		t.Fatal("regular N should be a DFA")
	}
	res := c.Parse("N", "908")
	if !res.OK {
		t.Fatalf("DFA parse: %s", res.Error)
	}
}

func TestDFARecursiveNotPromoted(t *testing.T) {
	t.Parallel()
	g := leftRecGrammar(false)
	c, err := engine.Compile(g)
	if err != nil {
		t.Fatal(err)
	}
	if c.IsDFA("E") {
		t.Fatal("recursive E must not be a DFA")
	}
	if !c.IsDFA("T") {
		t.Fatal("terminal T should be a DFA")
	}
}

func TestLexNonRegularError(t *testing.T) {
	t.Parallel()
	g := leftRecGrammar(true)
	_, err := engine.Compile(g)
	if err == nil {
		t.Fatal("expected #lex error")
	}
	if !strings.Contains(strings.ToLower(err.Error()), "lex") {
		t.Fatalf("error should mention lex: %v", err)
	}
}

func TestNonRegularWithoutLexStillParses(t *testing.T) {
	t.Parallel()
	g := leftRecGrammar(false)
	res := engine.Parse(g, "E", "1+1")
	if !res.OK {
		t.Fatalf("GLL should parse recursive E: %s", res.Error)
	}
}

func tokAlts() grammar.Term {
	return grammar.Alt{Terms: []grammar.Term{
		grammar.Named{Name: "kw", Term: grammar.String{Text: "if"}},
		grammar.Named{Name: "id", Term: grammar.Quant{
			Term: grammar.CharClass{Elems: []grammar.ClassElem{{Lo: "a", Hi: "z"}}},
			Min:  1,
			Max:  grammar.Unbounded,
		}},
	}}
}

func TestLabelLongestMatch(t *testing.T) {
	t.Parallel()
	g := &grammar.Grammar{Stmts: []grammar.Stmt{
		grammar.Rule{Name: "S", Body: grammar.Ident{Name: "tok", Label: "kw"}},
		grammar.Rule{Name: "tok", Body: tokAlts()},
	}}
	c, err := engine.Compile(g)
	if err != nil {
		t.Fatal(err)
	}
	if !c.IsDFA("tok") {
		t.Fatal("labelled tok should be a DFA")
	}
	win := engine.Parse(g, "S", "if")
	if !win.OK {
		t.Fatalf("tok::kw on if: %s", win.Error)
	}
	lose := engine.Parse(g, "S", "ifx")
	if lose.OK {
		t.Fatal("tok::kw must lose to longer ident ifx")
	}
	idGram := &grammar.Grammar{Stmts: []grammar.Stmt{
		grammar.Rule{Name: "S", Body: grammar.Ident{Name: "tok", Label: "id"}},
		grammar.Rule{Name: "tok", Body: tokAlts()},
	}}
	idWin := engine.Parse(idGram, "S", "ifx")
	if !idWin.OK {
		t.Fatalf("tok::id on ifx: %s", idWin.Error)
	}
	// Compiling only the kw alternative would wrongly accept tok::kw "x" on "ifx".
	seq := &grammar.Grammar{Stmts: []grammar.Stmt{
		grammar.Rule{Name: "S", Body: grammar.Seq{Terms: []grammar.Term{
			grammar.Ident{Name: "tok", Label: "kw"},
			grammar.String{Text: "x"},
		}}},
		grammar.Rule{Name: "tok", Body: tokAlts()},
	}}
	if engine.Parse(seq, "S", "ifx").OK {
		t.Fatal("tok::kw then x must not steal prefix if from longer ident ifx")
	}
	bang := &grammar.Grammar{Stmts: []grammar.Stmt{
		grammar.Rule{Name: "S", Body: grammar.Seq{Terms: []grammar.Term{
			grammar.Ident{Name: "tok", Label: "kw"},
			grammar.String{Text: "!"},
		}}},
		grammar.Rule{Name: "tok", Body: tokAlts()},
	}}
	if res := engine.Parse(bang, "S", "if!"); !res.OK {
		t.Fatalf("tok::kw then ! on if!: %s", res.Error)
	}
}

func leftRecGrammar(lex bool) *grammar.Grammar {
	mods := []string(nil)
	if lex {
		mods = []string{"lex"}
	}
	return &grammar.Grammar{Stmts: []grammar.Stmt{
		grammar.Rule{Name: "E", Mods: mods, Body: grammar.Alt{Terms: []grammar.Term{
			grammar.Seq{Terms: []grammar.Term{
				grammar.Ident{Name: "E"},
				grammar.String{Text: "+"},
				grammar.Ident{Name: "T"},
			}},
			grammar.Ident{Name: "T"},
		}}},
		grammar.Rule{Name: "T", Body: grammar.String{Text: "1"}},
	}}
}
