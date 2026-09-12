// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package engine

import (
	"encoding/json"
	"strings"
	"testing"
)

// checkStream asserts the docs/tree-stream.md invariants on res: balanced
// open/close with one root, lengths that tile input[0:End], and a decoded tree
// whose spans nest and whose leaves and skips reproduce the input.
func checkStream(t *testing.T, res *Result) Node {
	t.Helper()
	depth, roots, cur := 0, 0, 0
	var text strings.Builder
	for i, e := range res.Events {
		switch e.Op {
		case OpOpen:
			if depth == 0 {
				roots++
			}
			depth++
		case OpClose:
			depth--
			if depth < 0 {
				t.Fatalf("event %d: close below the root", i)
			}
		case OpLeaf, OpSkip:
			if e.Op == OpLeaf && depth == 0 {
				roots++
			}
			n := int(e.Len)
			if n < 0 || cur+n > len(res.Input) {
				t.Fatalf("event %d: len %d at %d overruns input %d", i, n, cur, len(res.Input))
			}
			text.WriteString(res.Input[cur : cur+n])
			cur += n
		default:
			t.Fatalf("event %d: bad op %d", i, e.Op)
		}
	}
	if depth != 0 || roots != 1 {
		t.Fatalf("stream has depth %d and %d roots after the walk", depth, roots)
	}
	if cur != res.End {
		t.Fatalf("lengths sum to %d, End is %d", cur, res.End)
	}
	if text.String() != res.Input[:res.End] {
		t.Fatalf("leaf and skip text %q != input %q", text.String(), res.Input[:res.End])
	}
	tree := res.Tree()
	var walk func(n Node, plo, phi int)
	walk = func(n Node, plo, phi int) {
		if n.Start < plo || n.End > phi || n.Start > n.End {
			t.Fatalf("%s %s: span [%d,%d) outside parent [%d,%d)", n.Kind, n.Name, n.Start, n.End, plo, phi)
		}
		lo := n.Start
		for _, c := range n.Children {
			if c.Start < lo {
				t.Fatalf("%s %s: child [%d,%d) starts before %d", n.Kind, n.Name, c.Start, c.End, lo)
			}
			walk(c, n.Start, n.End)
			lo = c.End
		}
	}
	walk(tree, 0, res.End)
	return tree
}

func TestStreamTilesInput(t *testing.T) {
	t.Parallel()
	// doc names a grammar under docs/; otherwise src is the grammar.
	for _, tc := range []struct{ name, doc, src, start, input string }{
		{"calc", "docs/examples/calc.xbnf", "", "expr", " 1 + (2*3) - f(x, 1) "},
		{"json", "docs/examples/json.xbnf", "", "json", nestedJSON(4 << 10)},
		{"dfa-start", "", "n -> /[0-9]+/ ;\n#wrap -> \\s* ;", "n", "  42 "},
		{"leading-wrap", "", "s -> \"a\" \"b\" ;\n#wrap -> \\s* ;", "s", "  a  b  "},
		{"partial", "", "s -> \"a\";\n#wrap -> ();\n", "s", "ab"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			c := (*Compiled)(nil)
			if tc.doc != "" {
				c = compileDoc(t, tc.doc)
			} else {
				c = compileSrc(t, tc.src)
			}
			res := c.Parse(tc.start, tc.input)
			if tc.name != "partial" && !res.OK {
				t.Fatal(res.Error)
			}
			checkStream(t, res)
		})
	}
}

