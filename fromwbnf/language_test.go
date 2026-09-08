// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package fromwbnf_test

import (
	"fmt"
	"strings"
	"testing"

	wparser "github.com/arr-ai/wbnf/parser"
	owbnf "github.com/arr-ai/wbnf/wbnf"

	"github.com/marcelocantos/xbnf/engine"
	"github.com/marcelocantos/xbnf/fromwbnf"
	"github.com/marcelocantos/xbnf/syntax"
)

var convertCorpus = []struct {
	file  string
	start string
}{
	{"xml.wbnf", "xml"},
	{"balancedbraces.wbnf", "text"},
	{"wbnf.wbnf", "grammar"},
	{"indents.wbnf", "block"},
}

func TestConvertCompileCorpus(t *testing.T) {
	t.Parallel()
	for _, tc := range convertCorpus {
		src, err := fromwbnf.Convert(testdata(t, tc.file))
		if err != nil {
			t.Fatalf("%s: convert: %v", tc.file, err)
		}
		if strings.Contains(src, " | ") {
			t.Fatalf("%s: unordered | leaked into conversion:\n%s", tc.file, src)
		}
		g, err := syntax.Parse([]byte(src))
		if err != nil {
			t.Fatalf("%s: xbnf parse: %v\n%s", tc.file, err, src)
		}
		if _, err := engine.Compile(g); err != nil {
			t.Fatalf("%s: compile: %v\n%s", tc.file, err, src)
		}
	}
}

// Pinned accept/reject sets. wbnf is the language oracle (not tree shape).
// None of these grammars use #macro; leftovers stay the TestConvertLeftoverKinds set.
func TestConvertLanguageOracle(t *testing.T) {
	t.Parallel()
	cases := map[string][]string{
		"xml.wbnf": {
			`<a/>`,
			`<a></a>`,
			`<a b="c"/>`,
			`<!--x-->`,
			`hello`,
			`<a><b/></a>`,
			`<a></b>`,
			`<a>`,
			`<>`,
			`<a b=c>`,
		},
		"balancedbraces.wbnf": {
			`()`,
			`(foo)`,
			`{[()]}`,
			`"hi"`,
			`(`,
			`(]`,
			`(()`,
			`)`,
		},
		"wbnf.wbnf": {
			`a -> "x";`,
			`a -> b | c;`,
			"// c\na -> x;",
			`a -> ;`,
			`-> x;`,
			`a b;`,
		},
		"indents.wbnf": {
			"\n  print foo",
			"\n  if cond:\n    print foo",
			"print foo",
			"\n  print",
			"\n  if cond",
		},
	}
	for _, tc := range convertCorpus {
		inputs := cases[tc.file]
		if len(inputs) == 0 {
			t.Fatalf("%s: no pinned inputs", tc.file)
		}
		wsrc := testdata(t, tc.file)
		wp, err := owbnf.Compile(string(wsrc), nil)
		if err != nil {
			t.Fatalf("%s: wbnf compile: %v", tc.file, err)
		}
		xsrc, err := fromwbnf.Convert(wsrc)
		if err != nil {
			t.Fatalf("%s: convert: %v", tc.file, err)
		}
		g, err := syntax.Parse([]byte(xsrc))
		if err != nil {
			t.Fatalf("%s: xbnf parse: %v", tc.file, err)
		}
		xc, err := engine.Compile(g)
		if err != nil {
			t.Fatalf("%s: compile: %v", tc.file, err)
		}
		accepted, rejected := 0, 0
		for _, in := range inputs {
			_, werr := wp.Parse(wparser.Rule(tc.start), wparser.NewScanner(in))
			wok := werr == nil
			res := xc.Parse(tc.start, in)
			if res.OK != wok {
				t.Errorf("%s %q: xbnf OK=%v (%s), wbnf OK=%v (%v)",
					tc.file, in, res.OK, res.Error, wok, werr)
			}
			if wok {
				accepted++
				if res.OK {
					checkPublicSpans(t, tc.file, in, res.Tree)
				}
			} else {
				rejected++
			}
		}
		if accepted == 0 || rejected == 0 {
			t.Fatalf("%s: pinned set must include accept and reject (accept=%d reject=%d)",
				tc.file, accepted, rejected)
		}
	}
}

