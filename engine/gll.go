// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package engine

import (
	"fmt"
	"strings"
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
	edges []gssEdge
	pops  []gssPop
}

type gssEdge struct {
	to int
}

type gssPop struct {
	i int
}

type famKey struct {
	nt   string
	l, r int
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
	U       map[desc]bool
	gss     []gssNode
	gssAt   map[gssKey]int
	sym     map[famKey][]int // prod ids completing (nt,l,r)
	packed  int
	start   string
	fail    failInfo
	wrapEnd []int // memoised skipWrap per position; -1 = unknown
	steps   int   // descriptors processed
}

func newGLL(c *Compiled, input, start string) *gll {
	p := &gll{
		c:     c,
		input: input,
		U:     map[desc]bool{},
		gssAt: map[gssKey]int{},
		sym:   map[famKey][]int{},
		start: start,
		fail:  failInfo{pos: -1, want: map[string]bool{}},
	}
	p.wrapEnd = make([]int, len(input)+1)
	for i := range p.wrapEnd {
		p.wrapEnd[i] = -1
	}
	return p
}

// skip is skipWrap memoised per position.
func (p *gll) skip(i int) int {
	if i < 0 || i > len(p.input) {
		return i
	}
	if e := p.wrapEnd[i]; e >= 0 {
		return e
	}
	e := p.c.skipWrap(p.input, i)
	p.wrapEnd[i] = e
	return e
}

func (p *gll) drain() {
	for len(p.R) > 0 {
		d := p.R[len(p.R)-1]
		p.R = p.R[:len(p.R)-1]
		p.steps++
		p.process(d)
	}
}

type gssKey struct {
	pid, ip, i int
}

func (c *Compiled) gll(start, input string) *Result {
	res, _ := c.run(start, input)
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
		end = c.skipWrap(input, end)
		if end != len(input) {
			line, col := lineCol(input, end)
			return &Result{
				Error: fmt.Sprintf("unconsumed input at %d:%d (byte %d)", line, col, end),
				End:   end,
			}, p
		}
		return &Result{OK: true, End: end, Tree: c.buildTree(start, input, pos)}, p
	}
	for _, pid := range c.ntProds[start] {
		p.add(slot{pid: pid, ip: 0}, dummy, pos)
	}
	p.drain()
	end := -1
	var packs int
	for k, prods := range p.sym {
		if k.nt == start && k.l == pos {
			if k.r > end {
				end = k.r
				packs = extraPacks(prods)
			} else if k.r == end {
				packs += extraPacks(prods)
			}
		}
	}
	if end < 0 {
		return &Result{Error: p.failMessage()}, p
	}
	endw := c.skipWrap(input, end)
	if endw != len(input) {
		line, col := lineCol(input, endw)
		return &Result{
			Error:  fmt.Sprintf("unconsumed input at %d:%d (byte %d)", line, col, endw),
			End:    endw,
			Packed: p.packed,
			Tree:   c.buildTree(start, input, pos),
		}, p
	}
	return &Result{
		OK:     true,
		End:    endw,
		Packed: p.packed + packs,
		Tree:   c.buildTree(start, input, pos),
	}, p
}

func extraPacks(prods []int) int {
	if len(prods) <= 1 {
		return 0
	}
	return len(prods) - 1
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
		p.fail.want = map[string]bool{want: true}
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

func (p *gll) add(sl slot, u, i int) {
	d := desc{sl: sl, u: u, i: i}
	if p.U[d] {
		return
	}
	if !p.admits(sl, i) {
		return
	}
	p.U[d] = true
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
		r, _ := utf8.DecodeRuneInString(p.input[j:])
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

func (p *gll) gssNode(sl slot, i int) int {
	k := gssKey{pid: sl.pid, ip: sl.ip, i: i}
	if id, ok := p.gssAt[k]; ok {
		return id
	}
	id := len(p.gss)
	p.gss = append(p.gss, gssNode{sl: sl, i: i})
	p.gssAt[k] = id
	return id
}

func (p *gll) create(ret slot, u, i int) int {
	v := p.gssNode(ret, i)
	found := false
	for _, e := range p.gss[v].edges {
		if e.to == u {
			found = true
			break
		}
	}
	if !found {
		p.gss[v].edges = append(p.gss[v].edges, gssEdge{to: u})
		for _, pop := range p.gss[v].pops {
			p.add(ret, u, pop.i)
		}
	}
	return v
}

func (p *gll) pop(u, i int) {
	if u < 0 || p.gss[u].sl.pid < 0 {
		// dummy GSS: completing start production
		return
	}
	p.gss[u].pops = append(p.gss[u].pops, gssPop{i: i})
	ret := p.gss[u].sl
	for _, e := range p.gss[u].edges {
		p.add(ret, e.to, i)
	}
}

func (p *gll) process(d desc) {
	pr := p.c.prods[d.sl.pid]
	if d.sl.ip == len(pr.rhs) {
		p.complete(pr.nt, d.sl.pid, p.gss[d.u].i, d.i)
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
			p.add(next, d.u, end)
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
		p.add(next, d.u, end)
	case ekNT:
		if p.c.IsDFA(e.nt) {
			j := p.skip(d.i)
			end, labs, ok := p.c.dfa[e.nt].match(p.input, j)
			if ok && hasLabel(labs, e.label) {
				p.add(next, d.u, end)
			} else {
				p.noteFail(j, describeElem(e), pr.nt, true)
			}
			return
		}
		v := p.create(next, d.u, d.i)
		for _, pid := range p.c.ntProds[e.nt] {
			p.add(slot{pid: pid, ip: 0}, v, d.i)
		}
	case ekLook:
		if p.succeeds(e.nt, d.i) {
			p.add(next, d.u, d.i)
		}
	case ekNegLook:
		if !p.succeeds(e.nt, d.i) {
			p.add(next, d.u, d.i)
		}
	}
}

func (p *gll) complete(nt string, pid, left, right int) {
	k := famKey{nt: nt, l: left, r: right}
	for _, old := range p.sym[k] {
		if old == pid {
			return
		}
	}
	if len(p.sym[k]) >= 1 {
		p.packed++
	}
	p.sym[k] = append(p.sym[k], pid)
}

func (p *gll) succeeds(nt string, i int) bool {
	if p.c.IsDFA(nt) {
		_, _, ok := p.c.dfa[nt].match(p.input, i)
		return ok
	}
	q := newGLL(p.c, p.input, nt)
	q.wrapEnd = p.wrapEnd
	dummy := q.gssNode(slot{pid: -1, ip: 0}, i)
	for _, pid := range q.c.ntProds[nt] {
		q.add(slot{pid: pid, ip: 0}, dummy, i)
	}
	q.drain()
	for k := range q.sym {
		if k.nt == nt && k.l == i {
			return true
		}
	}
	return false
}
