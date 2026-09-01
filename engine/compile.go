// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package engine

import (
	"fmt"

	"github.com/marcelocantos/xbnf/grammar"
)

const (
	ekNT = iota
	ekTerm
	ekDFA
	ekLook
	ekNegLook
)

type elem struct {
	kind  int
	nt    string
	label string
	term  grammar.Term
}

type prod struct {
	nt  string
	rhs []elem
}

// Compiled is a grammar compiled to GLL slots and DFAs.
type Compiled struct {
	first    string
	regular  map[string]bool
	dfa      map[string]*dfa
	prods    []prod
	ntProds  map[string][]int
	wrap     grammar.Term
	wrapDFA  *dfa
	rules    map[string]grammar.Rule
	Warnings []string
}

func Compile(g *grammar.Grammar) (*Compiled, error) {
	if g == nil || len(g.Stmts) == 0 {
		return nil, fmt.Errorf("empty grammar")
	}
	if err := rejectLater(g.Stmts); err != nil {
		return nil, err
	}
	c := &compiler{
		rules:   map[string]grammar.Rule{},
		regular: map[string]bool{},
	}
	for _, st := range g.Stmts {
		switch s := st.(type) {
		case grammar.Rule:
			if c.first == "" {
				c.first = s.Name
			}
			c.rules[s.Name] = s
			c.order = append(c.order, s.Name)
		case grammar.Wrap:
			c.wrap = s.Body
		}
	}
	c.analyzeRegular()
	for _, name := range c.order {
		r := c.rules[name]
		if hasMod(r.Mods, "lex") && !c.regular[name] {
			return nil, fmt.Errorf("#lex on non-regular rule %s", name)
		}
	}
	warns, err := c.checkDisambiguation()
	if err != nil {
		return nil, err
	}
	out := &Compiled{
		first:    c.first,
		regular:  c.regular,
		dfa:      map[string]*dfa{},
		ntProds:  map[string][]int{},
		wrap:     c.wrap,
		rules:    c.rules,
		Warnings: warns,
	}
	for _, name := range c.order {
		if c.regular[name] {
			d, err := buildDFA(c.rules[name].Body, c)
			if err != nil {
				return nil, err
			}
			out.dfa[name] = d
		}
	}
	if c.wrap != nil {
		if _, ok := c.wrap.(grammar.Empty); !ok {
			if d, err := buildDFA(c.wrap, c); err == nil {
				out.wrapDFA = d
			}
		}
	}
	for _, name := range c.order {
		if c.regular[name] {
			continue
		}
		c.emitRule(name, c.rules[name].Body)
	}
	out.prods = c.prods
	for i, p := range c.prods {
		out.ntProds[p.nt] = append(out.ntProds[p.nt], i)
	}
	for name, d := range c.extraDFA {
		out.dfa[name] = d
	}
	bindDFAOwner(out)
	return out, nil
}

func bindDFAOwner(c *Compiled) {
	if c == nil {
		return
	}
	for _, d := range c.dfa {
		bindOneDFA(d, c)
	}
	bindOneDFA(c.wrapDFA, c)
}

func bindOneDFA(d *dfa, c *Compiled) {
	if d == nil {
		return
	}
	d.owner = c
	for i := range d.alts {
		bindOneDFA(d.alts[i].d, c)
	}
	for i := range d.ordered {
		bindOneDFA(d.ordered[i], c)
	}
}

func (c *Compiled) IsDFA(rule string) bool {
	_, ok := c.dfa[rule]
	return ok
}

func (c *Compiled) Parse(start, input string) *Result {
	if start == "" {
		start = c.first
	}
	return c.gll(start, input)
}

func Parse(g *grammar.Grammar, start, input string) *Result {
	c, err := Compile(g)
	if err != nil {
		return &Result{Error: err.Error()}
	}
	return c.Parse(start, input)
}

type compiler struct {
	rules    map[string]grammar.Rule
	order    []string
	first    string
	wrap     grammar.Term
	regular  map[string]bool
	prods    []prod
	hid      int
	extraDFA map[string]*dfa
}

