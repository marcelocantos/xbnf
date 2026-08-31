// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package grammar_test

import (
	"fmt"
	"testing"

	"github.com/marcelocantos/xbnf/grammar"
)

func TestRule(t *testing.T) {
	t.Parallel()
	g := grammar.Grammar{Stmts: []grammar.Stmt{
		grammar.Rule{Name: "json", Body: grammar.Ident{Name: "value"}},
	}}
	r := as[grammar.Rule](t, g.Stmts[0])
	if r.Kind() != grammar.KindRule {
		t.Fatalf("Kind() = %v, want %v", r.Kind(), grammar.KindRule)
	}
	if r.Name != "json" {
		t.Fatalf("Name = %q, want json", r.Name)
	}
	id := as[grammar.Ident](t, r.Body)
	if id.Name != "value" {
		t.Fatalf("Body.Name = %q, want value", id.Name)
	}
	if len(r.Mods) != 0 {
		t.Fatalf("Mods = %v, want empty", r.Mods)
	}
}

func TestModifiersLex(t *testing.T) {
	t.Parallel()
	r := grammar.Rule{
		Name: "IDENT",
		Mods: []string{"lex"},
		Body: grammar.Ident{Name: "body"},
	}
	if r.Kind() != grammar.KindRule {
		t.Fatalf("Kind() = %v, want %v", r.Kind(), grammar.KindRule)
	}
	if len(r.Mods) != 1 || r.Mods[0] != "lex" {
		t.Fatalf("Mods = %v, want [lex]", r.Mods)
	}
}

func TestPragmaWrap(t *testing.T) {
	t.Parallel()
	g := grammar.Grammar{Stmts: []grammar.Stmt{
		grammar.Wrap{Body: grammar.Quant{Term: grammar.Escape{Code: "s"}, Min: 0, Max: grammar.Unbounded}},
	}}
	w := as[grammar.Wrap](t, g.Stmts[0])
	if w.Kind() != grammar.KindWrap {
		t.Fatalf("Kind() = %v, want %v", w.Kind(), grammar.KindWrap)
	}
	q := as[grammar.Quant](t, w.Body)
	esc := as[grammar.Escape](t, q.Term)
	if esc.Code != "s" || q.Min != 0 || q.Max != grammar.Unbounded {
		t.Fatalf("wrap body = \\%s{%d,%d}", esc.Code, q.Min, q.Max)
	}
}

func TestPragmaImport(t *testing.T) {
	t.Parallel()
	g := grammar.Grammar{Stmts: []grammar.Stmt{
		grammar.Import{Path: "lib/json.xbnf"},
	}}
	im := as[grammar.Import](t, g.Stmts[0])
	if im.Kind() != grammar.KindImport {
		t.Fatalf("Kind() = %v, want %v", im.Kind(), grammar.KindImport)
	}
	if im.Path != "lib/json.xbnf" {
		t.Fatalf("Path = %q, want lib/json.xbnf", im.Path)
	}
}

func TestPragmaMacro(t *testing.T) {
	t.Parallel()
	g := grammar.Grammar{Stmts: []grammar.Stmt{
		grammar.Macro{
			Name:   "patternterms",
			Params: []string{"top"},
			Body:   grammar.Ident{Name: "top"},
		},
	}}
	m := as[grammar.Macro](t, g.Stmts[0])
	if m.Kind() != grammar.KindMacro {
		t.Fatalf("Kind() = %v, want %v", m.Kind(), grammar.KindMacro)
	}
	if m.Name != "patternterms" {
		t.Fatalf("Name = %q, want patternterms", m.Name)
	}
	if len(m.Params) != 1 || m.Params[0] != "top" {
		t.Fatalf("Params = %v, want [top]", m.Params)
	}
	if as[grammar.Ident](t, m.Body).Name != "top" {
		t.Fatalf("Body = %v", m.Body)
	}
}

