// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

// Package engine matches input against a grammar.Grammar using GLL with a DFA
// terminal layer for regular-fragment rules.
package engine

// Result is the outcome of Parse.
type Result struct {
	OK     bool   `json:"ok"`
	Error  string `json:"error,omitempty"`
	End    int    `json:"end,omitempty"`
	Tree   Node   `json:"tree,omitempty"`
	Packed int    `json:"packed,omitempty"`
}

// Node is a sandbox-facing parse tree. Children of a tree from Parse, when
// non-empty, are a subslice of one backing array shared by that tree. Treat
// the tree as read-only: do not append to Children or retain a child slice
// after discarding the Result.
//
// Start and End are the half-open byte span [Start, End) of this node in the
// input that was parsed (docs/tree-positions.md). Leading #wrap is not in the
// span. When Text is a verbatim input slice, Text == input[Start:End].
type Node struct {
	Kind     string `json:"kind"`
	Name     string `json:"name,omitempty"`
	Text     string `json:"text,omitempty"`
	Start    int    `json:"start"`
	End      int    `json:"end"`
	Children []Node `json:"children,omitempty"`
}
