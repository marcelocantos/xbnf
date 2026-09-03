// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package engine

import (
	"fmt"
	"strings"
	"sync"
	"unicode/utf8"
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

type gssNode struct {
	sl    slot
	i     int
	ehead int // 0 = none; index into gll.edges
	phead int // 0 = none; index into gll.pops
}

type gssEdge struct {
	to, next int
}

type gssPop struct {
	i, next int
}

type famKey struct {
	nid, l, r int
}

type failInfo struct {
	pos  int
	want map[string]bool
	rule string
	dfa  bool
}

type gll struct {
	c       *Compiled
	input   string
	R       []desc
	U       map[uint64]struct{}
	Ubig    map[desc]bool // used when pid/u/i do not pack into 64 bits
	gss     []gssNode
	gssAt   map[uint64]int
	gssBig  map[gssKey]int
	sym     map[famKey]int   // first completing prod+1; 0 = none
	symMore map[famKey][]int // extra prods when a span is packed
	start   string
	fail    failInfo
	wrapEnd []int // memoised skipWrap per position; 0 = unknown, else end+1
	work    int   // descriptors processed
	stepAt  map[instKey]stepList
	steps   []step // dummy at 0 so 0 means "no step"
	edges   []gssEdge
	pops    []gssPop
	tnodes  []inode
	tkids   []int
}

type stepList struct {
	head, n int
}

// step records that element ip of production pid, in the instance that
// started at l, matched input[i:end]. The tree is rebuilt from these.
type step struct {
	pid, ip, l, i, end int
	next               int
}

var gllPool = sync.Pool{New: func() any { return new(gll) }}

func newGLL(c *Compiled, input, start string) *gll {
	n := len(input) + 1
	p := gllPool.Get().(*gll)
	p.c = c
	p.input = input
	p.start = start
	p.work = 0
	p.Ubig = nil
	p.gssBig = nil
	p.symMore = nil
	p.fail.pos = -1
	p.fail.rule = ""
	p.fail.dfa = false
	if p.fail.want != nil {
		clear(p.fail.want)
	}
	p.R = p.R[:0]
	if cap(p.R) < 64 {
		p.R = make([]desc, 0, 64)
	}
	if p.U == nil {
		p.U = make(map[uint64]struct{}, n*2)
	} else {
		clear(p.U)
	}
	p.gss = p.gss[:0]
	if cap(p.gss) < n/2+1 {
		p.gss = make([]gssNode, 0, n/2+1)
	}
	if p.gssAt == nil {
		p.gssAt = make(map[uint64]int, n)
	} else {
		clear(p.gssAt)
	}
	if p.sym == nil {
		p.sym = make(map[famKey]int, n)
	} else {
		clear(p.sym)
	}
	if p.stepAt == nil {
		p.stepAt = make(map[instKey]stepList, n)
	} else {
		clear(p.stepAt)
	}
	p.steps = keepDummy(p.steps, n)
	p.edges = keepDummy(p.edges, n/2+1)
	p.pops = keepDummy(p.pops, n/2+1)
	if cap(p.wrapEnd) < n {
		p.wrapEnd = make([]int, n)
	} else {
		p.wrapEnd = p.wrapEnd[:n]
		clear(p.wrapEnd)
	}
	return p
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
	clear(p.U)
	p.gss = p.gss[:0]
	clear(p.gssAt)
	clear(p.sym)
	clear(p.stepAt)
	if cap(p.steps) > 0 {
		p.steps = p.steps[:1]
	}
	if cap(p.edges) > 0 {
		p.edges = p.edges[:1]
	}
	if cap(p.pops) > 0 {
		p.pops = p.pops[:1]
	}
	p.tnodes = p.tnodes[:0]
	p.tkids = p.tkids[:0]
	p.Ubig = nil
	p.gssBig = nil
	p.symMore = nil
	p.wrapEnd = p.wrapEnd[:0]
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
		d := p.R[len(p.R)-1]
		p.R = p.R[:len(p.R)-1]
		p.work++
		p.process(d)
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
	pos := p.skip(0)
	dummy := p.gssNode(slot{pid: -1, ip: 0}, pos)
	if c.IsDFA(start) {
		end, _, ok := c.dfa[start].match(input, pos)
		if !ok {
			msg := formatExpect(input, pos, []string{displayNT(start)}, start, true)
			return &Result{Error: msg}, p
		}
		tree := c.dfaNode(input, start, pos, end)
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
	p.fork(start, dummy, pos)
	p.drain()
	end := -1
	sid := c.ntNID[start]
	for k := range p.sym {
		if k.nid == sid && k.l == pos && k.r > end {
			end = k.r
		}
	}
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

func (p *gll) add(sl slot, u, i int) {
	d := desc{sl: sl, u: u, i: i}
	if key, ok := packDesc(sl, u, i); ok {
		if _, hit := p.U[key]; hit {
			return
		}
		if !p.admits(sl, i) {
			return
		}
		p.U[key] = struct{}{}
		p.R = append(p.R, d)
		return
	}
	if p.Ubig == nil {
		p.Ubig = map[desc]bool{}
	}
	if p.Ubig[d] {
		return
	}
	if !p.admits(sl, i) {
		return
	}
	p.Ubig[d] = true
	p.R = append(p.R, d)
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
		var r rune
		if p.input[j] < 0x80 {
			r = rune(p.input[j])
		} else {
			r, _ = utf8.DecodeRuneInString(p.input[j:])
		}
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

// advance records that the element before slot next matched input[i:end]
// and enqueues next.
func (p *gll) advance(next slot, u, i, end int) {
	l := p.gss[u].i
	k := instKey{pid: next.pid, l: l}
	sl := p.stepAt[k]
	p.steps = append(p.steps, step{pid: next.pid, ip: next.ip - 1, l: l, i: i, end: end, next: sl.head})
	p.stepAt[k] = stepList{head: len(p.steps) - 1, n: sl.n + 1}
	p.add(next, u, end)
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
		if id, hit := p.gssAt[key]; hit {
			return id
		}
		id := len(p.gss)
		p.gss = append(p.gss, gssNode{sl: sl, i: i})
		p.gssAt[key] = id
		return id
	}
	k := gssKey{pid: sl.pid, ip: sl.ip, i: i}
	if p.gssBig == nil {
		p.gssBig = map[gssKey]int{}
	}
	if id, ok := p.gssBig[k]; ok {
		return id
	}
	id := len(p.gss)
	p.gss = append(p.gss, gssNode{sl: sl, i: i})
	p.gssBig[k] = id
	return id
}

func (p *gll) create(ret slot, u, i int) int {
	v := p.gssNode(ret, i)
	for e := p.gss[v].ehead; e != 0; e = p.edges[e].next {
		if p.edges[e].to == u {
			return v
		}
	}
	p.edges = append(p.edges, gssEdge{to: u, next: p.gss[v].ehead})
	p.gss[v].ehead = len(p.edges) - 1
	for pop := p.gss[v].phead; pop != 0; pop = p.pops[pop].next {
		p.advance(ret, u, i, p.pops[pop].i)
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
		p.advance(ret, p.edges[e].to, from, i)
	}
}

func (p *gll) process(d desc) {
	pr := p.c.prods[d.sl.pid]
	if d.sl.ip == len(pr.rhs) {
		p.complete(d.sl.pid, p.gss[d.u].i, d.i)
		p.pop(d.u, d.i)
		return
	}
	e := pr.rhs[d.sl.ip]
	next := slot{pid: d.sl.pid, ip: d.sl.ip + 1}
	switch e.kind {
	case ekTerm:
		j := p.skip(d.i)
		end, ok := matchTerminal(e.term, p.input, j)
		if ok {
			p.advance(next, d.u, d.i, end)
		} else {
			p.noteFail(j, describeElem(e), pr.nt, false)
		}
	case ekDFA:
		j := p.skip(d.i)
		df := p.c.dfa[e.nt]
		if df == nil {
			return
		}
		end, labs, ok := df.match(p.input, j)
		if !ok {
			p.noteFail(j, describeElem(e), pr.nt, true)
			return
		}
		if e.label != "" && !hasLabel(labs, e.label) {
			p.noteFail(j, describeElem(e), pr.nt, true)
			return
		}
		p.advance(next, d.u, d.i, end)
	case ekNT:
		if p.c.IsDFA(e.nt) {
			j := p.skip(d.i)
			end, labs, ok := p.c.dfa[e.nt].match(p.input, j)
			if ok && hasLabel(labs, e.label) {
				p.advance(next, d.u, d.i, end)
			} else {
				p.noteFail(j, describeElem(e), pr.nt, true)
			}
			return
		}
		v := p.create(next, d.u, d.i)
		p.fork(e.nt, v, d.i)
	case ekLook:
		if p.succeeds(e.nt, d.i) {
			p.advance(next, d.u, d.i, d.i)
		}
	case ekNegLook:
		if !p.succeeds(e.nt, d.i) {
			p.advance(next, d.u, d.i, d.i)
		}
	}
}

func (p *gll) complete(pid, left, right int) {
	k := famKey{nid: p.c.prods[pid].nid, l: left, r: right}
	cur := p.sym[k]
	if cur == 0 {
		p.sym[k] = pid + 1
		return
	}
	if cur-1 == pid {
		return
	}
	extra := p.symMore[k]
	for _, old := range extra {
		if old == pid {
			return
		}
	}
	if p.symMore == nil {
		p.symMore = map[famKey][]int{}
	}
	p.symMore[k] = append(extra, pid)
}

func (p *gll) pidsAt(nid, l, r int) (int, []int) {
	k := famKey{nid: nid, l: l, r: r}
	cur := p.sym[k]
	if cur == 0 {
		return -1, nil
	}
	return cur - 1, p.symMore[k]
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
	q.fork(nt, dummy, i)
	q.drain()
	nid := q.c.ntNID[nt]
	ok := false
	for k := range q.sym {
		if k.nid == nid && k.l == i {
			ok = true
			break
		}
	}
	q.wrapEnd = saved
	q.release()
	return ok
}