func TestStack(t *testing.T) {
	t.Parallel()
	s := grammar.Stack{Levels: []grammar.Term{
		grammar.Ident{Name: "add"},
		grammar.Ident{Name: "mul"},
		grammar.Ident{Name: "atom"},
	}}
	if s.Kind() != grammar.KindStack {
		t.Fatalf("Kind() = %v, want %v", s.Kind(), grammar.KindStack)
	}
	if len(s.Levels) != 3 {
		t.Fatalf("len(Levels) = %d, want 3", len(s.Levels))
	}
	if as[grammar.Ident](t, s.Levels[1]).Name != "mul" {
		t.Fatalf("Levels[1] = %v", s.Levels[1])
	}
}

func TestAlt(t *testing.T) {
	t.Parallel()
	a := grammar.Alt{Terms: []grammar.Term{
		grammar.Ident{Name: "object"},
		grammar.Ident{Name: "array"},
		grammar.String{Text: "null"},
	}}
	if a.Kind() != grammar.KindAlt {
		t.Fatalf("Kind() = %v, want %v", a.Kind(), grammar.KindAlt)
	}
	if len(a.Terms) != 3 {
		t.Fatalf("len(Terms) = %d, want 3", len(a.Terms))
	}
	if as[grammar.String](t, a.Terms[2]).Text != "null" {
		t.Fatalf("Terms[2] = %v", a.Terms[2])
	}
}

func TestSeq(t *testing.T) {
	t.Parallel()
	s := grammar.Seq{Terms: []grammar.Term{
		grammar.String{Text: "{"},
		grammar.Ident{Name: "member"},
		grammar.String{Text: "}"},
	}}
	if s.Kind() != grammar.KindSeq {
		t.Fatalf("Kind() = %v, want %v", s.Kind(), grammar.KindSeq)
	}
	if len(s.Terms) != 3 {
		t.Fatalf("len(Terms) = %d, want 3", len(s.Terms))
	}
	if len(s.Directives) != 0 {
		t.Fatalf("Directives = %v, want empty", s.Directives)
	}
	if as[grammar.Ident](t, s.Terms[1]).Name != "member" {
		t.Fatalf("Terms[1] = %v", s.Terms[1])
	}
}

func TestNamed(t *testing.T) {
	t.Parallel()
	n := grammar.Named{Name: "alias", Term: grammar.Ident{Name: "IDENT"}}
	if n.Kind() != grammar.KindNamed {
		t.Fatalf("Kind() = %v, want %v", n.Kind(), grammar.KindNamed)
	}
	if n.Name != "alias" {
		t.Fatalf("Name = %q, want alias", n.Name)
	}
	if as[grammar.Ident](t, n.Term).Name != "IDENT" {
		t.Fatalf("Term = %v", n.Term)
	}
}

func TestQuantifiers(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name string
		q    grammar.Quant
		min  int
		max  int
	}{
		{"optional", grammar.Quant{Term: grammar.Ident{Name: "x"}, Min: 0, Max: 1}, 0, 1},
		{"star", grammar.Quant{Term: grammar.Ident{Name: "x"}, Min: 0, Max: grammar.Unbounded}, 0, grammar.Unbounded},
		{"plus", grammar.Quant{Term: grammar.Ident{Name: "x"}, Min: 1, Max: grammar.Unbounded}, 1, grammar.Unbounded},
		{"exact", grammar.Quant{Term: grammar.Ident{Name: "x"}, Min: 4, Max: 4}, 4, 4},
		{"range", grammar.Quant{Term: grammar.Ident{Name: "x"}, Min: 1, Max: 3}, 1, 3},
		{"min_only", grammar.Quant{Term: grammar.Ident{Name: "x"}, Min: 1, Max: grammar.Unbounded}, 1, grammar.Unbounded},
		{"max_only", grammar.Quant{Term: grammar.Ident{Name: "x"}, Min: 0, Max: 3}, 0, 3},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if tc.q.Kind() != grammar.KindQuant {
				t.Fatalf("Kind() = %v, want %v", tc.q.Kind(), grammar.KindQuant)
			}
			if as[grammar.Ident](t, tc.q.Term).Name != "x" {
				t.Fatalf("Term = %v", tc.q.Term)
			}
			if tc.q.Min != tc.min || tc.q.Max != tc.max {
				t.Fatalf("Min,Max = %d,%d want %d,%d", tc.q.Min, tc.q.Max, tc.min, tc.max)
			}
		})
	}
}

