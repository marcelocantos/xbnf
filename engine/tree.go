// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package engine

import (
	"fmt"
	"sort"

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
	kids   []int32
	// contextAmb marks a selected completion with several valid capture paths.
	// path counts this together with its internal alternatives, once per path.
	contextAmb map[int32]bool
	// active is needed only for nonterminals in the zero-growth graph's
	// cyclic remainder. Re-entering a completion must choose another one.
	active map[int]bool // packed completion (production and evidence cell)
	cycle  bool         // the current attempted tree reached an active completion
}

// inode is one node of the arena the builder derives into, before
// materialize copies the tree out. Node ids, child ranges and input offsets
// are int32: an input large enough to overflow one would need tens of
// gigabytes of chart, and the arena is walked once per node, so its width is
// memory traffic on the hottest loop in the builder.
type inode struct {
	kind Kind
	name string
	// lo, hi are the public byte span [lo, hi); the node's text is
	// input[lo:hi].
	lo, hi int32
	k0, kn int32
}

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
	return &builder{p: p, nodes: nodes, kids: kids}
}

func (b *builder) keepArena() {
	b.p.tnodes = b.nodes
	b.p.tkids = b.kids
}

func (b *builder) addNode(kind Kind, name string, lo, hi int32, kids []int32) int32 {
	k0 := int32(len(b.kids))
	b.kids = append(b.kids, kids...)
	id := int32(len(b.nodes))
	b.nodes = append(b.nodes, inode{kind: kind, name: name, lo: lo, hi: hi, k0: k0, kn: int32(len(b.kids))})
	return id
}

// intern copies a Node tree the DFA walker produced into the arena. Child ids
// accumulate on a shared scratch stack rather than one slice per node; the
// scratch is separate from the derivation scratch, which is live in the caller
// while this runs. Spans on n come from the walk.
func (b *builder) intern(n Node) int32 {
	if len(n.Children) == 0 {
		return b.addNode(n.Kind, n.Name, int32(n.Start), int32(n.End), nil)
	}
	mark := len(b.p.iscratch)
	for _, c := range n.Children {
		b.p.iscratch = append(b.p.iscratch, b.intern(c))
	}
	id := b.addNode(n.Kind, n.Name, int32(n.Start), int32(n.End), b.p.iscratch[mark:])
	b.p.iscratch = b.p.iscratch[:mark]
	return id
}

// emit writes the event stream for the arena tree rooted at id, covering
// input[0:end]: a Skip for the gap before every node whose span starts past
// the cursor, and one for trailing wrap up to end. The walk is iterative; a
// frame is an arena node whose children are being visited and the index of
// the next one. It runs twice, once to count so the slice is allocated at its
// exact size, then to fill; the frame stack comes from the pool.
func (b *builder) emit(id int32, end int) []Event {
	n, _ := b.walk(id, end, nil)
	ev := make([]Event, 0, n)
	_, ev = b.walk(id, end, ev)
	return ev
}

// walk is emit's traversal. With ev nil it only counts; otherwise it appends
// to ev, which must have room for the count.
func (b *builder) walk(id int32, end int, ev []Event) (int, []Event) {
	nodes, kids := b.nodes, b.kids
	frames := b.p.eframes[:0]
	count, cur := 0, 0
	visit := func(id int32) {
		in := &nodes[id]
		lo, hi := int(in.lo), int(in.hi)
		if lo > cur {
			if ev != nil {
				ev = append(ev, Event{Op: OpSkip, Len: int32(lo - cur)})
			}
			count++
			cur = lo
		}
		if in.kn == in.k0 {
			length := max(hi-cur, 0)
			if ev != nil {
				ev = append(ev, Event{Op: OpLeaf, Kind: in.kind, Name: in.name, Len: int32(length)})
			}
			count++
			cur += length
			return
		}
		if ev != nil {
			ev = append(ev, Event{Op: OpOpen, Kind: in.kind, Name: in.name})
		}
		count++
		frames = append(frames, eframe{id: id, k: in.k0})
	}
	visit(id)
	for len(frames) > 0 {
		f := &frames[len(frames)-1]
		in := &nodes[f.id]
		if f.k < in.kn {
			child := kids[f.k]
			f.k++
			visit(child)
			continue
		}
		if hi := int(in.hi); hi > cur {
			if ev != nil {
				ev = append(ev, Event{Op: OpSkip, Len: int32(hi - cur)})
			}
			count++
			cur = hi
		}
		if ev != nil {
			ev = append(ev, Event{Op: OpClose})
		}
		count++
		frames = frames[:len(frames)-1]
	}
	b.p.eframes = frames
	if end > cur {
		count++
		if ev != nil {
			ev = skipTo(ev, cur, end)
		}
	}
	return count, ev
}

// eframe is one level of emit's walk: an arena node and its next child.
type eframe struct{ id, k int32 }

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

