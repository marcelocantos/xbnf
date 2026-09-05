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
	// The following are not measured on this path; they stay 0.
	CalleeReuse    int
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
	return &Profile{
		Descriptors: p.work,
		Packed:      packed,
		GSSNodes:    len(p.gss),
		GSSEdges:    edges,
		Completions: pops,
		Steps:       steps,
		DFAStart:    c != nil && c.IsDFA(p.start),
		ChartBytes:  chartBytes(p),
	}
}

func chartBytes(p *gll) int64 {
	n := int64(cap(p.R))*24 + int64(cap(p.gss))*32 + int64(cap(p.edges))*16
	n += int64(cap(p.pops))*16 + int64(cap(p.steps))*24 + int64(cap(p.wrapEnd))*8
	n += int64(cap(p.slabs))*16 + int64(cap(p.stepPack))*24
	return n
}
