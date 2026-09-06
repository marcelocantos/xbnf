// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package engine

import (
	"fmt"
	"strings"
	"sync"

	"github.com/marcelocantos/xbnf/grammar"
)

type slot struct {
	pid int
	ip  int
}

type desc struct {
	sl slot
	u  int
	i  int
}

// task is a scheduled descriptor plus the evidence cell holding the element
// matches that reach it. The builder walks those links backwards instead of
// searching a sorted step log.
type task struct {
	sl   slot
	u, i int
	cell int32
}

type gssNode struct {
	sl    slot
	i     int
	ehead int // 0 = none; index into gll.edges
	phead int // 0 = none; index into gll.pops
}

type gssEdge struct {
	to, next int
	cell     int32 // evidence cell of the caller descriptor that made this edge
}

type gssPop struct {
	i, next int
}

type famKey struct {
	nid, l, r int
}

// moreItem is one node of a per-family singly linked list of extra packComp
// values recorded when a span packs more than one production. Index 0 is the
// dummy slot; a real chain's terminal node has next == 0.
type moreItem struct {
	comp int
	next int
}

type failInfo struct {
	pos  int
	want map[string]bool
	rule string
	dfa  bool
}

type gll struct {
	c      *Compiled
	input  string
	R      []task
	uset   uMap           // packDesc → evidence cell this generation
	Ubig   map[desc]genID // used when pid/u/i do not pack into 64 bits
	gss    []gssNode
	gssAt  uMap
	gssBig map[gssKey]genID
	sym    uMap // packFam(nid,l,r) → packComp of the first prod this generation
	// startNT/startLeft identify the parse's root question; startEnd is the
	// furthest completion of that question (-1 = none). Completions are only
	// ever asked for their maximum end at the root, so this replaces a map.
	startNT   int
	startLeft int
	startEnd  int
	symBig    map[famKey]genID
	moreAt    uMap             // packFam(nid,l,r) → head+1 into moreSlab this generation
	moreBig   map[famKey]genID // same, keyed by famKey when packFam does not fit in 64 bits
	moreSlab  []moreItem       // dummy at 0; per-family singly linked lists of extra comps
	pickBuf   []int            // builder.pick's scratch candidate list; reset per pickAt call
	start     string
	fail      failInfo
	wrapEnd   []int     // memoised skipWrap per position; 0 = unknown, else end+1
	wrapIn    string    // input wrapEnd was filled for
	wrapC     *Compiled // #wrap that wrapEnd was filled for; reuse needs both
	work      int       // descriptors processed
	cells     []int32   // cell → head of its incoming step list; dummy at 0
	steps     []step    // dummy at 0
	edges     []gssEdge
	pops      []gssPop
	tnodes    []inode
	tkids     []int
	kscratch  []int
	spineBuf  []kidSpan
	spineOut  []int
	gen       uint32 // bumps each Parse; lookup maps are not cleared
}

func packFam(nid, l, r int) (uint64, bool) {
	if nid < 0 || nid > 0xFFF || l < 0 || l > 0xFFFFF || r < 0 || r > 0xFFFFF {
		return 0, false
	}
	return uint64(nid)<<40 | uint64(l)<<20 | uint64(r), true
}

type genID struct {
	gen uint32
	id  int
}

// step is one element match that reaches a descriptor: the element started at
// i, next is a competing match of the same element reaching the same
// descriptor, and prev is the evidence cell of the descriptor the match came
// from. The descriptor fixes the element index and the end position, so
// neither is stored. int32 holds an input position and a step index; a parse
// large enough to overflow one would need hundreds of gigabytes of chart.
type step struct {
	i, next, prev int32
}

// packComp is one completion: the production and the evidence cell of the
// descriptor that completed it. pid+1 keeps the packed value non-zero, which
// is how sym distinguishes a recorded span from an absent one. Like packGSS
// this needs a 64-bit int.
func packComp(pid int, cell int32) int {
	return (pid+1)<<32 | int(cell)
}