// root builds the event stream for the start rule over input[pos:end], with
// trailing wrap up to endw. It is the one place a nonterminal is still named:
// everything below works on ids.
func (b *builder) root(start string, pos, end, endw int) []Event {
	defer b.keepArena()
	c := b.p.c
	if c.IsDFA(start) {
		return b.emit(b.intern(c.dfaNode(&b.p.tw, b.p.input, start, b.p.skip(pos), end)), endw)
	}
	nid, ok := c.ntNID[start]
	if !ok {
		b.fail(fmt.Sprintf("internal: no derivation of %s over %s", displayNT(start), b.where(pos)))
		lo, hi := b.span(pos, end)
		return b.emit(b.addNode(KindRule, start, lo, hi, nil), endw)
	}
	mark := len(b.p.kscratch)
	b.p.kscratch = b.deriveInto(b.p.kscratch[:mark], nid, pos, end)
	kids := b.p.kscratch[mark:]
	if len(kids) == 1 {
		ev := b.emit(kids[0], endw)
		b.p.kscratch = b.p.kscratch[:mark]
		return ev
	}
	lo, hi := b.span(pos, end)
	id := b.addNode(KindRule, start, lo, hi, kids)
	b.p.kscratch = b.p.kscratch[:mark]
	return b.emit(id, endw)
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
	if b.contextAmb != nil {
		ambiguous = b.contextAmb[cell]
	}
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
	if b.active != nil && b.p.c.treeCycle[nid] {
		buf[0] = comp
		n := 0
		for _, c := range buf {
			if !b.active[c] {
				buf[n] = c
				n++
			}
		}
		if n == 0 {
			b.cycle = true
			b.fail(fmt.Sprintf("cyclic tree derivation of %s at %s: zero-width recursion",
				displayNT(b.p.c.ntInfo[nid].nt), b.where(l)))
			return -1, 0
		}
		buf, comp = buf[:n], buf[0]
	}
	if len(buf) == 1 {
		return compPID(comp), compCell(comp)
	}
	buf[0] = comp
	win := b.pick(buf, l, r)
	return compPID(win), compCell(win)
}

// pick compares packComp values. The production is in the high bits, so
// sorting the packed values orders them by production as before.
func (b *builder) pick(comps []int, l, r int) int {
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
	sort.Ints(cands)
	pid := compPID(cands[0])
	n := 1
	for n < len(cands) && compPID(cands[n]) == pid {
		n++
	}
	if n < len(cands) {
		b.packed++
	}
	if n > 1 {
		return b.pickContext(cands[:n], l, r)
	}
	return cands[0]
}

// pickContext applies the production's path policy across its completed
// capture contexts. Comparing complete paths, from the last boundary back,
// preserves the same left/right rule as path without splicing a prefix from
// one context onto a reference that only succeeded in another.
func (b *builder) pickContext(comps []int, l, r int) int {
	win := comps[0]
	pid := compPID(win)
	pr := &b.p.c.prods[pid]
	packed, err := b.packed, b.err
	var bestBuf, nextBuf [8]int
	best, ok := b.path(pid, l, r, compCell(win), bestBuf[:])
	b.packed, b.err = packed, err
	if !ok {
		b.fail(fmt.Sprintf("internal: no capture path through %s at %s", displayNT(pr.nt), b.where(l)))
		return win
	}
	different := false
	for _, comp := range comps[1:] {
		pos, ok := b.path(pid, l, r, compCell(comp), nextBuf[:])
		// Only the selected path's diagnostics count. prodKidsInto walks
		// it again when building the tree, including #assoc=none errors.
		b.packed, b.err = packed, err
		if !ok {
			b.fail(fmt.Sprintf("internal: no capture path through %s at %s", displayNT(pr.nt), b.where(l)))
			return win
		}
		for ip := len(pos) - 2; ip > 0; ip-- {
			if pos[ip] == best[ip] {
				continue
			}
			different = true
			if (pos[ip] > best[ip]) != (pr.dirs.assoc == assocRight) {
				win = comp
				copy(best, pos)
			}
			break
		}
	}
	if different {
		switch pr.dirs.assoc {
		case assocNone:
			b.fail(fmt.Sprintf("ambiguous derivation of %s at %s: #assoc=none forbids chaining",
				displayNT(pr.nt), b.where(l)))
		case assocLeft, assocRight:
		default:
			if b.contextAmb == nil {
				b.contextAmb = make(map[int32]bool)
			}
			b.contextAmb[compCell(win)] = true
		}
	}
	return win
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
	if c.treeCycle != nil && c.treeCycle[nid] {
		return b.deriveCycleInto(dst, nid, l, r)
	}
	pid, cell := b.pickAt(nid, l, r)
	return b.derivePickedInto(dst, nid, pid, cell, l, r)
}

