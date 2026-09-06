// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package engine

import (
	"fmt"
	"sort"

	"github.com/marcelocantos/xbnf/grammar"
)

// The Node.Kind vocabulary the builder emits. Compile time picks the kind for
// each nonterminal and each element, so these are the strings it stores.
const (
	nodeKindRule   = "rule"
	nodeKindQuant  = "quant"
	nodeKindDelim  = "delim"
	nodeKindSeq    = "seq"
	nodeKindLeaf   = "leaf"
	nodeKindRef    = "ref"
	nodeKindString = "string"
	nodeKindChar   = "char"
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
	kids   []int32
	// lits holds the text of the nodes whose text is not a span of the
	// input; see inode.lo.
	lits []string
}

// inode is one node of the arena the builder derives into, before
// materialize copies the tree out. Node ids, child ranges and input offsets
// are int32: an input large enough to overflow one would need tens of
// gigabytes of chart, and the arena is walked once per node, so its width is
// memory traffic on the hottest loop in the builder.
type inode struct {
	kind, name string
	// The node's text is input[lo:hi], except when lo == litText: a node
	// interned from the DFA walker can carry text that is not a span of the
	// input — a case-folded string literal keeps the grammar's spelling —
	// and then hi indexes builder.lits.
	lo, hi int32
	k0, kn int32
}

// litText marks an inode whose text is in builder.lits rather than a span of
// the input.
const litText = -1

func newBuilder(p *gll) *builder {
	n := len(p.input) + 1
	nodes := p.tnodes[:0]
	if cap(nodes) < n/2 {
		nodes = make([]inode, 0, n/2)
	}
	kids := p.tkids[:0]
	if cap(kids) < n {
		kids = make([]int32, 0, n)
	}
	return &builder{p: p, nodes: nodes, kids: kids, lits: p.tlits[:0]}
}

func (b *builder) keepArena() {
	b.p.tnodes = b.nodes
	b.p.tkids = b.kids
	b.p.tlits = b.lits
}

func (b *builder) addNode(kind, name string, lo, hi int32, kids []int32) int32 {
	k0 := int32(len(b.kids))
	b.kids = append(b.kids, kids...)
	id := int32(len(b.nodes))
	b.nodes = append(b.nodes, inode{kind: kind, name: name, lo: lo, hi: hi, k0: k0, kn: int32(len(b.kids))})
	return id
}

// addLit is addNode for text the input does not contain verbatim.
func (b *builder) addLit(kind, name, text string, kids []int32) int32 {
	b.lits = append(b.lits, text)
	return b.addNode(kind, name, litText, int32(len(b.lits)-1), kids)
}

// intern copies a Node tree the DFA walker produced into the arena. Child ids
// accumulate on a shared scratch stack rather than one slice per node; the
// scratch is separate from the derivation scratch, which is live in the caller
// while this runs.
func (b *builder) intern(n Node) int32 {
	if len(n.Children) == 0 {
		return b.addLit(n.Kind, n.Name, n.Text, nil)
	}
	mark := len(b.p.iscratch)
	for _, c := range n.Children {
		b.p.iscratch = append(b.p.iscratch, b.intern(c))
	}
	id := b.addLit(n.Kind, n.Name, n.Text, b.p.iscratch[mark:])
	b.p.iscratch = b.p.iscratch[:mark]
	return id
}

// materialize copies the arena tree rooted at id into the one []Node the
// public API hands back, breadth first: a node's children are the run of
// output slots that follows everything already queued, so the queue index is
// the output index and the whole copy is a single forward loop.
func (b *builder) materialize(id int32) Node {
	n := len(b.nodes)
	if n == 0 {
		return Node{}
	}
	out := make([]Node, n)
	// The queue never exceeds the arena: every node has one parent, so it is
	// enqueued once.
	q := b.p.mqueue
	if cap(q) < n {
		q = make([]int32, 1, n)
	} else {
		q = q[:1]
	}
	q[0] = id
	input := b.p.input
	for i := 0; i < len(q); i++ {
		in := &b.nodes[q[i]]
		var children []Node
		if in.kn > in.k0 {
			child0 := len(q)
			q = append(q, b.kids[in.k0:in.kn]...)
			children = out[child0:len(q)]
		}
		text := ""
		if in.lo >= 0 {
			text = input[in.lo:in.hi]
		} else {
			text = b.lits[in.hi]
		}
		out[i] = Node{Kind: in.kind, Name: in.name, Text: text, Children: children}
	}
	b.p.mqueue = q
	return out[0]
}

// span is the text of a node covering input[l:r] as the offsets an inode
// stores: leading #wrap text belongs to whatever preceded the node, so the
// left edge moves past it.
func (b *builder) span(l, r int) (int32, int32) {
	l = b.p.skip(l)
	if l > r {
		l = r
	}
	return int32(l), int32(r)
}