func compPID(v int) int { return v>>32 - 1 }

func compCell(v int) int32 { return int32(v & 0xFFFFFFFF) }

var gllPool = sync.Pool{New: func() any { return new(gll) }}

func newGLL(c *Compiled, input, start string) *gll {
	p := gllPool.Get().(*gll)
	p.bind(c, input, start)
	return p
}

func (p *gll) bind(c *Compiled, input, start string) {
	n := len(input) + 1
	p.c = c
	p.input = input
	p.start = start
	p.work = 0
	p.Ubig = nil
	p.gssBig = nil
	p.fail.pos = -1
	p.fail.rule = ""
	p.fail.dfa = false
	if p.fail.want != nil {
		clear(p.fail.want)
	}
	p.R = p.R[:0]
	if cap(p.R) < 64 {
		p.R = make([]task, 0, 64)
	}
	p.gen++
	if p.gen == 0 {
		p.uset.clear()
		p.gssAt.clear()
		p.sym.clear()
		p.moreAt.clear()
		clear(p.symBig)
		clear(p.moreBig)
		p.gen = 1
	}
	if p.uset.slots == nil {
		p.uset.init(n * 4)
	} else {
		p.uset.reset()
	}
	p.gss = p.gss[:0]
	if cap(p.gss) < n/2+1 {
		p.gss = make([]gssNode, 0, n/2+1)
	}
	if p.gssAt.slots == nil {
		p.gssAt.init(n)
	} else {
		p.gssAt.reset()
	}
	if p.sym.slots == nil {
		p.sym.init(n)
	} else {
		p.sym.reset()
	}
	if p.moreAt.slots == nil {
		p.moreAt.init(n / 16)
	} else {
		p.moreAt.reset()
	}
	p.spineBuf = p.spineBuf[:0]
	p.cells = keepDummy(p.cells, n)
	p.steps = keepDummy(p.steps, n)
	p.edges = keepDummy(p.edges, n/2+1)
	p.pops = keepDummy(p.pops, n/2+1)
	p.moreSlab = keepDummy(p.moreSlab, n/16)
	if p.wrapC == c && p.wrapIn == input && len(p.wrapEnd) == n {
		// same Compiled and input: skipWrap results are unchanged
	} else if cap(p.wrapEnd) < n {
		p.wrapEnd = make([]int, n)
		p.wrapIn = input
		p.wrapC = c
	} else {
		p.wrapEnd = p.wrapEnd[:n]
		clear(p.wrapEnd)
		p.wrapIn = input
		p.wrapC = c
	}
}

func keepDummy[T any](s []T, hint int) []T {
	if cap(s) < hint+1 {
		return make([]T, 1, hint+1)
	}
	s = s[:1]
	var z T
	s[0] = z
	return s
}

func (p *gll) release() {
	p.c = nil
	p.input = ""
	p.start = ""
	p.R = p.R[:0]
	p.gss = p.gss[:0]
	if cap(p.cells) > 0 {
		p.cells = p.cells[:1]
	}
	if cap(p.steps) > 0 {
		p.steps = p.steps[:1]
	}
	if cap(p.edges) > 0 {
		p.edges = p.edges[:1]
	}
	if cap(p.pops) > 0 {
		p.pops = p.pops[:1]
	}
	if cap(p.moreSlab) > 0 {
		p.moreSlab = p.moreSlab[:1]
	}
	p.tnodes = p.tnodes[:0]
	p.tkids = p.tkids[:0]
	p.kscratch = p.kscratch[:0]
	p.pickBuf = p.pickBuf[:0]
	p.spineBuf = p.spineBuf[:0]
	p.Ubig = nil
	p.gssBig = nil
	p.symBig = nil
	p.moreBig = nil
	p.work = 0
	p.fail.pos = -1
	p.fail.rule = ""
	p.fail.dfa = false
	if p.fail.want != nil {
		clear(p.fail.want)
	}
	gllPool.Put(p)
}

