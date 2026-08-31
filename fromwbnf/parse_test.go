// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package fromwbnf_test

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/marcelocantos/xbnf/fromwbnf"
)

func testdata(t *testing.T, name string) []byte {
	t.Helper()
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller")
	}
	b, err := os.ReadFile(filepath.Join(filepath.Dir(file), "testdata", name))
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func mustParse(t *testing.T, src []byte) *fromwbnf.File {
	t.Helper()
	f, err := fromwbnf.Parse(src)
	if err != nil {
		t.Fatal(err)
	}
	return f
}

func prod(t *testing.T, f *fromwbnf.File, name string) fromwbnf.Term {
	t.Helper()
	for _, st := range f.Stmts {
		if p, ok := st.(fromwbnf.Prod); ok && p.Name == name {
			return p.Body
		}
	}
	t.Fatalf("no prod %s", name)
	return nil
}

func TestParseXML(t *testing.T) {
	t.Parallel()
	f := mustParse(t, testdata(t, "xml.wbnf"))
	xml := prod(t, f, "xml")
	o, ok := xml.(fromwbnf.Oneof)
	if !ok || len(o) != 4 {
		t.Fatalf("xml: %#v", xml)
	}
	if _, ok := prod(t, f, ".wrapRE").(fromwbnf.RE); !ok {
		t.Fatalf("wrapRE: %#v", prod(t, f, ".wrapRE"))
	}
	if _, ok := prod(t, f, "NAME").(fromwbnf.RE); !ok {
		t.Fatalf("NAME: %#v", prod(t, f, "NAME"))
	}
	want := strings.TrimSpace(`
xml = oneof(seq("<", NAME, {*}(attr), "/>"), seq("<", tag=NAME, {*}(attr), ">", {*}(xml), "</", %tag, ">"), CDATA=re([^<]+), COMMENT)
attr = seq(NAME, "=", value=re("[^"]*"))
NAME = re([A-Za-z_:][-A-Za-z0-9._:]*)
COMMENT = re(<!--.*-->)
.wrapRE = re(\s*()\s*)
`)
	if got := fromwbnf.Dump(f); got != want {
		t.Fatalf("xml dump\n got: %s\nwant: %s", got, want)
	}
}

func TestParseBalancedBraces(t *testing.T) {
	t.Parallel()
	f := mustParse(t, testdata(t, "balancedbraces.wbnf"))
	text := prod(t, f, "text")
	o, ok := text.(fromwbnf.Oneof)
	if !ok || len(o) != 6 {
		t.Fatalf("text: %#v", text)
	}
	want := `text = oneof(seq("(", {?}(text), ")", {*}(text)), seq("{", {?}(text), "}", {*}(text)), seq("[", {?}(text), "]", {*}(text)), seq("<", {?}(text), ">", {*}(text)), seq("\"", re([^"]*), "\"", {*}(text)), re(\w+))`
	if got := fromwbnf.Dump(f); got != want {
		t.Fatalf("balanced dump\n got: %s\nwant: %s", got, want)
	}
}

func TestParseWbnfSelf(t *testing.T) {
	t.Parallel()
	f := mustParse(t, testdata(t, "wbnf.wbnf"))
	for _, name := range []string{"grammar", "stmt", "prod", "term", "atom", "IDENT", "RE", "STR", ".wrapRE"} {
		if prod(t, f, name) == nil {
			t.Fatalf("missing %s", name)
		}
	}
	if _, ok := prod(t, f, "atom").(fromwbnf.Oneof); !ok {
		t.Fatalf("atom: %#v", prod(t, f, "atom"))
	}
	term := prod(t, f, "term")
	st, ok := term.(fromwbnf.Stack)
	if !ok || len(st) != 4 {
		t.Fatalf("term: %#v", term)
	}

	if g, ok := prod(t, f, "grammar").(fromwbnf.Quant); !ok || g.Min != 1 || g.Max != 0 {
		t.Fatalf("grammar: %#v", prod(t, f, "grammar"))
	}
	if _, ok := prod(t, f, "stmt").(fromwbnf.Oneof); !ok {
		t.Fatalf("stmt: %#v", prod(t, f, "stmt"))
	}

	if got, want := fromwbnf.Dump(&fromwbnf.File{Stmts: []fromwbnf.Stmt{mustProd(t, f, "IDENT")}}),
		`IDENT = re(@|\.?[A-Za-z_]\w*)`; got != want {
		t.Fatalf("IDENT\n got: %s\nwant: %s", got, want)
	}
	if got, want := fromwbnf.Dump(&fromwbnf.File{Stmts: []fromwbnf.Stmt{mustProd(t, f, "INT")}}),
		`INT = re(\d+)`; got != want {
		t.Fatalf("INT\n got: %s\nwant: %s", got, want)
	}
	if got, want := fromwbnf.Dump(&fromwbnf.File{Stmts: []fromwbnf.Stmt{mustProd(t, f, ".wrapRE")}}),
		`.wrapRE = re(\s*()\s*)`; got != want {
		t.Fatalf("wrapRE\n got: %s\nwant: %s", got, want)
	}
	if got, want := fromwbnf.Dump(&fromwbnf.File{Stmts: []fromwbnf.Stmt{mustProd(t, f, "COMMENT")}}),
		`COMMENT = re(//.*$|(?s:/\*(?:[^*]|\*+[^*/])\*/))`; got != want {
		t.Fatalf("COMMENT\n got: %s\nwant: %s", got, want)
	}

	pragma := prod(t, f, "pragma")
	sc, ok := pragma.(fromwbnf.Scoped)
	if !ok {
		t.Fatalf("pragma: %#v", pragma)
	}
	if _, ok := sc.Term.(fromwbnf.Oneof); !ok {
		t.Fatalf("pragma term: %#v", sc.Term)
	}
	if prod(t, sc.Body, "import") == nil || prod(t, sc.Body, "macrodef") == nil {
		t.Fatalf("pragma scoped: %s", fromwbnf.Dump(sc.Body))
	}

	wantRE := `RE = re(/{(?:\\.|{(?:(?:\d+(?:,\d*)?|,\d+)\})?|\[(?:\\.|\[:^?[a-z]+:\]|[^\]])+]|[^\\{\}])*\}|(?:(?:\[(?:\\.|\[:^?[a-z]+:\]|[^\]])+]|\\[pP](?:[a-z]|\{[a-zA-Z_]+\})|\\[a-zA-Z]|[.^$])(?:(?:[+*?]|\{\d+,?\d?\})\??)?)+)`
	if got := fromwbnf.Dump(&fromwbnf.File{Stmts: []fromwbnf.Stmt{mustProd(t, f, "RE")}}); got != wantRE {
		t.Fatalf("RE\n got: %s\nwant: %s", got, wantRE)
	}

	wantTerm := `term = stack(delim[:](seq(@, {?}(seq("{", grammar, "}"))), op=">"), delim[:](@, op="|"), {+}(@), seq(named, {*}(quant)))`
	if got := fromwbnf.Dump(&fromwbnf.File{Stmts: []fromwbnf.Stmt{mustProd(t, f, "term")}}); got != wantTerm {
		t.Fatalf("term\n got: %s\nwant: %s", got, wantTerm)
	}
}