func TestDelimitedRepetition(t *testing.T) {
	t.Parallel()
	plain := grammar.Delim{
		Term: grammar.Ident{Name: "member"},
		Sep:  grammar.String{Text: ","},
	}
	if plain.Kind() != grammar.KindDelim {
		t.Fatalf("Kind() = %v, want %v", plain.Kind(), grammar.KindDelim)
	}
	if as[grammar.Ident](t, plain.Term).Name != "member" {
		t.Fatalf("Term = %v", plain.Term)
	}
	if as[grammar.String](t, plain.Sep).Text != "," {
		t.Fatalf("Sep = %v", plain.Sep)
	}
	if plain.Leading || plain.Trailing {
		t.Fatalf("Leading,Trailing = %v,%v want false,false", plain.Leading, plain.Trailing)
	}

	leading := grammar.Delim{Term: grammar.Ident{Name: "x"}, Sep: grammar.String{Text: ","}, Leading: true}
	if !leading.Leading || leading.Trailing {
		t.Fatalf("leading: Leading,Trailing = %v,%v", leading.Leading, leading.Trailing)
	}

	trailing := grammar.Delim{Term: grammar.Ident{Name: "x"}, Sep: grammar.String{Text: ","}, Trailing: true}
	if trailing.Leading || !trailing.Trailing {
		t.Fatalf("trailing: Leading,Trailing = %v,%v", trailing.Leading, trailing.Trailing)
	}

	both := grammar.Delim{Term: grammar.Ident{Name: "x"}, Sep: grammar.String{Text: ","}, Leading: true, Trailing: true}
	if !both.Leading || !both.Trailing {
		t.Fatalf("both: Leading,Trailing = %v,%v", both.Leading, both.Trailing)
	}
}

func TestDirectives(t *testing.T) {
	t.Parallel()
	s := grammar.Seq{
		Terms: []grammar.Term{
			grammar.Delim{
				Term: grammar.Self{},
				Sep:  grammar.Named{Name: "op", Term: grammar.String{Text: "+"}},
			},
		},
		Directives: []grammar.Directive{
			{Name: "assoc", Value: "left"},
			{Name: "prefer"},
			{Name: "priority", Value: "1"},
		},
	}
	if s.Kind() != grammar.KindSeq {
		t.Fatalf("Kind() = %v, want %v", s.Kind(), grammar.KindSeq)
	}
	if len(s.Directives) != 3 {
		t.Fatalf("len(Directives) = %d, want 3", len(s.Directives))
	}
	if s.Directives[0] != (grammar.Directive{Name: "assoc", Value: "left"}) {
		t.Fatalf("Directives[0] = %+v", s.Directives[0])
	}
	if s.Directives[1] != (grammar.Directive{Name: "prefer"}) {
		t.Fatalf("Directives[1] = %+v", s.Directives[1])
	}
	if s.Directives[2] != (grammar.Directive{Name: "priority", Value: "1"}) {
		t.Fatalf("Directives[2] = %+v", s.Directives[2])
	}
}

func TestScope(t *testing.T) {
	t.Parallel()
	sc := grammar.Scope{
		Decls: []grammar.Stmt{
			grammar.Wrap{Body: grammar.Empty{}},
			grammar.Rule{Name: "cc_item", Body: grammar.Ident{Name: "cc_atom"}},
		},
		Term: grammar.Seq{Terms: []grammar.Term{
			grammar.String{Text: "["},
			grammar.Ident{Name: "cc_item"},
			grammar.String{Text: "]"},
		}},
	}
	if sc.Kind() != grammar.KindScope {
		t.Fatalf("Kind() = %v, want %v", sc.Kind(), grammar.KindScope)
	}
	if as[grammar.Wrap](t, sc.Decls[0]).Kind() != grammar.KindWrap {
		t.Fatalf("Decls[0] = %v", sc.Decls[0])
	}
	if as[grammar.Rule](t, sc.Decls[1]).Name != "cc_item" {
		t.Fatalf("Decls[1] = %v", sc.Decls[1])
	}
	if as[grammar.Ident](t, as[grammar.Seq](t, sc.Term).Terms[1]).Name != "cc_item" {
		t.Fatalf("Term = %v", sc.Term)
	}
}

