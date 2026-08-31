// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package engine

import (
	"unicode/utf8"

	"github.com/marcelocantos/xbnf/grammar"
)

type nfaTrans struct {
	pred      func(rune) bool
	to        int
	call      string // regular rule invoked as an atomic longest-match DFA
	callLabel string // required ::label of that call, if any
}

type nfaState struct {
	eps    []int
	trans  []nfaTrans
	acc    bool
	labels []string
}

type nfa struct {
	states []nfaState
}

func (n *nfa) st() int {
	n.states = append(n.states, nfaState{})
	return len(n.states) - 1
}

type dfa struct {
	nfa     *nfa
	start   intset
	memo    map[string]*dfaState
	alts    []labAlt
	ordered []*dfa // |> : first matching alt wins
	owner   *Compiled
}

type labAlt struct {
	name string
	d    *dfa
}

type dfaState struct {
	set    intset
	trans  map[rune]*dfaState
	acc    bool
	labels []string
}

type intset []int

func (s intset) key() string {
	b := make([]byte, len(s)*4)
	for i, v := range s {
		b[i*4] = byte(v)
		b[i*4+1] = byte(v >> 8)
		b[i*4+2] = byte(v >> 16)
		b[i*4+3] = byte(v >> 24)
	}
	return string(b)
}

func buildDFA(body grammar.Term, c *compiler) (*dfa, error) {
	if o, ok := body.(grammar.OrderedAlt); ok {
		d := &dfa{nfa: &nfa{states: []nfaState{{}}}, memo: map[string]*dfaState{}}
		for _, t := range o.Terms {
			d.ordered = append(d.ordered, compileOneDFA(t, c))
		}
		return d, nil
	}
	d := compileOneDFA(body, c)
	if a, ok := body.(grammar.Alt); ok {
		allNamed := len(a.Terms) > 0
		for _, t := range a.Terms {
			if _, ok := t.(grammar.Named); !ok {
				allNamed = false
				break
			}
		}
		if allNamed {
			for _, t := range a.Terms {
				n := t.(grammar.Named)
				d.alts = append(d.alts, labAlt{name: n.Name, d: compileOneDFA(n.Term, c)})
			}
		}
	}
	return d, nil
}

func compileOneDFA(body grammar.Term, c *compiler) *dfa {
	n := &nfa{}
	b := nfaB{n: n, c: c, seen: map[string]bool{}}
	s, a := b.term(body)
	n.states[a].acc = true
	d := &dfa{nfa: n, memo: map[string]*dfaState{}}
	d.start = b.eps(intset{s})
	return d
}

type nfaB struct {
	n      *nfa
	c      *compiler
	seen   map[string]bool
	nowrap bool
}

