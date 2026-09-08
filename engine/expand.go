// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package engine

import (
	"fmt"
	"os"

	"github.com/marcelocantos/xbnf/grammar"
	"github.com/marcelocantos/xbnf/syntax"
)

// ExtRefFunc is a host hook for `%%name`. pos is after #wrap. end is the
// exclusive byte index consumed; ok is false to reject.
type ExtRefFunc func(input string, pos int) (end int, ok bool)

// CompileOpts is the extra Compile surface. A nil *CompileOpts is empty.
// Not functional options.
type CompileOpts struct {
	ExtRefs    map[string]ExtRefFunc
	ReadImport func(path string) ([]byte, error)
}

func (o *CompileOpts) readImport() func(string) ([]byte, error) {
	if o != nil && o.ReadImport != nil {
		return o.ReadImport
	}
	return os.ReadFile
}

func (o *CompileOpts) extRefs() map[string]ExtRefFunc {
	if o == nil {
		return nil
	}
	return o.ExtRefs
}

func prepareGrammar(g *grammar.Grammar, opts *CompileOpts) (*grammar.Grammar, error) {
	stmts, err := resolveImports(g.Stmts, opts.readImport(), map[string]bool{})
	if err != nil {
		return nil, err
	}
	stmts, err = expandStmts(stmts, nil)
	if err != nil {
		return nil, err
	}
	if len(stmts) == 0 {
		return nil, fmt.Errorf("empty grammar")
	}
	return &grammar.Grammar{Stmts: stmts}, nil
}

func resolveImports(stmts []grammar.Stmt, read func(string) ([]byte, error), seen map[string]bool) ([]grammar.Stmt, error) {
	var out, imported []grammar.Stmt
	for _, st := range stmts {
		imp, ok := st.(grammar.Import)
		if !ok {
			out = append(out, st)
			continue
		}
		if seen[imp.Path] {
			return nil, fmt.Errorf("#import cycle: %s", imp.Path)
		}
		seen[imp.Path] = true
		src, err := read(imp.Path)
		if err != nil {
			return nil, fmt.Errorf("#import %q: %w", imp.Path, err)
		}
		ng, err := syntax.Parse(src)
		if err != nil {
			return nil, fmt.Errorf("#import %q: %w", imp.Path, err)
		}
		nested, err := resolveImports(ng.Stmts, read, seen)
		if err != nil {
			return nil, err
		}
		delete(seen, imp.Path)
		imported = append(imported, nested...)
	}
	return append(out, imported...), nil
}

func expandStmts(stmts []grammar.Stmt, parent map[string]grammar.Macro) ([]grammar.Stmt, error) {
	macros := map[string]grammar.Macro{}
	for k, v := range parent {
		macros[k] = v
	}
	for _, st := range stmts {
		if m, ok := st.(grammar.Macro); ok {
			macros[m.Name] = m
		}
	}
	var out []grammar.Stmt
	for _, st := range stmts {
		switch s := st.(type) {
		case grammar.Macro:
			continue
		case grammar.Rule:
			body, err := expandTerm(s.Body, macros, nil)
			if err != nil {
				return nil, err
			}
			s.Body = body
			out = append(out, s)
		case grammar.Wrap:
			body, err := expandTerm(s.Body, macros, nil)
			if err != nil {
				return nil, err
			}
			s.Body = body
			out = append(out, s)
		default:
			out = append(out, s)
		}
	}
	return out, nil
}

