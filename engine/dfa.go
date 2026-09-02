// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package engine

import (
	"unicode/utf8"

	"github.com/marcelocantos/xbnf/grammar"
)

type nfaTrans struct {
	pred        func(rune) bool
	beyondASCII bool // pred can match a rune > 255 (from the original term)
	to          int
	call        string // regular rule invoked as an atomic longest-match DFA
	callLabel   string // required ::label of that call, if any
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
	start0  *dfaState // memoised state(start)
	memo    map[string]*dfaState
	alts    []labAlt
	ordered []*dfa // |> : first matching alt wins
	owner   *Compiled
	calls   bool // some transition invokes another regular rule
}

// dead marks a cached transition with no target.
var dead = &dfaState{}

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
	d.calls = d.hasCall()
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
		b.n.states[s].trans = append(b.n.states[s].trans, nfaTrans{
			pred: runePred(x), beyondASCII: termBeyondASCII(x), to: a,
		})
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
			a = b.gap(a)
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
		// Term (Sep Term)*, with #wrap between items and separators as in
		// the GLL path. Acceptance sits right after a term (or a trailing
		// separator), so trailing wrap belongs to the enclosing sequence.
		ts, ta := b.term(x.Term)
		ss, sa := b.term(x.Sep)
		acc := b.n.st()
		b.n.states[ta].eps = append(b.n.states[ta].eps, acc)
		afterTerm := b.gap(ta)
		b.n.states[afterTerm].eps = append(b.n.states[afterTerm].eps, ss)
		afterSep := b.gap(sa)
		b.n.states[afterSep].eps = append(b.n.states[afterSep].eps, ts)
		start := ts
		if x.Leading {
			st := b.n.st()
			b.n.states[st].eps = append(b.n.states[st].eps, ts, ss)
			start = st
		}
		if x.Trailing {
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
		savedWrap := b.nowrap
		type savedRule struct {
			name string
			r    grammar.Rule
			ok   bool
		}
		var saved []savedRule
		for _, d := range x.Decls {
			switch st := d.(type) {
			case grammar.Wrap:
				if _, emp := st.Body.(grammar.Empty); emp {
					b.nowrap = true
				}
			case grammar.Rule:
				old, ok := b.c.rules[st.Name]
				saved = append(saved, savedRule{st.Name, old, ok})
				if b.c.rules == nil {
					b.c.rules = map[string]grammar.Rule{}
				}
				b.c.rules[st.Name] = st
			}
		}
		s, a := b.term(x.Term)
		for i := len(saved) - 1; i >= 0; i-- {
			sr := saved[i]
			if sr.ok {
				b.c.rules[sr.name] = sr.r
			} else {
				delete(b.c.rules, sr.name)
			}
		}
		b.nowrap = savedWrap
		return s, a
	case grammar.Lookahead, grammar.NegLookahead:
		s := b.n.st()
		return s, s
	default:
		s := b.n.st()
		return s, s
	}
}

// gap appends the #wrap fragment after state a and returns the new tail.
// Inside a wrap-off scope, or with no wrap, it returns a unchanged.
func (b *nfaB) gap(a int) int {
	if b.nowrap || b.c.wrap == nil {
		return a
	}
	if _, emp := b.c.wrap.(grammar.Empty); emp {
		return a
	}
	b.nowrap = true
	ws, wa := b.term(b.c.wrap)
	b.nowrap = false
	b.n.states[a].eps = append(b.n.states[a].eps, ws)
	return wa
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
			pred:        func(x rune) bool { return x == rr },
			beyondASCII: rr > 255,
			to:          nx,
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
	// Each iteration after the first is preceded by #wrap, as in the GLL path.
	cur := start
	for i := 0; i < q.Min; i++ {
		if i > 0 {
			cur = b.gap(cur)
		}
		s, a := b.term(q.Term)
		b.n.states[cur].eps = append(b.n.states[cur].eps, s)
		cur = a
	}
	if q.Max == grammar.Unbounded {
		acc := b.n.st()
		b.n.states[cur].eps = append(b.n.states[cur].eps, acc)
		entry := cur
		if q.Min > 0 {
			entry = b.gap(cur)
		}
		s, a := b.term(q.Term)
		b.n.states[entry].eps = append(b.n.states[entry].eps, s)
		b.n.states[a].eps = append(b.n.states[a].eps, acc)
		again := b.gap(a)
		b.n.states[again].eps = append(b.n.states[again].eps, s)
		return start, acc
	}
	for i := q.Min; i < q.Max; i++ {
		acc := b.n.st()
		b.n.states[cur].eps = append(b.n.states[cur].eps, acc)
		entry := cur
		if i > 0 {
			entry = b.gap(cur)
		}
		s, a := b.term(q.Term)
		b.n.states[entry].eps = append(b.n.states[entry].eps, s)
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
	if d.calls {
		return d.matchCalls(input, pos)
	}
	st := d.initial()
	cur := pos
	end = pos
	labels = st.labels
	ok = st.acc
	for cur < len(input) {
		r, n := utf8.DecodeRuneInString(input[cur:])
		nx := d.step(st, r)
		if nx == nil {
			break
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

func (d *dfa) initial() *dfaState {
	if d.start0 == nil {
		d.start0 = d.state(d.start)
	}
	return d.start0
}

// step returns the state after consuming r from st, or nil. Misses are cached.
func (d *dfa) step(st *dfaState, r rune) *dfaState {
	nx := st.trans[r]
	if nx == nil {
		mv := d.move(st.set, r)
		if len(mv) == 0 {
			st.trans[r] = dead
			return nil
		}
		nx = d.state(mv)
		st.trans[r] = nx
	}
	if nx == dead {
		return nil
	}
	return nx
}

// canStart reports whether some match of d can begin with r. Conservative:
// DFAs that call other rules answer true.
func (d *dfa) canStart(r rune) bool {
	if len(d.ordered) > 0 {
		for _, a := range d.ordered {
			if a.canStart(r) {
				return true
			}
		}
		return false
	}
	if len(d.alts) > 0 {
		for _, a := range d.alts {
			if a.d.canStart(r) {
				return true
			}
		}
		return false
	}
	if d.calls {
		return true
	}
	return d.step(d.initial(), r) != nil
}

// nullable reports whether d can match the empty string. Conservative in the
// same way as canStart.
func (d *dfa) nullable() bool {
	if len(d.ordered) > 0 {
		for _, a := range d.ordered {
			if a.nullable() {
				return true
			}
		}
		return false
	}
	if len(d.alts) > 0 {
		for _, a := range d.alts {
			if a.d.nullable() {
				return true
			}
		}
		return false
	}
	if d.calls {
		return true
	}
	return d.initial().acc
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