func (c *compiler) analyzeRegular() {
	type info struct {
		refs []string
		bad  bool
	}
	inf := map[string]info{}
	var walk func(grammar.Term) (refs []string, bad bool)
	walk = func(t grammar.Term) (refs []string, bad bool) {
		if t == nil {
			return nil, false
		}
		switch x := t.(type) {
		case grammar.Ident:
			return []string{x.Name}, false
		case grammar.Stack, grammar.Self, grammar.Ref, grammar.ExtRef, grammar.MacroCall, grammar.PosProp:
			return nil, true
		case grammar.Alt, grammar.OrderedAlt:
			var terms []grammar.Term
			switch a := x.(type) {
			case grammar.Alt:
				terms = a.Terms
			case grammar.OrderedAlt:
				terms = a.Terms
			}
			for _, a := range terms {
				r, b := walk(a)
				refs = append(refs, r...)
				bad = bad || b
			}
		case grammar.Seq:
			for _, a := range x.Terms {
				r, b := walk(a)
				refs = append(refs, r...)
				bad = bad || b
			}
		case grammar.Named:
			return walk(x.Term)
		case grammar.Leaf:
			return walk(x.Term)
		case grammar.Quant:
			return walk(x.Term)
		case grammar.Delim:
			r1, b1 := walk(x.Term)
			r2, b2 := walk(x.Sep)
			return append(r1, r2...), b1 || b2
		case grammar.Scope:
			for _, d := range x.Decls {
				if ru, ok := d.(grammar.Rule); ok {
					r, b := walk(ru.Body)
					refs = append(refs, r...)
					bad = bad || b
				}
			}
			r, b := walk(x.Term)
			return append(refs, r...), bad || b
		case grammar.Lookahead:
			r, _ := walk(x.Term)
			return r, true
		case grammar.NegLookahead:
			r, _ := walk(x.Term)
			return r, true
		}
		return refs, bad
	}
	for name, r := range c.rules {
		refs, bad := walk(r.Body)
		inf[name] = info{refs: refs, bad: bad}
	}
	rec := map[string]bool{}
	var onstack []string
	seen := map[string]int{}
	var dfs func(string)
	dfs = func(n string) {
		if _, ok := seen[n]; ok {
			return
		}
		seen[n] = 1
		onstack = append(onstack, n)
		idx := map[string]int{}
		for i, s := range onstack {
			idx[s] = i
		}
		for _, m := range inf[n].refs {
			if j, ok := idx[m]; ok {
				for _, s := range onstack[j:] {
					rec[s] = true
				}
				continue
			}
			if _, known := inf[m]; known {
				dfs(m)
			}
		}
		onstack = onstack[:len(onstack)-1]
		seen[n] = 2
	}
	for _, n := range c.order {
		dfs(n)
	}
	changed := true
	for name := range c.rules {
		c.regular[name] = !inf[name].bad && !rec[name]
	}
	for changed {
		changed = false
		for name := range c.rules {
			if !c.regular[name] {
				continue
			}
			for _, r := range inf[name].refs {
				if _, ok := c.rules[r]; ok && !c.regular[r] {
					c.regular[name] = false
					changed = true
				}
			}
		}
	}
}

func (c *compiler) leafDFA(x grammar.Leaf) string {
	h := c.fresh("lf")
	d := compileOneDFA(x, c)
	if c.extraDFA == nil {
		c.extraDFA = map[string]*dfa{}
	}
	c.extraDFA[h] = d
	return h
}

func (c *compiler) fresh(kind string) string {
	c.hid++
	return fmt.Sprintf("$%s%d", kind, c.hid)
}

func (c *compiler) addProd(nt string, rhs []elem) {
	c.prods = append(c.prods, prod{nt: nt, rhs: rhs})
}

func (c *compiler) emitRule(name string, body grammar.Term) {
	for _, a := range splitAlt(body) {
		c.addProd(name, c.flatten(a))
	}
}

func pegEncode(o grammar.OrderedAlt) grammar.Alt {
	alts := make([]grammar.Term, len(o.Terms))
	for i, t := range o.Terms {
		if i == 0 {
			alts[i] = t
			continue
		}
		seq := make([]grammar.Term, 0, i+1)
		for j := 0; j < i; j++ {
			seq = append(seq, grammar.NegLookahead{Term: o.Terms[j]})
		}
		seq = append(seq, t)
		if len(seq) == 1 {
			alts[i] = seq[0]
		} else {
			alts[i] = grammar.Seq{Terms: seq}
		}
	}
	return grammar.Alt{Terms: alts}
}

