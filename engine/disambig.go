// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package engine

import (
	"fmt"
	"strings"
	"unicode/utf8"

	"github.com/marcelocantos/xbnf/grammar"
)

type firstSet struct {
	eps  bool
	toks []ftok
}

type ftok struct {
	any bool
	str string
	esc string
	cls *grammar.CharClass
}

func (s firstSet) add(t ftok) firstSet {
	for _, o := range s.toks {
		if o.eq(t) {
			return s
		}
	}
	s.toks = append(s.toks, t)
	return s
}

func (s firstSet) union(o firstSet) firstSet {
	for _, t := range o.toks {
		s = s.add(t)
	}
	if o.eps {
		s.eps = true
	}
	return s
}

func (a ftok) eq(b ftok) bool {
	if a.any != b.any || a.str != b.str || a.esc != b.esc {
		return false
	}
	if (a.cls == nil) != (b.cls == nil) {
		return false
	}
	if a.cls != nil {
		return classText(*a.cls) == classText(*b.cls)
	}
	return true
}

func (s firstSet) overlaps(o firstSet) (bool, string) {
	for _, a := range s.toks {
		for _, b := range o.toks {
			if a.overlaps(b) {
				return true, a.String()
			}
		}
	}
	return false, ""
}

func (a ftok) String() string {
	switch {
	case a.any:
		return "."
	case a.str != "":
		return `"` + a.str + `"`
	case a.esc != "":
		return `\` + a.esc
	case a.cls != nil:
		return classText(*a.cls)
	}
	return "?"
}

func (a ftok) overlaps(b ftok) bool {
	if a.any || b.any {
		return true
	}
	if a.str != "" && b.str != "" {
		return a.str == b.str || strings.HasPrefix(a.str, b.str) || strings.HasPrefix(b.str, a.str)
	}
	if a.str != "" {
		return tokMatches(b, a.str)
	}
	if b.str != "" {
		return tokMatches(a, b.str)
	}
	if a.esc != "" && b.esc != "" {
		return a.esc == b.esc || escOverlap(a.esc, b.esc)
	}
	if a.cls != nil && b.cls != nil {
		return classOverlap(*a.cls, *b.cls)
	}
	if a.esc != "" && b.cls != nil {
		return escClassOverlap(a.esc, *b.cls)
	}
	if b.esc != "" && a.cls != nil {
		return escClassOverlap(b.esc, *a.cls)
	}
	return false
}

func tokMatches(t ftok, s string) bool {
	if s == "" {
		return false
	}
	r, _ := utf8.DecodeRuneInString(s)
	if t.esc != "" {
		return escapeMatch(t.esc, r)
	}
	if t.cls != nil {
		return classMatch(*t.cls, r)
	}
	return false
}

func escOverlap(a, b string) bool {
	samples := []rune{'a', 'Z', '0', ' ', '\n', '_', '.', 'x'}
	for _, r := range samples {
		if escapeMatch(a, r) && escapeMatch(b, r) {
			return true
		}
	}
	return false
}

func escClassOverlap(esc string, c grammar.CharClass) bool {
	for _, r := range sampleClass(c) {
		if escapeMatch(esc, r) && classMatch(c, r) {
			return true
		}
	}
	if c.Negated {
		return true
	}
	return false
}

func classOverlap(a, b grammar.CharClass) bool {
	for _, r := range sampleClass(a) {
		if classMatch(a, r) && classMatch(b, r) {
			return true
		}
	}
	for _, r := range sampleClass(b) {
		if classMatch(a, r) && classMatch(b, r) {
			return true
		}
	}
	if a.Negated || b.Negated {
		return true
	}
	return false
}

func sampleClass(c grammar.CharClass) []rune {
	var out []rune
	for _, e := range c.Elems {
		r, _ := utf8.DecodeRuneInString(e.Lo)
		out = append(out, r)
		if e.Hi != "" {
			h, _ := utf8.DecodeRuneInString(e.Hi)
			out = append(out, h)
			if h > r {
				out = append(out, r+(h-r)/2)
			}
		}
	}
	if c.Negated {
		out = append(out, 'a', '0', ' ', '\n', '_')
	}
	return out
}

func (c *compiler) checkDisambiguation() ([]string, error) {
	fs := c.computeFirst()
	var warns, errs []string
	for _, name := range c.order {
		c.walkDecision(name, c.rules[name].Body, nil, c.regular[name], fs, &warns, &errs)
	}
	if len(errs) > 0 {
		return warns, fmt.Errorf("%s", strings.Join(errs, "\n"))
	}
	return warns, nil
}

func (c *compiler) computeFirst() map[string]firstSet {
	fs := map[string]firstSet{}
	for name := range c.rules {
		fs[name] = firstSet{}
	}
	for i := 0; i < len(c.rules)+2; i++ {
		changed := false
		for _, name := range c.order {
			n := c.firstOf(c.rules[name].Body, fs)
			if !firstEq(fs[name], n) {
				fs[name] = n
				changed = true
			}
		}
		if !changed {
			break
		}
	}
	return fs
}

func firstEq(a, b firstSet) bool {
	if a.eps != b.eps || len(a.toks) != len(b.toks) {
		return false
	}
	for i := range a.toks {
		if !a.toks[i].eq(b.toks[i]) {
			return false
		}
	}
	return true
}

func (c *compiler) firstOf(t grammar.Term, fs map[string]firstSet) firstSet {
	if t == nil {
		return firstSet{eps: true}
	}
	switch x := t.(type) {
	case grammar.String:
		if x.Text == "" {
			return firstSet{eps: true}
		}
		return firstSet{toks: []ftok{{str: x.Text}}}
	case grammar.CharClass:
		cc := x
		return firstSet{toks: []ftok{{cls: &cc}}}
	case grammar.Escape:
		return firstSet{toks: []ftok{{esc: x.Code}}}
	case grammar.AnyChar:
		return firstSet{toks: []ftok{{any: true}}}
	case grammar.Empty, grammar.Lookahead, grammar.NegLookahead:
		return firstSet{eps: true}
	case grammar.Ident:
		if s, ok := fs[x.Name]; ok {
			return s
		}
		return firstSet{}
	case grammar.Self:
		return firstSet{eps: true}
	case grammar.Named:
		return c.firstOf(x.Term, fs)
	case grammar.Leaf:
		return c.firstOf(x.Term, fs)
	case grammar.Seq:
		out := firstSet{eps: true}
		for _, u := range x.Terms {
			if !out.eps {
				break
			}
			f := c.firstOf(u, fs)
			kept := firstSet{toks: out.toks}
			out = kept.union(f)
			if !f.eps {
				out.eps = false
				break
			}
			out.eps = true
		}
		return out
	case grammar.Alt:
		out := firstSet{}
		for _, u := range x.Terms {
			out = out.union(c.firstOf(u, fs))
		}
		return out
	case grammar.OrderedAlt:
		out := firstSet{}
		for _, u := range x.Terms {
			out = out.union(c.firstOf(u, fs))
		}
		return out
	case grammar.Stack:
		out := firstSet{}
		for _, u := range x.Levels {
			out = out.union(c.firstOf(u, fs))
		}
		return out
	case grammar.Quant:
		f := c.firstOf(x.Term, fs)
		if x.Min == 0 {
			f.eps = true
		}
		return f
	case grammar.Delim:
		return c.firstOf(x.Term, fs)
	case grammar.Scope:
		return c.firstOf(x.Term, fs)
	default:
		return firstSet{eps: true}
	}
}

func disambigNames(dirs []grammar.Directive) bool {
	for _, d := range dirs {
		switch d.Name {
		case "prefer", "avoid", "assoc", "priority", "longest":
			return true
		}
	}
	return false
}

func (c *compiler) walkDecision(rule string, t grammar.Term, dirs []grammar.Directive, regular bool, fs map[string]firstSet, warns, errs *[]string) {
	if t == nil {
		return
	}
	switch x := t.(type) {
	case grammar.Seq:
		d := append(append([]grammar.Directive{}, dirs...), x.Directives...)
		if len(x.Terms) == 1 {
			c.walkDecision(rule, x.Terms[0], d, regular, fs, warns, errs)
			return
		}
		for _, u := range x.Terms {
			c.walkDecision(rule, u, nil, regular, fs, warns, errs)
		}
		if disambigNames(x.Directives) && !seqNeedsDisambig(x.Terms) {
			*warns = append(*warns, fmt.Sprintf("unnecessary disambiguation in rule %s", rule))
		}
	case grammar.Alt:
		c.checkAlt(rule, x.Terms, dirs, regular, fs, warns, errs)
		for _, u := range x.Terms {
			c.walkDecision(rule, u, nil, regular, fs, warns, errs)
		}
	case grammar.OrderedAlt:
		for _, u := range x.Terms {
			c.walkDecision(rule, u, nil, regular, fs, warns, errs)
		}
		if disambigNames(dirs) {
			*warns = append(*warns, fmt.Sprintf("unnecessary disambiguation in rule %s: |> is already committed choice", rule))
		}
	case grammar.Stack:
		for _, u := range x.Levels {
			c.walkDecision(rule, u, dirs, regular, fs, warns, errs)
		}
	case grammar.Named:
		c.walkDecision(rule, x.Term, dirs, regular, fs, warns, errs)
	case grammar.Leaf:
		c.walkDecision(rule, x.Term, dirs, true, fs, warns, errs)
	case grammar.Quant:
		c.walkDecision(rule, x.Term, dirs, regular, fs, warns, errs)
	case grammar.Delim:
		c.walkDecision(rule, x.Term, dirs, regular, fs, warns, errs)
		c.walkDecision(rule, x.Sep, nil, regular, fs, warns, errs)
	case grammar.Scope:
		c.walkDecision(rule, x.Term, dirs, regular, fs, warns, errs)
	case grammar.Lookahead:
		c.walkDecision(rule, x.Term, dirs, regular, fs, warns, errs)
	case grammar.NegLookahead:
		c.walkDecision(rule, x.Term, dirs, regular, fs, warns, errs)
	}
}

func seqNeedsDisambig(terms []grammar.Term) bool {
	for _, u := range terms {
		switch u.(type) {
		case grammar.Alt, grammar.Self:
			return true
		}
	}
	return false
}

func (c *compiler) checkAlt(rule string, alts []grammar.Term, dirs []grammar.Directive, regular bool, fs map[string]firstSet, warns, errs *[]string) {
	if len(alts) < 2 {
		return
	}
	overlap := false
	tag := ""
	for i := 0; i < len(alts); i++ {
		for j := i + 1; j < len(alts); j++ {
			if ok, t := c.termsOverlap(alts[i], alts[j], fs, 0); ok {
				overlap = true
				tag = t
			}
		}
	}
	has := disambigNames(dirs)
	if overlap && !has {
		if !regular {
			*errs = append(*errs, fmt.Sprintf("ambiguous decision in rule %s: alternatives starting with %s", rule, tag))
		}
		return
	}
	if !overlap && has {
		*warns = append(*warns, fmt.Sprintf("unnecessary disambiguation in rule %s", rule))
	}
}

const lookaheadBound = 8

func unwrapTerm(t grammar.Term) grammar.Term {
	for t != nil {
		switch x := t.(type) {
		case grammar.Named:
			t = x.Term
		case grammar.Leaf:
			t = x.Term
		case grammar.Seq:
			if len(x.Terms) == 1 {
				t = x.Terms[0]
				continue
			}
			if len(x.Terms) == 0 {
				return grammar.Empty{}
			}
			return x
		default:
			return t
		}
	}
	return grammar.Empty{}
}

type phead struct {
	ident string
	tok   ftok
}

func (a phead) sameConcrete(b phead) bool {
	if a.ident != "" || b.ident != "" {
		return a.ident != "" && a.ident == b.ident
	}
	if a.tok.any && b.tok.any {
		return true
	}
	if a.tok.str != "" && a.tok.str == b.tok.str {
		return true
	}
	if a.tok.esc != "" && a.tok.esc == b.tok.esc {
		return true
	}
	if a.tok.cls != nil && b.tok.cls != nil {
		return classText(*a.tok.cls) == classText(*b.tok.cls)
	}
	return false
}

func seqRest(terms []grammar.Term, dirs []grammar.Directive) grammar.Term {
	if len(terms) == 0 {
		return grammar.Empty{}
	}
	if len(terms) == 1 && len(dirs) == 0 {
		return terms[0]
	}
	return grammar.Seq{Terms: terms, Directives: dirs}
}

func (c *compiler) peel(t grammar.Term) (phead, grammar.Term, bool) {
	t = unwrapTerm(t)
	switch x := t.(type) {
	case grammar.String:
		if x.Text == "" {
			return phead{}, nil, false
		}
		return phead{tok: ftok{str: x.Text}}, grammar.Empty{}, true
	case grammar.CharClass:
		cc := x
		return phead{tok: ftok{cls: &cc}}, grammar.Empty{}, true
	case grammar.Escape:
		return phead{tok: ftok{esc: x.Code}}, grammar.Empty{}, true
	case grammar.AnyChar:
		return phead{tok: ftok{any: true}}, grammar.Empty{}, true
	case grammar.Ident:
		if c.regular[x.Name] {
			return phead{ident: x.Name}, grammar.Empty{}, true
		}
		return phead{}, nil, false
	case grammar.Seq:
		if len(x.Terms) == 0 {
			return phead{}, grammar.Empty{}, true
		}
		h, rest, ok := c.peel(x.Terms[0])
		if !ok {
			return phead{}, nil, false
		}
		var terms []grammar.Term
		if rest != nil {
			if _, emp := rest.(grammar.Empty); !emp {
				terms = append(terms, rest)
			}
		}
		terms = append(terms, x.Terms[1:]...)
		return h, seqRest(terms, x.Directives), true
	default:
		return phead{}, nil, false
	}
}

func (c *compiler) termsOverlap(ta, tb grammar.Term, fs map[string]firstSet, depth int) (bool, string) {
	if depth > lookaheadBound {
		return true, "…"
	}
	ta = unwrapTerm(ta)
	tb = unwrapTerm(tb)
	ha, ra, oka := c.peel(ta)
	hb, rb, okb := c.peel(tb)
	if oka && okb && ha.sameConcrete(hb) {
		return c.termsOverlap(ra, rb, fs, depth+1)
	}
	fa := c.firstOf(ta, fs)
	fb := c.firstOf(tb, fs)
	if fa.eps && fb.eps {
		return true, "ε"
	}
	if ok, tag := fa.overlaps(fb); ok {
		return true, tag
	}
	if fa.eps || fb.eps {
		return true, "prefix"
	}
	return false, ""
}