// skip is skipWrap memoised per position.
// lineIndent is the column of the first non-space on the line containing i.
// @col compares this indent, not the cursor, so a constraint can sit at the
// beginning of the line while wrap still skips the indent for the next token.
func lineIndent(input string, i int) int {
	if i < 0 {
		i = 0
	}
	if i > len(input) {
		i = len(input)
	}
	start := i
	for start > 0 && input[start-1] != '\n' {
		start--
	}
	col := 0
	for start < len(input) {
		switch input[start] {
		case ' ':
			col++
			start++
		case '\t':
			col += 8 - col%8
			start++
		default:
			return col
		}
	}
	return col
}

func colOK(input string, i int, p grammar.PosProp) bool {
	if p.Name != "col" {
		return false
	}
	col := lineIndent(input, i)
	n := 0
	if p.Arg != "parent" && p.Arg != "" {
		for _, c := range p.Arg {
			if c < '0' || c > '9' {
				return false
			}
			n = n*10 + int(c-'0')
		}
	}
	switch p.Op {
	case "", "=":
		return col == n
	case ">":
		return col > n
	case "<":
		return col < n
	case ">=":
		return col >= n
	case "<=":
		return col <= n
	case "!=":
		return col != n
	default:
		return false
	}
}

// boundText is the text an earlier element of this production instance
// matched, for a %ref. cur is the element now being processed and cell its
// evidence, so the walk back along the links reports the match on the
// derivation that reached here rather than the newest match anywhere in the
// instance.
func (p *gll) boundText(pr prod, cur int, cell int32, end int, name string) (string, bool) {
	ip := -1
	for i, e := range pr.rhs {
		if e.name == name {
			ip = i
			break
		}
	}
	if ip < 0 || ip >= cur {
		return "", false
	}
	for e := cur - 1; e > ip; e-- {
		h := p.cells[cell]
		if h == 0 {
			return "", false
		}
		st := p.steps[h]
		end = int(st.i)
		cell = st.prev
	}
	h := p.cells[cell]
	if h == 0 {
		return "", false
	}
	a := int(p.steps[h].i)
	if a < 0 {
		a = 0
	}
	if e := p.skip(a); e <= end {
		a = e
	}
	if a > end {
		a = end
	}
	if end > len(p.input) {
		return "", false
	}
	return p.input[a:end], true
}

func (p *gll) skip(i int) int {
	if i < 0 || i > len(p.input) {
		return i
	}
	if e := p.wrapEnd[i]; e != 0 {
		return e - 1
	}
	e := p.c.skipWrap(p.input, i)
	p.wrapEnd[i] = e + 1
	return e
}

func (p *gll) drain() {
	for len(p.R) > 0 {
		t := p.R[len(p.R)-1]
		p.R = p.R[:len(p.R)-1]
		p.work++
		p.process(t)
	}
}

type gssKey struct {
	pid, ip, i int
}

func (c *Compiled) gll(start, input string) *Result {
	res, p := c.run(start, input)
	if p != nil {
		p.release()
	}
	return res
}

// run parses and also returns the parser state, for tests that inspect work done.
func (c *Compiled) run(start, input string) (*Result, *gll) {
	if start == "" {
		start = c.first
	}
	if start == "" {
		return &Result{Error: "no start rule"}, nil
	}
	if _, ok := c.ntProds[start]; !ok && !c.IsDFA(start) {
		return &Result{Error: "unknown rule " + start}, nil
	}
	p := newGLL(c, input, start)
	return c.runOn(p, start, input)
}