// deriveCycleInto tries another completion when a preferred subtree would
// recurse forever. The retry must happen at the branching ancestor: a repeated
// unit wrapper itself may have no alternative, while its caller has a base case.
// The ordinary consuming path avoids this speculative arena/diagnostic rollback.
func (b *builder) deriveCycleInto(dst []int32, nid, l, r int) []int32 {
	if b.active == nil {
		b.active = make(map[int]bool)
	}
	var tried []int
	mark := len(dst)
	nodes, kids := len(b.nodes), len(b.kids)
	packed, err, cycle := b.packed, b.err, b.cycle
	for {
		// Exclude failed alternatives while picking, but permit them inside
		// another candidate's finite tree. Only ancestors stay active there.
		for _, comp := range tried {
			b.active[comp] = true
		}
		pid, cell := b.pickAt(nid, l, r)
		for _, comp := range tried {
			delete(b.active, comp)
		}
		if pid < 0 {
			return b.derivePickedInto(dst, nid, pid, cell, l, r)
		}
		comp := packComp(pid, cell)
		b.active[comp] = true
		b.cycle = false
		dst = b.derivePickedInto(dst, nid, pid, cell, l, r)
		delete(b.active, comp)
		if !b.cycle {
			b.cycle = cycle
			return dst
		}
		tried = append(tried, comp)
		dst = dst[:mark]
		b.nodes, b.kids = b.nodes[:nodes], b.kids[:kids]
		b.packed, b.err, b.cycle = packed, err, cycle
	}
}

func (b *builder) derivePickedInto(dst []int32, nid, pid int, cell int32, l, r int) []int32 {
	c := b.p.c
	info := &c.ntInfo[nid]
	if pid < 0 {
		b.fail(fmt.Sprintf("internal: no derivation of %s over %s", displayNT(info.nt), b.where(l)))
		lo, hi := b.span(l, r)
		return append(dst, b.addNode(KindRule, info.nt, lo, hi, nil))
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
		if e.kind == ekNT || (e.kind == ekDFA && e.treeKind == KindNone) {
			dst = b.appendElem(dst, e, pos[ip], pos[ip+1])
			continue
		}
		if e.treeKind == KindNone {
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
		n := b.p.c.dfaNode(&b.p.tw, b.p.input, e.nt, b.p.skip(i), end)
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
			id := b.addNode(KindSeq, e.name, lo, hi, added)
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
// until the next dfaNode call with that walker. Every caller here interns
// the node immediately. The walker is the parse's own (gll.tw), so parses
// on one Compiled do not share it.
func (c *Compiled) dfaNode(w *twalk, input, name string, l, r int) Node {
	rule, ok := c.rules[name]
	if !ok {
		return Node{Kind: KindLeaf, Name: name, Start: l, End: r}
	}
	if _, isLeaf := rule.Body.(grammar.Leaf); isLeaf {
		return Node{Kind: KindLeaf, Name: name, Start: l, End: r}
	}
	w.c = c
	w.input = input
	w.busy = w.busy[:0]
	w.stack = w.stack[:0]
	w.arena = w.arena[:0]
	end, n, ok := w.term(rule.Body, l, false)
	if !ok || end != r {
		return Node{Kind: KindLeaf, Name: name, Start: l, End: r}
	}
	if n.Kind == KindLeaf {
		n.Name = name
		return n
	}
	n.Kind = KindRule
	n.Name = name
	n.Start, n.End = l, r
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
	case KindEmpty, KindLookahead, KindNegLookahead:
		return
	case KindSeq:
		if n.Name == "" {
			w.stack = append(w.stack, n.Children...)
			return
		}
	case KindQuant:
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
			return end, Node{Kind: KindLeaf, Name: name, Start: pos, End: end}, true
		}
	}
	end, n, ok := w.term(r.Body, pos, nowrap)
	if !ok {
		return pos, Node{}, false
	}
	if n.Kind == KindLeaf {
		n.Name = name
		return end, n, true
	}
	n.Kind = KindRule
	n.Name = name
	n.Start, n.End = pos, end
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
		return end, Node{Kind: KindLeaf, Start: pos, End: end}, true
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
		return end, Node{Kind: KindString, Start: pos, End: end}, true
	case grammar.CharClass, grammar.Escape, grammar.AnyChar:
		end, ok := matchTerminal(x, w.input, pos)
		if !ok {
			return pos, Node{}, false
		}
		return end, Node{Kind: KindChar, Start: pos, End: end}, true
	case grammar.Empty, grammar.PosProp:
		return pos, Node{Kind: KindEmpty, Start: pos, End: pos}, true
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
		return cur, Node{Kind: KindSeq, Children: w.commit(mark), Start: pos, End: cur}, true
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
		return cur, Node{Kind: KindQuant, Children: w.commit(mark), Start: pos, End: cur}, true
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
		return pos, Node{Kind: KindLookahead, Start: pos, End: pos}, true
	case grammar.NegLookahead:
		_, _, ok := w.term(x.Term, pos, nowrap)
		if ok {
			return pos, Node{}, false
		}
		return pos, Node{Kind: KindNegLookahead, Start: pos, End: pos}, true
	default:
		end, ok := matchTerminal(t, w.input, pos)
		if !ok {
			return pos, Node{}, false
		}
		return end, Node{Kind: KindTerm, Start: pos, End: end}, true
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
	return cur, Node{Kind: KindDelim, Children: w.commit(mark), Start: pos, End: cur}, true
}
