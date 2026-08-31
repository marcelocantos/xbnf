// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package engine

import (
	"fmt"
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

type gll struct {
	c      *Compiled
	input  string
	R      []desc
	U      map[desc]bool
	gss    []gssNode
	gssAt  map[gssKey]int
	sym    map[famKey][]int // prod ids completing (nt,l,r)
	packed int
	start  string
}

type gssKey struct {
	pid, ip, i int
}

func (c *Compiled) gll(start, input string) *Result {
	if start == "" {
		start = c.first
	}
	if start == "" {
		return &Result{Error: "no start rule"}
	}
	if _, ok := c.rules[start]; !ok && !c.IsDFA(start) {
		if _, ok := c.ntProds[start]; !ok && !c.IsDFA(start) {
			return &Result{Error: "unknown rule " + start}
		}
	}
	p := &gll{
		c:     c,
		input: input,
		U:     map[desc]bool{},
		gssAt: map[gssKey]int{},
		sym:   map[famKey][]int{},
		start: start,
	}
	pos := c.skipWrap(input, 0)
	dummy := p.gssNode(slot{pid: -1, ip: 0}, pos)
	if c.IsDFA(start) {
		end, _, ok := c.dfa[start].match(input, pos)
		if !ok {
			return &Result{Error: "no match"}
		}
		end = c.skipWrap(input, end)
		if end != len(input) {
			return &Result{Error: fmt.Sprintf("unconsumed input at byte %d", end), End: end}
		}
		return &Result{OK: true, End: end, Tree: Node{Kind: "rule", Name: start, Text: input[pos:end]}}
	}
	for _, pid := range c.ntProds[start] {
		p.add(slot{pid: pid, ip: 0}, dummy, pos)
	}
	if len(c.ntProds[start]) == 0 {
		return &Result{Error: "unknown rule " + start}
	}
	for len(p.R) > 0 {
		d := p.R[0]
		p.R = p.R[1:]
		p.process(d)
	}
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
		return &Result{Error: "no match"}
	}
	endw := c.skipWrap(input, end)
	if endw != len(input) {
		return &Result{
			Error:  fmt.Sprintf("unconsumed input at byte %d", endw),
			End:    endw,
			Packed: p.packed,
			Tree:   Node{Kind: "rule", Name: start, Text: input[pos:end]},
		}
	}
	return &Result{
		OK:     true,
		End:    endw,
		Packed: p.packed + packs,
		Tree:   Node{Kind: "rule", Name: start, Text: input[pos:end]},
	}
}

func extraPacks(prods []int) int {
	if len(prods) <= 1 {
		return 0
	}
	return len(prods) - 1
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
	p.U[d] = true
	p.R = append(p.R, d)
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
		j := p.c.skipWrap(p.input, d.i)
		end, ok := matchTerminal(e.term, p.input, j)
		if ok {
			p.add(next, d.u, end)
		}
	case ekDFA:
		j := p.c.skipWrap(p.input, d.i)
		df := p.c.dfa[e.nt]
		if df == nil {
			return
		}
		end, labs, ok := df.match(p.input, j)
		if !ok {
			return
		}
		if e.label != "" && !hasLabel(labs, e.label) {
			return
		}
		p.add(next, d.u, end)
	case ekNT:
		v := p.create(next, d.u, d.i)
		if p.c.IsDFA(e.nt) {
			j := p.c.skipWrap(p.input, d.i)
			end, labs, ok := p.c.dfa[e.nt].match(p.input, j)
			if ok && hasLabel(labs, e.label) {
				p.add(next, d.u, end)
			}
			_ = v
			return
		}
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
	q := &gll{
		c:     p.c,
		input: p.input,
		U:     map[desc]bool{},
		gssAt: map[gssKey]int{},
		sym:   map[famKey][]int{},
		start: nt,
	}
	dummy := q.gssNode(slot{pid: -1, ip: 0}, i)
	for _, pid := range q.c.ntProds[nt] {
		q.add(slot{pid: pid, ip: 0}, dummy, i)
	}
	for len(q.R) > 0 {
		d := q.R[0]
		q.R = q.R[1:]
		q.process(d)
	}
	for k := range q.sym {
		if k.nt == nt && k.l == i {
			return true
		}
	}
	return false
}