func (c *Compiled) runOn(p *gll, start, input string) (*Result, *gll) {
	pos := p.skip(0)
	dummy := p.gssNode(slot{pid: -1, ip: 0}, pos)
	if c.IsDFA(start) {
		end, _, ok := c.dfa[start].match(input, pos)
		if !ok {
			msg := formatExpect(input, pos, []string{displayNT(start)}, start, true)
			return &Result{Error: msg}, p
		}
		tree := c.dfaNodeOwned(input, start, pos, end)
		end = c.skipWrap(input, end)
		if end != len(input) {
			line, col := lineCol(input, end)
			return &Result{
				Error: fmt.Sprintf("unconsumed input at %d:%d (byte %d)", line, col, end),
				End:   end,
				Tree:  tree,
			}, p
		}
		return &Result{OK: true, End: end, Tree: tree}, p
	}
	p.rootAt(c.ntNID[start], pos)
	p.fork(start, dummy, pos)
	p.drain()
	end := p.maxRight(c.ntNID[start], pos)
	if end < 0 {
		return &Result{Error: p.failMessage()}, p
	}
	b := newBuilder(p)
	tree := b.root(start, pos, end)
	endw := c.skipWrap(input, end)
	if endw != len(input) {
		line, col := lineCol(input, endw)
		return &Result{
			Error:  fmt.Sprintf("unconsumed input at %d:%d (byte %d)", line, col, endw),
			End:    endw,
			Packed: b.packed,
			Tree:   tree,
		}, p
	}
	if b.err != "" {
		return &Result{Error: b.err, End: endw, Packed: b.packed, Tree: tree}, p
	}
	return &Result{
		OK:     true,
		End:    endw,
		Packed: b.packed,
		Tree:   tree,
	}, p
}

func (p *gll) noteFail(pos int, want, rule string, dfa bool) {
	if p.fail.want == nil {
		p.fail.want = map[string]bool{}
		p.fail.pos = -1
	}
	if pos < p.fail.pos {
		return
	}
	if pos > p.fail.pos {
		p.fail.pos = pos
		clear(p.fail.want)
		p.fail.want[want] = true
		p.fail.rule = rule
		p.fail.dfa = dfa
		return
	}
	p.fail.want[want] = true
	if !dfa {
		p.fail.dfa = false
		if !strings.HasPrefix(rule, "$") || strings.HasPrefix(p.fail.rule, "$") {
			p.fail.rule = rule
		}
	}
}

func (p *gll) failMessage() string {
	if p.fail.pos < 0 || len(p.fail.want) == 0 {
		line, col := lineCol(p.input, 0)
		return fmt.Sprintf("in rule %s, expected start at %d:%d; got end of input", displayNT(p.start), line, col)
	}
	want := make([]string, 0, len(p.fail.want))
	for w := range p.fail.want {
		want = append(want, w)
	}
	return formatExpect(p.input, p.fail.pos, want, p.fail.rule, p.fail.dfa)
}

func (c *Compiled) skipWrap(input string, pos int) int {
	if c.wrapDFA == nil {
		return pos
	}
	end, _, ok := c.wrapDFA.match(input, pos)
	if !ok {
		return pos
	}
	return end
}

// fork enqueues the productions of nt that can start at position i.
// When compile-time FIRST is a disjoint ASCII table, only the matching
// production is created.
func (p *gll) fork(nt string, u, i int) {
	if id, ok := p.c.ntNID[nt]; ok {
		p.forkID(id, u, i)
		return
	}
	p.forkNamed(nt, u, i)
}

func (p *gll) forkID(nid, u, i int) {
	if nid >= 0 && nid < len(p.c.predN) {
		if bp := p.c.predN[nid]; bp != nil {
			j := p.skip(i)
			if j < len(p.input) {
				b := p.input[j]
				if b < 0x80 {
					switch pid := bp.byByte[b]; pid {
					case nonePID:
					case manyPID:
					default:
						p.add(slot{pid: int(pid), ip: 0}, u, i)
						return
					}
				}
			}
		}
	}
	if nid >= 0 && nid < len(p.c.ntProdsN) {
		for _, pid := range p.c.ntProdsN[nid] {
			p.add(slot{pid: pid, ip: 0}, u, i)
		}
		return
	}
}

