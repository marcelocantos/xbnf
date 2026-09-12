// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package engine_test

import (
	"testing"

	"github.com/marcelocantos/xbnf/engine"
	"github.com/marcelocantos/xbnf/syntax"
)

func TestPositionsGLL(t *testing.T) {
	t.Parallel()
	const input = "1-2"
	res := wantTree(t, `E -> E "-" E #assoc=left | /[0-9]+/ ;`, "E", input, "E[E[1] - E[2]]")
	wantSpan(t, input, res.Tree(), 0, 3)
	if len(res.Tree().Children) != 3 {
		t.Fatalf("children: %+v", res.Tree().Children)
	}
	wantSpan(t, input, res.Tree().Children[0], 0, 1)
	wantSpan(t, input, res.Tree().Children[1], 1, 2)
	wantSpan(t, input, res.Tree().Children[2], 2, 3)
	checkSpans(t, input, res.Tree())
}

func TestPositionsDFA(t *testing.T) {
	t.Parallel()
	const input = "42"
	res := parseTree(t, `n -> /[0-9]+/ ;`, "n", input)
	if !res.OK {
		t.Fatal(res.Error)
	}
	wantSpan(t, input, res.Tree(), 0, 2)
	if res.Tree().Kind != engine.KindLeaf || res.Tree().Name != "n" {
		t.Fatalf("dfa leaf: %+v", res.Tree())
	}
	checkSpans(t, input, res.Tree())
}

func TestPositionsWrap(t *testing.T) {
	t.Parallel()
	const src = "s -> \"a\" \"b\" ;\n#wrap -> \\s* ;"
	const input = "a  b"
	res := parseTree(t, src, "s", input)
	if !res.OK {
		t.Fatal(res.Error)
	}
	wantSpan(t, input, res.Tree(), 0, 4)
	if len(res.Tree().Children) != 2 {
		t.Fatalf("children: %+v", res.Tree().Children)
	}
	wantSpan(t, input, res.Tree().Children[0], 0, 1)
	wantSpan(t, input, res.Tree().Children[1], 3, 4)
	checkSpans(t, input, res.Tree())
}

func TestPositionsNested(t *testing.T) {
	t.Parallel()
	src := string(docsFile(t, "docs/examples/calc.xbnf"))
	const input = "1+2*3"
	res := wantTree(t, src, "expr", input, "expr[[NUMBER:1 op:+ [NUMBER:2 op:* NUMBER:3]]]")
	wantSpan(t, input, res.Tree(), 0, 5)
	checkSpans(t, input, res.Tree())
	one := findNamed(t, res, "NUMBER", "1")
	wantSpan(t, input, one, 0, 1)
	two := findNamed(t, res, "NUMBER", "2")
	wantSpan(t, input, two, 2, 3)
	three := findNamed(t, res, "NUMBER", "3")
	wantSpan(t, input, three, 4, 5)
}

func TestPositionsParseAndCompiledParse(t *testing.T) {
	t.Parallel()
	const gsrc = "s -> \"a\" \"b\" ;\n#wrap -> \\s* ;"
	const input = "a b"
	g, err := syntax.Parse([]byte(gsrc))
	if err != nil {
		t.Fatal(err)
	}
	direct := engine.Parse(g, "s", input)
	if !direct.OK {
		t.Fatal(direct.Error)
	}
	c, err := engine.Compile(g)
	if err != nil {
		t.Fatal(err)
	}
	compiled := c.Parse("s", input)
	if !compiled.OK {
		t.Fatal(compiled.Error)
	}
	wantSpan(t, input, direct.Tree(), 0, 3)
	wantSpan(t, input, compiled.Tree(), 0, 3)
	if direct.Tree().Start != compiled.Tree().Start || direct.Tree().End != compiled.Tree().End {
		t.Fatalf("Parse vs Compiled.Parse: %+v vs %+v", direct.Tree(), compiled.Tree())
	}
}

func TestPositionsCaseFoldLiteral(t *testing.T) {
	t.Parallel()
	const input = "ab"
	res := parseTree(t, `n -> (?i:"Ab") ;`, "n", input)
	if !res.OK {
		t.Fatal(res.Error)
	}
	wantSpan(t, input, res.Tree(), 0, 2)
}

func wantSpan(t *testing.T, input string, n engine.Node, start, end int) {
	t.Helper()
	if n.Start != start || n.End != end {
		t.Fatalf("%s %q: start/end = %d/%d, want %d/%d", n.Kind, n.Text(input), n.Start, n.End, start, end)
	}
}

func checkSpans(t *testing.T, input string, n engine.Node) {
	t.Helper()
	var walk func(engine.Node, int, int)
	walk = func(n engine.Node, plo, phi int) {
		if n.Start < plo || n.End > phi || n.Start > n.End || n.Start < 0 || n.End > len(input) {
			t.Fatalf("%s %q: span [%d,%d) outside parent [%d,%d) input %d",
				n.Kind, n.Text(input), n.Start, n.End, plo, phi, len(input))
		}
		for _, c := range n.Children {
			walk(c, n.Start, n.End)
		}
	}
	walk(n, 0, len(input))
}

func findNamed(t *testing.T, res *engine.Result, name, text string) engine.Node {
	t.Helper()
	var found []engine.Node
	var walk func(engine.Node)
	walk = func(n engine.Node) {
		if n.Name == name && n.Text(res.Input) == text {
			found = append(found, n)
		}
		for _, c := range n.Children {
			walk(c)
		}
	}
	walk(res.Tree())
	if len(found) != 1 {
		t.Fatalf("want one %s:%s, got %d", name, text, len(found))
	}
	return found[0]
}

