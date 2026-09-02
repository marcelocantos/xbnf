// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package engine

import (
	"fmt"
	"sort"
	"strings"

	"github.com/marcelocantos/xbnf/grammar"
)

// builder turns the derivations recorded by a GLL parse into one tree.
//
// The parse leaves two things behind: sym, which says which productions of
// each nonterminal span (l, r), and trace, which says for each production
// instance which input span every element matched. A tree is a path through
// those records. Where more than one path exists the production's
// disambiguation directives choose; where they do not, the choice is made
// deterministically and counted in packed.
type builder struct {
	p      *gll
	packed int
	err    string
	inst   map[instKey]*instIndex
	steps  map[instKey][]step
}

type instKey struct{ pid, l int }

// instIndex indexes one production instance's steps by (element, end).
type instIndex struct {
	pred []map[int][]int // pred[ip][end] = starts i with a step (ip, i → end)
	back map[[2]int]bool // memo: is (ip, pos) reachable from (0, l)?
}

func newBuilder(p *gll) *builder {
	b := &builder{p: p, inst: map[instKey]*instIndex{}, steps: map[instKey][]step{}}
	for _, st := range p.trace {
		k := instKey{pid: st.pid, l: st.l}
		b.steps[k] = append(b.steps[k], st)
	}
	return b
}

func (b *builder) text(l, r int) string {
	l = b.p.skip(l)
	if l > r {
		l = r
	}
	return b.p.input[l:r]
}

// root builds the tree for the start rule over input[pos:end].
func (b *builder) root(start string, pos, end int) Node {
	kids := b.derive(start, pos, end)
	if len(kids) == 1 {
		return kids[0]
	}
	return Node{Kind: "rule", Name: start, Text: b.text(pos, end), Children: kids}
}

func (b *builder) index(k instKey) *instIndex {
	if idx, ok := b.inst[k]; ok {
		return idx
	}
	n := len(b.p.c.prods[k.pid].rhs)
	idx := &instIndex{pred: make([]map[int][]int, n), back: map[[2]int]bool{}}
	for ip := range idx.pred {
		idx.pred[ip] = map[int][]int{}
	}
	for _, st := range b.steps[k] {
		if st.ip < 0 || st.ip >= n {
			continue
		}
		list := idx.pred[st.ip][st.end]
		dup := false
		for _, i := range list {
			if i == st.i {
				dup = true
				break
			}
		}
		if !dup {
			idx.pred[st.ip][st.end] = append(list, st.i)
		}
	}
	b.inst[k] = idx
	return idx
}

// reachable reports whether element boundary (ip, pos) of the instance can be
// reached from its start.
func (b *builder) reachable(idx *instIndex, l, ip, pos int) bool {
	if ip == 0 {
		return pos == l
	}
	key := [2]int{ip, pos}
	if v, ok := idx.back[key]; ok {
		return v
	}
	idx.back[key] = false // cycle guard
	out := false
	for _, i := range idx.pred[ip-1][pos] {
		if b.reachable(idx, l, ip-1, i) {
			out = true
			break
		}
	}
	idx.back[key] = out
	return out
}

// path returns the element boundaries of one derivation of production pid
// over input[l:r]: positions[0] == l and positions[len(rhs)] == r.
func (b *builder) path(pid, l, r int) ([]int, bool) {
	pr := b.p.c.prods[pid]
	n := len(pr.rhs)
	pos := make([]int, n+1)
	pos[n] = r
	if n == 0 {
		return pos, l == r
	}
	idx := b.index(instKey{pid: pid, l: l})
	ambiguous := false
	for ip := n; ip >= 1; ip-- {
		var cands []int
		for _, i := range idx.pred[ip-1][pos[ip]] {
			if b.reachable(idx, l, ip-1, i) {
				cands = append(cands, i)
			}
		}
		if len(cands) == 0 {
			return nil, false
		}
		if len(cands) > 1 {
			// Larger start = shorter final operand = left nesting.
			switch pr.dirs.assoc {
			case "right":
				sort.Ints(cands)
			case "none":
				b.fail(fmt.Sprintf("ambiguous derivation of %s at %s: #assoc=none forbids chaining",
					displayNT(pr.nt), b.where(l)))
				sort.Sort(sort.Reverse(sort.IntSlice(cands)))
			case "left":
				sort.Sort(sort.Reverse(sort.IntSlice(cands)))
			default:
				ambiguous = true
				sort.Sort(sort.Reverse(sort.IntSlice(cands)))
			}
		}
		pos[ip-1] = cands[0]
	}
	if pos[0] != l {
		return nil, false
	}
	if ambiguous {
		b.packed++
	}
	return pos, true
}

