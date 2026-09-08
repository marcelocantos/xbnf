// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package engine_test

import (
	"testing"

	"github.com/marcelocantos/xbnf/engine"
	"github.com/marcelocantos/xbnf/syntax"
)

func TestWordBoundary(t *testing.T) {
	t.Parallel()
	g, err := syntax.Parse([]byte(`s -> \b "rec" \b ;`))
	if err != nil {
		t.Fatal(err)
	}
	c, err := engine.Compile(g)
	if err != nil {
		t.Fatal(err)
	}
	if !c.Parse("s", "rec").OK {
		t.Fatal("rec")
	}
	if c.Parse("s", "record").OK {
		t.Fatal("record")
	}
	if c.Parse("s", "prec").OK {
		t.Fatal("prec")
	}
	if c.Parse("s", "re").OK {
		t.Fatal("re")
	}
}

func TestLeafOrderedAltNowrap(t *testing.T) {
	t.Parallel()
	g, err := syntax.Parse([]byte(`
s -> /("." |> [a-z][a-z0-9]*)/ ;
#wrap -> \s* ;
`))
	if err != nil {
		t.Fatal(err)
	}
	c, err := engine.Compile(g)
	if err != nil {
		t.Fatal(err)
	}
	if c.Parse("s", "let x").OK {
		t.Fatal("leaf must not insert #wrap between class atoms")
	}
	if !c.Parse("s", "let").OK {
		t.Fatal("let")
	}
}

func TestNegWordBoundary(t *testing.T) {
	t.Parallel()
	g, err := syntax.Parse([]byte(`s -> "x" \B "y" ;`))
	if err != nil {
		t.Fatal(err)
	}
	c, err := engine.Compile(g)
	if err != nil {
		t.Fatal(err)
	}
	if !c.Parse("s", "xy").OK {
		t.Fatal("xy is not a boundary")
	}
	if c.Parse("s", "x y").OK {
		t.Fatal("x y is a boundary")
	}
}