func TestAtoms(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name  string
		term  grammar.Term
		kind  grammar.Kind
		check func(*testing.T, grammar.Term)
	}{
		{
			name: "ident",
			term: grammar.Ident{Name: "value"},
			kind: grammar.KindIdent,
			check: func(t *testing.T, term grammar.Term) {
				id := as[grammar.Ident](t, term)
				if id.Name != "value" || id.Label != "" {
					t.Fatalf("%+v", id)
				}
			},
		},
		{
			name: "ident_label",
			term: grammar.Ident{Name: "tok", Label: "kw_if"},
			kind: grammar.KindIdent,
			check: func(t *testing.T, term grammar.Term) {
				id := as[grammar.Ident](t, term)
				if id.Name != "tok" || id.Label != "kw_if" {
					t.Fatalf("%+v", id)
				}
			},
		},
		{
			name: "string",
			term: grammar.String{Text: "let"},
			kind: grammar.KindString,
			check: func(t *testing.T, term grammar.Term) {
				if as[grammar.String](t, term).Text != "let" {
					t.Fatalf("%v", term)
				}
			},
		},
		{
			name: "char_class",
			term: grammar.CharClass{Elems: []grammar.ClassElem{{Lo: "a", Hi: "z"}, {Lo: "_"}}},
			kind: grammar.KindCharClass,
			check: func(t *testing.T, term grammar.Term) {
				cc := as[grammar.CharClass](t, term)
				if cc.Negated || len(cc.Elems) != 2 || cc.Elems[0] != (grammar.ClassElem{Lo: "a", Hi: "z"}) || cc.Elems[1].Lo != "_" {
					t.Fatalf("%+v", cc)
				}
			},
		},
		{
			name: "char_class_negated",
			term: grammar.CharClass{Negated: true, Elems: []grammar.ClassElem{{Lo: "\n"}}},
			kind: grammar.KindCharClass,
			check: func(t *testing.T, term grammar.Term) {
				cc := as[grammar.CharClass](t, term)
				if !cc.Negated || cc.Elems[0].Lo != "\n" {
					t.Fatalf("%+v", cc)
				}
			},
		},
		{
			name: "escape",
			term: grammar.Escape{Code: "d"},
			kind: grammar.KindEscape,
			check: func(t *testing.T, term grammar.Term) {
				if as[grammar.Escape](t, term).Code != "d" {
					t.Fatalf("%v", term)
				}
			},
		},
		{
			name: "leaf",
			term: grammar.Leaf{Term: grammar.Quant{
				Term: grammar.CharClass{Elems: []grammar.ClassElem{{Lo: "a", Hi: "z"}}},
				Min:  1,
				Max:  grammar.Unbounded,
			}},
			kind: grammar.KindLeaf,
			check: func(t *testing.T, term grammar.Term) {
				q := as[grammar.Quant](t, as[grammar.Leaf](t, term).Term)
				if q.Min != 1 || q.Max != grammar.Unbounded {
					t.Fatalf("%v", term)
				}
			},
		},
		{
			name: "any_char",
			term: grammar.AnyChar{},
			kind: grammar.KindAnyChar,
			check: func(t *testing.T, term grammar.Term) {
				_ = as[grammar.AnyChar](t, term)
			},
		},
		{
			name: "ref",
			term: grammar.Ref{Name: "indent"},
			kind: grammar.KindRef,
			check: func(t *testing.T, term grammar.Term) {
				r := as[grammar.Ref](t, term)
				if r.Name != "indent" || r.Default != "" {
					t.Fatalf("%+v", r)
				}
			},
		},
		{
			name: "ref_default",
			term: grammar.Ref{Name: "tag", Default: "EOF"},
			kind: grammar.KindRef,
			check: func(t *testing.T, term grammar.Term) {
				r := as[grammar.Ref](t, term)
				if r.Name != "tag" || r.Default != "EOF" {
					t.Fatalf("%+v", r)
				}
			},
		},
		{
			name: "extref",
			term: grammar.ExtRef{Name: "bind"},
			kind: grammar.KindExtRef,
			check: func(t *testing.T, term grammar.Term) {
				if as[grammar.ExtRef](t, term).Name != "bind" {
					t.Fatalf("%v", term)
				}
			},
		},
		{
			name: "macro_call",
			term: grammar.MacroCall{Name: "patternterms", Args: []grammar.Term{grammar.Ident{Name: "expr"}}},
			kind: grammar.KindMacroCall,
			check: func(t *testing.T, term grammar.Term) {
				m := as[grammar.MacroCall](t, term)
				if m.Name != "patternterms" || len(m.Args) != 1 || as[grammar.Ident](t, m.Args[0]).Name != "expr" {
					t.Fatalf("%+v", m)
				}
			},
		},
		{
			name: "lookahead",
			term: grammar.Lookahead{Term: grammar.Ident{Name: "KEYWORD"}},
			kind: grammar.KindLookahead,
			check: func(t *testing.T, term grammar.Term) {
				if as[grammar.Ident](t, as[grammar.Lookahead](t, term).Term).Name != "KEYWORD" {
					t.Fatalf("%v", term)
				}
			},
		},
		{
			name: "neg_lookahead",
			term: grammar.NegLookahead{Term: grammar.Ident{Name: "KEYWORD"}},
			kind: grammar.KindNegLookahead,
			check: func(t *testing.T, term grammar.Term) {
				if as[grammar.Ident](t, as[grammar.NegLookahead](t, term).Term).Name != "KEYWORD" {
					t.Fatalf("%v", term)
				}
			},
		},
		{
			name: "self",
			term: grammar.Self{},
			kind: grammar.KindSelf,
			check: func(t *testing.T, term grammar.Term) {
				_ = as[grammar.Self](t, term)
			},
		},
		{
			name: "pos_prop",
			term: grammar.PosProp{Name: "col"},
			kind: grammar.KindPosProp,
			check: func(t *testing.T, term grammar.Term) {
				p := as[grammar.PosProp](t, term)
				if p.Name != "col" || p.Op != "" || p.Arg != "" {
					t.Fatalf("%+v", p)
				}
			},
		},
		{
			name: "pos_prop_cmp",
			term: grammar.PosProp{Name: "col", Op: "=", Arg: "0"},
			kind: grammar.KindPosProp,
			check: func(t *testing.T, term grammar.Term) {
				p := as[grammar.PosProp](t, term)
				if p.Name != "col" || p.Op != "=" || p.Arg != "0" {
					t.Fatalf("%+v", p)
				}
			},
		},
		{
			name: "empty",
			term: grammar.Empty{},
			kind: grammar.KindEmpty,
			check: func(t *testing.T, term grammar.Term) {
				_ = as[grammar.Empty](t, term)
			},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if tc.term.Kind() != tc.kind {
				t.Fatalf("Kind() = %v, want %v", tc.term.Kind(), tc.kind)
			}
			tc.check(t, tc.term)
		})
	}
}