func (b *builder) fail(msg string) {
	if b.err == "" {
		b.err = msg
	}
}

func (b *builder) where(pos int) string {
	line, col := lineCol(b.p.input, pos)
	return fmt.Sprintf("%d:%d", line, col)
}

// pick chooses among productions that all span the same input. Priority is
// compared first, then #prefer / #avoid. Stack fallbacks lose to anything.
func (b *builder) pick(pids []int) int {
	if len(pids) == 1 {
		return pids[0]
	}
	prods := b.p.c.prods
	cands := append([]int{}, pids...)
	best := prods[cands[0]].dirs.priority
	for _, pid := range cands[1:] {
		if p := prods[pid].dirs.priority; p > best {
			best = p
		}
	}
	cands = filter(cands, func(pid int) bool { return prods[pid].dirs.priority == best })
	if pref := filter(cands, func(pid int) bool { return prods[pid].dirs.prefer }); len(pref) > 0 {
		cands = pref
	} else if keep := filter(cands, func(pid int) bool {
		return !prods[pid].dirs.avoid && !prods[pid].fallback
	}); len(keep) > 0 {
		cands = keep
	}
	if len(cands) > 1 {
		b.packed++
	}
	sort.Ints(cands)
	return cands[0]
}

func filter(xs []int, keep func(int) bool) []int {
	var out []int
	for _, x := range xs {
		if keep(x) {
			out = append(out, x)
		}
	}
	return out
}

// derive returns the nodes for nonterminal nt over input[l:r]. Synthetic
// nonterminals introduced by compilation are transparent, or become the
// quant / delim / seq node their construct calls for.
func (b *builder) derive(nt string, l, r int) []Node {
	c := b.p.c
	if c.IsDFA(nt) {
		return []Node{c.dfaNode(b.p.input, nt, b.p.skip(l), r)}
	}
	pids := b.p.sym[famKey{nt: nt, l: l, r: r}]
	if len(pids) == 0 {
		b.fail(fmt.Sprintf("internal: no derivation of %s over %s", displayNT(nt), b.where(l)))
		return []Node{{Kind: "rule", Name: nt, Text: b.text(l, r)}}
	}
	pid := b.pick(pids)
	kids := b.prodKids(pid, l, r)
	switch {
	case !strings.HasPrefix(nt, "$"):
		return []Node{{Kind: "rule", Name: nt, Text: b.text(l, r), Children: kids}}
	case strings.HasPrefix(nt, "$q") && !strings.HasPrefix(nt, "$qs") && !strings.HasPrefix(nt, "$qo"):
		if len(kids) == 0 {
			return nil
		}
		return []Node{{Kind: "quant", Text: b.text(l, r), Children: kids}}
	case strings.HasPrefix(nt, "$d") && !strings.HasPrefix(nt, "$dl") && !strings.HasPrefix(nt, "$dtrail"):
		if len(kids) == 1 {
			return kids
		}
		return []Node{{Kind: "delim", Text: b.text(l, r), Children: kids}}
	case strings.HasPrefix(nt, "$st"):
		if len(kids) == 1 {
			return kids
		}
		return []Node{{Kind: "seq", Text: b.text(l, r), Children: kids}}
	default:
		return kids
	}
}