func splitAlt(t grammar.Term) []grammar.Term {
	if a, ok := t.(grammar.Alt); ok {
		return a.Terms
	}
	return []grammar.Term{t}
}

func (c *compiler) flatten(t grammar.Term) []elem {
	switch x := t.(type) {
	case grammar.Seq:
		var out []elem
		for _, s := range x.Terms {
			out = append(out, c.flatten(s)...)
		}
		return out
	case grammar.Alt:
		h := c.fresh("alt")
		c.emitRule(h, x)
		return []elem{{kind: ekNT, nt: h}}
	case grammar.OrderedAlt:
		return c.flatten(pegEncode(x))
	case grammar.Ident:
		if c.regular[x.Name] {
			return []elem{{kind: ekDFA, nt: x.Name, label: x.Label}}
		}
		return []elem{{kind: ekNT, nt: x.Name, label: x.Label}}
	case grammar.Named:
		return c.flatten(x.Term)
	case grammar.Leaf:
		return []elem{{kind: ekDFA, nt: c.leafDFA(x)}}
	case grammar.Quant:
		return []elem{{kind: ekNT, nt: c.quantNT(c.flatten(x.Term), x.Min, x.Max)}}
	case grammar.Delim:
		return []elem{{kind: ekNT, nt: c.delimNT(x)}}
	case grammar.Stack:
		return []elem{{kind: ekNT, nt: c.stackNT(x)}}
	case grammar.Scope:
		return []elem{{kind: ekNT, nt: c.scopeNT(x)}}
	case grammar.Lookahead:
		h := c.fresh("la")
		c.emitRule(h, x.Term)
		return []elem{{kind: ekLook, nt: h}}
	case grammar.NegLookahead:
		h := c.fresh("nl")
		c.emitRule(h, x.Term)
		return []elem{{kind: ekNegLook, nt: h}}
	case grammar.Empty:
		return nil
	case grammar.String, grammar.CharClass, grammar.Escape, grammar.AnyChar:
		return []elem{{kind: ekTerm, term: x}}
	case grammar.Self:
		return nil
	default:
		return []elem{{kind: ekTerm, term: grammar.Empty{}}}
	}
}

func (c *compiler) quantNT(inner []elem, min, max int) string {
	h := c.fresh("q")
	star := c.fresh("qs")
	// star ::= ε | inner star
	c.addProd(star, nil)
	c.addProd(star, append(append([]elem{}, inner...), elem{kind: ekNT, nt: star}))
	var rhs []elem
	for i := 0; i < min; i++ {
		rhs = append(rhs, inner...)
	}
	if max == grammar.Unbounded {
		rhs = append(rhs, elem{kind: ekNT, nt: star})
		c.addProd(h, rhs)
		return h
	}
	opt := c.fresh("qo")
	c.addProd(opt, nil)
	c.addProd(opt, inner)
	for i := min; i < max; i++ {
		rhs = append(rhs, elem{kind: ekNT, nt: opt})
	}
	c.addProd(h, rhs)
	return h
}

func (c *compiler) delimNT(d grammar.Delim) string {
	h := c.fresh("d")
	term := c.flatten(d.Term)
	sep := c.flatten(d.Sep)
	tail := c.fresh("dt")
	c.addProd(tail, nil)
	c.addProd(tail, append(append([]elem{}, sep...), append(term, elem{kind: ekNT, nt: tail})...))
	body := append(append([]elem{}, term...), elem{kind: ekNT, nt: tail})
	if d.Leading {
		lead := c.fresh("dl")
		c.addProd(lead, body)
		c.addProd(lead, append(append([]elem{}, sep...), body...))
		body = []elem{{kind: ekNT, nt: lead}}
	}
	if d.Trailing {
		body = append(body, sep...)
		opt := c.fresh("dtr")
		c.addProd(opt, nil)
		c.addProd(opt, body)
		c.addProd(h, []elem{{kind: ekNT, nt: opt}})
		return h
	}
	c.addProd(h, body)
	return h
}

func (c *compiler) stackNT(st grammar.Stack) string {
	names := make([]string, len(st.Levels))
	for i := range st.Levels {
		names[i] = c.fresh("st")
	}
	for i, lev := range st.Levels {
		tighter := ""
		if i+1 < len(names) {
			tighter = names[i+1]
		}
		c.emitRule(names[i], rewriteSelf(lev, tighter))
	}
	if len(names) == 0 {
		h := c.fresh("st")
		c.addProd(h, nil)
		return h
	}
	return names[0]
}