func TestTermKindsDistinct(t *testing.T) {
	t.Parallel()
	terms := []grammar.Term{
		grammar.Stack{},
		grammar.Alt{},
		grammar.Seq{},
		grammar.Named{},
		grammar.Quant{},
		grammar.Delim{},
		grammar.Scope{},
		grammar.Ident{},
		grammar.String{},
		grammar.CharClass{},
		grammar.Escape{},
		grammar.Leaf{},
		grammar.AnyChar{},
		grammar.Ref{},
		grammar.ExtRef{},
		grammar.MacroCall{},
		grammar.Lookahead{},
		grammar.NegLookahead{},
		grammar.Self{},
		grammar.PosProp{},
		grammar.Empty{},
	}
	seen := map[grammar.Kind]string{}
	for _, a := range terms {
		k := a.Kind()
		if k == 0 {
			t.Fatalf("%T: Kind() is zero", a)
		}
		if prev, ok := seen[k]; ok {
			t.Fatalf("Kind %v shared by %T and %s", k, a, prev)
		}
		seen[k] = fmt.Sprintf("%T", a)
	}
	if len(seen) != len(terms) {
		t.Fatalf("got %d distinct kinds, want %d", len(seen), len(terms))
	}
}

func TestNestedStackAltSeqNamedQuant(t *testing.T) {
	t.Parallel()
	// expr -> add=a > b | c{2}  d*
	term := grammar.Stack{Levels: []grammar.Term{
		grammar.Named{Name: "add", Term: grammar.Ident{Name: "a"}},
		grammar.Alt{Terms: []grammar.Term{
			grammar.Ident{Name: "b"},
			grammar.Seq{Terms: []grammar.Term{
				grammar.Quant{Term: grammar.Ident{Name: "c"}, Min: 2, Max: 2},
				grammar.Quant{Term: grammar.Ident{Name: "d"}, Min: 0, Max: grammar.Unbounded},
			}},
		}},
	}}
	if term.Kind() != grammar.KindStack {
		t.Fatalf("Kind() = %v, want %v", term.Kind(), grammar.KindStack)
	}
	named := as[grammar.Named](t, term.Levels[0])
	if named.Name != "add" || as[grammar.Ident](t, named.Term).Name != "a" {
		t.Fatalf("Levels[0] = %+v", named)
	}
	alt := as[grammar.Alt](t, term.Levels[1])
	if as[grammar.Ident](t, alt.Terms[0]).Name != "b" {
		t.Fatalf("alt.Terms[0] = %v", alt.Terms[0])
	}
	seq := as[grammar.Seq](t, alt.Terms[1])
	c := as[grammar.Quant](t, seq.Terms[0])
	if as[grammar.Ident](t, c.Term).Name != "c" || c.Min != 2 || c.Max != 2 {
		t.Fatalf("c = %+v", c)
	}
	d := as[grammar.Quant](t, seq.Terms[1])
	if as[grammar.Ident](t, d.Term).Name != "d" || d.Min != 0 || d.Max != grammar.Unbounded {
		t.Fatalf("d = %+v", d)
	}
}

