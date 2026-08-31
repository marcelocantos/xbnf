// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package fromwbnf_test

import (
	"testing"

	"github.com/arr-ai/wbnf/parser"
	"github.com/arr-ai/wbnf/parser/diff"
	owbnf "github.com/arr-ai/wbnf/wbnf"

	"github.com/marcelocantos/xbnf/fromwbnf"
)

func TestIsomorphicToWbnfParser(t *testing.T) {
	t.Parallel()
	for _, name := range []string{"xml.wbnf", "balancedbraces.wbnf", "wbnf.wbnf", "indents.wbnf"} {
		src := testdata(t, name)
		f, err := fromwbnf.Parse(src)
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		n, err := owbnf.ParseString(string(src))
		if err != nil {
			t.Fatalf("%s oracle parse: %v", name, err)
		}
		want := stripCuts(owbnf.NewFromAst(n.Node))
		got := toParserGrammar(f)
		if d := diff.Grammars(want, got); !d.Equal() {
			t.Fatalf("%s not isomorphic: %+v\nwant %v\ngot %v", name, d, want, got)
		}
	}
}

func toParserGrammar(f *fromwbnf.File) parser.Grammar {
	g := parser.Grammar{}
	if f == nil {
		return g
	}
	for _, st := range f.Stmts {
		if p, ok := st.(fromwbnf.Prod); ok {
			g[parser.Rule(p.Name)] = toParserTerm(p.Body)
		}
	}
	return g
}

func toParserTerm(t fromwbnf.Term) parser.Term {
	switch x := t.(type) {
	case fromwbnf.Seq:
		s := make(parser.Seq, len(x))
		for i, u := range x {
			s[i] = toParserTerm(u)
		}
		return s
	case fromwbnf.Oneof:
		s := make(parser.Oneof, len(x))
		for i, u := range x {
			s[i] = toParserTerm(u)
		}
		return s
	case fromwbnf.Stack:
		s := make(parser.Stack, len(x))
		for i, u := range x {
			s[i] = toParserTerm(u)
		}
		return s
	case fromwbnf.Quant:
		return parser.Quant{Term: toParserTerm(x.Term), Min: x.Min, Max: x.Max}
	case fromwbnf.Delim:
		return parser.Delim{
			Term:            toParserTerm(x.Term),
			Sep:             toParserTerm(x.Sep),
			Assoc:           parser.NewAssociativity(x.Assoc),
			CanStartWithSep: x.Leading,
			CanEndWithSep:   x.Trailing,
		}
	case fromwbnf.Named:
		return parser.Named{Name: x.Name, Term: toParserTerm(x.Term)}
	case fromwbnf.Ident:
		return parser.Rule(x)
	case fromwbnf.String:
		return parser.S(x)
	case fromwbnf.RE:
		return parser.RE(x)
	case fromwbnf.Ref:
		r := parser.REF{Ident: x.Ident}
		if x.Default != nil {
			r.Default = parser.S(*x.Default)
		}
		return r
	case fromwbnf.ExtRef:
		return parser.ExtRef(x)
	case fromwbnf.Lookahead:
		return parser.LookAhead{Term: toParserTerm(x.Term)}
	case fromwbnf.Scoped:
		sg := parser.ScopedGrammar{Term: toParserTerm(x.Term), Grammar: parser.Grammar{}}
		if x.Body != nil {
			sg.Grammar = toParserGrammar(x.Body)
		}
		return sg
	case fromwbnf.Empty:
		return parser.Seq{}
	default:
		return parser.Seq{}
	}
}

func stripCuts(g parser.Grammar) parser.Grammar {
	out := parser.Grammar{}
	for k, v := range g {
		out[k] = stripTerm(v)
	}
	return out
}

func stripTerm(t parser.Term) parser.Term {
	switch x := t.(type) {
	case parser.CutPoint:
		return stripTerm(x.Term)
	case parser.Seq:
		s := make(parser.Seq, len(x))
		for i, u := range x {
			s[i] = stripTerm(u)
		}
		return s
	case parser.Oneof:
		s := make(parser.Oneof, len(x))
		for i, u := range x {
			s[i] = stripTerm(u)
		}
		return s
	case parser.Stack:
		s := make(parser.Stack, len(x))
		for i, u := range x {
			s[i] = stripTerm(u)
		}
		return s
	case parser.Quant:
		x.Term = stripTerm(x.Term)
		return x
	case parser.Delim:
		x.Term = stripTerm(x.Term)
		x.Sep = stripTerm(x.Sep)
		return x
	case parser.Named:
		x.Term = stripTerm(x.Term)
		return x
	case parser.LookAhead:
		x.Term = stripTerm(x.Term)
		return x
	case parser.ScopedGrammar:
		x.Term = stripTerm(x.Term)
		x.Grammar = stripCuts(x.Grammar)
		return x
	default:
		return t
	}
}
