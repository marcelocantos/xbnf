// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package engine

import (
	"strings"
	"testing"
)

// TestCaptureRefCompetingDerivations probes an H1-adjacent hazard in boundText
// (engine/gll.go): the step slab it reads is keyed only by (pid, left), the
// calling production's own identity and start position, not by which specific
// derivation of a named element is currently active. When a named element (here
// `n=A`) has two productions that both complete unconditionally from the same
// start position with DIFFERENT lengths — one via a direct literal, one via an
// indirected nonterminal chain that a compile-time disjoint-FIRST check cannot
// fold away — both completions get written into the same slab, and boundText
// returns whichever happens to be nearer the list head (the most recently
// written entry), not necessarily the one belonging to the derivation path that
// is asking.
//
// This grammar is built specifically to exercise that shared slab: A's two
// alternatives ("aa" and, via X -> Y, "a") both complete trivially from
// position 0 regardless of context, so for any input starting with two 'a's,
// entries for both lengths coexist in s's slab by the time %n is checked.
// Reference: exists k in {1, 2} with 2*k == len(input) (a run of length k,
// captured by n, followed by %n requiring a byte-for-byte copy of it).
func TestCaptureRefCompetingDerivations(t *testing.T) {
	c := mustCompileXBNF(t, `
s -> n=A %n ;
A -> (?="a") "aa" #prefer | (?="a") X ;
X -> Y ;
Y -> "a" ;
#wrap -> () ;
`)
	for l := 0; l <= 8; l++ {
		in := strings.Repeat("a", l)
		res := c.Parse("s", in)
		want := l == 2 || l == 4
		if res.OK != want {
			t.Errorf("len=%d input=%q: OK=%v want=%v (order-dependent %%n binding in boundText; see engine/gll.go boundText and docs/parse-performance-research.md 5.1)",
				l, in, res.OK, want)
		}
	}
}
