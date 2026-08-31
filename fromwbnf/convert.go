// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package fromwbnf

import (
	"fmt"
	"strings"

	"github.com/marcelocantos/xbnf/grammar"
)

// Issue is one untranslatable wbnf construct.
type Issue struct {
	At   string
	Kind string
	Msg  string
}

func (i Issue) String() string {
	return fmt.Sprintf("%s: %s: %s", i.At, i.Kind, i.Msg)
}

// ConvertError lists named leftovers. Nothing is dropped silently.
type ConvertError struct {
	Issues []Issue
}

func (e *ConvertError) Error() string {
	var b strings.Builder
	b.WriteString("fromwbnf: untranslatable constructs:")
	for _, i := range e.Issues {
		b.WriteString("\n  ")
		b.WriteString(i.String())
	}
	return b.String()
}

// Leftover kinds with no xbnf spelling (ConvertError, never silent):
//
//	unicode-property  \p{…} / \P{…}
//	regex-anchor      \A \z \b \B
//	regex-flag        (?i: (?m: and friends; (?s: is handled)
//	regex             \Q\E and malformed regex
//	lazy-quant        *? +? ??
//	posix-class       unknown or negated POSIX classes
//
// Convert turns parsed .wbnf meaning into xbnf source.
func Convert(src []byte) (string, error) {
	f, err := Parse(src)
	if err != nil {
		return "", err
	}
	return ConvertFile(f)
}

// ConvertFile emits xbnf for a parsed wbnf grammar.
func ConvertFile(f *File) (string, error) {
	c := &converter{}
	g := c.grammar(f)
	if len(c.issues) > 0 {
		return "", &ConvertError{Issues: c.issues}
	}
	return emitGrammar(g), nil
}

type converter struct {
	issues []Issue
	at     string
}

func (c *converter) issue(kind, msg string) {
	at := c.at
	if at == "" {
		at = "."
	}
	c.issues = append(c.issues, Issue{At: at, Kind: kind, Msg: msg})
}

func (c *converter) grammar(f *File) *grammar.Grammar {
	if f == nil {
		c.issue("empty", "no grammar")
		return &grammar.Grammar{}
	}
	g := &grammar.Grammar{}
	sawWrap := false
	for _, st := range f.Stmts {
		switch s := st.(type) {
		case Prod:
			if s.Name == ".wrapRE" {
				sawWrap = true
				c.at = "#wrap"
				g.Stmts = append(g.Stmts, grammar.Wrap{Body: c.wrapBody(s.Body)})
				continue
			}
			c.at = s.Name
			if !isXBNFIdent(s.Name) {
				c.issue("ident", fmt.Sprintf("rule name %q is not an xbnf identifier", s.Name))
				continue
			}
			g.Stmts = append(g.Stmts, grammar.Rule{Name: s.Name, Body: c.term(s.Body)})
		case Import:
			g.Stmts = append(g.Stmts, grammar.Import{Path: convertImportPath(s.Path)})
		case MacroDef:
			c.at = "#macro " + s.Name
			g.Stmts = append(g.Stmts, grammar.Macro{Name: s.Name, Params: s.Args, Body: c.term(s.Body)})
		}
	}
	if !sawWrap {
		g.Stmts = append(g.Stmts, grammar.Wrap{Body: grammar.Empty{}})
	}
	return g
}

func convertImportPath(path string) string {
	if strings.HasSuffix(path, ".wbnf") {
		return strings.TrimSuffix(path, ".wbnf") + ".xbnf"
	}
	return path
}

func (c *converter) wrapBody(t Term) grammar.Term {
	switch x := t.(type) {
	case RE:
		return c.wrapRE(string(x))
	case Oneof:
		alts := make([]grammar.Term, len(x))
		for i, u := range x {
			alts[i] = c.wrapBody(u)
		}
		return grammar.OrderedAlt{Terms: alts}
	case Seq:
		terms := make([]grammar.Term, len(x))
		for i, u := range x {
			terms[i] = c.wrapBody(u)
		}
		return grammar.Seq{Terms: terms}
	default:
		return c.term(t)
	}
}

func (c *converter) wrapRE(s string) grammar.Term {
	left, right, ok := strings.Cut(s, "()")
	if !ok {
		return c.re(s)
	}
	var parts []grammar.Term
	if left != "" {
		parts = append(parts, c.re(left))
	}
	if right != "" {
		parts = append(parts, c.re(right))
	}
	switch len(parts) {
	case 0:
		return grammar.Empty{}
	case 1:
		return parts[0]
	default:
		if eqTerm(parts[0], parts[1]) {
			return parts[0]
		}
		return grammar.Seq{Terms: parts}
	}
}

