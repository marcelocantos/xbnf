// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package engine

import "testing"

// TestProfileNestedJSON logs the H1 callee-sharing measurement (docs/parse-performance-research.md
// 5.1) for nestedJSON(64<<10) on docs/examples/json.xbnf: how many GSS nodes ask the same
// (nonterminal, position) question as another node, and how many descriptors that duplication
// costs. This is a measurement log, not a ratchet; it only asserts the parse still succeeds.
func TestProfileNestedJSON(t *testing.T) {
	c := compileDoc(t, "docs/examples/json.xbnf")
	in := nestedJSON(64 << 10)
	res, prof := c.ParseProfile("json", in)
	if !res.OK {
		t.Fatalf("parse failed: %s", res.Error)
	}
	t.Logf("nestedJSON(64<<10): Descriptors=%d GSSNodes=%d CalleeReuse=%d CalleeDupDescriptors=%d",
		prof.Descriptors, prof.GSSNodes, prof.CalleeReuse, prof.CalleeDupDescriptors)
}
