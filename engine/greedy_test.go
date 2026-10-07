// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package engine_test

import "testing"

// scopeGrammar has the shape of arr.ai's expression stack: the tightest
// level's `let` form ends in `@`, which wraps around to the loosest level.
const scopeGrammar = `
expr -> @:op="+"
      > @:op="*"
      > let=("let" IDENT "=" @ ";" @) |> "(" @ ")" |> IDENT |> NUM ;
IDENT -> /[a-z]+/ ;
NUM -> /[0-9]+/ ;
#wrap -> \s* ;
`

// TestGreedyStackTail pins the PEG rule for a wrap-around tail: the `@` at
// the end of a tightest-level form consumes everything it can, so the
// form's derivation beats a looser level's operator for the same span.
// wbnf, and therefore arr.ai, parse these the same way.
func TestGreedyStackTail(t *testing.T) {
	t.Parallel()
	cases := []struct{ in, want string }{
		{"let x = 1; x + x", "expr[let[let IDENT:x = NUM:1 ; [IDENT:x op:+ IDENT:x]]]"},
		{"1 + let x = 2; x * 3", "expr[[NUM:1 op:+ let[let IDENT:x = NUM:2 ; [IDENT:x op:* NUM:3]]]]"},
		{"let x = 1; x + x * 2", "expr[let[let IDENT:x = NUM:1 ; [IDENT:x op:+ [IDENT:x op:* NUM:2]]]]"},
		// A bounded form is not greedy: the operator level wins as before.
		{"(let x = 1; x) + x", "expr[[[( let[let IDENT:x = NUM:1 ; IDENT:x] )] op:+ IDENT:x]]"},
		{"1 + 2 * 3", "expr[[NUM:1 op:+ [NUM:2 op:* NUM:3]]]"},
	}
	for _, tc := range cases {
		wantTree(t, scopeGrammar, "expr", tc.in, tc.want)
	}
}
