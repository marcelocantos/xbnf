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
// The parse leaves two things behind: sym, which says which productions
// derive each nonterminal span (l, r) and with which evidence cell, and the
// step links, which say for each descriptor where the matches reaching it
// began and which cell they came from. A tree is a walk back along those
// links from a completion. Where more than one match reaches a descriptor
// the production's disambiguation directives choose; where they do not, the
// choice is made deterministically and counted in packed.
type builder struct {
	p      *gll
	packed int
	err    string
	nodes  []inode
	kids   []int
}

type inode struct {
	kind, name, text string
	k0, kn           int
}

func newBuilder(p *gll) *builder {
	n := len(p.input) + 1
	nodes := p.tnodes[:0]
	if cap(nodes) < n/2 {
		nodes = make([]inode, 0, n/2)
	}
	kids := p.tkids[:0]
	if cap(kids) < n {
		kids = make([]int, 0, n)
	}
	return &builder{p: p, nodes: nodes, kids: kids}
}

func (b *builder) keepArena() {
	b.p.tnodes = b.nodes
	b.p.tkids = b.kids
}

func (b *builder) addNode(kind, name, text string, kids []int) int {
	k0 := len(b.kids)
	b.kids = append(b.kids, kids...)
	id := len(b.nodes)
	b.nodes = append(b.nodes, inode{kind: kind, name: name, text: text, k0: k0, kn: k0 + len(kids)})
	return id
}

func (b *builder) intern(n Node) int {
	if len(n.Children) == 0 {
		return b.addNode(n.Kind, n.Name, n.Text, nil)
	}
	ids := make([]int, len(n.Children))
	for i, c := range n.Children {
		ids[i] = b.intern(c)
	}
	return b.addNode(n.Kind, n.Name, n.Text, ids)
}

func (b *builder) materialize(id int) Node {
	n := len(b.nodes)
	if n == 0 {
		return Node{}
	}
	out := make([]Node, n)
	next := 1
	var fill func(inodeID, outIdx int)
	fill = func(inodeID, outIdx int) {
		in := b.nodes[inodeID]
		nk := in.kn - in.k0
		child0 := next
		next += nk
		var children []Node
		if nk > 0 {
			children = out[child0 : child0+nk]
		}
		out[outIdx] = Node{Kind: in.kind, Name: in.name, Text: in.text, Children: children}
		for i, k := range b.kids[in.k0:in.kn] {
			fill(k, child0+i)
		}
	}
	fill(id, 0)
	return out[0]
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
	defer b.keepArena()
	mark := len(b.p.kscratch)
	b.p.kscratch = b.deriveInto(b.p.kscratch[:mark], start, pos, end)
	kids := b.p.kscratch[mark:]
	if len(kids) == 1 {
		n := b.materialize(kids[0])
		b.p.kscratch = b.p.kscratch[:mark]
		return n
	}
	id := b.addNode("rule", start, b.text(pos, end), kids)
	b.p.kscratch = b.p.kscratch[:mark]
	return b.materialize(id)
}

