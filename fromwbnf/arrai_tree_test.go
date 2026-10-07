// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package fromwbnf_test

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/arr-ai/wbnf/ast"
	wparser "github.com/arr-ai/wbnf/parser"
	owbnf "github.com/arr-ai/wbnf/wbnf"

	"github.com/marcelocantos/xbnf/engine"
	"github.com/marcelocantos/xbnf/fromwbnf"
	"github.com/marcelocantos/xbnf/syntax"
)

// arraiStack is arr.ai's expression rule: a precedence stack whose levels
// all surface as "expr" in wbnf's ast.Branch.
const arraiStack = "expr"

// arraiEngines compiles the pinned arr.ai grammar for both engines with the
// two host hooks arr.ai wires: %%bind is zero-width, and %%ast (a macro
// body) consumes up to the closing ":}". arr.ai evaluates both at parse
// time; here they only have to agree on extent.
type arraiEngines struct {
	x    *engine.Compiled
	w    wparser.Parsers
	exts wparser.ExternalRefs
}

func newArraiEngines(t testing.TB) *arraiEngines {
	t.Helper()
	wsrc := testdata(t, "arrai.wbnf")
	xsrc, err := fromwbnf.Convert(wsrc)
	if err != nil {
		t.Fatal(err)
	}
	g, err := syntax.Parse([]byte(xsrc))
	if err != nil {
		t.Fatalf("xbnf parse: %v\n%s", err, xsrc)
	}
	xc, err := engine.CompileWith(g, &engine.CompileOpts{
		ExtRefs: map[string]engine.ExtRefFunc{
			"bind": func(_ string, pos int) (int, bool) { return pos, true },
			"ast": func(input string, pos int) (int, bool) {
				i := strings.Index(input[pos:], ":}")
				if i < 0 {
					return pos, false
				}
				return pos + i, true
			},
		},
	})
	if err != nil {
		t.Fatalf("compile: %v", err)
	}
	wp, err := owbnf.Compile(string(wsrc), nil)
	if err != nil {
		t.Fatal(err)
	}
	exts := wparser.ExternalRefs{
		"bind": func(wparser.Scope, *wparser.Scanner) (wparser.TreeElement, error) {
			return nil, nil
		},
		"ast": func(_ wparser.Scope, s *wparser.Scanner) (wparser.TreeElement, error) {
			i := strings.Index(s.String(), ":}")
			if i < 0 {
				return nil, fmt.Errorf("unterminated macro body")
			}
			var body wparser.Scanner
			s.Eat(i, &body)
			// A Node whose Extra is a Branch is how ast.FromParserNode
			// admits an external ref's result; arr.ai does the same.
			return wparser.Node{Tag: "ast", Extra: ast.Branch{"": ast.One{Node: ast.Leaf(body)}}}, nil
		},
	}
	return &arraiEngines{x: xc, w: wp, exts: exts}
}

// Adapted tree. Both engines project to the same shape, which is what
// arr.ai's compiler reads from ast.Branch: named captures and rule names
// bracket their children, anonymous literals are bare text, the stack
// rule's levels are anonymous groups (shown as "@") and collapse when
// they hold one item, and children are in source order. Cardinality
// (One against Many) is a property of the grammar, not of a parse, so a
// single child and a list of one project the same. "@choice", "@skip" and
// the other bookkeeping keys are dropped; arr.ai does not read them.
type item struct {
	name  string // "" for a literal
	text  string // literal text
	kids  []item
	start int
}

func (it item) String() string {
	if it.name == "" && it.kids == nil {
		return fmt.Sprintf("%q", it.text)
	}
	var b strings.Builder
	b.WriteByte('(')
	b.WriteString(it.name)
	for _, k := range it.kids {
		b.WriteByte(' ')
		b.WriteString(k.String())
	}
	b.WriteByte(')')
	return b.String()
}

func renderItems(items []item) string {
	parts := make([]string, len(items))
	for i, it := range items {
		parts[i] = it.String()
	}
	return strings.Join(parts, " ")
}

func groupIfMulti(items []item) []item {
	if len(items) == 1 {
		return items
	}
	return []item{{name: "@", kids: items, start: itemsStart(items)}}
}

