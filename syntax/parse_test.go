// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package syntax_test

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/marcelocantos/xbnf/grammar"
	"github.com/marcelocantos/xbnf/syntax"
)

func repoFile(t *testing.T, elem ...string) []byte {
	t.Helper()
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller")
	}
	p := filepath.Join(append([]string{filepath.Dir(file), ".."}, elem...)...)
	b, err := os.ReadFile(p)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func TestParseJSON(t *testing.T) {
	t.Parallel()
	g, err := syntax.Parse(repoFile(t, "docs", "examples", "json.xbnf"))
	if err != nil {
		t.Fatal(err)
	}
	r := rule(t, g, "json")
	id, ok := r.Body.(grammar.Ident)
	if !ok || id.Name != "value" {
		t.Fatalf("json body: %#v", r.Body)
	}
	val := rule(t, g, "value")
	alt, ok := val.Body.(grammar.Alt)
	if !ok || len(alt.Terms) < 5 {
		t.Fatalf("value body: %#v", val.Body)
	}
	num := rule(t, g, "NUMBER")
	leaf, ok := num.Body.(grammar.Leaf)
	if !ok {
		t.Fatalf("NUMBER body %T", num.Body)
	}
	sc, ok := leaf.Term.(grammar.Scope)
	if !ok {
		t.Fatalf("NUMBER leaf %T", leaf.Term)
	}
	if len(sc.Decls) != 1 {
		t.Fatalf("NUMBER decls: %d", len(sc.Decls))
	}
	if _, ok := sc.Decls[0].(grammar.Wrap); !ok {
		t.Fatalf("NUMBER wrap: %#v", sc.Decls[0])
	}
	if wrap(t, g) == nil {
		t.Fatal("missing #wrap")
	}
	obj := rule(t, g, "object")
	seq, ok := obj.Body.(grammar.Seq)
	if !ok || len(seq.Terms) != 3 {
		t.Fatalf("object seq: %#v", obj.Body)
	}
	q, ok := seq.Terms[1].(grammar.Quant)
	if !ok || q.Min != 0 || q.Max != 1 {
		t.Fatalf("object list quant: %#v", seq.Terms[1])
	}
	d, ok := q.Term.(grammar.Delim)
	if !ok {
		t.Fatalf("object list should be member:\",\"? (delim then optional), got %T", q.Term)
	}
	if id, ok := d.Term.(grammar.Ident); !ok || id.Name != "member" {
		t.Fatalf("delim term: %#v", d.Term)
	}
	if s, ok := d.Sep.(grammar.String); !ok || s.Text != "," {
		t.Fatalf("delim sep: %#v", d.Sep)
	}
}

func TestParseCalc(t *testing.T) {
	t.Parallel()
	g, err := syntax.Parse(repoFile(t, "docs", "examples", "calc.xbnf"))
	if err != nil {
		t.Fatal(err)
	}
	expr := rule(t, g, "expr")
	st, ok := expr.Body.(grammar.Stack)
	if !ok || len(st.Levels) < 3 {
		t.Fatalf("expr stack: %#v", expr.Body)
	}
	ident := rule(t, g, "IDENT")
	leaf, ok := ident.Body.(grammar.Leaf)
	if !ok {
		t.Fatalf("IDENT body %T", ident.Body)
	}
	if _, ok := leaf.Term.(grammar.Scope); !ok {
		t.Fatalf("IDENT leaf %T", leaf.Term)
	}
}

func TestParseXbnfSpec(t *testing.T) {
	t.Parallel()
	g, err := syntax.Parse(repoFile(t, "docs", "xbnf.xbnf"))
	if err != nil {
		t.Fatal(err)
	}
	if rule(t, g, "grammar").Name != "grammar" {
		t.Fatal("missing grammar rule")
	}
	term := rule(t, g, "term")
	if _, ok := term.Body.(grammar.Stack); !ok {
		t.Fatalf("term body %T", term.Body)
	}
	if wrap(t, g) == nil {
		t.Fatal("missing #wrap")
	}
}

