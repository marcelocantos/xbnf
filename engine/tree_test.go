// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package engine_test

import (
	"strings"
	"testing"

	"github.com/marcelocantos/xbnf/engine"
	"github.com/marcelocantos/xbnf/syntax"
)

// render writes a tree compactly: rules as name[...], lists as [...],
// terminals as name:text or text.
func render(n engine.Node) string {
	var b strings.Builder
	var walk func(n engine.Node)
	walk = func(n engine.Node) {
		if len(n.Children) == 0 {
			if n.Name != "" {
				b.WriteString(n.Name + ":")
			}
			b.WriteString(n.Text)
			return
		}
		b.WriteString(n.Name)
		b.WriteByte('[')
		for i, c := range n.Children {
			if i > 0 {
				b.WriteByte(' ')
			}
			walk(c)
		}
		b.WriteByte(']')
	}
	walk(n)
	return b.String()
}

func parseTree(t *testing.T, gsrc, start, input string) *engine.Result {
	t.Helper()
	g, err := syntax.Parse([]byte(gsrc))
	if err != nil {
		t.Fatalf("grammar: %v", err)
	}
	c, err := engine.Compile(g)
	if err != nil {
		t.Fatalf("compile: %v", err)
	}
	return c.Parse(start, input)
}

func wantTree(t *testing.T, gsrc, start, input, want string) *engine.Result {
	t.Helper()
	res := parseTree(t, gsrc, start, input)
	if !res.OK {
		t.Fatalf("%q: %s", input, res.Error)
	}
	if got := render(res.Tree); got != want {
		t.Fatalf("%q:\n got %s\nwant %s", input, got, want)
	}
	return res
}

func TestTreeCalcPrecedence(t *testing.T) {
	t.Parallel()
	src := string(docsFile(t, "docs/examples/calc.xbnf"))
	wantTree(t, src, "expr", "1+2*3", "expr[[NUMBER:1 op:+ [NUMBER:2 op:* NUMBER:3]]]")
	wantTree(t, src, "expr", "(1+2)*3", "expr[[[( expr[[NUMBER:1 op:+ NUMBER:2]] )] op:* NUMBER:3]]")
	wantTree(t, src, "expr", "f(x, 1)", "expr[[IDENT:f ( [[expr[IDENT:x] , expr[NUMBER:1]]] )]]")
	res := wantTree(t, src, "expr", "1-2-3", "expr[[NUMBER:1 op:- NUMBER:2 op:- NUMBER:3]]")
	if res.Packed != 0 {
		t.Fatalf("1-2-3 should not be ambiguous, packed=%d", res.Packed)
	}
}

func TestTreeAssoc(t *testing.T) {
	t.Parallel()
	const left = "E -> E \"-\" E #assoc=left | /[0-9]+/ ;"
	const right = "E -> E \"-\" E #assoc=right | /[0-9]+/ ;"
	const none = "E -> E \"-\" E #assoc=none | /[0-9]+/ ;"
	const outer = "E -> (E \"-\" E | /[0-9]+/) #assoc=left ;"
	res := wantTree(t, left, "E", "1-2-3", "E[E[E[1] - E[2]] - E[3]]")
	if res.Packed != 0 {
		t.Fatalf("assoc=left left packed=%d", res.Packed)
	}
	wantTree(t, right, "E", "1-2-3", "E[E[1] - E[E[2] - E[3]]]")
	wantTree(t, outer, "E", "1-2-3-4", "E[E[E[E[1] - E[2]] - E[3]] - E[4]]")
	if res := parseTree(t, none, "E", "1-2-3"); res.OK || !strings.Contains(res.Error, "assoc=none") {
		t.Fatalf("assoc=none should reject a chain, got ok=%v err=%q", res.OK, res.Error)
	}
	if res := parseTree(t, none, "E", "1-2"); !res.OK {
		t.Fatalf("assoc=none single operator: %s", res.Error)
	}
}

func TestTreePreferAvoidPriority(t *testing.T) {
	t.Parallel()
	const tail = "\nA -> \"x\" | \"(\" A \")\" ;\nB -> \"x\" | \"[\" B \"]\" ;"
	res := wantTree(t, "S -> A #prefer | B ;"+tail, "S", "x", "S[A[x]]")
	if res.Packed != 0 {
		t.Fatalf("prefer left packed=%d", res.Packed)
	}
	wantTree(t, "S -> A | B #prefer ;"+tail, "S", "x", "S[B[x]]")
	wantTree(t, "S -> A | B #avoid ;"+tail, "S", "x", "S[A[x]]")
	wantTree(t, "S -> A #priority=1 | B #priority=2 ;"+tail, "S", "x", "S[B[x]]")
	wantTree(t, "S -> A #priority=2 | B #priority=1 ;"+tail, "S", "x", "S[A[x]]")
	res = wantTree(t, "S -> (A | B) #prefer ;"+tail, "S", "x", "S[A[x]]")
	if res.Packed != 1 {
		t.Fatalf("both preferred should stay ambiguous, packed=%d", res.Packed)
	}
}

func TestTreeQuantDelimNamed(t *testing.T) {
	t.Parallel()
	// The regular spelling goes through the DFA-span walk; the recursive
	// spelling goes through recorded derivations. Both must agree.
	const regular = "s -> \"[\" item:\",\" \"]\" xs=\"z\"* ;\nitem -> k=IDENT (\"=\" v=IDENT)? ;\nIDENT -> /[a-z]+/ ;\n#wrap -> \\s* ;"
	const recursive = "s -> \"[\" item:\",\" \"]\" xs=\"z\"* ;\nitem -> k=IDENT (\"=\" v=IDENT)? | \"(\" item \")\" ;\nIDENT -> /[a-z]+/ ;\n#wrap -> \\s* ;"
	for _, src := range []string{regular, recursive} {
		wantTree(t, src, "s", "[a, b=c] z z", "s[[ [item[k:a] , item[k:b [= v:c]]] ] [xs:z xs:z]]")
		wantTree(t, src, "s", "[a]", "s[[ item[k:a] ]]")
	}
	wantTree(t, recursive, "s", "[(a)]", "s[[ item[( item[k:a] )] ]]")
}

func TestTreeMatchesRecognizer(t *testing.T) {
	t.Parallel()
	// A greedy walk would take a="ab" and then fail on "b"; the derivation
	// recorded by the parse takes a="a".
	src := "s -> a \"b\" ;\na -> (\"a\" | \"a\" x) #prefer ;\nx -> \"b\" | \"(\" x \")\" ;"
	wantTree(t, src, "s", "ab", "s[a[a] b]")
}

func TestSelfHostTreeShape(t *testing.T) {
	t.Parallel()
	src := docsFile(t, "docs/xbnf.xbnf")
	g, err := syntax.Parse(src)
	if err != nil {
		t.Fatal(err)
	}
	res := engine.Parse(g, "grammar", string(src))
	if !res.OK {
		t.Fatal(res.Error)
	}
	if res.Tree.Name != "grammar" || len(res.Tree.Children) != 1 {
		t.Fatalf("want grammar[quant], got %s", render(res.Tree)[:80])
	}
	stmts := 0
	for _, st := range res.Tree.Children[0].Children {
		if st.Name != "stmt" || len(st.Children) != 1 {
			t.Fatalf("want stmt[...], got %+v", st)
		}
		if k := st.Children[0].Name; k == "rule" || k == "pragma" {
			stmts++
		}
	}
	if stmts != len(g.Stmts) {
		t.Fatalf("engine saw %d rules/pragmas, bootstrap parser saw %d", stmts, len(g.Stmts))
	}
}
