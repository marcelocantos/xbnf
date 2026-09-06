// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package engine

// Profile is a diagnostic snapshot of one Parse. Headline timing and B/op
// must use Parse, not ParseProfile: this path keeps the gll long enough to
// copy counters. GSSEdges is the number of GSS edges, not forest ambiguity;
// Packed is competing derivations.
type Profile struct {
	Descriptors int   // GLL descriptors processed (work)
	Packed      int   // packed forest nodes; 0 means a unique derivation
	GSSNodes    int   // GSS vertices created this parse
	GSSEdges    int   // GSS edges; do not treat as ambiguity
	Completions int   // pop/completion records
	Steps       int   // derivation steps recorded
	DFAStart    bool  // start rule ran as a DFA, not GLL
	ChartBytes  int64 // approximate backing capacity of charts and steps
	// CalleeReuse and CalleeDupDescriptors measure H1 (docs/parse-performance-research.md
	// 5.1): the current GSS is keyed by (return slot, position), so two call sites that
	// invoke the same nonterminal at the same position get distinct GSS nodes. CalleeReuse
	// is GSSNodes minus the number of distinct (nonterminal, position) pairs among them.
	// CalleeDupDescriptors sums, over each duplicated (nonterminal, position) group of size
	// k>1, (k-1) times that nonterminal's production count: the descriptors that per-callee
	// sharing would not have scheduled.
	CalleeReuse          int
	CalleeDupDescriptors int
	// The following are not measured on this path; they stay 0.
	ContnLifetimes int
	DFATransitions int
}

// ParseProfile runs the same parse as Parse and returns a Profile. Do not use
// it inside a timed headline section.
func (c *Compiled) ParseProfile(start, input string) (*Result, *Profile) {
	res, p := c.run(start, input)
	prof := snapshotProfile(c, p, res)
	if p != nil {
		p.release()
	}
	return res, prof
}

func snapshotProfile(c *Compiled, p *gll, res *Result) *Profile {
	if p == nil {
		return &Profile{}
	}
	packed := 0
	if res != nil {
		packed = res.Packed
	}
	edges, pops, steps := 0, 0, 0
	if n := len(p.edges); n > 1 {
		edges = n - 1
	}
	if n := len(p.pops); n > 1 {
		pops = n - 1
	}
	if n := len(p.steps); n > 1 {
		steps = n - 1
	}
	reuse, dup := calleeSharing(c, p)
	return &Profile{
		Descriptors:          p.work,
		Packed:               packed,
		GSSNodes:             len(p.gss),
		GSSEdges:             edges,
		Completions:          pops,
		Steps:                steps,
		DFAStart:             c != nil && c.IsDFA(p.start),
		ChartBytes:           chartBytes(p),
		CalleeReuse:          reuse,
		CalleeDupDescriptors: dup,
	}
}

// calleeKey identifies the (nonterminal, position) pair a GSS node calls. nid holds the
// dense nonterminal id when resolved (>= 0); unresolved calls (nid < 0, e.g. a rule folded
// into a DFA elsewhere) key on name instead, matching the fallback in the ekNT case of
// process.
type calleeKey struct {
	nid  int
	name string
	pos  int
}

// calleeSharing implements the H1 measurement (docs/parse-performance-research.md 5.1):
// how many GSS nodes ask the same (nonterminal, position) question as another node, and
// how many descriptors that duplication costs. A GSS node is keyed by (return slot,
// position); the nonterminal it calls is the element before the return slot,
// c.prods[sl.pid].rhs[sl.ip-1]. The dummy start node (pid < 0) is not a call and is
// skipped.
func calleeSharing(c *Compiled, p *gll) (reuse, dupDescriptors int) {
	counts := map[calleeKey]int{}
	for _, n := range p.gss {
		if n.sl.pid < 0 {
			continue
		}
		e := c.prods[n.sl.pid].rhs[n.sl.ip-1]
		k := calleeKey{pos: n.i, nid: e.nid}
		if e.nid < 0 {
			k.name = e.nt
		}
		counts[k]++
	}
	for k, n := range counts {
		if n <= 1 {
			continue
		}
		reuse += n - 1
		nProds := 0
		if k.nid >= 0 {
			nProds = len(c.ntProdsN[k.nid])
		} else {
			nProds = len(c.ntProds[k.name])
		}
		dupDescriptors += (n - 1) * nProds
	}
	return reuse, dupDescriptors
}

func chartBytes(p *gll) int64 {
	n := int64(cap(p.R))*24 + int64(cap(p.gss))*32 + int64(cap(p.edges))*16
	n += int64(cap(p.pops))*16 + int64(cap(p.steps))*24 + int64(cap(p.wrapEnd))*8
	n += int64(cap(p.slabs))*16 + int64(cap(p.stepPack))*24
	return n
}
