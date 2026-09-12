// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package engine

// Op is the kind of an Event.
type Op uint8

const (
	// OpOpen begins a node with children; Kind and Name describe it.
	OpOpen Op = iota + 1
	// OpClose ends the innermost open node.
	OpClose
	// OpLeaf is a node without children covering the next Len input bytes.
	OpLeaf
	// OpSkip covers the next Len input bytes with no node: #wrap text.
	OpSkip
)

var opNames = [...]string{OpOpen: "open", OpClose: "close", OpLeaf: "leaf", OpSkip: "skip"}

func (o Op) String() string {
	if int(o) < len(opNames) && opNames[o] != "" {
		return opNames[o]
	}
	return "op?"
}

// MarshalText encodes the op as its name, so an Event's JSON reads
// {"op":"leaf",...}.
func (o Op) MarshalText() ([]byte, error) {
	return []byte(o.String()), nil
}

// UnmarshalText accepts the names MarshalText produces.
func (o *Op) UnmarshalText(text []byte) error {
	for i, name := range opNames {
		if name != "" && name == string(text) {
			*o = Op(i)
			return nil
		}
	}
	return &unknownOpError{string(text)}
}

type unknownOpError struct{ name string }

func (e *unknownOpError) Error() string { return "xbnf: unknown event op " + e.name }

// Kind is the grammatical role of a node: what construct produced it.
type Kind uint8

const (
	// KindNone is the zero Kind; Close and Skip events carry it.
	KindNone Kind = iota
	// KindRule is a named rule with children.
	KindRule
	// KindQuant is a repetition; KindDelim a delimited list; KindSeq a sequence
	// that needed a node of its own (a named capture of several elements).
	KindQuant
	KindDelim
	KindSeq
	// KindLeaf is a regular rule collapsed to one string, or a /term/ leaf.
	KindLeaf
	// KindRef is a back-reference match; KindString a string literal; KindChar
	// a character class, escape or any-char match; KindTerm any other terminal.
	KindRef
	KindString
	KindChar
	KindTerm
	// KindEmpty, KindLookahead and KindNegLookahead are zero-width nodes a
	// regular rule body's structure can carry.
	KindEmpty
	KindLookahead
	KindNegLookahead
)

var kindNames = [...]string{
	KindNone: "", KindRule: "rule", KindQuant: "quant", KindDelim: "delim", KindSeq: "seq",
	KindLeaf: "leaf", KindRef: "ref", KindString: "string", KindChar: "char", KindTerm: "term",
	KindEmpty: "empty", KindLookahead: "lookahead", KindNegLookahead: "neg_lookahead",
}

func (k Kind) String() string {
	if int(k) < len(kindNames) {
		return kindNames[k]
	}
	return "kind?"
}

// MarshalText encodes the kind as its name, so JSON reads "kind":"rule".
func (k Kind) MarshalText() ([]byte, error) {
	return []byte(k.String()), nil
}

// UnmarshalText accepts the names MarshalText produces.
func (k *Kind) UnmarshalText(text []byte) error {
	for i, name := range kindNames {
		if name == string(text) {
			*k = Kind(i)
			return nil
		}
	}
	return &unknownKindError{string(text)}
}

type unknownKindError struct{ name string }

func (e *unknownKindError) Error() string { return "xbnf: unknown node kind " + e.name }

// Event is one step of a preorder walk over a parse tree
// (docs/tree-stream.md). Open and Close bracket a node with children; Leaf
// is a childless node; Skip is input that belongs to no node. The lengths of
// Leaf and Skip events sum to the end of the parse, and every byte before it
// is covered by exactly one of them.
//
// Len is int32 for the same reason the builder's arena is: an input that
// overflows it would need tens of gigabytes of chart, and events are the
// output's memory footprint. An Event is 24 bytes.
type Event struct {
	Op   Op     `json:"op"`
	Kind Kind   `json:"kind,omitempty"`
	Len  int32  `json:"len,omitempty"`
	Name string `json:"name,omitempty"`
}

