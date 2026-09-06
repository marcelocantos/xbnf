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
	// CalleeReuse and CalleeDupDescriptors measured H1 (docs/parse-performance-research.md
	// 5.1): how many GSS nodes asked the same (nonterminal, position) question as another
	// node, and how many descriptors that duplication cost. The GSS is now keyed by
	// (nonterminal, position) itself, so both are 0 by construction. calleeSharing still
	// recomputes them from the chart rather than hardcoding 0, so a regression that
	// reintroduced per-call-site nodes would show up here.
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

// calleeKey identifies the (nonterminal, position) question a GSS node asks.
type calleeKey struct {
	nid int
	pos int
}

// calleeSharing is the H1 invariant check (docs/parse-performance-research.md 5.1). GSS
// nodes are keyed by (nonterminal, position), so no two nodes can ask the same question
// and both returns are 0. Recomputing them from the chart — rather than returning 0
// outright — keeps the counter honest if the keying ever regresses to per-call-site
// nodes. The sentinel nodes (dummyNID, unresolvedNID) name no nonterminal and are
// skipped.
func calleeSharing(c *Compiled, p *gll) (reuse, dupDescriptors int) {
	counts := map[calleeKey]int{}
	for _, n := range p.gss {
		if n.nid < 0 {
			continue
		}
		counts[calleeKey{nid: n.nid, pos: n.i}]++
	}
	for k, n := range counts {
		if n <= 1 {
			continue
		}
		reuse += n - 1
		dupDescriptors += (n - 1) * len(c.ntProdsN[k.nid])
	}
	return reuse, dupDescriptors
}

func chartBytes(p *gll) int64 {
	n := int64(cap(p.R))*24 + int64(cap(p.gss))*32 + int64(cap(p.edges))*40
	n += int64(cap(p.pops))*24 + int64(cap(p.steps))*12 + int64(cap(p.wrapEnd))*8
	n += int64(cap(p.cells)) * 4
	return n
}