func itemsStart(items []item) int {
	if len(items) == 0 {
		return -1
	}
	return items[0].start
}

func sortItems(items []item) {
	sort.SliceStable(items, func(i, j int) bool { return items[i].start < items[j].start })
}

func projectWbnf(b ast.Branch) []item {
	var items []item
	for key, children := range b {
		if strings.HasPrefix(key, "@") {
			continue
		}
		var nodes []ast.Node
		switch c := children.(type) {
		case ast.One:
			nodes = []ast.Node{c.Node}
		case ast.Many:
			nodes = c
		}
		for _, n := range nodes {
			switch n := n.(type) {
			case ast.Leaf:
				s := wparser.Scanner(n)
				if s.String() == "" {
					continue
				}
				leaf := item{text: s.String(), start: s.Offset()}
				if key == "" {
					items = append(items, leaf)
				} else {
					items = append(items, item{name: key, kids: []item{leaf}, start: leaf.start})
				}
			case ast.Branch:
				kids := projectWbnf(n)
				if len(kids) == 0 {
					continue
				}
				switch {
				case key == arraiStack, key == "ast":
					items = append(items, groupIfMulti(kids)...)
				default:
					items = append(items, item{name: key, kids: kids, start: itemsStart(kids)})
				}
			}
		}
	}
	sortItems(items)
	return items
}

func projectXbnf(n engine.Node, input string) []item {
	// A childless node is a leaf whatever its kind (docs/tree-stream.md).
	if len(n.Children) == 0 {
		text := n.Text(input)
		if text == "" {
			return nil
		}
		leaf := item{text: text, start: n.Start}
		if n.Name == "" {
			return []item{leaf}
		}
		return []item{{name: n.Name, kids: []item{leaf}, start: n.Start}}
	}
	var kids []item
	for _, c := range n.Children {
		kids = append(kids, projectXbnf(c, input)...)
	}
	if len(kids) == 0 {
		return nil
	}
	switch {
	case n.Kind == engine.KindRule && n.Name == arraiStack:
		return groupIfMulti(kids)
	case n.Name != "":
		return []item{{name: n.Name, kids: kids, start: itemsStart(kids)}}
	case n.Kind == engine.KindSeq, n.Kind == engine.KindDelim && isStackLevel(kids):
		// An anonymous seq is a stack level; so is a delim whose separator
		// is one of the stack's operator captures. wbnf gives each level a
		// Branch of its own. Any other anonymous delim (a comma list) is
		// flattened into its parent in ast.Branch, so it is here too.
		return groupIfMulti(kids)
	default:
		return kids
	}
}

// arraiStackOps are the separator captures of arr.ai's infix stack levels.
var arraiStackOps = map[string]bool{"binop": true, "compare": true, "mergeop": true, "rbinop": true}

func isStackLevel(kids []item) bool {
	return len(kids) > 1 && arraiStackOps[kids[1].name]
}

// parseBoth parses input with both engines and returns the two adapted
// trees, or the engine that rejected it.
func (e *arraiEngines) parseBoth(input string) (wbnf, xbnf string, werr error, xres *engine.Result) {
	sc := wparser.NewScanner(input)
	wnode, werr := e.w.ParseWithExternals(wparser.Rule(arraiStack), sc, e.exts)
	if werr == nil && sc.String() != "" {
		werr = fmt.Errorf("wbnf: unconsumed input %q", sc.String())
	}
	if werr == nil {
		wbnf = renderItems(groupIfMulti(projectWbnf(ast.FromParserNode(e.w.Grammar(), wnode))))
	}
	xres = e.x.Parse(arraiStack, input)
	if xres.OK && xres.End == len(input) {
		xbnf = renderItems(projectXbnf(xres.Tree(), input))
	}
	return wbnf, xbnf, werr, xres
}

