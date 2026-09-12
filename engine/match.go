// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

// Package engine matches input against a grammar.Grammar using GLL with a DFA
// terminal layer for regular-fragment rules.
package engine

// Result is the outcome of Parse. The parse tree is Events, a preorder
// stream whose leaf and skip lengths tile input[0:End] (docs/tree-stream.md);
// Tree decodes it into Nodes on demand.
type Result struct {
	OK     bool    `json:"ok"`
	Error  string  `json:"error,omitempty"`
	End    int     `json:"end,omitempty"`
	Packed int     `json:"packed,omitempty"`
	Events []Event `json:"events,omitempty"`
	// Input is the string that was parsed. Node text is a slice of it.
	Input string `json:"-"`
}

// Tree decodes Events into a tree. Children of the result, when non-empty,
// are a subslice of one backing array shared by that tree; do not append to
// Children. A Result with no events decodes to the zero Node.
func (r *Result) Tree() Node {
	return Decode(r.Events)
}

// Node is one node of the tree Result.Tree decodes from the event stream.
//
// Start and End are the half-open byte span [Start, End) of this node in the
// input that was parsed (docs/tree-positions.md). Leading #wrap is not in the
// span; the node's text is input[Start:End], which Text returns.
type Node struct {
	Kind     Kind   `json:"kind"`
	Name     string `json:"name,omitempty"`
	Start    int    `json:"start"`
	End      int    `json:"end"`
	Children []Node `json:"children,omitempty"`
}

// Text is the input this node covers, or "" when the span does not lie
// within input.
func (n Node) Text(input string) string {
	if n.Start < 0 || n.Start > n.End || n.End > len(input) {
		return ""
	}
	return input[n.Start:n.End]
}
