// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package engine_test

import (
	"testing"

	"github.com/marcelocantos/xbnf/engine"
	"github.com/marcelocantos/xbnf/syntax"
)

func TestMacroExpand(t *testing.T) {
	t.Parallel()
	g, err := syntax.Parse([]byte(`
#macro hi() { "hi" }
start -> %!hi() ;
`))
	if err != nil {
		t.Fatal(err)
	}
	c, err := engine.Compile(g)
	if err != nil {
		t.Fatal(err)
	}
	if !c.Parse("start", "hi").OK {
		t.Fatal("hi")
	}
	if c.Parse("start", "ho").OK {
		t.Fatal("ho")
	}
}

func TestMacroArgsAndNested(t *testing.T) {
	t.Parallel()
	g, err := syntax.Parse([]byte(`
#macro patternterms(top) {
    "{" top "}"
    |> "[" %!sparse_sequence(top) "]"
}
#macro sparse_sequence(top) { top }
start -> %!patternterms(atom) ;
atom -> "x" ;
#wrap -> () ;
`))
	if err != nil {
		t.Fatal(err)
	}
	c, err := engine.Compile(g)
	if err != nil {
		t.Fatal(err)
	}
	for _, in := range []string{"{x}", "[x]"} {
		if !c.Parse("start", in).OK {
			t.Fatalf("accept %q", in)
		}
	}
	for _, in := range []string{"{y}", "[]", "x"} {
		if c.Parse("start", in).OK {
			t.Fatalf("reject %q", in)
		}
	}
}

func TestExtRefHook(t *testing.T) {
	t.Parallel()
	g, err := syntax.Parse([]byte(`start -> %%lit ;`))
	if err != nil {
		t.Fatal(err)
	}
	opts := &engine.CompileOpts{
		ExtRefs: map[string]engine.ExtRefFunc{
			"lit": func(input string, pos int) (int, bool) {
				const want = "hello"
				if pos+len(want) <= len(input) && input[pos:pos+len(want)] == want {
					return pos + len(want), true
				}
				return pos, false
			},
		},
	}
	c, err := engine.CompileWith(g, opts)
	if err != nil {
		t.Fatal(err)
	}
	if !c.Parse("start", "hello").OK {
		t.Fatal("hello")
	}
	if c.Parse("start", "hell").OK {
		t.Fatal("hell")
	}
}

func TestExtRefMissingHook(t *testing.T) {
	t.Parallel()
	g, err := syntax.Parse([]byte(`start -> %%bind ;`))
	if err != nil {
		t.Fatal(err)
	}
	c, err := engine.Compile(g)
	if err != nil {
		t.Fatal(err)
	}
	if c.Parse("start", "x").OK {
		t.Fatal("missing hook must reject")
	}
}

func TestLetBindDoesNotDropSemi(t *testing.T) {
	t.Parallel()
	g, err := syntax.Parse([]byte(`
start -> "let" ident "=" num %%bind ";" ident |> ident ;
ident -> /[a-z]+/ ;
num -> /[0-9]+/ ;
#wrap -> \s* ;
`))
	if err != nil {
		t.Fatal(err)
	}
	opts := &engine.CompileOpts{
		ExtRefs: map[string]engine.ExtRefFunc{
			"bind": func(_ string, pos int) (int, bool) { return pos, true },
		},
	}
	c, err := engine.CompileWith(g, opts)
	if err != nil {
		t.Fatal(err)
	}
	if c.Parse("start", "let x = 1").OK {
		t.Fatal("let without ;ident must fail")
	}
	if !c.Parse("start", "let x = 1; y").OK {
		t.Fatal("let x = 1; y")
	}
	if !c.Parse("start", "let").OK {
		t.Fatal("ident let")
	}
}

func TestStackSelfWrapsToTop(t *testing.T) {
	t.Parallel()
	g, err := syntax.Parse([]byte(`
expr -> @:op="+" > "(" @ ")" |> num ;
num -> /[0-9]+/ ;
#wrap -> () ;
`))
	if err != nil {
		t.Fatal(err)
	}
	c, err := engine.Compile(g)
	if err != nil {
		t.Fatal(err)
	}
	if !c.Parse("expr", "(1)").OK {
		t.Fatal("(1)")
	}
	if !c.Parse("expr", "(1+2)").OK {
		t.Fatal("(1+2)")
	}
}

func TestParenGroupAfterTuple(t *testing.T) {
	t.Parallel()
	g, err := syntax.Parse([]byte(`
start -> "(" name ":" num ")" |> "(" num ")" ;
name -> /[a-z]+/ ;
num -> /[0-9]+/ ;
#wrap -> () ;
`))
	if err != nil {
		t.Fatal(err)
	}
	c, err := engine.Compile(g)
	if err != nil {
		t.Fatal(err)
	}
	if !c.Parse("start", "(1)").OK {
		t.Fatal("(1) grouping")
	}
	if !c.Parse("start", "(x:1)").OK {
		t.Fatal("(x:1) tuple")
	}
}

func TestImportMerge(t *testing.T) {
	t.Parallel()
	g, err := syntax.Parse([]byte(`
#import "lib.xbnf" ;
start -> word ;
`))
	if err != nil {
		t.Fatal(err)
	}
	opts := &engine.CompileOpts{
		ReadImport: func(path string) ([]byte, error) {
			if path != "lib.xbnf" {
				t.Fatalf("path %s", path)
			}
			return []byte(`word -> "ok" ;`), nil
		},
	}
	c, err := engine.CompileWith(g, opts)
	if err != nil {
		t.Fatal(err)
	}
	if !c.Parse("start", "ok").OK {
		t.Fatal("ok")
	}
	if c.Parse("start", "no").OK {
		t.Fatal("no")
	}
}
