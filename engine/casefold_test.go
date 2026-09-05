// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package engine_test

import (
	"strings"
	"testing"

	"github.com/marcelocantos/xbnf/engine"
	"github.com/marcelocantos/xbnf/syntax"
)

func parseOK(t *testing.T, src, start, input string) {
	t.Helper()
	g, err := syntax.Parse([]byte(src))
	if err != nil {
		t.Fatalf("syntax: %v", err)
	}
	res := engine.Parse(g, start, input)
	if !res.OK {
		t.Fatalf("Parse %q: %s", input, res.Error)
	}
}

func parseFail(t *testing.T, src, start, input string) {
	t.Helper()
	g, err := syntax.Parse([]byte(src))
	if err != nil {
		t.Fatalf("syntax: %v", err)
	}
	res := engine.Parse(g, start, input)
	if res.OK {
		t.Fatalf("Parse %q: want fail", input)
	}
}

func TestCaseFoldString(t *testing.T) {
	t.Parallel()
	src := `s -> (?i:"select") ; #wrap -> () ;`
	parseOK(t, src, "s", "select")
	parseOK(t, src, "s", "SELECT")
	parseOK(t, src, "s", "Select")
	parseFail(t, src, "s", "selec")
}

func TestCaseFoldSequence(t *testing.T) {
	t.Parallel()
	src := `s -> (?i:"select" "from") ; #wrap -> \s* ;`
	parseOK(t, src, "s", "SELECT FROM")
	parseOK(t, src, "s", "select from")
	parseOK(t, src, "s", "Select From")
}

func TestCaseFoldOffNested(t *testing.T) {
	t.Parallel()
	src := `s -> (?i:(?~i:"Ab")) ; #wrap -> () ;`
	parseOK(t, src, "s", "Ab")
	parseFail(t, src, "s", "AB")
	parseFail(t, src, "s", "ab")
}

func TestCaseFoldOffOutsideIsNoop(t *testing.T) {
	t.Parallel()
	src := `s -> (?~i:"Ab") ; #wrap -> () ;`
	parseOK(t, src, "s", "Ab")
	parseFail(t, src, "s", "AB")
}

func TestCaseFoldClass(t *testing.T) {
	t.Parallel()
	src := `s -> (?i:[a-z]+) ; #wrap -> () ;`
	parseOK(t, src, "s", "abc")
	parseOK(t, src, "s", "ABC")
	parseOK(t, src, "s", "AbC")
}

func TestCaseFoldRef(t *testing.T) {
	t.Parallel()
	src := `
kw -> "select" ;
s -> (?i: kw) ;
#wrap -> () ;
`
	parseOK(t, src, "s", "SELECT")
	parseOK(t, src, "s", "select")
}

func TestInvalidUTF8Rejected(t *testing.T) {
	t.Parallel()
	src := `s -> "a" ; #wrap -> () ;`
	g, err := syntax.Parse([]byte(src))
	if err != nil {
		t.Fatal(err)
	}
	res := engine.Parse(g, "s", "a\xff")
	if res.OK {
		t.Fatal("invalid UTF-8 must not parse")
	}
	if !strings.Contains(res.Error, "UTF-8") {
		t.Fatalf("got %q", res.Error)
	}
}

func TestUnknownFlagError(t *testing.T) {
	t.Parallel()
	_, err := syntax.Parse([]byte(`s -> (?x:"a") ;`))
	if err == nil {
		t.Fatal("want unknown flag error")
	}
	if !strings.Contains(err.Error(), "unknown (? flag") {
		t.Fatalf("got %v", err)
	}
}