func mustProd(t *testing.T, f *fromwbnf.File, name string) fromwbnf.Prod {
	t.Helper()
	for _, st := range f.Stmts {
		if p, ok := st.(fromwbnf.Prod); ok && p.Name == name {
			return p
		}
	}
	t.Fatalf("no prod %s", name)
	return fromwbnf.Prod{}
}

func TestParseIndents(t *testing.T) {
	t.Parallel()
	f := mustParse(t, testdata(t, "indents.wbnf"))
	want := `block = seq(indent=seq(%indent="\n", re(\s+)), delim[:](stmt, %indent))
stmt = oneof(seq(op="print", re(\s+), IDENT), seq(op=oneof("if", "while"), re(\s+), IDENT, ":", block))
IDENT = re(\w+)`
	if got := fromwbnf.Dump(f); got != want {
		t.Fatalf("indents dump\n got: %s\nwant: %s", got, want)
	}
}

func TestParseMacroImportAndCall(t *testing.T) {
	t.Parallel()
	f := mustParse(t, []byte(`
.import foo/bar.wbnf
.macro Indented(x) { "{" x "}" }
start -> %!Indented(item) | %%ext | ();
`))
	want := `.import foo/bar.wbnf
.macro Indented(x) { seq("{", x, "}") }
start = oneof(%!Indented(item), %%ext, ())`
	if got := fromwbnf.Dump(f); got != want {
		t.Fatalf("macro dump\n got: %s\nwant: %s", got, want)
	}
}

func TestParseDelimAndRange(t *testing.T) {
	t.Parallel()
	f := mustParse(t, []byte(`
list -> x:"," ;
lead -> x:,"," ;
trail -> x: ",", ;
both -> x:, ",", ;
left -> x:>sep ;
right -> x<:sep ;
range -> a{2,4} b{1,} c{,3};
`))
	want := `list = delim[:](x, ",")
lead = delim[:L](x, ",")
trail = delim[:T](x, ",")
both = delim[:LT](x, ",")
left = delim[:>](x, sep)
right = delim[<:](x, sep)
range = seq({2,4}(a), {+}(b), {0,3}(c))`
	if got := fromwbnf.Dump(f); got != want {
		t.Fatalf("delim dump\n got: %s\nwant: %s", got, want)
	}
}

func TestParseLookaheadAndNamed(t *testing.T) {
	t.Parallel()
	f := mustParse(t, []byte(`s -> (?="x") tag=IDENT;`))
	want := `s = seq(lookahead("x"), tag=IDENT)`
	if got := fromwbnf.Dump(f); got != want {
		t.Fatalf("got %s want %s", got, want)
	}
}

func TestParseREForms(t *testing.T) {
	t.Parallel()
	cases := []struct {
		src, dump string
	}{
		{`n -> \d+;`, `n = re(\d+)`},
		{`n -> [A-Z]+;`, `n = re([A-Z]+)`},
		{`n -> .;`, `n = re(.)`},
		{`n -> /{\w+};`, `n = re(\w+)`},
		{`n -> /{\s*()\s*};`, `n = re(\s*()\s*)`},
		{`.wrapRE -> /{\s*()\s*};`, `.wrapRE = re(\s*()\s*)`},
		{`n -> [0-9a-fA-F]{2};`, `n = re([0-9a-fA-F]{2})`},
	}
	for _, c := range cases {
		f := mustParse(t, []byte(c.src))
		if got := fromwbnf.Dump(f); got != c.dump {
			t.Fatalf("%s\n got: %s\nwant: %s", c.src, got, c.dump)
		}
	}
}

func TestParseEmptyError(t *testing.T) {
	t.Parallel()
	if _, err := fromwbnf.Parse(nil); err == nil {
		t.Fatal("expected error")
	}
	if _, err := fromwbnf.Parse([]byte("   // hi\n")); err == nil {
		t.Fatal("expected error")
	}
}

func TestParseNotAParserEngine(t *testing.T) {
	t.Parallel()
	// T12: the IR is inspected, never executed. This test exists so a later
	// change that wires fromwbnf into engine.Parse is a review signal.
	f := mustParse(t, testdata(t, "xml.wbnf"))
	if fromwbnf.Dump(f) == "" {
		t.Fatal("empty dump")
	}
}