func rewriteSelf(t grammar.Term, tighter string) grammar.Term {
	switch x := t.(type) {
	case grammar.Self:
		if tighter == "" {
			return grammar.Empty{}
		}
		return grammar.Ident{Name: tighter}
	case grammar.Seq:
		ts := make([]grammar.Term, len(x.Terms))
		for i, s := range x.Terms {
			ts[i] = rewriteSelf(s, tighter)
		}
		return grammar.Seq{Terms: ts, Directives: x.Directives}
	case grammar.Alt:
		ts := make([]grammar.Term, len(x.Terms))
		for i, s := range x.Terms {
			ts[i] = rewriteSelf(s, tighter)
		}
		return grammar.Alt{Terms: ts}
	case grammar.OrderedAlt:
		ts := make([]grammar.Term, len(x.Terms))
		for i, s := range x.Terms {
			ts[i] = rewriteSelf(s, tighter)
		}
		return grammar.OrderedAlt{Terms: ts}
	case grammar.Named:
		return grammar.Named{Name: x.Name, Term: rewriteSelf(x.Term, tighter)}
	case grammar.Leaf:
		return grammar.Leaf{Term: rewriteSelf(x.Term, tighter)}
	case grammar.Quant:
		return grammar.Quant{Term: rewriteSelf(x.Term, tighter), Min: x.Min, Max: x.Max}
	case grammar.Delim:
		return grammar.Delim{
			Term: rewriteSelf(x.Term, tighter), Sep: rewriteSelf(x.Sep, tighter),
			Leading: x.Leading, Trailing: x.Trailing,
		}
	case grammar.Lookahead:
		return grammar.Lookahead{Term: rewriteSelf(x.Term, tighter)}
	case grammar.NegLookahead:
		return grammar.NegLookahead{Term: rewriteSelf(x.Term, tighter)}
	default:
		return t
	}
}

func (c *compiler) scopeNT(sc grammar.Scope) string {
	saved := map[string]grammar.Rule{}
	savedReg := map[string]bool{}
	for _, d := range sc.Decls {
		if r, ok := d.(grammar.Rule); ok {
			saved[r.Name] = c.rules[r.Name]
			savedReg[r.Name] = c.regular[r.Name]
			c.rules[r.Name] = r
			refs, bad := func() ([]string, bool) { return nil, false }()
			_ = refs
			c.regular[r.Name] = !bad && !c.regular[r.Name] && c.regular[r.Name]
			// Local rules: recompute regularity simply — if body has no Ident cycles through parent.
			c.regular[r.Name] = localRegular(r.Body)
			if c.regular[r.Name] {
				// leave for DFA at use via flatten Ident — but c.regular is used in flatten.
			}
			c.order = append(c.order, r.Name)
			if c.regular[r.Name] {
				// DFA built later only in Compile loop over original order.
			}
		}
	}
	h := c.fresh("sc")
	c.emitRule(h, sc.Term)
	for name, r := range saved {
		if r.Name == "" {
			delete(c.rules, name)
			delete(c.regular, name)
			continue
		}
		c.rules[name] = r
		c.regular[name] = savedReg[name]
	}
	return h
}

func localRegular(t grammar.Term) bool {
	switch x := t.(type) {
	case grammar.Ident, grammar.Stack, grammar.Self, grammar.Ref, grammar.ExtRef, grammar.MacroCall, grammar.PosProp:
		return false
	case grammar.Alt, grammar.OrderedAlt:
		var terms []grammar.Term
		switch a := x.(type) {
		case grammar.Alt:
			terms = a.Terms
		case grammar.OrderedAlt:
			terms = a.Terms
		}
		for _, a := range terms {
			if !localRegular(a) {
				return false
			}
		}
	case grammar.Seq:
		for _, a := range x.Terms {
			if !localRegular(a) {
				return false
			}
		}
	case grammar.Named:
		return localRegular(x.Term)
	case grammar.Leaf:
		return localRegular(x.Term)
	case grammar.Quant:
		return localRegular(x.Term)
	case grammar.Delim:
		return localRegular(x.Term) && localRegular(x.Sep)
	case grammar.Lookahead, grammar.NegLookahead:
		return false
	case grammar.Scope:
		return localRegular(x.Term)
	}
	return true
}
