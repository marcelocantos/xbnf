// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package engine

import (
	"fmt"
	"slices"
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
	nodes  []inode
	kids   []int
	run    []step
	runK   instKey
	hasRun bool
}

type inode struct {
	kind, name, text string
	k0, kn           int
}

type instKey struct{ pid, l int }

func newBuilder(p *gll) *builder {
	p.packSteps()
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

// packSteps flattens each instance into a contiguous run sorted by (ip, end).
func (p *gll) packSteps() {
	need := 0
	for i := 1; i < len(p.slabs); i++ {
		need += p.slabs[i].n
	}
	packed := keepCap(p.stepPack, need)
	for i := 1; i < len(p.slabs); i++ {
		sl := &p.slabs[i]
		if sl.n == 0 {
			sl.packed = false
			continue
		}
		start := len(packed)
		for id := sl.head; id != 0; id = p.steps[id].next {
			packed = append(packed, p.steps[id])
		}
		p.sortTmp = sortRunByIPEnd(packed[start:], p.sortTmp)
		sl.head = start
		sl.packed = true
	}
	p.stepPack = packed
}

const sortIPCap = 16

func sortRunByIPEnd(run, tmp []step) []step {
	if len(run) < 2 {
		return tmp
	}
	var cnt [sortIPCap]int
	maxip := 0
	used := 0
	for i := range run {
		ip := run[i].ip
		if ip < 0 || ip >= sortIPCap {
			slices.SortFunc(run, func(a, b step) int {
				if a.ip != b.ip {
					return a.ip - b.ip
				}
				return a.end - b.end
			})
			return tmp
		}
		if cnt[ip] == 0 {
			used++
		}
		if ip > maxip {
			maxip = ip
		}
		cnt[ip]++
	}
	if cap(tmp) < len(run) {
		tmp = make([]step, len(run))
	} else {
		tmp = tmp[:len(run)]
	}
	if used == 1 {
		sortByEnd(run, tmp)
		return tmp
	}
	var off [sortIPCap]int
	s := 0
	for ip := 0; ip <= maxip; ip++ {
		off[ip] = s
		s += cnt[ip]
	}
	for i := range run {
		ip := run[i].ip
		tmp[off[ip]] = run[i]
		off[ip]++
	}
	copy(run, tmp)
	start := 0
	for ip := 0; ip <= maxip; ip++ {
		end := start + cnt[ip]
		if cnt[ip] > 1 {
			sortByEnd(run[start:end], tmp)
		}
		start = end
	}
	return tmp
}

func sortByEnd(run, tmp []step) {
	n := len(run)
	if n < 2 {
		return
	}
	if n <= 48 {
		for i := 1; i < n; i++ {
			x := run[i]
			j := i
			for j > 0 && run[j-1].end > x.end {
				run[j] = run[j-1]
				j--
			}
			run[j] = x
		}
		return
	}
	radixByEnd(run, tmp)
}

func radixByEnd(run, tmp []step) {
	n := len(run)
	if cap(tmp) < n {
		tmp = make([]step, n)
	} else {
		tmp = tmp[:n]
	}
	var cnt [256]int
	for i := range run {
		cnt[run[i].end&255]++
	}
	sum := 0
	for i := range cnt {
		c := cnt[i]
		cnt[i] = sum
		sum += c
	}
	for i := range run {
		b := run[i].end & 255
		tmp[cnt[b]] = run[i]
		cnt[b]++
	}
	var cnt2 [256]int
	for i := range tmp {
		cnt2[(tmp[i].end>>8)&255]++
	}
	sum = 0
	for i := range cnt2 {
		c := cnt2[i]
		cnt2[i] = sum
		sum += c
	}
	for i := range tmp {
		b := (tmp[i].end >> 8) & 255
		run[cnt2[b]] = tmp[i]
		cnt2[b]++
	}
	maxEnd := 0
	for i := range run {
		if run[i].end > maxEnd {
			maxEnd = run[i].end
		}
	}
	if maxEnd < 1<<16 {
		return
	}
	clear(cnt[:])
	for i := range run {
		cnt[(run[i].end>>16)&255]++
	}
	sum = 0
	for i := range cnt {
		c := cnt[i]
		cnt[i] = sum
		sum += c
	}
	for i := range run {
		b := (run[i].end >> 16) & 255
		tmp[cnt[b]] = run[i]
		cnt[b]++
	}
	copy(run, tmp)
}

func (b *builder) stepRun(pid, l int) []step {
	k := instKey{pid: pid, l: l}
	if b.hasRun && b.runK == k {
		return b.run
	}
	var run []step
	if id, ok := b.p.stepAt.get(packInst(pid, l), b.p.gen); ok {
		sl := b.p.slabs[id]
		if sl.n > 0 && sl.packed {
			run = b.p.stepPack[sl.head : sl.head+sl.n]
		}
	}
	b.runK = k
	b.hasRun = true
	b.run = run
	return run
}

func matchSteps(run []step, ip, end int) []step {
	if len(run) == 0 {
		return nil
	}
	i := sort.Search(len(run), func(i int) bool {
		if run[i].ip != ip {
			return run[i].ip >= ip
		}
		return run[i].end >= end
	})
	if i >= len(run) || run[i].ip != ip || run[i].end != end {
		return nil
	}
	j := i + 1
	for j < len(run) && run[j].ip == ip && run[j].end == end {
		j++
	}
	return run[i:j]
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

// pathScanCutoff is the step-run size above which reachability is memoised.
// All runs are sorted by (ip, end) and matchSteps binary-searches.
const pathScanCutoff = 32

// path returns the element boundaries of one derivation of production pid
// over input[l:r]: positions[0] == l and positions[len(rhs)] == r.
func (b *builder) path(pid, l, r int, buf []int) ([]int, bool) {
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
	run := b.stepRun(pid, l)
	var memo *uMap
	gen := b.p.gen
	if len(run) > pathScanCutoff {
		memo = &b.p.reach
	}
	ambiguous := false
	var candBuf [8]int
	assoc := pr.dirs.assoc
	for ip := n; ip >= 1; ip-- {
		cands := candBuf[:0]
		end := pos[ip]
		for _, st := range matchSteps(run, ip-1, end) {
			if stepReach(run, pid, l, ip-1, st.i, memo, gen) {
				dup := false
				for _, c := range cands {
					if c == st.i {
						dup = true
						break
					}
				}
				if !dup {
					cands = append(cands, st.i)
				}
			}
		}
		if len(cands) == 0 {
			return nil, false
		}
		best := cands[0]
		if len(cands) > 1 {
			if assoc == "right" {
				for _, c := range cands[1:] {
					if c < best {
						best = c
					}
				}
			} else {
				for _, c := range cands[1:] {
					if c > best {
						best = c
					}
				}
				if assoc == "none" {
					b.fail(fmt.Sprintf("ambiguous derivation of %s at %s: #assoc=none forbids chaining",
						displayNT(pr.nt), b.where(l)))
				} else if assoc != "left" {
					ambiguous = true
				}
			}
		}
		pos[ip-1] = best
	}
	if pos[0] != l {
		return nil, false
	}
	if ambiguous {
		b.packed++
	}
	return pos, true
}

func stepReach(run []step, pid, l, ip, pos int, memo *uMap, gen uint32) bool {
	if ip == 0 {
		return pos == l
	}
	if key, ok := packReach(pid, l, ip, pos); ok && memo != nil {
		if v, hit := memo.get(key, gen); hit {
			return v != 0
		}
		memo.put(key, gen, 0) // cycle guard
		out := stepReachOnce(run, pid, l, ip, pos, memo, gen)
		if out {
			memo.put(key, gen, 1)
		}
		return out
	}
	return stepReachOnce(run, pid, l, ip, pos, nil, gen)
}

func stepReachOnce(run []step, pid, l, ip, pos int, memo *uMap, gen uint32) bool {
	for _, st := range matchSteps(run, ip-1, pos) {
		if stepReach(run, pid, l, ip-1, st.i, memo, gen) {
			return true
		}
	}
	return false
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
func (b *builder) pickAt(nt string, l, r int) int {
	pid, extra := b.p.pidsAt(b.p.c.ntNID[nt], l, r)
	if pid < 0 {
		return -1
	}
	if extra == nil {
		return pid
	}
	pids := make([]int, 1, 1+len(extra))
	pids[0] = pid
	pids = append(pids, extra...)
	return b.pick(pids)
}

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
	pid := b.pickAt(nt, l, r)
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
		dst = b.prodKidsInto(dst, pid, l, r)
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
		pid := b.pickAt(nt, l, curR)
		if pid < 0 {
			b.fail(fmt.Sprintf("internal: no derivation of %s over %s", displayNT(nt), b.where(l)))
			b.p.spineBuf = b.p.spineBuf[:mark]
			return dst[:start]
		}
		pr := b.p.c.prods[pid]
		if !leftRec(pr) {
			dst = b.prodKidsInto(dst, pid, l, curR)
			dst = reorderSpine(dst, start, b.p.spineBuf[mark:], &b.p.spineOut)
			b.p.spineBuf = b.p.spineBuf[:mark]
			return dst
		}
		var buf [8]int
		pos, ok := b.path(pid, l, curR, buf[:])
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

func (b *builder) prodKidsInto(dst []int, pid, l, r int) []int {
	pr := b.p.c.prods[pid]
	var buf [8]int
	pos, ok := b.path(pid, l, r, buf[:])
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
	case grammar.Empty, grammar.PosProp:
		return pos, Node{Kind: "empty"}, true
	case grammar.Ref:
		return pos, Node{}, false
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
