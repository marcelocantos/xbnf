// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package engine

import (
	"strings"
	"testing"

	"github.com/marcelocantos/xbnf/syntax"
)

func compileXBNF(t *testing.T, src string) (*Compiled, error) {
	t.Helper()
	g, err := syntax.Parse([]byte(src))
	if err != nil {
		t.Fatal(err)
	}
	return Compile(g)
}

func mustCompileXBNF(t *testing.T, src string) *Compiled {
	t.Helper()
	c, err := compileXBNF(t, src)
	if err != nil {
		t.Fatal(err)
	}
	return c
}

func TestUndefinedRefDFA(t *testing.T) {
	_, err := compileXBNF(t, `a -> b "x" ;`)
	if err == nil || !strings.Contains(err.Error(), "b") {
		t.Fatalf("DFA path: want undefined error naming b, got %v", err)
	}
}

func TestUndefinedRefGLL(t *testing.T) {
	_, err := compileXBNF(t, `a -> b "x" | "(" a ")" ;`)
	if err == nil || !strings.Contains(err.Error(), "b") {
		t.Fatalf("GLL path: want undefined error naming b, got %v", err)
	}
}

func TestScopedLocalRegularInCFG(t *testing.T) {
	c := mustCompileXBNF(t, `a -> { NUM -> /[0-9]+/ ; NUM ("+" a)* ; } ;`)
	res := c.Parse("a", "1+23+4")
	if !res.OK {
		t.Fatalf("parse: %s", res.Error)
	}
	if !treeHasName(res.Tree, "NUM") {
		t.Fatalf("tree missing NUM nodes: %+v", res.Tree)
	}
}

func TestInnerAssocSatisfiesAmbiguity(t *testing.T) {
	c, err := compileXBNF(t, `
E -> E "-" E #assoc=left | T ;
T -> /[0-9]+/ ;
`)
	if err != nil {
		t.Fatalf("inner #assoc should satisfy the ambiguity check: %v", err)
	}
	res := c.Parse("E", "1-2-3")
	if !res.OK {
		t.Fatalf("parse: %s", res.Error)
	}
}

func TestDFAQuantInsertsWrap(t *testing.T) {
	c := mustCompileXBNF(t, `
s -> "a"+ ;
#wrap -> \s* ;
`)
	if !c.IsDFA("s") {
		t.Fatal("s should be regular (DFA path)")
	}
	res := c.Parse("s", "a a a")
	if !res.OK {
		t.Fatalf(`DFA "a"+ with wrap should accept "a a a": %s`, res.Error)
	}
}

func TestGLLQuantInsertsWrap(t *testing.T) {
	c := mustCompileXBNF(t, `
s -> a+ ;
a -> "a" | "(" s ")" ;
#wrap -> \s* ;
`)
	if c.IsDFA("s") {
		t.Fatal("s should be GLL")
	}
	res := c.Parse("s", "a a a")
	if !res.OK {
		t.Fatalf(`GLL a+ with wrap should accept "a a a": %s`, res.Error)
	}
}

func TestDelimWrapAndOptionalTrailing(t *testing.T) {
	dfa := mustCompileXBNF(t, `
s -> "a":"," ;
#wrap -> \s* ;
`)
	gll := mustCompileXBNF(t, `
s -> a:"," ;
a -> "a" | "[" s "]" ;
#wrap -> \s* ;
`)
	for _, in := range []string{"a", "a,a", "a , a"} {
		d := dfa.Parse("s", in)
		g := gll.Parse("s", in)
		if !d.OK {
			t.Fatalf("DFA delim %q: %s", in, d.Error)
		}
		if !g.OK {
			t.Fatalf("GLL delim %q: %s", in, g.Error)
		}
	}
	// Without ':?', a trailing separator is rejected on both paths.
	for _, in := range []string{"a,", "a,a,"} {
		d := dfa.Parse("s", in)
		g := gll.Parse("s", in)
		if d.OK || g.OK {
			t.Fatalf("trailing delim without ?: %q DFA OK=%v GLL OK=%v", in, d.OK, g.OK)
		}
	}

	optDFA := mustCompileXBNF(t, `
s -> "a":",", ;
#wrap -> \s* ;
`)
	optGLL := mustCompileXBNF(t, `
s -> a:",", ;
a -> "a" | "[" s "]" ;
#wrap -> \s* ;
`)
	for _, in := range []string{"a", "a,", "a,a", "a,a,"} {
		d := optDFA.Parse("s", in)
		g := optGLL.Parse("s", in)
		if !d.OK {
			t.Fatalf("DFA delim? %q: %s", in, d.Error)
		}
		if !g.OK {
			t.Fatalf("GLL delim? %q: %s", in, g.Error)
		}
	}
}

func treeHasName(n Node, name string) bool {
	if n.Name == name {
		return true
	}
	for _, c := range n.Children {
		if treeHasName(c, name) {
			return true
		}
	}
	return false
}