func TestStreamWrapIsSkip(t *testing.T) {
	t.Parallel()
	res := compileSrc(t, "s -> \"a\" \"b\" ;\n#wrap -> \\s* ;").Parse("s", "  a  b  ")
	if !res.OK {
		t.Fatal(res.Error)
	}
	want := []Event{
		{Op: OpSkip, Len: 2},
		{Op: OpOpen, Kind: KindRule, Name: "s"},
		{Op: OpLeaf, Kind: KindString, Len: 1},
		{Op: OpSkip, Len: 2},
		{Op: OpLeaf, Kind: KindString, Len: 1},
		{Op: OpClose},
		{Op: OpSkip, Len: 2},
	}
	if len(res.Events) != len(want) {
		t.Fatalf("events %+v, want %+v", res.Events, want)
	}
	for i := range want {
		if res.Events[i] != want[i] {
			t.Fatalf("event %d: %+v, want %+v", i, res.Events[i], want[i])
		}
	}
	tree := checkStream(t, res)
	if tree.Start != 2 || tree.End != 6 || tree.Children[0].Start != 2 || tree.Children[1].Start != 5 {
		t.Fatalf("decoded spans: %+v", tree)
	}
}

func TestStreamDFAStart(t *testing.T) {
	t.Parallel()
	res := compileSrc(t, "n -> /[0-9]+/ ;\n#wrap -> \\s* ;").Parse("n", " 42 ")
	if !res.OK {
		t.Fatal(res.Error)
	}
	want := []Event{{Op: OpSkip, Len: 1}, {Op: OpLeaf, Kind: KindLeaf, Name: "n", Len: 2}, {Op: OpSkip, Len: 1}}
	if len(res.Events) != len(want) {
		t.Fatalf("events %+v, want %+v", res.Events, want)
	}
	for i := range want {
		if res.Events[i] != want[i] {
			t.Fatalf("event %d: %+v, want %+v", i, res.Events[i], want[i])
		}
	}
	tree := checkStream(t, res)
	if tree.Kind != KindLeaf || tree.Name != "n" || tree.Text(res.Input) != "42" {
		t.Fatalf("decoded: %+v", tree)
	}
}

func TestStreamJSON(t *testing.T) {
	t.Parallel()
	res := compileSrc(t, "s -> \"a\" n ;\nn -> /[0-9]+/ ;\n#wrap -> \\s* ;").Parse("s", "a 42")
	if !res.OK {
		t.Fatal(res.Error)
	}
	got, err := json.Marshal(res)
	if err != nil {
		t.Fatal(err)
	}
	const want = `{"ok":true,"end":4,"events":[` +
		`{"op":"open","kind":"rule","name":"s"},` +
		`{"op":"leaf","kind":"string","len":1},` +
		`{"op":"skip","len":1},` +
		`{"op":"leaf","kind":"leaf","len":2,"name":"n"},` +
		`{"op":"close"}]}`
	if string(got) != want {
		t.Fatalf("json:\n got %s\nwant %s", got, want)
	}
	var back Result
	if err := json.Unmarshal(got, &back); err != nil {
		t.Fatal(err)
	}
	back.Input = res.Input
	if FingerprintResult(&back) != FingerprintResult(res) {
		t.Fatalf("round trip changed the tree: %+v", back.Events)
	}
}

func TestDecodeMalformed(t *testing.T) {
	t.Parallel()
	if got := Decode(nil); got.Kind != KindNone || got.Children != nil {
		t.Fatalf("empty stream: %+v", got)
	}
	// A stray close, a negative length and a second root neither panic nor
	// reach the first root.
	got := Decode([]Event{
		{Op: OpClose},
		{Op: OpOpen, Kind: KindRule, Name: "s"},
		{Op: OpLeaf, Kind: KindLeaf, Len: -3},
		{Op: OpLeaf, Kind: KindLeaf, Len: 2},
		{Op: OpClose},
		{Op: OpClose},
		{Op: OpLeaf, Kind: KindLeaf, Name: "second", Len: 1},
	})
	if got.Kind != KindRule || got.Start != 0 || got.End != 2 || len(got.Children) != 2 {
		t.Fatalf("decoded: %+v", got)
	}
	if got.Children[0].End != 0 || got.Children[1].Start != 0 || got.Children[1].End != 2 {
		t.Fatalf("children: %+v", got.Children)
	}
}