func (p *gll) forkNamed(nt string, u, i int) {
	if bp := p.c.pred[nt]; bp != nil {
		j := p.skip(i)
		if j < len(p.input) {
			b := p.input[j]
			if b < 0x80 {
				switch pid := bp.byByte[b]; pid {
				case nonePID:
				case manyPID:
				default:
					p.add(slot{pid: int(pid), ip: 0}, u, i)
					return
				}
			}
		}
	}
	for _, pid := range p.c.ntProds[nt] {
		p.add(slot{pid: pid, ip: 0}, u, i)
	}
}

func packDesc(sl slot, u, i int) (uint64, bool) {
	if sl.pid < 0 || sl.pid > 0xFFFF || sl.ip < 0 || sl.ip > 0xFF || u < 0 || u > 0xFFFFF || i < 0 || i > 0xFFFFF {
		return 0, false
	}
	return uint64(sl.pid)<<48 | uint64(sl.ip)<<40 | uint64(u)<<20 | uint64(i), true
}

// claim looks up the descriptor for slot sl at position i under GSS node u
// and returns the evidence cell that collects the element matches reaching
// it. fresh is true when this call is the one that claimed it, so the caller
// owns running it — either by pushing it onto R or by continuing in place.
// ok is false when FIRST rejects the slot, in which case no match can reach
// it and no evidence is kept. A slot at element 0 has nothing before it, so
// it needs no cell.
func (p *gll) claim(sl slot, u, i int) (cell int32, fresh, ok bool) {
	d := desc{sl: sl, u: u, i: i}
	if key, packed := packDesc(sl, u, i); packed {
		idx, hit := p.uset.probe(key, p.gen)
		if hit {
			return int32(p.uset.idAt(idx)), false, true
		}
		if !p.admits(sl, i) {
			return 0, false, false
		}
		cell = p.newCell(sl.ip)
		p.uset.placeAt(idx, key, p.gen, int(cell))
		return cell, true, true
	}
	if p.Ubig == nil {
		p.Ubig = map[desc]genID{}
	}
	if ref, hit := p.Ubig[d]; hit && ref.gen == p.gen {
		return int32(ref.id), false, true
	}
	if !p.admits(sl, i) {
		return 0, false, false
	}
	cell = p.newCell(sl.ip)
	p.Ubig[d] = genID{gen: p.gen, id: int(cell)}
	return cell, true, true
}

func (p *gll) newCell(ip int) int32 {
	if ip == 0 {
		return 0
	}
	p.cells = append(p.cells, 0)
	return int32(len(p.cells) - 1)
}

func (p *gll) push(sl slot, u, i int, cell int32) {
	p.R = append(p.R, task{sl: sl, u: u, i: i, cell: cell})
}

// add schedules a production's first slot. fork is the only caller, and an
// element-0 slot has no incoming matches, so the cell stays 0.
func (p *gll) add(sl slot, u, i int) {
	if cell, fresh, ok := p.claim(sl, u, i); ok && fresh {
		p.push(sl, u, i, cell)
	}
}

// admits is the GLL test: can the remainder of the slot start at position i?
// A rejected slot records what it expected, so error messages are unchanged.
func (p *gll) admits(sl slot, i int) bool {
	f := p.c.slotFirst[sl.pid][sl.ip]
	if f.nullable {
		return true
	}
	j := p.skip(i)
	if j < len(p.input) {
		r, _ := decodeRune(p.input, j)
		if f.admits(r) {
			return true
		}
	}
	if j >= p.fail.pos {
		nt := p.c.prods[sl.pid].nt
		for _, a := range f.atoms {
			p.noteFail(j, a.describe(), nt, a.dfa != nil)
		}
	}
	return false
}