// T33.1: wbnf | is ordered choice. Convert emits |>; accept/reject matches wbnf.
func TestConvertOrderedChoiceOracle(t *testing.T) {
	t.Parallel()
	wsrc := []byte(`s -> "if" | /{\w+};`)
	xsrc, err := fromwbnf.Convert(wsrc)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(xsrc, " | ") {
		t.Fatalf("unordered | leaked:\n%s", xsrc)
	}
	if !strings.Contains(xsrc, "|>") {
		t.Fatalf("want |>:\n%s", xsrc)
	}
	g, err := syntax.Parse([]byte(xsrc))
	if err != nil {
		t.Fatalf("xbnf parse: %v\n%s", err, xsrc)
	}
	xc, err := engine.Compile(g)
	if err != nil {
		t.Fatalf("compile: %v\n%s", err, xsrc)
	}
	wp, err := owbnf.Compile(string(wsrc), nil)
	if err != nil {
		t.Fatal(err)
	}
	for _, in := range []string{"if", "foo", "ifx", "", "if "} {
		_, werr := wp.Parse(wparser.Rule("s"), wparser.NewScanner(in))
		wok := werr == nil
		res := xc.Parse("s", in)
		if res.OK != wok {
			t.Errorf("%q: xbnf OK=%v (%s), wbnf OK=%v (%v)", in, res.OK, res.Error, wok, werr)
		}
	}
}

// T33.5: arrai rec=/{\brec\b} converts and matches as wbnf does.
func TestConvertWordBoundaryOracle(t *testing.T) {
	t.Parallel()
	wsrc := []byte("s -> rec=/{\\brec\\b};")
	xsrc, err := fromwbnf.Convert(wsrc)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(xsrc, `\b`) {
		t.Fatalf("want \\b:\n%s", xsrc)
	}
	wp, err := owbnf.Compile(string(wsrc), nil)
	if err != nil {
		t.Fatal(err)
	}
	g, err := syntax.Parse([]byte(xsrc))
	if err != nil {
		t.Fatalf("xbnf parse: %v\n%s", err, xsrc)
	}
	xc, err := engine.Compile(g)
	if err != nil {
		t.Fatalf("compile: %v\n%s", err, xsrc)
	}
	for _, in := range []string{"rec", "record", "prec", "re", "", "rec ", " rec"} {
		_, werr := wp.Parse(wparser.Rule("s"), wparser.NewScanner(in))
		wok := werr == nil
		res := xc.Parse("s", in)
		if res.OK != wok {
			t.Errorf("%q: xbnf OK=%v (%s), wbnf OK=%v (%v)", in, res.OK, res.Error, wok, werr)
		}
	}
}

func TestConvertMacroOracle(t *testing.T) {
	t.Parallel()
	wsrc := []byte(`
s -> %!patternterms(atom);
atom -> "x";
.macro patternterms(top) {
  "{" top "}"
  | "[" %!sparse_sequence(top) "]"
}
.macro sparse_sequence(top) {
  top
}
`)
	xsrc, err := fromwbnf.Convert(wsrc)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(xsrc, "#macro") || !strings.Contains(xsrc, "%!patternterms") {
		t.Fatalf("macros:\n%s", xsrc)
	}
	wp, err := owbnf.Compile(string(wsrc), nil)
	if err != nil {
		t.Fatal(err)
	}
	g, err := syntax.Parse([]byte(xsrc))
	if err != nil {
		t.Fatalf("xbnf parse: %v\n%s", err, xsrc)
	}
	xc, err := engine.Compile(g)
	if err != nil {
		t.Fatalf("compile: %v\n%s", err, xsrc)
	}
	for _, in := range []string{"{x}", "[x]", "{y}", "[]", "x"} {
		_, werr := wp.Parse(wparser.Rule("s"), wparser.NewScanner(in))
		wok := werr == nil
		res := xc.Parse("s", in)
		if res.OK != wok {
			t.Errorf("%q: xbnf OK=%v (%s), wbnf OK=%v (%v)", in, res.OK, res.Error, wok, werr)
		}
	}
}

