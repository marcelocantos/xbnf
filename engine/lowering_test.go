// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package engine

import (
	"strings"
	"testing"
)

// TestOptionalLowersToOneNT locks the `X?` shape: a single `$q ::= ε | inner`,
// with no `$qo` unit wrapper and no dead `$qs` loop.
func TestOptionalLowersToOneNT(t *testing.T) {
	c := compileSrc(t, "s -> \"a\" t? \"c\" ;\nt -> \"(\" s \")\" | \"b\" ;\n")
	var q string
	for _, pr := range c.prods {
		if strings.HasPrefix(pr.nt, "$qo") || strings.HasPrefix(pr.nt, "$qs") {
			t.Fatalf("`X?` still emits the %s wrapper", pr.nt)
		}
		if strings.HasPrefix(pr.nt, "$q") {
			q = pr.nt
		}
	}
	if q == "" {
		t.Fatal("`X?` emitted no $q nonterminal")
	}
	pids := c.ntProds[q]
	if len(pids) != 2 {
		t.Fatalf("%s has %d productions, want 2 (ε and inner)", q, len(pids))
	}
	if n := len(c.prods[pids[0]].rhs); n != 0 {
		t.Fatalf("%s: first production has %d elements, want the ε alternative first", q, n)
	}
	if n := len(c.prods[pids[1]].rhs); n != 1 {
		t.Fatalf("%s: second production has %d elements, want 1", q, n)
	}
}

// TestBoundedQuantStillWraps: `{m,n}` with n > 1 still needs the shared `$qo`
// optional, one per repeat slot.
func TestBoundedQuantStillWraps(t *testing.T) {
	c := compileSrc(t, "s -> t{1,3} ;\nt -> \"(\" s \")\" | \"b\" ;\n")
	res := c.Parse("s", "bb")
	if !res.OK {
		t.Fatalf("`t{1,3}` rejected \"bb\": %s", res.Error)
	}
	if res := c.Parse("s", "bbbb"); res.OK {
		t.Fatal("`t{1,3}` accepted \"bbbb\"")
	}
	opt := false
	for _, pr := range c.prods {
		if strings.HasPrefix(pr.nt, "$qo") {
			opt = true
		}
	}
	if !opt {
		t.Fatal("`t{1,3}` no longer emits the $qo optional")
	}
}