func (b *builder) prodKids(pid, l, r int) []Node {
	pr := b.p.c.prods[pid]
	pos, ok := b.path(pid, l, r)
	if !ok {
		b.fail(fmt.Sprintf("internal: no path through %s over %s", displayNT(pr.nt), b.where(l)))
		return nil
	}
	var kids []Node
	for ip, e := range pr.rhs {
		i, end := pos[ip], pos[ip+1]
		var nodes []Node
		switch e.kind {
		case ekTerm:
			switch e.term.(type) {
			case grammar.Empty:
				continue
			case grammar.String:
				nodes = []Node{{Kind: "string", Text: b.text(i, end)}}
			default:
				nodes = []Node{{Kind: "char", Text: b.text(i, end)}}
			}
		case ekDFA:
			if strings.HasPrefix(e.nt, "$lf") {
				nodes = []Node{{Kind: "leaf", Text: b.text(i, end)}}
			} else {
				nodes = []Node{b.p.c.dfaNode(b.p.input, e.nt, b.p.skip(i), end)}
			}
		case ekNT:
			nodes = b.derive(e.nt, i, end)
		default:
			continue
		}
		if e.name != "" {
			if len(nodes) == 1 {
				nodes[0].Name = e.name
			} else {
				nodes = []Node{{Kind: "seq", Name: e.name, Text: b.text(i, end), Children: nodes}}
			}
		}
		kids = append(kids, nodes...)
	}
	return kids
}

// dfaNode is the node for a regular rule matched over input[l:r]. A /leaf/
// body is one string. Other regular bodies keep their structure by walking
// the rule body over the span; if that walk does not land exactly on r the
// node degrades to the matched text.
func (c *Compiled) dfaNode(input, name string, l, r int) Node {
	rule, ok := c.rules[name]
	if !ok {
		return Node{Kind: "leaf", Name: name, Text: input[l:r]}
	}
	if _, isLeaf := rule.Body.(grammar.Leaf); isLeaf {
		return Node{Kind: "leaf", Name: name, Text: input[l:r]}
	}
	w := &twalk{c: c, input: input, busy: map[string]int{}}
	end, n, ok := w.term(rule.Body, l, false)
	if !ok || end != r {
		return Node{Kind: "leaf", Name: name, Text: input[l:r]}
	}
	if n.Kind == "leaf" {
		n.Name = name
		return n
	}
	n.Kind = "rule"
	n.Name = name
	n.Text = input[l:r]
	return n
}

// twalk walks a regular rule body over input to recover structure that the
// DFA match flattened. It is greedy: alternatives take the longest match and
// repetition never backs off. dfaNode checks its result against the DFA's
// extent before trusting it.
type twalk struct {
	c     *Compiled
	input string
	busy  map[string]int
}

// appendKid adds n to kids the way the derivation builder would: unnamed
// groups are spliced, and nodes that match nothing are dropped.
func appendKid(kids []Node, n Node) []Node {
	switch n.Kind {
	case "empty", "lookahead", "neg_lookahead":
		return kids
	case "seq":
		if n.Name == "" {
			return append(kids, n.Children...)
		}
	case "quant":
		if len(n.Children) == 0 {
			return kids
		}
	}
	return append(kids, n)
}

func (w *twalk) skip(pos int, nowrap bool) int {
	if nowrap {
		return pos
	}
	return w.c.skipWrap(w.input, pos)
}

func (w *twalk) rule(name, label string, pos int, nowrap bool) (int, Node, bool) {
	r, ok := w.c.rules[name]
	if !ok {
		return pos, Node{}, false
	}
	if p, busy := w.busy[name]; busy && p == pos {
		return pos, Node{}, false
	}
	w.busy[name] = pos
	defer delete(w.busy, name)

	if w.c.IsDFA(name) {
		if _, isLeaf := r.Body.(grammar.Leaf); isLeaf {
			end, labs, ok := w.c.dfa[name].match(w.input, pos)
			if !ok || !hasLabel(labs, label) {
				return pos, Node{}, false
			}
			return end, Node{Kind: "leaf", Name: name, Text: w.input[pos:end]}, true
		}
	}
	end, n, ok := w.term(r.Body, pos, nowrap)
	if !ok {
		return pos, Node{}, false
	}
	if n.Kind == "leaf" {
		n.Name = name
		return end, n, true
	}
	n.Kind = "rule"
	n.Name = name
	n.Text = w.input[pos:end]
	return end, n, true
}