func (b *nfaB) term(t grammar.Term) (int, int) {
	switch x := t.(type) {
	case grammar.String:
		return b.lit(x.Text)
	case grammar.CharClass, grammar.Escape, grammar.AnyChar:
		s := b.n.st()
		a := b.n.st()
		b.n.states[s].trans = append(b.n.states[s].trans, nfaTrans{pred: runePred(x), to: a})
		return s, a
	case grammar.Empty:
		s := b.n.st()
		return s, s
	case grammar.Leaf:
		saved := b.nowrap
		b.nowrap = true
		s, a := b.term(x.Term)
		b.nowrap = saved
		return s, a
	case grammar.Seq:
		if len(x.Terms) == 0 {
			s := b.n.st()
			return s, s
		}
		s, a := b.term(x.Terms[0])
		for _, t := range x.Terms[1:] {
			if !b.nowrap && b.c.wrap != nil {
				if _, emp := b.c.wrap.(grammar.Empty); !emp {
					saved := b.nowrap
					b.nowrap = true
					ws, wa := b.term(b.c.wrap)
					b.nowrap = saved
					b.n.states[a].eps = append(b.n.states[a].eps, ws)
					a = wa
				}
			}
			s2, a2 := b.term(t)
			b.n.states[a].eps = append(b.n.states[a].eps, s2)
			a = a2
		}
		return s, a
	case grammar.OrderedAlt:
		s := b.n.st()
		a := b.n.st()
		od := &dfa{nfa: &nfa{states: []nfaState{{}}}, memo: map[string]*dfaState{}}
		for _, t := range x.Terms {
			od.ordered = append(od.ordered, compileOneDFA(t, b.c))
		}
		h := b.c.fresh("oa")
		if b.c.extraDFA == nil {
			b.c.extraDFA = map[string]*dfa{}
		}
		b.c.extraDFA[h] = od
		b.n.states[s].trans = append(b.n.states[s].trans, nfaTrans{call: h, to: a})
		return s, a
	case grammar.Alt:
		s := b.n.st()
		a := b.n.st()
		for _, t := range x.Terms {
			lab := ""
			inner := t
			if n, ok := t.(grammar.Named); ok {
				lab = n.Name
				inner = n.Term
			}
			s1, a1 := b.term(inner)
			if lab != "" {
				b.n.states[a1].labels = append(b.n.states[a1].labels, lab)
			}
			b.n.states[s].eps = append(b.n.states[s].eps, s1)
			b.n.states[a1].eps = append(b.n.states[a1].eps, a)
		}
		return s, a
	case grammar.Named:
		s, a := b.term(x.Term)
		b.n.states[a].labels = append(b.n.states[a].labels, x.Name)
		return s, a
	case grammar.Quant:
		return b.quant(x)
	case grammar.Delim:
		// Term (Sep Term)*
		ts, ta := b.term(x.Term)
		ss, sa := b.term(x.Sep)
		loop := b.n.st()
		acc := b.n.st()
		b.n.states[ta].eps = append(b.n.states[ta].eps, loop)
		b.n.states[loop].eps = append(b.n.states[loop].eps, acc, ss)
		b.n.states[sa].eps = append(b.n.states[sa].eps, ts)
		start := ts
		if x.Leading {
			st := b.n.st()
			b.n.states[st].eps = append(b.n.states[st].eps, ts, ss)
			start = st
		}
		if x.Trailing {
			b.n.states[loop].eps = append(b.n.states[loop].eps, ss)
			b.n.states[sa].eps = append(b.n.states[sa].eps, acc)
		}
		return start, acc
	case grammar.Ident:
		if x.Label != "" {
			// Atomic call: longest-match the named rule, then keep this label.
			s := b.n.st()
			a := b.n.st()
			b.n.states[s].trans = append(b.n.states[s].trans, nfaTrans{
				call: x.Name, callLabel: x.Label, to: a,
			})
			return s, a
		}
		r, ok := b.c.rules[x.Name]
		if !ok {
			s := b.n.st()
			return s, s
		}
		if b.seen[x.Name] {
			s := b.n.st()
			return s, s
		}
		b.seen[x.Name] = true
		s, a := b.term(r.Body)
		delete(b.seen, x.Name)
		return s, a
	case grammar.Scope:
		saved := b.nowrap
		for _, d := range x.Decls {
			if w, ok := d.(grammar.Wrap); ok {
				if _, emp := w.Body.(grammar.Empty); emp {
					b.nowrap = true
				}
			}
		}
		s, a := b.term(x.Term)
		b.nowrap = saved
		return s, a
	case grammar.Lookahead, grammar.NegLookahead:
		s := b.n.st()
		return s, s
	default:
		s := b.n.st()
		return s, s
	}
}

func (b *nfaB) lit(s string) (int, int) {
	if s == "" {
		st := b.n.st()
		return st, st
	}
	start := b.n.st()
	cur := start
	for _, r := range s {
		nx := b.n.st()
		rr := r
		b.n.states[cur].trans = append(b.n.states[cur].trans, nfaTrans{
			pred: func(x rune) bool { return x == rr },
			to:   nx,
		})
		cur = nx
	}
	return start, cur
}

func (b *nfaB) quant(q grammar.Quant) (int, int) {
	start := b.n.st()
	if q.Min < 0 {
		q.Min = 0
	}
	if q.Max != grammar.Unbounded && q.Max < q.Min {
		return start, start
	}
	if q.Max == 0 {
		return start, start
	}
	cur := start
	for i := 0; i < q.Min; i++ {
		s, a := b.term(q.Term)
		b.n.states[cur].eps = append(b.n.states[cur].eps, s)
		cur = a
	}
	if q.Max == grammar.Unbounded {
		s, a := b.term(q.Term)
		st := b.n.st()
		acc := b.n.st()
		b.n.states[st].eps = append(b.n.states[st].eps, s, acc)
		b.n.states[a].eps = append(b.n.states[a].eps, s, acc)
		b.n.states[cur].eps = append(b.n.states[cur].eps, st)
		return start, acc
	}
	for i := 0; i < q.Max-q.Min; i++ {
		s, a := b.term(q.Term)
		acc := b.n.st()
		b.n.states[cur].eps = append(b.n.states[cur].eps, s, acc)
		b.n.states[a].eps = append(b.n.states[a].eps, acc)
		cur = acc
	}
	return start, cur
}

func (b *nfaB) eps(s intset) intset {
	seen := map[int]bool{}
	var out intset
	var stack []int
	for _, v := range s {
		stack = append(stack, v)
	}
	for len(stack) > 0 {
		x := stack[len(stack)-1]
		stack = stack[:len(stack)-1]
		if seen[x] {
			continue
		}
		seen[x] = true
		out = append(out, x)
		for _, e := range b.n.states[x].eps {
			stack = append(stack, e)
		}
	}
	return out
}