// root builds the tree for the start rule over input[pos:end]. It is the one
// place a nonterminal is still named: everything below works on ids.
func (b *builder) root(start string, pos, end int) Node {
	defer b.keepArena()
	c := b.p.c
	if c.IsDFA(start) {
		return b.materialize(b.intern(c.dfaNode(b.p.input, start, b.p.skip(pos), end)))
	}
	nid, ok := c.ntNID[start]
	if !ok {
		b.fail(fmt.Sprintf("internal: no derivation of %s over %s", displayNT(start), b.where(pos)))
		lo, hi := b.span(pos, end)
		return b.materialize(b.addNode(nodeKindRule, start, lo, hi, nil))
	}
	mark := len(b.p.kscratch)
	b.p.kscratch = b.deriveInto(b.p.kscratch[:mark], nid, pos, end)
	kids := b.p.kscratch[mark:]
	if len(kids) == 1 {
		n := b.materialize(kids[0])
		b.p.kscratch = b.p.kscratch[:mark]
		return n
	}
	lo, hi := b.span(pos, end)
	id := b.addNode(nodeKindRule, start, lo, hi, kids)
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
	pr := &b.p.c.prods[pid]
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
	right := pr.dirs.assoc == assocRight
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
				case assocNone:
					b.fail(fmt.Sprintf("ambiguous derivation of %s at %s: #assoc=none forbids chaining",
						displayNT(pr.nt), b.where(l)))
				case assocLeft:
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
//
// pickAt and pick share one scratch slice (b.p.pickBuf) across every
// ambiguous span in the parse: compsAt appends the extra completions onto it
// (after a slot reserved for the primary one), and pick filters it in place,
// so a packed span costs no allocation once the buffer has grown to its
// high-water mark.
func (b *builder) pickAt(nid, l, r int) (int, int32) {
	buf := append(b.p.pickBuf[:0], 0) // reserve slot 0 for the primary completion
	comp, buf := b.p.compsAt(nid, l, r, buf)
	b.p.pickBuf = buf
	if comp == 0 {
		return -1, 0
	}
	if len(buf) == 1 {
		return compPID(comp), compCell(comp)
	}
	buf[0] = comp
	win := b.pick(buf)
	return compPID(win), compCell(win)
}

// pick compares packComp values. The production is in the high bits, so
// sorting the packed values orders them by production as before.
func (b *builder) pick(comps []int) int {
	if len(comps) == 1 {
		return comps[0]
	}
	prods := b.p.c.prods
	cands := comps
	best := prods[compPID(cands[0])].dirs.priority
	for _, c := range cands[1:] {
		if p := prods[compPID(c)].dirs.priority; p > best {
			best = p
		}
	}
	cands, _ = filter(cands, func(c int) bool { return prods[compPID(c)].dirs.priority == best })
	if pref, ok := filter(cands, func(c int) bool { return prods[compPID(c)].dirs.prefer }); ok {
		cands = pref
	} else if keep, ok := filter(cands, func(c int) bool {
		return !prods[compPID(c)].dirs.avoid && !prods[compPID(c)].fallback
	}); ok {
		cands = keep
	}
	if len(cands) > 1 {
		b.packed++
	}
	sort.Ints(cands)
	return cands[0]
}

// filter compacts xs in place to the elements for which keep is true and
// reports whether any matched. When none match, xs is returned unchanged
// (matched=false) so a caller can fall back to a broader candidate set
// without having kept a separate copy around.
func filter(xs []int, keep func(int) bool) ([]int, bool) {
	n := 0
	for _, x := range xs {
		if keep(x) {
			n++
		}
	}
	if n == 0 {
		return xs, false
	}
	if n == len(xs) {
		return xs, true
	}
	w := 0
	for _, x := range xs {
		if keep(x) {
			xs[w] = x
			w++
		}
	}
	return xs[:w], true
}

func leftRec(pr *prod) bool {
	return len(pr.rhs) > 0 && pr.rhs[0].kind == ekNT && pr.rhs[0].nt == pr.nt
}

// deriveInto appends the nodes for the nonterminal with dense id nid over
// input[l:r] onto dst. Synthetic nonterminals are transparent, or become the
// quant / delim / seq node their construct calls for; which of those it is was
// decided at compile time and is read out of ntInfo. dst is the builder
// scratch (same backing across recursive calls) so kid lists are not allocated
// per node.
//
// No DFA rule reaches here: resolveElems turned every reference to one into an
// ekDFA element, and runOn handles a regular start rule before the builder
// runs.
func (b *builder) deriveInto(dst []int32, nid, l, r int) []int32 {
	c := b.p.c
	if nid < 0 || nid >= len(c.ntInfo) {
		// resolveElems leaves nid == -1 on a reference it could not resolve.
		// The parse cannot have reached here, but a tree is no place to panic.
		b.fail(fmt.Sprintf("internal: unresolved nonterminal over %s", b.where(l)))
		return dst
	}
	info := &c.ntInfo[nid]
	pid, cell := b.pickAt(nid, l, r)
	if pid < 0 {
		b.fail(fmt.Sprintf("internal: no derivation of %s over %s", displayNT(info.nt), b.where(l)))
		lo, hi := b.span(l, r)
		return append(dst, b.addNode(nodeKindRule, info.nt, lo, hi, nil))
	}
	kidStart := len(dst)
	// Transparent left-recursive lists ($dl, $qs) must not append the prefix
	// children at every spine node — that is O(n²) Node copies.
	if c.spineProd[pid] {
		dst = b.leftRecKidsInto(dst, nid, pid, cell, l, r)
	} else {
		dst = b.prodKidsInto(dst, pid, l, r, cell)
	}
	kids := dst[kidStart:]
	switch info.class {
	case ntClassQuant:
		if len(kids) == 0 {
			return dst[:kidStart]
		}
	case ntClassDelim, ntClassSeq:
		if len(kids) == 1 {
			return dst[:kidStart+1]
		}
	case ntClassRule:
	default:
		return dst
	}
	lo, hi := b.span(l, r)
	id := b.addNode(info.kind, info.name, lo, hi, kids)
	return append(dst[:kidStart], id)
}

type kidSpan struct{ start, n int }

// leftRecKidsInto walks a transparent left-recursive spine N ::= N rest | base
// once and appends base+rest onto dst. User-level left recursion is not
// spliced and still nests via prodKidsInto. pid and cell are the spine
// production over (l, r) that deriveInto already resolved; the walk resolves
// each shorter prefix itself.
func (b *builder) leftRecKidsInto(dst []int32, nid, pid int, cell int32, l, r int) []int32 {
	c := b.p.c
	nt := c.ntInfo[nid].nt
	mark := len(b.p.spineBuf)
	start := len(dst)
	curR := r
	for {
		if pid < 0 {
			b.fail(fmt.Sprintf("internal: no derivation of %s over %s", displayNT(nt), b.where(l)))
			b.p.spineBuf = b.p.spineBuf[:mark]
			return dst[:start]
		}
		if !c.spineProd[pid] {
			dst = b.prodKidsInto(dst, pid, l, curR, cell)
			dst = reorderSpine(dst, start, b.p.spineBuf[mark:], &b.p.spineOut)
			b.p.spineBuf = b.p.spineBuf[:mark]
			return dst
		}
		pr := &c.prods[pid]
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
		dst = b.rhsNodesInto(dst, pr.rhs, pos, 1)
		b.p.spineBuf = append(b.p.spineBuf, kidSpan{start: restStart, n: len(dst) - restStart})
		curR = pos[1]
		pid, cell = b.pickAt(nid, l, curR)
	}
}

func reorderSpine(dst []int32, start int, spans []kidSpan, buf *[]int32) []int32 {
	if len(spans) == 0 {
		return dst
	}
	n := len(dst) - start
	out := *buf
	if cap(out) < n {
		out = make([]int32, n)
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

func (b *builder) prodKidsInto(dst []int32, pid, l, r int, cell int32) []int32 {
	pr := &b.p.c.prods[pid]
	var buf [8]int
	pos, ok := b.path(pid, l, r, cell, buf[:])
	if !ok {
		b.fail(fmt.Sprintf("internal: no path through %s over %s", displayNT(pr.nt), b.where(l)))
		return dst
	}
	return b.rhsNodesInto(dst, pr.rhs, pos, 0)
}

// rhsNodesInto appends the nodes for the elements of rhs from index from on,
// with pos holding their boundaries. A leaf element — a terminal, or a DFA
// rule flat enough for compile time to have settled its node kind and name —
// makes exactly one node and is built here, so only the elements that recurse
// pay for a call.
func (b *builder) rhsNodesInto(dst []int32, rhs []elem, pos []int, from int) []int32 {
	for ip := from; ip < len(rhs); ip++ {
		e := &rhs[ip]
		if e.kind == ekNT || (e.kind == ekDFA && e.treeKind == "") {
			dst = b.appendElem(dst, e, pos[ip], pos[ip+1])
			continue
		}
		if e.treeKind == "" {
			continue // Empty / PosProp and the lookaheads make no node
		}
		lo, hi := b.span(pos[ip], pos[ip+1])
		dst = append(dst, b.addNode(e.treeKind, e.treeName, lo, hi, nil))
	}
	return dst
}

// appendElem appends the nodes for one right-hand-side element matched over
// input[i:end], for the elements rhsNodesInto does not build itself: a
// nonterminal, and a regular rule whose structure has to be recovered.
func (b *builder) appendElem(kids []int32, e *elem, i, end int) []int32 {
	if e.kind == ekDFA {
		// A regular rule with structure: recover it by walking the body.
		n := b.p.c.dfaNode(b.p.input, e.nt, b.p.skip(i), end)
		if e.name != "" {
			n.Name = e.name
		}
		return append(kids, b.intern(n))
	}
	start := len(kids)
	kids = b.deriveInto(kids, e.nid, i, end)
	if e.name != "" {
		added := kids[start:]
		if len(added) == 1 {
			b.nodes[added[0]].name = e.name
		} else if len(added) > 1 {
			lo, hi := b.span(i, end)
			id := b.addNode(nodeKindSeq, e.name, lo, hi, added)
			kids = append(kids[:start], id)
		}
	}
	return kids
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