func TestConvertArraiOracle(t *testing.T) {
	t.Parallel()
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
			"ast":  func(_ string, pos int) (int, bool) { return pos, false },
		},
	})
	if err != nil {
		t.Fatalf("compile: %v", err)
	}
	// bind is a zero-width host action (arr.ai returns nil, nil).
	wp, err := owbnf.Compile(string(wsrc), nil)
	if err != nil {
		t.Fatal(err)
	}
	exts := wparser.ExternalRefs{
		"bind": func(wparser.Scope, *wparser.Scanner) (wparser.TreeElement, error) {
			return nil, nil
		},
		"ast": func(wparser.Scope, *wparser.Scanner) (wparser.TreeElement, error) {
			return nil, fmt.Errorf("ast unused")
		},
	}
	inputs := []string{
		`1`,
		`1+2`,
		`foo`,
		`"hi"`,
		`{1, 2}`,
		`[1, 2]`,
		`let x = 1; x`,
		`let x = 1`,
		`(1)`,
		`()`,
		`(x:1)`,
		`rec`,
		`1 if 2 else 3`,
		``,
		`+`,
		`let x =`,
		`{`,
		`[`,
		`1 +`,
	}
	accepted, rejected := 0, 0
	for _, in := range inputs {
		_, werr := wp.ParseWithExternals(wparser.Rule("expr"), wparser.NewScanner(in), exts)
		wok := werr == nil
		res := xc.Parse("expr", in)
		if res.OK != wok {
			t.Errorf("%q: xbnf OK=%v (%s), wbnf OK=%v (%v)", in, res.OK, res.Error, wok, werr)
		}
		if wok {
			accepted++
		} else {
			rejected++
		}
	}
	if accepted == 0 || rejected == 0 {
		t.Fatalf("pinned set must include accept and reject (accept=%d reject=%d)", accepted, rejected)
	}
}

func TestConvertImportCompiles(t *testing.T) {
	t.Parallel()
	libW := []byte(`word -> "ok";`)
	mainW := []byte(".import lib.wbnf\ns -> word;\n")
	libX, err := fromwbnf.Convert(libW)
	if err != nil {
		t.Fatal(err)
	}
	mainX, err := fromwbnf.Convert(mainW)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(mainX, `#import "lib.xbnf"`) {
		t.Fatalf("import:\n%s", mainX)
	}
	g, err := syntax.Parse([]byte(mainX))
	if err != nil {
		t.Fatal(err)
	}
	xc, err := engine.CompileWith(g, &engine.CompileOpts{
		ReadImport: func(path string) ([]byte, error) {
			if path != "lib.xbnf" {
				t.Fatalf("path %s", path)
			}
			return []byte(libX), nil
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if !xc.Parse("s", "ok").OK {
		t.Fatal("ok")
	}
}

func checkPublicSpans(t *testing.T, file, input string, n engine.Node) {
	t.Helper()
	var walk func(engine.Node, int, int)
	walk = func(n engine.Node, plo, phi int) {
		if n.Start < plo || n.End > phi || n.Start > n.End || n.Start < 0 || n.End > len(input) {
			t.Fatalf("%s %s %q: span [%d,%d) outside [%d,%d)",
				file, n.Kind, n.Text, n.Start, n.End, plo, phi)
		}
		if n.Kind != "string" && input[n.Start:n.End] != n.Text {
			t.Fatalf("%s %s: text %q != input[%d:%d] %q",
				file, n.Kind, n.Text, n.Start, n.End, input[n.Start:n.End])
		}
		for _, c := range n.Children {
			walk(c, n.Start, n.End)
		}
	}
	walk(n, 0, len(input))
}