func (d *dfa) state(set intset) *dfaState {
	k := set.key()
	if st, ok := d.memo[k]; ok {
		return st
	}
	st := &dfaState{set: set, trans: map[rune]*dfaState{}}
	labs := map[string]bool{}
	for _, i := range set {
		ns := d.nfa.states[i]
		if ns.acc {
			st.acc = true
		}
		for _, l := range ns.labels {
			labs[l] = true
		}
		// Named labels live on accept of alt branch; treat any labelled state in the set as live.
		if len(ns.labels) > 0 {
			st.acc = true
		}
	}
	for l := range labs {
		st.labels = append(st.labels, l)
	}
	d.memo[k] = st
	return st
}

func (d *dfa) move(set intset, r rune) intset {
	nb := nfaB{n: d.nfa}
	var out intset
	seen := map[int]bool{}
	for _, i := range set {
		for _, tr := range d.nfa.states[i].trans {
			if tr.pred != nil && tr.pred(r) && !seen[tr.to] {
				seen[tr.to] = true
				out = append(out, tr.to)
			}
		}
	}
	return nb.eps(out)
}

// match consumes the longest prefix from pos. ok is true if some accept was seen
// (including empty if start is accepting). labels are those of the longest accept.
func (d *dfa) match(input string, pos int) (end int, labels []string, ok bool) {
	if len(d.ordered) > 0 {
		for _, a := range d.ordered {
			e, labs, hit := a.match(input, pos)
			if hit {
				return e, labs, true
			}
		}
		return pos, nil, false
	}
	if len(d.alts) > 0 {
		return d.matchAlts(input, pos)
	}
	if d.hasCall() {
		return d.matchCalls(input, pos)
	}
	st := d.state(d.start)
	cur := pos
	end = pos
	labels = st.labels
	ok = st.acc
	for cur < len(input) {
		r, n := utf8.DecodeRuneInString(input[cur:])
		nx := st.trans[r]
		if nx == nil {
			mv := d.move(st.set, r)
			if len(mv) == 0 {
				break
			}
			nx = d.state(mv)
			st.trans[r] = nx
		}
		st = nx
		cur += n
		if st.acc {
			ok = true
			end = cur
			labels = st.labels
		}
	}
	return end, labels, ok
}

func (d *dfa) matchAlts(input string, pos int) (end int, labels []string, ok bool) {
	best := pos - 1
	for _, a := range d.alts {
		e, _, hit := a.d.match(input, pos)
		if !hit {
			continue
		}
		if e > best {
			best = e
			labels = []string{a.name}
			ok = true
			end = e
		} else if e == best {
			labels = append(labels, a.name)
		}
	}
	return end, labels, ok
}

func (d *dfa) hasCall() bool {
	for i := range d.nfa.states {
		for _, tr := range d.nfa.states[i].trans {
			if tr.call != "" {
				return true
			}
		}
	}
	return false
}

func (d *dfa) callee(name string) *dfa {
	if d.owner == nil {
		return nil
	}
	return d.owner.dfa[name]
}

type posSet struct {
	k string
	p int
}

func (d *dfa) matchCalls(input string, pos int) (end int, labels []string, ok bool) {
	type cur struct {
		set intset
		pos int
	}
	nb := nfaB{n: d.nfa}
	seen := map[posSet]bool{}
	q := []cur{{set: d.start, pos: pos}}
	for len(q) > 0 {
		c := q[0]
		q = q[1:]
		k := posSet{k: c.set.key(), p: c.pos}
		if seen[k] {
			continue
		}
		seen[k] = true
		st := d.state(c.set)
		if st.acc && (!ok || c.pos > end) {
			ok = true
			end = c.pos
			labels = st.labels
		}
		for _, i := range c.set {
			for _, tr := range d.nfa.states[i].trans {
				if tr.call != "" {
					sub := d.callee(tr.call)
					if sub == nil {
						continue
					}
					e, labs, hit := sub.match(input, c.pos)
					if !hit || !hasLabel(labs, tr.callLabel) {
						continue
					}
					q = append(q, cur{set: nb.eps(intset{tr.to}), pos: e})
					continue
				}
			}
		}
		if c.pos < len(input) {
			r, n := utf8.DecodeRuneInString(input[c.pos:])
			mv := d.move(c.set, r)
			if len(mv) > 0 {
				q = append(q, cur{set: mv, pos: c.pos + n})
			}
		}
	}
	return end, labels, ok
}

func hasLabel(labels []string, want string) bool {
	if want == "" {
		return true
	}
	for _, l := range labels {
		if l == want {
			return true
		}
	}
	return false
}