// TestConvertArraiTree is the tree-level arr.ai oracle (🎯T26 for this
// corpus): the adapted trees must agree, not only accept/reject. The
// probes pin scope-extending forms (a trailing `@` is greedy), regex
// alternation inside a leaf, if/else, and a macro body; the stdlib files
// are the pinned corpus in testdata/arrai/SOURCES.md.
func TestConvertArraiTree(t *testing.T) {
	t.Parallel()
	e := newArraiEngines(t)
	probes := []string{
		`let x = 1; x + x`,
		`\x x + 1`,
		`1 + let x = 2; x * 3`,
		`{1} (<>) {2}`,
		`{1} (<>=) {2}`,
		`a if b else c + 1`,
		`{:m: <a>b</a> :}`,
		`let x = 1; x + x * 2`,
		`1 + 2 * 3`,
		`2 * 3 + 1`,
		`-1 ^ 2`,
		`x => . + 1`,
		`\x x.y(1)`,
		`{1: 2}`,
		`(a: 1, b: 2)`,
		`[1, 2]`,
		"# c\n1",
		`cond {1: 2, _: 3}`,
		`a where . > 1`,
		`a.b?:c + 1`,
		`$"x${1 + 2}y"`,
		`//str.lower("A")`,
		`let rec f = \n n; f(1)`,
	}
	for _, in := range probes {
		t.Run(in, func(t *testing.T) {
			w, x, werr, xres := e.parseBoth(in)
			if werr != nil {
				t.Fatalf("wbnf rejected: %v", werr)
			}
			if !xres.OK || xres.End != len(in) {
				t.Fatalf("xbnf rejected: end=%d/%d %s", xres.End, len(in), xres.Error)
			}
			if w != x {
				t.Errorf("adapted trees differ\n wbnf: %s\n xbnf: %s", w, x)
			}
		})
	}
	files, err := filepath.Glob("testdata/arrai/stdlib/*.arrai")
	if err != nil || len(files) == 0 {
		t.Fatalf("corpus: %v (%d files)", err, len(files))
	}
	for _, f := range files {
		t.Run(filepath.Base(f), func(t *testing.T) {
			src, err := os.ReadFile(f)
			if err != nil {
				t.Fatal(err)
			}
			w, x, werr, xres := e.parseBoth(string(src))
			if werr != nil {
				t.Fatalf("wbnf rejected: %v", werr)
			}
			if !xres.OK || xres.End != len(src) {
				t.Fatalf("xbnf rejected: end=%d/%d %s", xres.End, len(src), xres.Error)
			}
			if w != x {
				t.Errorf("adapted trees differ:\n%s", firstDiff(w, x))
			}
		})
	}
}

func firstDiff(a, b string) string {
	i := 0
	for i < len(a) && i < len(b) && a[i] == b[i] {
		i++
	}
	lo := i - 120
	if lo < 0 {
		lo = 0
	}
	cut := func(s string) string {
		hi := i + 200
		if hi > len(s) {
			hi = len(s)
		}
		return s[lo:hi]
	}
	return fmt.Sprintf(" at byte %d of %d/%d\n wbnf: …%s…\n xbnf: …%s…", i, len(a), len(b), cut(a), cut(b))
}

// BenchmarkArrai times both engines over the pinned stdlib corpus. Phase 0
// of the arr.ai migration asks for xbnf within 10× of wbnf here.
func BenchmarkArrai(b *testing.B) {
	e := newArraiEngines(b)
	files, _ := filepath.Glob("testdata/arrai/stdlib/*.arrai")
	var inputs []string
	for _, f := range files {
		src, err := os.ReadFile(f)
		if err != nil {
			b.Fatal(err)
		}
		inputs = append(inputs, string(src))
	}
	b.Run("xbnf", func(b *testing.B) {
		for i := 0; i < b.N; i++ {
			for _, in := range inputs {
				if res := e.x.Parse(arraiStack, in); !res.OK {
					b.Fatal(res.Error)
				}
			}
		}
	})
	b.Run("wbnf", func(b *testing.B) {
		for i := 0; i < b.N; i++ {
			for _, in := range inputs {
				if _, err := e.w.ParseWithExternals(wparser.Rule(arraiStack), wparser.NewScanner(in), e.exts); err != nil {
					b.Fatal(err)
				}
			}
		}
	})
}