func expandTerm(t grammar.Term, macros map[string]grammar.Macro, stack []string) (grammar.Term, error) {
	if t == nil {
		return nil, nil
	}
	switch x := t.(type) {
	case grammar.MacroCall:
		m, ok := macros[x.Name]
		if !ok {
			return nil, fmt.Errorf("undefined macro %s", x.Name)
		}
		if len(x.Args) != len(m.Params) {
			return nil, fmt.Errorf("macro %s expected %d args, got %d", x.Name, len(m.Params), len(x.Args))
		}
		for _, name := range stack {
			if name == x.Name {
				return nil, fmt.Errorf("macro cycle: %s", x.Name)
			}
		}
		args := make([]grammar.Term, len(x.Args))
		for i, a := range x.Args {
			ea, err := expandTerm(a, macros, stack)
			if err != nil {
				return nil, err
			}
			args[i] = ea
		}
		body := substTerm(m.Body, m.Params, args)
		return expandTerm(body, macros, append(stack, x.Name))
	case grammar.Ident, grammar.String, grammar.CharClass, grammar.Escape, grammar.AnyChar,
		grammar.Ref, grammar.ExtRef, grammar.Self, grammar.PosProp, grammar.Empty:
		return t, nil
	case grammar.Stack:
		ls := make([]grammar.Term, len(x.Levels))
		for i, l := range x.Levels {
			e, err := expandTerm(l, macros, stack)
			if err != nil {
				return nil, err
			}
			ls[i] = e
		}
		x.Levels = ls
		return x, nil
	case grammar.Alt:
		ts, err := expandTerms(x.Terms, macros, stack)
		if err != nil {
			return nil, err
		}
		x.Terms = ts
		return x, nil
	case grammar.OrderedAlt:
		ts, err := expandTerms(x.Terms, macros, stack)
		if err != nil {
			return nil, err
		}
		x.Terms = ts
		return x, nil
	case grammar.Seq:
		ts, err := expandTerms(x.Terms, macros, stack)
		if err != nil {
			return nil, err
		}
		x.Terms = ts
		return x, nil
	case grammar.Named:
		e, err := expandTerm(x.Term, macros, stack)
		if err != nil {
			return nil, err
		}
		x.Term = e
		return x, nil
	case grammar.Leaf:
		e, err := expandTerm(x.Term, macros, stack)
		if err != nil {
			return nil, err
		}
		x.Term = e
		return x, nil
	case grammar.Quant:
		e, err := expandTerm(x.Term, macros, stack)
		if err != nil {
			return nil, err
		}
		x.Term = e
		return x, nil
	case grammar.Delim:
		a, err := expandTerm(x.Term, macros, stack)
		if err != nil {
			return nil, err
		}
		b, err := expandTerm(x.Sep, macros, stack)
		if err != nil {
			return nil, err
		}
		x.Term, x.Sep = a, b
		return x, nil
	case grammar.Lookahead:
		e, err := expandTerm(x.Term, macros, stack)
		if err != nil {
			return nil, err
		}
		x.Term = e
		return x, nil
	case grammar.NegLookahead:
		e, err := expandTerm(x.Term, macros, stack)
		if err != nil {
			return nil, err
		}
		x.Term = e
		return x, nil
	case grammar.CaseFold:
		e, err := expandTerm(x.Term, macros, stack)
		if err != nil {
			return nil, err
		}
		x.Term = e
		return x, nil
	case grammar.Scope:
		decls, err := expandStmts(x.Decls, macros)
		if err != nil {
			return nil, err
		}
		e, err := expandTerm(x.Term, macros, stack)
		if err != nil {
			return nil, err
		}
		x.Decls, x.Term = decls, e
		return x, nil
	default:
		return t, nil
	}
}

func expandTerms(ts []grammar.Term, macros map[string]grammar.Macro, stack []string) ([]grammar.Term, error) {
	out := make([]grammar.Term, len(ts))
	for i, t := range ts {
		e, err := expandTerm(t, macros, stack)
		if err != nil {
			return nil, err
		}
		out[i] = e
	}
	return out, nil
}

func substTerm(t grammar.Term, params []string, args []grammar.Term) grammar.Term {
	if t == nil {
		return nil
	}
	switch x := t.(type) {
	case grammar.Ident:
		for i, p := range params {
			if x.Name == p {
				return args[i]
			}
		}
		return t
	case grammar.MacroCall:
		as := make([]grammar.Term, len(x.Args))
		for i, a := range x.Args {
			as[i] = substTerm(a, params, args)
		}
		x.Args = as
		return x
	case grammar.Stack:
		ls := make([]grammar.Term, len(x.Levels))
		for i, l := range x.Levels {
			ls[i] = substTerm(l, params, args)
		}
		x.Levels = ls
		return x
	case grammar.Alt:
		x.Terms = substTerms(x.Terms, params, args)
		return x
	case grammar.OrderedAlt:
		x.Terms = substTerms(x.Terms, params, args)
		return x
	case grammar.Seq:
		x.Terms = substTerms(x.Terms, params, args)
		return x
	case grammar.Named:
		x.Term = substTerm(x.Term, params, args)
		return x
	case grammar.Leaf:
		x.Term = substTerm(x.Term, params, args)
		return x
	case grammar.Quant:
		x.Term = substTerm(x.Term, params, args)
		return x
	case grammar.Delim:
		x.Term = substTerm(x.Term, params, args)
		x.Sep = substTerm(x.Sep, params, args)
		return x
	case grammar.Lookahead:
		x.Term = substTerm(x.Term, params, args)
		return x
	case grammar.NegLookahead:
		x.Term = substTerm(x.Term, params, args)
		return x
	case grammar.CaseFold:
		x.Term = substTerm(x.Term, params, args)
		return x
	case grammar.Scope:
		decls := make([]grammar.Stmt, len(x.Decls))
		for i, d := range x.Decls {
			if r, ok := d.(grammar.Rule); ok {
				r.Body = substTerm(r.Body, params, args)
				decls[i] = r
				continue
			}
			decls[i] = d
		}
		x.Decls = decls
		x.Term = substTerm(x.Term, params, args)
		return x
	default:
		return t
	}
}

func substTerms(ts []grammar.Term, params []string, args []grammar.Term) []grammar.Term {
	out := make([]grammar.Term, len(ts))
	for i, t := range ts {
		out[i] = substTerm(t, params, args)
	}
	return out
}
