// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package engine

import (
	"github.com/marcelocantos/xbnf/grammar"
)

// twalk builds a sandbox tree from IR. /term/ is a terminal (one string);
// combinators around it keep their structure.
type twalk struct {
	c     *Compiled
	input string
	busy  map[string]int
}

func (c *Compiled) buildTree(start, input string, pos int) Node {
	w := &twalk{c: c, input: input, busy: map[string]int{}}
	end, n, ok := w.rule(start, "", pos, false)
	if !ok {
		return Node{Kind: "rule", Name: start, Text: input[pos:]}
	}
	if n.Name == "" {
		n.Name = start
	}
	if n.Kind == "" {
		n.Kind = "rule"
	}
	if n.Text == "" {
		n.Text = input[pos:end]
	}
	return n
}

func (w *twalk) skip(pos int, nowrap bool) int {
	if nowrap {
		return pos
	}
	return w.c.skipWrap(w.input, pos)
}

func (w *twalk) rule(name, label string, pos int, nowrap bool) (int, Node, bool) {
	r, ok := w.c.rules[name]
	if !ok {
		return pos, Node{}, false
	}
	if p, busy := w.busy[name]; busy && p == pos {
		return pos, Node{}, false
	}
	w.busy[name] = pos
	defer delete(w.busy, name)

	if w.c.IsDFA(name) {
		if _, isLeaf := r.Body.(grammar.Leaf); isLeaf {
			end, labs, ok := w.c.dfa[name].match(w.input, pos)
			if !ok || !hasLabel(labs, label) {
				return pos, Node{}, false
			}
			return end, Node{Kind: "leaf", Name: name, Text: w.input[pos:end]}, true
		}
	}
	end, n, ok := w.term(r.Body, pos, nowrap)
	if !ok {
		return pos, Node{}, false
	}
	if n.Kind == "leaf" {
		n.Name = name
		return end, n, true
	}
	n.Kind = "rule"
	n.Name = name
	n.Text = w.input[pos:end]
	return end, n, true
}

func (w *twalk) term(t grammar.Term, pos int, nowrap bool) (int, Node, bool) {
	pos = w.skip(pos, nowrap)
	switch x := t.(type) {
	case grammar.Leaf:
		end, _, ok := w.term(x.Term, pos, true)
		if !ok {
			return pos, Node{}, false
		}
		return end, Node{Kind: "leaf", Text: w.input[pos:end]}, true
	case grammar.Ident:
		return w.rule(x.Name, x.Label, pos, nowrap)
	case grammar.Named:
		end, n, ok := w.term(x.Term, pos, nowrap)
		if !ok {
			return pos, Node{}, false
		}
		n.Name = x.Name
		return end, n, true
	case grammar.String:
		end, ok := matchTerminal(x, w.input, pos)
		if !ok {
			return pos, Node{}, false
		}
		return end, Node{Kind: "string", Text: x.Text}, true
	case grammar.CharClass, grammar.Escape, grammar.AnyChar:
		end, ok := matchTerminal(x, w.input, pos)
		if !ok {
			return pos, Node{}, false
		}
		return end, Node{Kind: "char", Text: w.input[pos:end]}, true
	case grammar.Empty:
		return pos, Node{Kind: "empty"}, true
	case grammar.Seq:
		kids := make([]Node, 0, len(x.Terms))
		cur := pos
		for _, t := range x.Terms {
			end, n, ok := w.term(t, cur, nowrap)
			if !ok {
				return pos, Node{}, false
			}
			kids = append(kids, n)
			cur = end
		}
		return cur, Node{Kind: "seq", Children: kids, Text: w.input[pos:cur]}, true
	case grammar.OrderedAlt:
		for _, a := range x.Terms {
			end, n, ok := w.term(a, pos, nowrap)
			if ok {
				return end, n, true
			}
		}
		return pos, Node{}, false
	case grammar.Alt:
		bestEnd := pos
		var best Node
		hit := false
		for _, a := range x.Terms {
			end, n, ok := w.term(a, pos, nowrap)
			if !ok {
				continue
			}
			if !hit || end > bestEnd {
				hit = true
				bestEnd = end
				best = n
			}
		}
		if !hit {
			return pos, Node{}, false
		}
		return bestEnd, best, true
	case grammar.Quant:
		var kids []Node
		cur := pos
		n := 0
		for x.Max == grammar.Unbounded || n < x.Max {
			end, node, ok := w.term(x.Term, cur, nowrap)
			if !ok || end < cur {
				break
			}
			if end == cur {
				if x.Max == grammar.Unbounded {
					break
				}
				kids = append(kids, node)
				n++
				break
			}
			kids = append(kids, node)
			cur = end
			n++
		}
		if n < x.Min {
			return pos, Node{}, false
		}
		return cur, Node{Kind: "quant", Children: kids, Text: w.input[pos:cur]}, true
	case grammar.Delim:
		return w.delim(x, pos, nowrap)
	case grammar.Scope:
		return w.term(x.Term, pos, nowrap)
	case grammar.Lookahead:
		_, _, ok := w.term(x.Term, pos, nowrap)
		if !ok {
			return pos, Node{}, false
		}
		return pos, Node{Kind: "lookahead"}, true
	case grammar.NegLookahead:
		_, _, ok := w.term(x.Term, pos, nowrap)
		if ok {
			return pos, Node{}, false
		}
		return pos, Node{Kind: "neg_lookahead"}, true
	case grammar.Stack:
		if len(x.Levels) == 0 {
			return pos, Node{}, false
		}
		return w.term(x.Levels[0], pos, nowrap)
	default:
		end, ok := matchTerminal(t, w.input, pos)
		if !ok {
			return pos, Node{}, false
		}
		return end, Node{Kind: "term", Text: w.input[pos:end]}, true
	}
}

func (w *twalk) delim(d grammar.Delim, pos int, nowrap bool) (int, Node, bool) {
	cur := pos
	var kids []Node
	if d.Leading {
		if end, n, ok := w.term(d.Sep, cur, nowrap); ok {
			kids = append(kids, n)
			cur = end
		}
	}
	end, n, ok := w.term(d.Term, cur, nowrap)
	if !ok {
		return pos, Node{}, false
	}
	kids = append(kids, n)
	cur = end
	for {
		save := cur
		se, sn, sok := w.term(d.Sep, cur, nowrap)
		if !sok {
			break
		}
		te, tn, tok := w.term(d.Term, se, nowrap)
		if !tok {
			if d.Trailing {
				kids = append(kids, sn)
				cur = se
			} else {
				cur = save
			}
			break
		}
		kids = append(kids, sn, tn)
		cur = te
	}
	return cur, Node{Kind: "delim", Children: kids, Text: w.input[pos:cur]}, true
}