// Decode builds the tree an event stream describes. Node spans are the byte
// cursor at the node's first and last event. Children of every node are a
// subslice of one backing array, so the tree is n Nodes in one allocation.
//
// The stream is trusted to be well formed; a malformed one still decodes
// without panicking: a Close with nothing open is ignored, a negative length
// counts as zero, and only the first root is returned.
func Decode(events []Event) Node {
	// Pass one: the child count of every node, in preorder, so pass two can
	// hand each node a contiguous block of the output for its children.
	n := 0
	for i := range events {
		if events[i].Op == OpOpen || events[i].Op == OpLeaf {
			n++
		}
	}
	if n == 0 {
		return Node{}
	}
	counts := make([]int32, n)
	var stack []int32
	idx := int32(0)
	for i := range events {
		switch events[i].Op {
		case OpOpen:
			if len(stack) > 0 {
				counts[stack[len(stack)-1]]++
			}
			stack = append(stack, idx)
			idx++
		case OpLeaf:
			if len(stack) > 0 {
				counts[stack[len(stack)-1]]++
			}
			idx++
		case OpClose:
			if len(stack) > 0 {
				stack = stack[:len(stack)-1]
			}
		}
	}

	// Pass two: place nodes. A frame is an open node's output slot and the
	// slot its next child takes.
	type frame struct{ slot, next int32 }
	out := make([]Node, n)
	frames := make([]frame, 0, 16)
	free := int32(1) // first unassigned slot; slot 0 is the root
	cur := 0
	idx = 0
	for i := range events {
		e := &events[i]
		switch e.Op {
		case OpOpen, OpLeaf:
			var slot int32
			if len(frames) > 0 {
				f := &frames[len(frames)-1]
				slot = f.next
				f.next++
			} else if idx == 0 {
				slot = 0
			} else {
				slot = free // a second root: placed, but unreachable
				free++
			}
			out[slot] = Node{Kind: e.Kind, Name: e.Name, Start: cur, End: cur}
			if k := counts[idx]; k > 0 {
				out[slot].Children = out[free : free+k : free+k]
				free += k
			}
			if e.Op == OpOpen {
				frames = append(frames, frame{slot: slot, next: free - counts[idx]})
			} else {
				cur += max(int(e.Len), 0)
				out[slot].End = cur
			}
			idx++
		case OpClose:
			if len(frames) > 0 {
				out[frames[len(frames)-1].slot].End = cur
				frames = frames[:len(frames)-1]
			}
		case OpSkip:
			cur += max(int(e.Len), 0)
		}
	}
	return out[0]
}

// appendNodeEvents appends the events for the tree rooted at n, starting from
// byte cursor cur, and returns the cursor after it. A gap between cur and a
// node's Start becomes a Skip.
func appendNodeEvents(ev []Event, n *Node, cur int) ([]Event, int) {
	if n.Start > cur {
		ev = append(ev, Event{Op: OpSkip, Len: int32(n.Start - cur)})
		cur = n.Start
	}
	if len(n.Children) == 0 {
		length := max(n.End-cur, 0)
		ev = append(ev, Event{Op: OpLeaf, Kind: n.Kind, Name: n.Name, Len: int32(length)})
		return ev, cur + length
	}
	ev = append(ev, Event{Op: OpOpen, Kind: n.Kind, Name: n.Name})
	for i := range n.Children {
		ev, cur = appendNodeEvents(ev, &n.Children[i], cur)
	}
	if n.End > cur {
		ev = append(ev, Event{Op: OpSkip, Len: int32(n.End - cur)})
		cur = n.End
	}
	return append(ev, Event{Op: OpClose}), cur
}

// countNodeEvents is the number of events appendNodeEvents will append for
// n from cursor cur, and the cursor after it.
func countNodeEvents(n *Node, cur int) (int, int) {
	count := 0
	if n.Start > cur {
		count++
		cur = n.Start
	}
	if len(n.Children) == 0 {
		return count + 1, cur + max(n.End-cur, 0)
	}
	count++
	for i := range n.Children {
		var k int
		k, cur = countNodeEvents(&n.Children[i], cur)
		count += k
	}
	if n.End > cur {
		count++
		cur = n.End
	}
	return count + 1, cur
}

// skipTo appends a Skip covering [cur, end) when end is past cur.
func skipTo(ev []Event, cur, end int) []Event {
	if end > cur {
		ev = append(ev, Event{Op: OpSkip, Len: int32(end - cur)})
	}
	return ev
}