// advance records that the element before slot next matched input[i:end] and
// enqueues next. prev is the evidence cell of the descriptor the match came
// from, so the recorded step already knows its whole chain: the builder walks
// prev links instead of matching and re-testing a sorted step log.
func (p *gll) advance(next slot, u, i, end int, prev int32) {
	cell, fresh, ok := p.claim(next, u, end)
	if !ok {
		return
	}
	p.link(cell, i, prev)
	if fresh {
		p.push(next, u, end, cell)
	}
}

// link adds one element match to the descriptor cell it reaches. A match that
// arrives later than the descriptor was scheduled is a competing derivation of
// the same prefix, and joins the same list.
func (p *gll) link(cell int32, i int, prev int32) {
	p.steps = append(p.steps, step{i: int32(i), next: p.cells[cell], prev: prev})
	p.cells[cell] = int32(len(p.steps) - 1)
}

func packGSS(sl slot, i int) (uint64, bool) {
	pid := sl.pid + 1 // dummy slot uses pid -1
	if pid < 0 || pid > 0xFFFF || sl.ip < 0 || sl.ip > 0xFF || i < 0 || i > 0xFFFFFFFF {
		return 0, false
	}
	return uint64(pid)<<40 | uint64(sl.ip)<<32 | uint64(uint32(i)), true
}

func (p *gll) gssNode(sl slot, i int) int {
	if key, ok := packGSS(sl, i); ok {
		if id, ok := p.gssAt.get(key, p.gen); ok {
			return id
		}
		id := len(p.gss)
		p.gss = append(p.gss, gssNode{sl: sl, i: i})
		p.gssAt.put(key, p.gen, id)
		return id
	}
	k := gssKey{pid: sl.pid, ip: sl.ip, i: i}
	if p.gssBig == nil {
		p.gssBig = map[gssKey]genID{}
	}
	if ref, ok := p.gssBig[k]; ok && ref.gen == p.gen {
		return ref.id
	}
	id := len(p.gss)
	p.gss = append(p.gss, gssNode{sl: sl, i: i})
	p.gssBig[k] = genID{gen: p.gen, id: id}
	return id
}

// create links the caller frame u into the GSS node for return slot ret. The
// edge carries the caller descriptor's evidence cell, which is what a later
// pop must record as the predecessor of the returning match.
func (p *gll) create(ret slot, u, i int, cell int32) int {
	v := p.gssNode(ret, i)
	for e := p.gss[v].ehead; e != 0; e = p.edges[e].next {
		if p.edges[e].to == u {
			return v
		}
	}
	p.edges = append(p.edges, gssEdge{to: u, next: p.gss[v].ehead, cell: cell})
	p.gss[v].ehead = len(p.edges) - 1
	for pop := p.gss[v].phead; pop != 0; pop = p.pops[pop].next {
		p.advance(ret, u, i, p.pops[pop].i, cell)
	}
	return v
}

func (p *gll) pop(u, i int) {
	if u < 0 || p.gss[u].sl.pid < 0 {
		// dummy GSS: completing start production
		return
	}
	p.pops = append(p.pops, gssPop{i: i, next: p.gss[u].phead})
	p.gss[u].phead = len(p.pops) - 1
	ret := p.gss[u].sl
	from := p.gss[u].i
	for e := p.gss[u].ehead; e != 0; e = p.edges[e].next {
		p.advance(ret, p.edges[e].to, from, i, p.edges[e].cell)
	}
}

