// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package engine

import "testing"

func TestColConstraint(t *testing.T) {
	c := mustCompileXBNF(t, `
s -> (@col(=0) "a" "\n")+ ;
#wrap -> [ \t]* ;
`)
	if !c.Parse("s", "a\na\n").OK {
		t.Fatal("col 0 lines should parse")
	}
	if c.Parse("s", " a\n").OK {
		t.Fatal("indented line should fail @col(=0)")
	}

	ind := mustCompileXBNF(t, `
s -> (@col(>0) "a" "\n")+ ;
#wrap -> [ \t]* ;
`)
	res := ind.Parse("s", " a\n  a\n")
	if !res.OK {
		t.Fatalf("indented a should parse @col(>0): %s", res.Error)
	}
	if ind.Parse("s", "a\n").OK {
		t.Fatal("column 0 should fail @col(>0)")
	}
}

func TestCaptureRefSimple(t *testing.T) {
	c := mustCompileXBNF(t, `
s -> n=Name ":" %n ;
Name -> /[A-Za-z]+/ ;
#wrap -> () ;
`)
	ok := c.Parse("s", "ab:ab")
	if !ok.OK {
		t.Fatalf("bound copy: %s", ok.Error)
	}
	if c.Parse("s", "ab:cd").OK {
		t.Fatal("mismatched copy should fail")
	}
}

func TestCaptureRefNestedEmpty(t *testing.T) {
	c := mustCompileXBNF(t, `
elem -> "<" n=Name ">" elem? "</" %n ">" ;
Name -> /[A-Za-z]+/ ;
#wrap -> () ;
`)
	res := c.Parse("elem", "<a><b></b></a>")
	if !res.OK {
		t.Fatalf("nested empty: %s", res.Error)
	}
}

func TestCaptureRefMatchesSameProduction(t *testing.T) {
	c := mustCompileXBNF(t, `
elem -> "<" n=Name ">" (elem | text)* "</" %n ">" ;
Name -> /[A-Za-z]+/ ;
text -> /[^<]+/ ;
#wrap -> \s* ;
`)
	ok := c.Parse("elem", "<a><b>x</b></a>")
	if !ok.OK {
		t.Fatalf("matching tags: %s", ok.Error)
	}
	bad := c.Parse("elem", "<a></b>")
	if bad.OK {
		t.Fatal("mismatched tags should fail")
	}
	nested := c.Parse("elem", "<a><a>z</a></a>")
	if !nested.OK {
		t.Fatalf("nested same name: %s", nested.Error)
	}
}