func (c *converter) term(t Term) grammar.Term {
	switch x := t.(type) {
	case Seq:
		terms := make([]grammar.Term, len(x))
		for i, u := range x {
			terms[i] = c.term(u)
		}
		return grammar.Seq{Terms: terms}
	case Oneof:
		terms := make([]grammar.Term, len(x))
		for i, u := range x {
			terms[i] = c.term(u)
		}
		return grammar.OrderedAlt{Terms: terms}
	case Stack:
		levels := make([]grammar.Term, len(x))
		for i, u := range x {
			levels[i] = c.term(u)
		}
		return grammar.Stack{Levels: levels}
	case Quant:
		max := x.Max
		if x.Max == 0 {
			max = grammar.Unbounded
		}
		return grammar.Quant{Term: c.term(x.Term), Min: x.Min, Max: max}
	case Delim:
		d := grammar.Delim{
			Term:     c.term(x.Term),
			Sep:      c.term(x.Sep),
			Leading:  x.Leading,
			Trailing: x.Trailing,
		}
		switch x.Assoc {
		case "", ":":
			return d
		case ":>":
			return grammar.Seq{Terms: []grammar.Term{d}, Directives: []grammar.Directive{{Name: "assoc", Value: "left"}}}
		case "<:":
			return grammar.Seq{Terms: []grammar.Term{d}, Directives: []grammar.Directive{{Name: "assoc", Value: "right"}}}
		default:
			c.issue("assoc", fmt.Sprintf("unknown delim associativity %q", x.Assoc))
			return d
		}
	case Named:
		if !isXBNFIdent(x.Name) {
			c.issue("ident", fmt.Sprintf("name %q is not an xbnf identifier", x.Name))
		}
		return grammar.Named{Name: x.Name, Term: c.term(x.Term)}
	case Ident:
		switch string(x) {
		case "@":
			return grammar.Self{}
		case ".wrapRE":
			c.issue("ident", ".wrapRE is #wrap, not a rule reference")
			return grammar.Ident{Name: "wrapRE"}
		}
		if !isXBNFIdent(string(x)) {
			c.issue("ident", fmt.Sprintf("ident %q is not an xbnf identifier", x))
		}
		return grammar.Ident{Name: string(x)}
	case String:
		return grammar.String{Text: string(x)}
	case RE:
		return grammar.Leaf{Term: c.re(string(x))}
	case Ref:
		r := grammar.Ref{Name: x.Ident}
		if x.Default != nil {
			r.Default = string(*x.Default)
			r.HasDefault = true
		}
		if !isXBNFIdent(r.Name) {
			c.issue("ident", fmt.Sprintf("ref %q is not an xbnf identifier", r.Name))
		}
		return r
	case ExtRef:
		if !isXBNFIdent(string(x)) {
			c.issue("ident", fmt.Sprintf("extref %q is not an xbnf identifier", x))
		}
		return grammar.ExtRef{Name: string(x)}
	case Lookahead:
		return grammar.Lookahead{Term: c.term(x.Term)}
	case Scoped:
		sc := grammar.Scope{Term: c.term(x.Term)}
		if x.Body != nil {
			save := c.at
			for _, st := range x.Body.Stmts {
				switch s := st.(type) {
				case Prod:
					if s.Name == ".wrapRE" {
						c.at = "#wrap"
						sc.Decls = append(sc.Decls, grammar.Wrap{Body: c.wrapBody(s.Body)})
						continue
					}
					c.at = s.Name
					if !isXBNFIdent(s.Name) {
						c.issue("ident", fmt.Sprintf("rule name %q is not an xbnf identifier", s.Name))
						continue
					}
					sc.Decls = append(sc.Decls, grammar.Rule{Name: s.Name, Body: c.term(s.Body)})
				case Import:
					sc.Decls = append(sc.Decls, grammar.Import{Path: convertImportPath(s.Path)})
				case MacroDef:
					c.at = "#macro " + s.Name
					sc.Decls = append(sc.Decls, grammar.Macro{Name: s.Name, Params: s.Args, Body: c.term(s.Body)})
				}
			}
			c.at = save
		}
		return sc
	case MacroCall:
		args := make([]grammar.Term, len(x.Args))
		for i, a := range x.Args {
			args[i] = c.term(a)
		}
		return grammar.MacroCall{Name: x.Name, Args: args}
	case Empty:
		return grammar.Empty{}
	default:
		c.issue("term", fmt.Sprintf("unknown term %T", t))
		return grammar.Empty{}
	}
}

func (c *converter) re(pat string) grammar.Term {
	if pat == "" {
		return grammar.Empty{}
	}
	p := &reParser{s: pat, at: c.at, issues: &c.issues}
	t := p.alt()
	p.skip()
	if p.i < len(p.s) {
		c.issue("regex", fmt.Sprintf("unparsed suffix %q", p.s[p.i:]))
	}
	return t
}

func isXBNFIdent(s string) bool {
	if s == "" {
		return false
	}
	c := s[0]
	if !((c >= 'A' && c <= 'Z') || (c >= 'a' && c <= 'z') || c == '_') {
		return false
	}
	for i := 1; i < len(s); i++ {
		c := s[i]
		if !((c >= 'A' && c <= 'Z') || (c >= 'a' && c <= 'z') || (c >= '0' && c <= '9') || c == '_') {
			return false
		}
	}
	return true
}

func eqTerm(a, b grammar.Term) bool {
	switch x := a.(type) {
	case grammar.Escape:
		y, ok := b.(grammar.Escape)
		return ok && x.Code == y.Code
	case grammar.String:
		y, ok := b.(grammar.String)
		return ok && x.Text == y.Text
	case grammar.CharClass:
		y, ok := b.(grammar.CharClass)
		if !ok || x.Negated != y.Negated || len(x.Elems) != len(y.Elems) {
			return false
		}
		for i := range x.Elems {
			if x.Elems[i] != y.Elems[i] {
				return false
			}
		}
		return true
	case grammar.Quant:
		y, ok := b.(grammar.Quant)
		return ok && x.Min == y.Min && x.Max == y.Max && eqTerm(x.Term, y.Term)
	case grammar.Seq:
		y, ok := b.(grammar.Seq)
		if !ok || len(x.Terms) != len(y.Terms) {
			return false
		}
		for i := range x.Terms {
			if !eqTerm(x.Terms[i], y.Terms[i]) {
				return false
			}
		}
		return true
	case grammar.Empty:
		_, ok := b.(grammar.Empty)
		return ok
	default:
		return false
	}
}