// process runs one descriptor, and then as much of the production as it can
// without rescheduling. A terminal or DFA element that matches yields exactly
// one endpoint, so the descriptor for the element after it can run here: the
// match is linked to its cell as advance would and claim takes it out of U as
// add would, so no other path can run it a second time. R is LIFO and a
// terminal's advance was already the last act of process, so a fused run
// visits slots in the order drain would have. A nonterminal, a lookahead or
// the end of the production ends the run and goes through the usual
// create/fork/advance/complete path.
func (p *gll) process(d task) {
	sl, i, cell := d.sl, d.i, d.cell
	for {
		pr := p.c.prods[sl.pid]
		if sl.ip == len(pr.rhs) {
			p.complete(sl.pid, p.gss[d.u].i, i, cell)
			p.pop(d.u, i)
			return
		}
		e := pr.rhs[sl.ip]
		next := slot{pid: sl.pid, ip: sl.ip + 1}
		var end int
		switch e.kind {
		case ekTerm:
			switch x := e.term.(type) {
			case grammar.PosProp:
				if !colOK(p.input, i, x) {
					p.noteFail(i, describeElem(e), pr.nt, false)
					return
				}
				end = i
			case grammar.Ref:
				j := p.skip(i)
				want, ok := p.boundText(pr, sl.ip, cell, i, x.Name)
				if !ok && x.HasDefault {
					want, ok = x.Default, true
				}
				if !ok || j+len(want) > len(p.input) || p.input[j:j+len(want)] != want {
					p.noteFail(j, "%"+x.Name, pr.nt, false)
					return
				}
				end = j + len(want)
			default:
				j := p.skip(i)
				m, ok := matchTerminal(e.term, p.input, j)
				if !ok {
					p.noteFail(j, describeElem(e), pr.nt, false)
					return
				}
				end = m
			}
		case ekDFA:
			j := p.skip(i)
			df := e.df
			if df == nil {
				df = p.c.dfa[e.nt]
			}
			if df == nil {
				return
			}
			m, labs, ok := df.match(p.input, j)
			if !ok {
				p.noteFail(j, describeElem(e), pr.nt, true)
				return
			}
			if e.label != "" && !hasLabel(labs, e.label) {
				p.noteFail(j, describeElem(e), pr.nt, true)
				return
			}
			end = m
		case ekNT:
			v := p.create(next, d.u, i, cell)
			if e.nid >= 0 {
				p.forkID(e.nid, v, i)
			} else {
				p.fork(e.nt, v, i)
			}
			return
		case ekLook:
			if p.succeeds(e.nt, i) {
				p.advance(next, d.u, i, i, cell)
			}
			return
		case ekNegLook:
			if !p.succeeds(e.nt, i) {
				p.advance(next, d.u, i, i, cell)
			}
			return
		default:
			return
		}
		nextCell, fresh, ok := p.claim(next, d.u, end)
		if !ok {
			return
		}
		p.link(nextCell, i, cell)
		if !fresh {
			return
		}
		sl, i, cell = next, end, nextCell
	}
}

func (p *gll) complete(pid, left, right int, cell int32) {
	p.recordProd(p.c.prods[pid].nid, left, right, pid, cell)
}

// recordProd records that pid derives (nid, left, right), together with the
// evidence cell of the descriptor that completed it. The first production of
// a span wins; competitors go to symMore for pick to sort out.
func (p *gll) recordProd(nid, left, right, pid int, cell int32) {
	p.noteEnd(nid, left, right)
	fk := famKey{nid: nid, l: left, r: right}
	comp := packComp(pid, cell)
	if key, ok := packFam(nid, left, right); ok {
		idx, hit := p.sym.probe(key, p.gen)
		if hit {
			if compPID(p.sym.idAt(idx)) == pid {
				return
			}
			p.addSymMore(fk, comp)
			return
		}
		p.sym.placeAt(idx, key, p.gen, comp)
		return
	}
	if ref, hit := p.symBig[fk]; hit && ref.gen == p.gen {
		if compPID(ref.id) == pid {
			return
		}
		p.addSymMore(fk, comp)
		return
	}
	if p.symBig == nil {
		p.symBig = map[famKey]genID{}
	}
	p.symBig[fk] = genID{gen: p.gen, id: comp}
}

func (p *gll) noteEnd(nid, left, right int) {
	if nid == p.startNT && left == p.startLeft && right > p.startEnd {
		p.startEnd = right
	}
}

// rootAt declares the parse's root question before the first fork.
func (p *gll) rootAt(nid, left int) {
	p.startNT = nid
	p.startLeft = left
	p.startEnd = -1
}