func TestGroupingIsNesting(t *testing.T) {
	t.Parallel()
	// a (b | c) is Seq of Ident and Alt; there is no Group node.
	term := grammar.Seq{Terms: []grammar.Term{
		grammar.Ident{Name: "a"},
		grammar.Alt{Terms: []grammar.Term{
			grammar.Ident{Name: "b"},
			grammar.Ident{Name: "c"},
		}},
	}}
	if term.Kind() != grammar.KindSeq {
		t.Fatalf("Kind() = %v, want %v", term.Kind(), grammar.KindSeq)
	}
	if as[grammar.Ident](t, term.Terms[0]).Name != "a" {
		t.Fatalf("Terms[0] = %v", term.Terms[0])
	}
	alt := as[grammar.Alt](t, term.Terms[1])
	if as[grammar.Ident](t, alt.Terms[0]).Name != "b" || as[grammar.Ident](t, alt.Terms[1]).Name != "c" {
		t.Fatalf("Alt = %+v", alt)
	}
}

func TestStmtKindsDistinct(t *testing.T) {
	t.Parallel()
	stmts := []grammar.Stmt{
		grammar.Rule{Name: "r", Body: grammar.Empty{}},
		grammar.Wrap{Body: grammar.Empty{}},
		grammar.Import{Path: "p"},
		grammar.Macro{Name: "m", Body: grammar.Empty{}},
	}
	seen := map[grammar.Kind]string{}
	for _, s := range stmts {
		k := s.Kind()
		if prev, ok := seen[k]; ok {
			t.Fatalf("Kind %v shared by %T and %s", k, s, prev)
		}
		seen[k] = fmt.Sprintf("%T", s)
	}
}

func as[T any](t *testing.T, v any) T {
	t.Helper()
	x, ok := v.(T)
	if !ok {
		t.Fatalf("got %T, want %T", v, *new(T))
	}
	return x
}