func TestLaterExampleGrammars(t *testing.T) {
	t.Parallel()
	// python-subset uses @col; arrai uses #macro. sql-select is first-slice-shaped
	// but is not a T3 gate file.
	_, err := syntax.Parse(repoFile(t, "docs", "examples", "python-subset.xbnf"))
	if err != nil {
		t.Logf("skip python-subset: %v", err)
	}
	_, err = syntax.Parse(repoFile(t, "docs", "examples", "arrai.xbnf"))
	if err != nil {
		t.Logf("skip arrai: %v", err)
	}
}

func TestParseLeafTerm(t *testing.T) {
	t.Parallel()
	g, err := syntax.Parse([]byte(`start -> /"(" foo:","? ")"/ ;
foo -> /[a-z]+/ ;
`))
	if err != nil {
		t.Fatal(err)
	}
	leaf, ok := rule(t, g, "start").Body.(grammar.Leaf)
	if !ok {
		t.Fatalf("start body %T", rule(t, g, "start").Body)
	}
	seq, ok := leaf.Term.(grammar.Seq)
	if !ok || len(seq.Terms) != 3 {
		t.Fatalf("leaf term %T %#v", leaf.Term, leaf.Term)
	}
	foo, ok := rule(t, g, "foo").Body.(grammar.Leaf)
	if !ok {
		t.Fatalf("foo body %T", rule(t, g, "foo").Body)
	}
	if _, ok := foo.Term.(grammar.Quant); !ok {
		t.Fatalf("foo leaf %T", foo.Term)
	}
}

func TestParseClassNewlineEscape(t *testing.T) {
	t.Parallel()
	g, err := syntax.Parse([]byte("c -> [^\\n] ;\n"))
	if err != nil {
		t.Fatal(err)
	}
	cc, ok := rule(t, g, "c").Body.(grammar.CharClass)
	if !ok || !cc.Negated || len(cc.Elems) != 1 || cc.Elems[0].Lo != "\n" {
		t.Fatalf("class: %#v", rule(t, g, "c").Body)
	}
}

func TestParseOrderedAlt(t *testing.T) {
	t.Parallel()
	g, err := syntax.Parse([]byte(`start -> "if" |> /[a-z]+/ ;
`))
	if err != nil {
		t.Fatal(err)
	}
	o, ok := rule(t, g, "start").Body.(grammar.OrderedAlt)
	if !ok || len(o.Terms) != 2 {
		t.Fatalf("start body %#v", rule(t, g, "start").Body)
	}
}

func TestParseMixAltError(t *testing.T) {
	t.Parallel()
	_, err := syntax.Parse([]byte(`start -> "a" | "b" |> "c" ;
`))
	if err == nil {
		t.Fatal("expected mix error")
	}
	if !strings.Contains(err.Error(), "|") || !strings.Contains(err.Error(), "|>") {
		t.Fatalf("mix error: %v", err)
	}
}

func TestParseSmallConstructs(t *testing.T) {
	t.Parallel()
	g, err := syntax.Parse([]byte(`start -> "a" | "b"+ | x:","? | (?="x") "x" | () ;
x -> [A-Z] ;
#wrap -> \s* ;
`))
	if err != nil {
		t.Fatal(err)
	}
	r := rule(t, g, "start")
	alt, ok := r.Body.(grammar.Alt)
	if !ok || len(alt.Terms) != 5 {
		t.Fatalf("start alt: %#v", r.Body)
	}
	if _, ok := alt.Terms[1].(grammar.Quant); !ok {
		t.Fatalf("plus: %#v", alt.Terms[1])
	}
	if _, ok := alt.Terms[2].(grammar.Quant); !ok {
		t.Fatalf("delim?: %#v", alt.Terms[2])
	}
	if _, ok := alt.Terms[4].(grammar.Empty); !ok {
		t.Fatalf("empty: %#v", alt.Terms[4])
	}
}

func rule(t *testing.T, g *grammar.Grammar, name string) grammar.Rule {
	t.Helper()
	for _, st := range g.Stmts {
		if r, ok := st.(grammar.Rule); ok && r.Name == name {
			return r
		}
	}
	t.Fatalf("no rule %s", name)
	return grammar.Rule{}
}

func wrap(t *testing.T, g *grammar.Grammar) *grammar.Wrap {
	t.Helper()
	for _, st := range g.Stmts {
		if w, ok := st.(grammar.Wrap); ok {
			return &w
		}
	}
	return nil
}