// addSymMore records comp as an extra completion for family k, deduplicating
// on production against whatever is already chained there. The chain lives
// in moreSlab, a pooled slab of (comp, next) nodes; its head (or, when
// packFam doesn't fit k in 64 bits, its famKey-keyed head) is tracked per
// generation exactly like sym/symBig track the family's primary completion,
// so a cleared generation costs no work beyond the gen bump those tables
// already pay for.
func (p *gll) addSymMore(k famKey, comp int) {
	if key, ok := packFam(k.nid, k.l, k.r); ok {
		head, _ := p.moreAt.get(key, p.gen)
		head, added := p.chainMore(head, comp)
		if added {
			p.moreAt.put(key, p.gen, head)
		}
		return
	}
	head := 0
	if ref, hit := p.moreBig[k]; hit && ref.gen == p.gen {
		head = ref.id
	}
	head, added := p.chainMore(head, comp)
	if added {
		if p.moreBig == nil {
			p.moreBig = map[famKey]genID{}
		}
		p.moreBig[k] = genID{gen: p.gen, id: head}
	}
}

// chainMore appends comp to the tail of the moreSlab chain starting at head,
// preserving insertion order, unless a completion of the same production is
// already present. It reports the (possibly unchanged) head and whether comp
// was newly added.
func (p *gll) chainMore(head, comp int) (int, bool) {
	pid := compPID(comp)
	tail := 0
	for h := head; h != 0; h = p.moreSlab[h].next {
		if compPID(p.moreSlab[h].comp) == pid {
			return head, false
		}
		tail = h
	}
	p.moreSlab = append(p.moreSlab, moreItem{comp: comp})
	id := len(p.moreSlab) - 1
	if tail == 0 {
		head = id
	} else {
		p.moreSlab[tail].next = id
	}
	return head, true
}

// compsAt reports the primary completion recorded for family (nid, l, r),
// plus any extra completions appended onto dst (in the order they were first
// recorded), or (0, dst) if the family was never recorded this generation.
func (p *gll) compsAt(nid, l, r int, dst []int) (int, []int) {
	fk := famKey{nid: nid, l: l, r: r}
	if key, ok := packFam(nid, l, r); ok {
		id, hit := p.sym.get(key, p.gen)
		if !hit {
			return 0, dst
		}
		head, _ := p.moreAt.get(key, p.gen)
		return id, p.appendMoreChain(dst, head)
	}
	ref, hit := p.symBig[fk]
	if !hit || ref.gen != p.gen {
		return 0, dst
	}
	head := 0
	if mref, hit := p.moreBig[fk]; hit && mref.gen == p.gen {
		head = mref.id
	}
	return ref.id, p.appendMoreChain(dst, head)
}

func (p *gll) appendMoreChain(dst []int, head int) []int {
	for h := head; h != 0; h = p.moreSlab[h].next {
		dst = append(dst, p.moreSlab[h].comp)
	}
	return dst
}

// maxRight is the furthest end of the root question; -1 if it never completed.
// Only the root is ever asked (runOn and succeeds), so it must match rootAt.
func (p *gll) maxRight(nid, left int) int {
	if nid != p.startNT || left != p.startLeft {
		panic("maxRight asked for a non-root question")
	}
	return p.startEnd
}

func (p *gll) succeeds(nt string, i int) bool {
	if p.c.IsDFA(nt) {
		_, _, ok := p.c.dfa[nt].match(p.input, i)
		return ok
	}
	q := newGLL(p.c, p.input, nt)
	saved := q.wrapEnd
	q.wrapEnd = p.wrapEnd
	dummy := q.gssNode(slot{pid: -1, ip: 0}, i)
	q.rootAt(q.c.ntNID[nt], i)
	q.fork(nt, dummy, i)
	q.drain()
	ok := q.maxRight(q.c.ntNID[nt], i) >= 0
	q.wrapEnd = saved
	q.release()
	return ok
}
