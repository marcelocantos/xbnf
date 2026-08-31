// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package fromwbnf_test

import (
	"strings"
	"testing"

	"github.com/marcelocantos/xbnf/fromwbnf"
	"github.com/marcelocantos/xbnf/grammar"
	"github.com/marcelocantos/xbnf/syntax"
)

func TestConvertXML(t *testing.T) {
	t.Parallel()
	src, err := fromwbnf.Convert(testdata(t, "xml.wbnf"))
	if err != nil {
		t.Fatal(err)
	}
	g, err := syntax.Parse([]byte(src))
	if err != nil {
		t.Fatalf("xbnf parse:\n%s\n%v", src, err)
	}
	xml := rule(t, g, "xml")
	if _, ok := xml.Body.(grammar.OrderedAlt); !ok {
		t.Fatalf("xml should be ordered choice, got %T\n%s", xml.Body, src)
	}
	if wrap(t, g) == nil {
		t.Fatal("missing #wrap")
	}
	if !strings.Contains(src, "#wrap -> \\s*") {
		t.Fatalf("wrap:\n%s", src)
	}
	if strings.Contains(src, ".wrapRE") {
		t.Fatalf("wrapRE leaked:\n%s", src)
	}
	if strings.Contains(src, " | ") {
		t.Fatalf("unordered | in conversion:\n%s", src)
	}
}

func TestConvertBalancedBraces(t *testing.T) {
	t.Parallel()
	src, err := fromwbnf.Convert(testdata(t, "balancedbraces.wbnf"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := syntax.Parse([]byte(src)); err != nil {
		t.Fatalf("xbnf parse:\n%s\n%v", src, err)
	}
	if !strings.Contains(src, "#wrap -> ()") {
		t.Fatalf("no wrapRE should disable wrap:\n%s", src)
	}
}

func TestConvertWbnfSelf(t *testing.T) {
	t.Parallel()
	src, err := fromwbnf.Convert(testdata(t, "wbnf.wbnf"))
	if err != nil {
		t.Fatal(err)
	}
	g, err := syntax.Parse([]byte(src))
	if err != nil {
		t.Fatalf("xbnf parse:\n%s\n%v", src, err)
	}
	term := rule(t, g, "term")
	if _, ok := term.Body.(grammar.Stack); !ok {
		t.Fatalf("term should be stack, got %T", term.Body)
	}
	if _, ok := rule(t, g, "atom").Body.(grammar.OrderedAlt); !ok {
		t.Fatalf("atom should be ordered alt")
	}
}

func TestConvertIndents(t *testing.T) {
	t.Parallel()
	src, err := fromwbnf.Convert(testdata(t, "indents.wbnf"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := syntax.Parse([]byte(src)); err != nil {
		t.Fatalf("xbnf parse:\n%s\n%v", src, err)
	}
}

func TestConvertSysl(t *testing.T) {
	t.Parallel()
	for _, name := range []string{
		"sysl/stmt.wbnf", "sysl/endpoints.wbnf", "sysl/views.wbnf", "sysl/sysl.wbnf",
	} {
		src, err := fromwbnf.Convert(testdata(t, name))
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		if _, err := syntax.Parse([]byte(src)); err != nil {
			t.Fatalf("%s xbnf parse: %v\n%s", name, err, src)
		}
	}
}

func TestConvertImportPath(t *testing.T) {
	t.Parallel()
	src, err := fromwbnf.Convert([]byte(".import stmt.wbnf\nstart -> x;\n"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(src, `#import "stmt.xbnf"`) {
		t.Fatalf("import path: %s", src)
	}
}

func TestConvertCorpus(t *testing.T) {
	t.Parallel()
	for _, name := range []string{"xml.wbnf", "balancedbraces.wbnf", "wbnf.wbnf", "indents.wbnf"} {
		src, err := fromwbnf.Convert(testdata(t, name))
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		if _, err := syntax.Parse([]byte(src)); err != nil {
			t.Fatalf("%s xbnf parse: %v\n%s", name, err, src)
		}
	}
}

func TestConvertEmptyRefDefault(t *testing.T) {
	t.Parallel()
	src, err := fromwbnf.Convert([]byte(`n -> %level="" \s+;`))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(src, `%level=`) {
		t.Fatalf("empty default dropped: %s", src)
	}
}

func TestConvertPOSIXClass(t *testing.T) {
	t.Parallel()
	src, err := fromwbnf.Convert([]byte(`n -> [[:digit:]]+;`))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(src, "[0-9]") {
		t.Fatalf("posix digit: %s", src)
	}
}

func TestConvertLookaheadInRE(t *testing.T) {
	t.Parallel()
	src, err := fromwbnf.Convert([]byte(`n -> /{(?=a)b};`))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(src, "(?=") {
		t.Fatalf("lookahead: %s", src)
	}
}

func TestConvertLeftoverKinds(t *testing.T) {
	t.Parallel()
	allowed := map[string]bool{
		"unicode-property": true,
		"regex-anchor":     true,
		"regex-flag":       true,
		"regex":            true,
		"lazy-quant":       true,
		"posix-class":      true,
	}
	snippets := []string{
		`n -> \p{Greek};`,
		`n -> \b;`,
		`n -> /{(?i:x)};`,
		`n -> /{a*?};`,
		`n -> /{\QA\E};`,
		`n -> [[:foo:]];`,
	}
	seen := map[string]bool{}
	for _, s := range snippets {
		_, err := fromwbnf.Convert([]byte(s))
		ce, ok := err.(*fromwbnf.ConvertError)
		if !ok {
			t.Fatalf("%s: want ConvertError, got %v", s, err)
		}
		for _, i := range ce.Issues {
			if !allowed[i.Kind] {
				t.Fatalf("%s: undocumented leftover kind %q", s, i.Kind)
			}
			seen[i.Kind] = true
		}
	}
	for k := range allowed {
		if !seen[k] {
			t.Fatalf("no snippet produced leftover kind %q", k)
		}
	}
}

func TestConvertNamesUntranslatable(t *testing.T) {
	t.Parallel()
	_, err := fromwbnf.Convert([]byte(`n -> \p{Greek};`))
	if err == nil {
		t.Fatal("expected untranslatable unicode property")
	}
	ce, ok := err.(*fromwbnf.ConvertError)
	if !ok || len(ce.Issues) == 0 {
		t.Fatalf("want ConvertError with issues, got %T %v", err, err)
	}
	if ce.Issues[0].Kind != "unicode-property" {
		t.Fatalf("kind %q", ce.Issues[0].Kind)
	}
}

func TestConvertRELeaf(t *testing.T) {
	t.Parallel()
	src, err := fromwbnf.Convert([]byte(`n -> \d+ ;`))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(src, `n -> /\d+/;`) && !strings.Contains(src, `n -> /\\d+/;`) {
		if !strings.Contains(src, `/\d+/`) {
			t.Fatalf("expected leaf digit: %s", src)
		}
	}
}

func rule(t *testing.T, g *grammar.Grammar, name string) grammar.Rule {
	t.Helper()
	for _, st := range g.Stmts {
		if r, ok := st.(grammar.Rule); ok && r.Name == name {
			return r
		}
	}
	t.Fatalf("no rule %s", name)
	return grammar.Rule{}
}

func wrap(t *testing.T, g *grammar.Grammar) *grammar.Wrap {
	t.Helper()
	for _, st := range g.Stmts {
		if w, ok := st.(grammar.Wrap); ok {
			return &w
		}
	}
	return nil
}