// path returns the element boundaries of one derivation of production pid
// over input[l:r]: positions[0] == l and positions[len(rhs)] == r. cell is
// the evidence cell of the completed production, from compsAt.
//
// Every step recorded against a descriptor is reachable from element 0 by
// construction — a step exists only because the descriptor before it was
// scheduled — so the walk needs no reachability test. The competitors for
// element ip-1 are exactly the steps on the current cell, and the winner's
// prev cell carries the competitors for the element before it.
func (b *builder) path(pid, l, r int, cell int32, buf []int) ([]int, bool) {
	pr := b.p.c.prods[pid]
	n := len(pr.rhs)
	var pos []int
	if n+1 <= len(buf) {
		pos = buf[:n+1]
	} else {
		pos = make([]int, n+1)
	}
	pos[n] = r
	if n == 0 {
		return pos, l == r
	}
	steps := b.p.steps
	right := pr.dirs.assoc == "right"
	ambiguous := false
	var candBuf [8]int32
	for ip := n; ip >= 1; ip-- {
		h := b.p.cells[cell]
		if h == 0 {
			return nil, false
		}
		st := steps[h]
		best, from := st.i, st.prev
		if st.next != 0 {
			cands := append(candBuf[:0], st.i)
			for k := st.next; k != 0; k = steps[k].next {
				c := steps[k]
				dup := false
				for _, x := range cands {
					if x == c.i {
						dup = true
						break
					}
				}
				if dup {
					continue
				}
				cands = append(cands, c.i)
				if (c.i > best) != right {
					best, from = c.i, c.prev
				}
			}
			if len(cands) > 1 && !right {
				switch pr.dirs.assoc {
				case "none":
					b.fail(fmt.Sprintf("ambiguous derivation of %s at %s: #assoc=none forbids chaining",
						displayNT(pr.nt), b.where(l)))
				case "left":
				default:
					ambiguous = true
				}
			}
		}
		pos[ip-1] = int(best)
		cell = from
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

// pickAt chooses among the productions that all span the same input, and
// returns the winner with the evidence cell of its completion. Priority is
// compared first, then #prefer / #avoid. Stack fallbacks lose to anything.
func (b *builder) pickAt(nt string, l, r int) (int, int32) {
	comp, extra := b.p.compsAt(b.p.c.ntNID[nt], l, r)
	if comp == 0 {
		return -1, 0
	}
	if extra == nil {
		return compPID(comp), compCell(comp)
	}
	comps := make([]int, 1, 1+len(extra))
	comps[0] = comp
	comps = append(comps, extra...)
	win := b.pick(comps)
	return compPID(win), compCell(win)
}

// pick compares packComp values. The production is in the high bits, so
// sorting the packed values orders them by production as before.
func (b *builder) pick(comps []int) int {
	if len(comps) == 1 {
		return comps[0]
	}
	prods := b.p.c.prods
	cands := append([]int{}, comps...)
	best := prods[compPID(cands[0])].dirs.priority
	for _, c := range cands[1:] {
		if p := prods[compPID(c)].dirs.priority; p > best {
			best = p
		}
	}
	cands = filter(cands, func(c int) bool { return prods[compPID(c)].dirs.priority == best })
	if pref := filter(cands, func(c int) bool { return prods[compPID(c)].dirs.prefer }); len(pref) > 0 {
		cands = pref
	} else if keep := filter(cands, func(c int) bool {
		return !prods[compPID(c)].dirs.avoid && !prods[compPID(c)].fallback
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

// splices reports whether a synthetic nonterminal is transparent: its
// children are returned as-is instead of being wrapped in a rule/quant/delim/seq
// node. Named rules and the $q/$d/$st wrappers are not spliced.
func splices(nt string) bool {
	if !strings.HasPrefix(nt, "$") {
		return false
	}
	switch {
	case strings.HasPrefix(nt, "$q") && !strings.HasPrefix(nt, "$qs") && !strings.HasPrefix(nt, "$qo"):
		return false
	case strings.HasPrefix(nt, "$d") && !strings.HasPrefix(nt, "$dl") && !strings.HasPrefix(nt, "$dtrail"):
		return false
	case strings.HasPrefix(nt, "$st"):
		return false
	default:
		return true
	}
}

func leftRec(pr prod) bool {
	return len(pr.rhs) > 0 && pr.rhs[0].kind == ekNT && pr.rhs[0].nt == pr.nt
}

// deriveInto appends the nodes for nonterminal nt over input[l:r] onto dst.
// Synthetic nonterminals are transparent, or become the quant / delim / seq
// node their construct calls for. dst is the builder scratch (same backing
// across recursive calls) so kid lists are not allocated per node.
func (b *builder) deriveInto(dst []int, nt string, l, r int) []int {
	c := b.p.c
	if c.IsDFA(nt) {
		return append(dst, b.intern(c.dfaNode(b.p.input, nt, b.p.skip(l), r)))
	}
	pid, cell := b.pickAt(nt, l, r)
	if pid < 0 {
		b.fail(fmt.Sprintf("internal: no derivation of %s over %s", displayNT(nt), b.where(l)))
		return append(dst, b.addNode("rule", nt, b.text(l, r), nil))
	}
	kidStart := len(dst)
	// Transparent left-recursive lists ($dl, $qs) must not append the prefix
	// children at every spine node — that is O(n²) Node copies.
	if splices(nt) && leftRec(c.prods[pid]) {
		dst = b.leftRecKidsInto(dst, nt, l, r)
	} else {
		dst = b.prodKidsInto(dst, pid, l, r, cell)
	}
	kids := dst[kidStart:]
	switch {
	case !strings.HasPrefix(nt, "$"):
		id := b.addNode("rule", nt, b.text(l, r), kids)
		return append(dst[:kidStart], id)
	case strings.HasPrefix(nt, "$q") && !strings.HasPrefix(nt, "$qs") && !strings.HasPrefix(nt, "$qo"):
		if len(kids) == 0 {
			return dst[:kidStart]
		}
		id := b.addNode("quant", "", b.text(l, r), kids)
		return append(dst[:kidStart], id)
	case strings.HasPrefix(nt, "$d") && !strings.HasPrefix(nt, "$dl") && !strings.HasPrefix(nt, "$dtrail"):
		if len(kids) == 1 {
			return dst[:kidStart+1]
		}
		id := b.addNode("delim", "", b.text(l, r), kids)
		return append(dst[:kidStart], id)
	case strings.HasPrefix(nt, "$st"):
		if len(kids) == 1 {
			return dst[:kidStart+1]
		}
		id := b.addNode("seq", "", b.text(l, r), kids)
		return append(dst[:kidStart], id)
	default:
		return dst
	}
}

type kidSpan struct{ start, n int }

// leftRecKidsInto walks a transparent left-recursive spine N ::= N rest | base
// once and appends base+rest onto dst. User-level left recursion is not
// spliced and still nests via prodKidsInto.
func (b *builder) leftRecKidsInto(dst []int, nt string, l, r int) []int {
	mark := len(b.p.spineBuf)
	start := len(dst)
	curR := r
	for {
		pid, cell := b.pickAt(nt, l, curR)
		if pid < 0 {
			b.fail(fmt.Sprintf("internal: no derivation of %s over %s", displayNT(nt), b.where(l)))
			b.p.spineBuf = b.p.spineBuf[:mark]
			return dst[:start]
		}
		pr := b.p.c.prods[pid]
		if !leftRec(pr) {
			dst = b.prodKidsInto(dst, pid, l, curR, cell)
			dst = reorderSpine(dst, start, b.p.spineBuf[mark:], &b.p.spineOut)
			b.p.spineBuf = b.p.spineBuf[:mark]
			return dst
		}
		var buf [8]int
		pos, ok := b.path(pid, l, curR, cell, buf[:])
		if !ok {
			b.fail(fmt.Sprintf("internal: no path through %s over %s", displayNT(pr.nt), b.where(l)))
			b.p.spineBuf = b.p.spineBuf[:mark]
			return dst[:start]
		}
		if pos[1] >= curR {
			b.fail(fmt.Sprintf("internal: left-recursive %s did not shrink over %s", displayNT(nt), b.where(l)))
			b.p.spineBuf = b.p.spineBuf[:mark]
			return dst[:start]
		}
		restStart := len(dst)
		dst = b.rhsNodesInto(dst, pr, pos, 1)
		b.p.spineBuf = append(b.p.spineBuf, kidSpan{start: restStart, n: len(dst) - restStart})
		curR = pos[1]
	}
}

func reorderSpine(dst []int, start int, spans []kidSpan, buf *[]int) []int {
	if len(spans) == 0 {
		return dst
	}
	n := len(dst) - start
	out := *buf
	if cap(out) < n {
		out = make([]int, n)
	} else {
		out = out[:n]
	}
	*buf = out
	baseStart := spans[len(spans)-1].start + spans[len(spans)-1].n
	w := copy(out, dst[baseStart:])
	for i := len(spans) - 1; i >= 0; i-- {
		s := spans[i]
		w += copy(out[w:], dst[s.start:s.start+s.n])
	}
	copy(dst[start:], out[:n])
	return dst[:start+n]
}

func (b *builder) prodKidsInto(dst []int, pid, l, r int, cell int32) []int {
	pr := b.p.c.prods[pid]
	var buf [8]int
	pos, ok := b.path(pid, l, r, cell, buf[:])
	if !ok {
		b.fail(fmt.Sprintf("internal: no path through %s over %s", displayNT(pr.nt), b.where(l)))
		return dst
	}
	return b.rhsNodesInto(dst, pr, pos, 0)
}

func (b *builder) rhsNodesInto(dst []int, pr prod, pos []int, from int) []int {
	for ip := from; ip < len(pr.rhs); ip++ {
		dst = b.appendElem(dst, pr.rhs[ip], pos[ip], pos[ip+1])
	}
	return dst
}

func (b *builder) appendElem(kids []int, e elem, i, end int) []int {
	switch e.kind {
	case ekTerm:
		switch e.term.(type) {
		case grammar.Empty, grammar.PosProp:
			return kids
		case grammar.Ref:
			return append(kids, b.addNode("ref", e.name, b.text(i, end), nil))
		case grammar.String:
			return append(kids, b.addNode("string", e.name, b.text(i, end), nil))
		default:
			return append(kids, b.addNode("char", e.name, b.text(i, end), nil))
		}
	case ekDFA:
		if strings.HasPrefix(e.nt, "$lf") {
			return append(kids, b.addNode("leaf", e.name, b.text(i, end), nil))
		}
		n := b.p.c.dfaNode(b.p.input, e.nt, b.p.skip(i), end)
		if e.name != "" {
			n.Name = e.name
		}
		return append(kids, b.intern(n))
	case ekNT:
		start := len(kids)
		kids = b.deriveInto(kids, e.nt, i, end)
		if e.name != "" {
			added := kids[start:]
			if len(added) == 1 {
				b.nodes[added[0]].name = e.name
			} else if len(added) > 1 {
				id := b.addNode("seq", e.name, b.text(i, end), added)
				kids = append(kids[:start], id)
			}
		}
		return kids
	default:
		return kids
	}
}

// dfaNode is the node for a regular rule matched over input[l:r]. A /leaf/
// body is one string. Other regular bodies keep their structure by walking
// the rule body over the span; if that walk does not land exactly on r the
// node degrades to the matched text.
//
// The Children of the result borrow the walker's arena and stay valid only
// until the next dfaNode call on this Compiled. Every caller here interns
// the node immediately; one that keeps it must use dfaNodeOwned.
func (c *Compiled) dfaNode(input, name string, l, r int) Node {
	rule, ok := c.rules[name]
	if !ok {
		return Node{Kind: "leaf", Name: name, Text: input[l:r]}
	}
	if _, isLeaf := rule.Body.(grammar.Leaf); isLeaf {
		return Node{Kind: "leaf", Name: name, Text: input[l:r]}
	}
	w := &c.tw
	w.c = c
	w.input = input
	w.busy = w.busy[:0]
	w.stack = w.stack[:0]
	w.arena = w.arena[:0]
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

// dfaNodeOwned is dfaNode for a caller that keeps the tree past the next
// call: it copies the children out of the walker's arena.
func (c *Compiled) dfaNodeOwned(input, name string, l, r int) Node {
	return cloneNode(c.dfaNode(input, name, l, r))
}

func cloneNode(n Node) Node {
	if len(n.Children) == 0 {
		return n
	}
	kids := make([]Node, len(n.Children))
	for i, c := range n.Children {
		kids[i] = cloneNode(c)
	}
	n.Children = kids
	return n
}

// twalk walks a regular rule body over input to recover structure that the
// DFA match flattened. It is greedy: alternatives take the longest match and
// repetition never backs off. dfaNode checks its result against the DFA's
// extent before trusting it.
type twalk struct {
	c     *Compiled
	input string
	// busy is the set of rules already being walked, one entry per name, as
	// the map it replaces was.
	busy []busyRule
	// stack holds the kids of every node under construction; commit moves a
	// finished run into arena, which backs the Children of the tree handed
	// back to the caller.
	stack []Node
	arena []Node
}

type busyRule struct {
	name string
	pos  int
}

func (w *twalk) busyIndex(name string) int {
	for i := range w.busy {
		if w.busy[i].name == name {
			return i
		}
	}
	return -1
}

func (w *twalk) dropBusy(name string) {
	if i := w.busyIndex(name); i >= 0 {
		w.busy = append(w.busy[:i], w.busy[i+1:]...)
	}
}

// push adds n to the scratch stack the way the derivation builder would:
// unnamed groups are spliced, and nodes that match nothing are dropped.
func (w *twalk) push(n Node) {
	switch n.Kind {
	case "empty", "lookahead", "neg_lookahead":
		return
	case "seq":
		if n.Name == "" {
			w.stack = append(w.stack, n.Children...)
			return
		}
	case "quant":
		if len(n.Children) == 0 {
			return
		}
	}
	w.stack = append(w.stack, n)
}

// commit copies the scratch entries above mark into the arena and returns
// them as one node's Children. Growing the arena leaves earlier runs behind
// in the old backing array, which is what they already point at.
func (w *twalk) commit(mark int) []Node {
	kids := w.stack[mark:]
	if len(kids) == 0 {
		w.stack = w.stack[:mark]
		return nil
	}
	off := len(w.arena)
	w.arena = append(w.arena, kids...)
	w.stack = w.stack[:mark]
	return w.arena[off:len(w.arena):len(w.arena)]
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
	if i := w.busyIndex(name); i >= 0 {
		if w.busy[i].pos == pos {
			return pos, Node{}, false
		}
		w.busy[i].pos = pos
	} else {
		w.busy = append(w.busy, busyRule{name: name, pos: pos})
	}
	defer w.dropBusy(name)

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
	case grammar.Empty, grammar.PosProp:
		return pos, Node{Kind: "empty"}, true
	case grammar.Ref:
		return pos, Node{}, false
	case grammar.Seq:
		mark := len(w.stack)
		cur := pos
		for _, t := range x.Terms {
			end, n, ok := w.term(t, cur, nowrap)
			if !ok {
				w.stack = w.stack[:mark]
				return pos, Node{}, false
			}
			w.push(n)
			cur = end
		}
		return cur, Node{Kind: "seq", Children: w.commit(mark), Text: w.input[pos:cur]}, true
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
		mark := len(w.stack)
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
				w.push(node)
				n++
				break
			}
			w.push(node)
			cur = end
			n++
		}
		if n < x.Min {
			w.stack = w.stack[:mark]
			return pos, Node{}, false
		}
		return cur, Node{Kind: "quant", Children: w.commit(mark), Text: w.input[pos:cur]}, true
	case grammar.Delim:
		return w.delim(x, pos, nowrap)
	case grammar.Scope:
		return w.term(x.Term, pos, nowrap)
	case grammar.CaseFold:
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
	mark := len(w.stack)
	cur := pos
	if d.Leading {
		if end, n, ok := w.term(d.Sep, cur, nowrap); ok {
			w.push(n)
			cur = end
		}
	}
	end, n, ok := w.term(d.Term, cur, nowrap)
	if !ok {
		w.stack = w.stack[:mark]
		return pos, Node{}, false
	}
	w.push(n)
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
				w.push(sn)
				cur = se
			} else {
				cur = save
			}
			break
		}
		w.push(sn)
		w.push(tn)
		cur = te
	}
	if len(w.stack)-mark == 1 {
		only := w.stack[mark]
		w.stack = w.stack[:mark]
		return cur, only, true
	}
	return cur, Node{Kind: "delim", Children: w.commit(mark), Text: w.input[pos:cur]}, true
}