func (w *twalk) term(t grammar.Term, pos int, nowrap bool) (int, Node, bool) {
	pos = w.skip(pos, nowrap)
	switch x := t.(type) {
	case grammar.Leaf:
		end, _, ok := w.term(x.Term, pos, true)
		if !ok {
			return pos, Node{}, false
		}
		return end, Node{Kind: "leaf", Text: w.input[pos:end]}, true
	case grammar.Ident:
		return w.rule(x.Name, x.Label, pos, nowrap)
	case grammar.Named:
		end, n, ok := w.term(x.Term, pos, nowrap)
		if !ok {
			return pos, Node{}, false
		}
		n.Name = x.Name
		return end, n, true
	case grammar.String:
		end, ok := matchTerminal(x, w.input, pos)
		if !ok {
			return pos, Node{}, false
		}
		return end, Node{Kind: "string", Text: x.Text}, true
	case grammar.CharClass, grammar.Escape, grammar.AnyChar:
		end, ok := matchTerminal(x, w.input, pos)
		if !ok {
			return pos, Node{}, false
		}
		return end, Node{Kind: "char", Text: w.input[pos:end]}, true
	case grammar.Empty:
		return pos, Node{Kind: "empty"}, true
	case grammar.Seq:
		kids := make([]Node, 0, len(x.Terms))
		cur := pos
		for _, t := range x.Terms {
			end, n, ok := w.term(t, cur, nowrap)
			if !ok {
				return pos, Node{}, false
			}
			kids = appendKid(kids, n)
			cur = end
		}
		return cur, Node{Kind: "seq", Children: kids, Text: w.input[pos:cur]}, true
	case grammar.OrderedAlt:
		for _, a := range x.Terms {
			end, n, ok := w.term(a, pos, nowrap)
			if ok {
				return end, n, true
			}
		}
		return pos, Node{}, false
	case grammar.Alt:
		bestEnd := pos
		var best Node
		hit := false
		for _, a := range x.Terms {
			end, n, ok := w.term(a, pos, nowrap)
			if !ok {
				continue
			}
			if !hit || end > bestEnd {
				hit = true
				bestEnd = end
				best = n
			}
		}
		if !hit {
			return pos, Node{}, false
		}
		return bestEnd, best, true
	case grammar.Quant:
		var kids []Node
		cur := pos
		n := 0
		for x.Max == grammar.Unbounded || n < x.Max {
			end, node, ok := w.term(x.Term, cur, nowrap)
			if !ok || end < cur {
				break
			}
			if end == cur {
				if x.Max == grammar.Unbounded {
					break
				}
				kids = appendKid(kids, node)
				n++
				break
			}
			kids = appendKid(kids, node)
			cur = end
			n++
		}
		if n < x.Min {
			return pos, Node{}, false
		}
		return cur, Node{Kind: "quant", Children: kids, Text: w.input[pos:cur]}, true
	case grammar.Delim:
		return w.delim(x, pos, nowrap)
	case grammar.Scope:
		return w.term(x.Term, pos, nowrap)
	case grammar.Lookahead:
		_, _, ok := w.term(x.Term, pos, nowrap)
		if !ok {
			return pos, Node{}, false
		}
		return pos, Node{Kind: "lookahead"}, true
	case grammar.NegLookahead:
		_, _, ok := w.term(x.Term, pos, nowrap)
		if ok {
			return pos, Node{}, false
		}
		return pos, Node{Kind: "neg_lookahead"}, true
	default:
		end, ok := matchTerminal(t, w.input, pos)
		if !ok {
			return pos, Node{}, false
		}
		return end, Node{Kind: "term", Text: w.input[pos:end]}, true
	}
}

func (w *twalk) delim(d grammar.Delim, pos int, nowrap bool) (int, Node, bool) {
	cur := pos
	var kids []Node
	if d.Leading {
		if end, n, ok := w.term(d.Sep, cur, nowrap); ok {
			kids = appendKid(kids, n)
			cur = end
		}
	}
	end, n, ok := w.term(d.Term, cur, nowrap)
	if !ok {
		return pos, Node{}, false
	}
	kids = appendKid(kids, n)
	cur = end
	for {
		save := cur
		se, sn, sok := w.term(d.Sep, cur, nowrap)
		if !sok {
			break
		}
		te, tn, tok := w.term(d.Term, se, nowrap)
		if !tok {
			if d.Trailing {
				kids = appendKid(kids, sn)
				cur = se
			} else {
				cur = save
			}
			break
		}
		kids = appendKid(appendKid(kids, sn), tn)
		cur = te
	}
	if len(kids) == 1 {
		return cur, kids[0], true
	}
	return cur, Node{Kind: "delim", Children: kids, Text: w.input[pos:cur]}, true
}
