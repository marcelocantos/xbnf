// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

// Package engine matches input against a grammar.Grammar.
//
// This is a longest-match interpreter of the IR. Left-recursive rules fail at
// the same input position (GLL, T4, replaces this without changing the IR).
package engine

import (
	"fmt"
	"regexp"
	"unicode"
	"unicode/utf8"

	"github.com/marcelocantos/xbnf/grammar"
)

// Result is the outcome of Parse.
type Result struct {
	OK    bool   `json:"ok"`
	Error string `json:"error,omitempty"`
	End   int    `json:"end,omitempty"`
	Tree  Node   `json:"tree,omitempty"`
}

// Node is a sandbox-facing parse tree.
type Node struct {
	Kind     string `json:"kind"`
	Name     string `json:"name,omitempty"`
	Text     string `json:"text,omitempty"`
	Children []Node `json:"children,omitempty"`
}

// Parse matches input against g starting at start (empty: first rule).
func Parse(g *grammar.Grammar, start, input string) *Result {
	if g == nil || len(g.Stmts) == 0 {
		return &Result{Error: "empty grammar"}
	}
	m := newMatcher(g)
	if start == "" {
		start = m.firstRule
	}
	if start == "" {
		return &Result{Error: "no start rule"}
	}
	if err := rejectLater(g.Stmts); err != nil {
		return &Result{Error: err.Error()}
	}
	pos := m.skipWrap(input, 0)
	end, node, err := m.matchRule(start, input, pos)
	if err != nil {
		return &Result{Error: err.Error(), End: end}
	}
	end = m.skipWrap(input, end)
	if end != len(input) {
		return &Result{
			Error: fmt.Sprintf("unconsumed input at byte %d", end),
			End:   end,
			Tree:  node,
		}
	}
	return &Result{OK: true, End: end, Tree: node}
}

type matcher struct {
	rules     map[string]grammar.Rule
	firstRule string
	wrap      grammar.Term
	tighter   func(string, int) (int, Node, error)
	active    map[string]int
}

func newMatcher(g *grammar.Grammar) *matcher {
	m := &matcher{
		rules:  make(map[string]grammar.Rule),
		active: make(map[string]int),
	}
	applyStmts(m, g.Stmts)
	return m
}

func applyStmts(m *matcher, stmts []grammar.Stmt) {
	for _, st := range stmts {
		switch s := st.(type) {
		case grammar.Rule:
			if m.firstRule == "" {
				m.firstRule = s.Name
			}
			m.rules[s.Name] = s
		case grammar.Wrap:
			m.wrap = s.Body
		}
	}
}

func rejectLater(stmts []grammar.Stmt) error {
	var walkTerm func(grammar.Term) error
	var walkStmts func([]grammar.Stmt) error
	walkTerm = func(t grammar.Term) error {
		switch x := t.(type) {
		case grammar.Stack:
			for _, l := range x.Levels {
				if err := walkTerm(l); err != nil {
					return err
				}
			}
		case grammar.Alt:
			for _, c := range x.Terms {
				if err := walkTerm(c); err != nil {
					return err
				}
			}
		case grammar.Seq:
			for _, c := range x.Terms {
				if err := walkTerm(c); err != nil {
					return err
				}
			}
		case grammar.Named:
			return walkTerm(x.Term)
		case grammar.Quant:
			return walkTerm(x.Term)
		case grammar.Delim:
			if err := walkTerm(x.Term); err != nil {
				return err
			}
			return walkTerm(x.Sep)
		case grammar.Scope:
			if err := walkStmts(x.Decls); err != nil {
				return err
			}
			return walkTerm(x.Term)
		case grammar.Ident:
			if x.Label != "" {
				return fmt.Errorf("first-slice runner: %s::%s is not executed", x.Name, x.Label)
			}
		case grammar.Lookahead:
			return walkTerm(x.Term)
		case grammar.NegLookahead:
			return walkTerm(x.Term)
		case grammar.Ref:
			return fmt.Errorf("first-slice runner: %% ref is not executed")
		case grammar.ExtRef:
			return fmt.Errorf("first-slice runner: %%%% extref is not executed")
		case grammar.MacroCall:
			return fmt.Errorf("first-slice runner: macro call is not executed")
		case grammar.PosProp:
			return fmt.Errorf("first-slice runner: @%s is not executed", x.Name)
		}
		return nil
	}
	walkStmts = func(stmts []grammar.Stmt) error {
		for _, st := range stmts {
			switch s := st.(type) {
			case grammar.Import:
				return fmt.Errorf("first-slice runner: #import is not executed")
			case grammar.Macro:
				return fmt.Errorf("first-slice runner: #macro is not executed")
			case grammar.Rule:
				if err := walkTerm(s.Body); err != nil {
					return err
				}
			case grammar.Wrap:
				if err := walkTerm(s.Body); err != nil {
					return err
				}
			}
		}
		return nil
	}
	return walkStmts(stmts)
}

func (m *matcher) matchRule(name, input string, pos int) (int, Node, error) {
	r, ok := m.rules[name]
	if !ok {
		return pos, Node{}, fmt.Errorf("unknown rule %s", name)
	}
	if p, busy := m.active[name]; busy && p == pos {
		return pos, Node{}, fmt.Errorf("left recursion on %s", name)
	}
	m.active[name] = pos
	end, node, err := m.matchTerm(r.Body, input, pos)
	delete(m.active, name)
	if err != nil {
		return end, node, err
	}
	if hasMod(r.Mods, "leaf") {
		return end, Node{Kind: "leaf", Name: name, Text: input[pos:end]}, nil
	}
	node.Name = name
	if node.Kind == "" {
		node.Kind = "rule"
	}
	return end, node, nil
}

func hasMod(mods []string, name string) bool {
	for _, m := range mods {
		if m == name {
			return true
		}
	}
	return false
}

func (m *matcher) skipWrap(input string, pos int) int {
	if m.wrap == nil {
		return pos
	}
	if _, ok := m.wrap.(grammar.Empty); ok {
		return pos
	}
	saved := m.wrap
	m.wrap = nil
	end, _, err := m.matchTerm(saved, input, pos)
	m.wrap = saved
	if err != nil || end < pos {
		return pos
	}
	return end
}

func (m *matcher) matchTerm(t grammar.Term, input string, pos int) (int, Node, error) {
	switch x := t.(type) {
	case grammar.Stack:
		return m.matchStack(x, input, pos)
	case grammar.Alt:
		return m.matchAlt(x.Terms, input, pos)
	case grammar.Seq:
		return m.matchSeq(x.Terms, input, pos)
	case grammar.Named:
		end, n, err := m.matchTerm(x.Term, input, pos)
		if err != nil {
			return end, n, err
		}
		n.Name = x.Name
		return end, n, nil
	case grammar.Quant:
		return m.matchQuant(x, input, pos)
	case grammar.Delim:
		return m.matchDelim(x, input, pos)
	case grammar.Scope:
		return m.matchScope(x, input, pos)
	case grammar.Ident:
		if x.Label != "" {
			return pos, Node{}, fmt.Errorf("label filter %s::%s is not executed yet", x.Name, x.Label)
		}
		return m.matchRule(x.Name, input, pos)
	case grammar.String:
		pos = m.skipWrap(input, pos)
		if !hasPrefix(input, pos, x.Text) {
			return pos, Node{}, fmt.Errorf("expected %q", x.Text)
		}
		end := pos + len(x.Text)
		return end, Node{Kind: "string", Text: x.Text}, nil
	case grammar.CharClass:
		pos = m.skipWrap(input, pos)
		if pos >= len(input) {
			return pos, Node{}, fmt.Errorf("expected character class")
		}
		r, n := utf8.DecodeRuneInString(input[pos:])
		if !classMatch(x, r) {
			return pos, Node{}, fmt.Errorf("character %q fails class", string(r))
		}
		return pos + n, Node{Kind: "class", Text: string(r)}, nil
	case grammar.Escape:
		pos = m.skipWrap(input, pos)
		if pos >= len(input) {
			return pos, Node{}, fmt.Errorf("expected \\%s", x.Code)
		}
		r, n := utf8.DecodeRuneInString(input[pos:])
		if !escapeMatch(x.Code, r) {
			return pos, Node{}, fmt.Errorf("character %q fails \\%s", string(r), x.Code)
		}
		return pos + n, Node{Kind: "escape", Text: string(r)}, nil
	case grammar.Leaf:
		pos = m.skipWrap(input, pos)
		re, err := leafRegexp(x.Pattern)
		if err != nil {
			return pos, Node{}, err
		}
		loc := re.FindStringIndex(input[pos:])
		if loc == nil || loc[0] != 0 {
			return pos, Node{}, fmt.Errorf("leaf /%s/ failed", x.Pattern)
		}
		end := pos + loc[1]
		return end, Node{Kind: "leaf", Text: input[pos:end]}, nil
	case grammar.AnyChar:
		pos = m.skipWrap(input, pos)
		if pos >= len(input) {
			return pos, Node{}, fmt.Errorf("expected any character")
		}
		_, n := utf8.DecodeRuneInString(input[pos:])
		return pos + n, Node{Kind: "any", Text: input[pos : pos+n]}, nil
	case grammar.Lookahead:
		_, _, err := m.matchTerm(x.Term, input, pos)
		if err != nil {
			return pos, Node{}, err
		}
		return pos, Node{Kind: "lookahead"}, nil
	case grammar.NegLookahead:
		_, _, err := m.matchTerm(x.Term, input, pos)
		if err == nil {
			return pos, Node{}, fmt.Errorf("negative lookahead matched")
		}
		return pos, Node{Kind: "neg_lookahead"}, nil
	case grammar.Self:
		if m.tighter == nil {
			return pos, Node{}, fmt.Errorf("@ outside a stack")
		}
		return m.tighter(input, pos)
	case grammar.Empty:
		return pos, Node{Kind: "empty"}, nil
	case grammar.Ref, grammar.ExtRef, grammar.MacroCall, grammar.PosProp:
		return pos, Node{}, fmt.Errorf("%s is not executed in the first-slice runner", t.Kind())
	default:
		return pos, Node{}, fmt.Errorf("unhandled term %T", t)
	}
}

func (m *matcher) matchAlt(terms []grammar.Term, input string, pos int) (int, Node, error) {
	var bestEnd int
	var best Node
	var bestOK bool
	var last error
	for _, t := range terms {
		end, n, err := m.matchTerm(t, input, pos)
		if err != nil {
			last = err
			continue
		}
		if !bestOK || end > bestEnd {
			bestOK = true
			bestEnd = end
			best = n
		}
	}
	if !bestOK {
		if last == nil {
			last = fmt.Errorf("no alternative matched")
		}
		return pos, Node{}, last
	}
	return bestEnd, Node{Kind: "alt", Children: []Node{best}}, nil
}

func (m *matcher) matchSeq(terms []grammar.Term, input string, pos int) (int, Node, error) {
	kids := make([]Node, 0, len(terms))
	cur := pos
	for _, t := range terms {
		end, n, err := m.matchTerm(t, input, cur)
		if err != nil {
			return cur, Node{}, err
		}
		kids = append(kids, n)
		cur = end
	}
	return cur, Node{Kind: "seq", Children: kids, Text: input[pos:cur]}, nil
}

func (m *matcher) matchQuant(q grammar.Quant, input string, pos int) (int, Node, error) {
	var kids []Node
	cur := pos
	n := 0
	max := q.Max
	for max == grammar.Unbounded || n < max {
		end, node, err := m.matchTerm(q.Term, input, cur)
		if err != nil || end < cur {
			break
		}
		if end == cur {
			if q.Max == grammar.Unbounded {
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
	if n < q.Min {
		return pos, Node{}, fmt.Errorf("need %d repetitions, got %d", q.Min, n)
	}
	return cur, Node{Kind: "quant", Children: kids, Text: input[pos:cur]}, nil
}

func (m *matcher) matchDelim(d grammar.Delim, input string, pos int) (int, Node, error) {
	cur := pos
	var kids []Node
	if d.Leading {
		if end, n, err := m.matchTerm(d.Sep, input, cur); err == nil {
			kids = append(kids, n)
			cur = end
		}
	}
	end, n, err := m.matchTerm(d.Term, input, cur)
	if err != nil {
		return pos, Node{}, err
	}
	kids = append(kids, n)
	cur = end
	for {
		save := cur
		se, sn, serr := m.matchTerm(d.Sep, input, cur)
		if serr != nil {
			break
		}
		te, tn, terr := m.matchTerm(d.Term, input, se)
		if terr != nil {
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
	return cur, Node{Kind: "delim", Children: kids, Text: input[pos:cur]}, nil
}

func (m *matcher) matchScope(sc grammar.Scope, input string, pos int) (int, Node, error) {
	savedRules := m.rules
	savedWrap := m.wrap
	savedFirst := m.firstRule
	cp := make(map[string]grammar.Rule, len(m.rules))
	for k, v := range m.rules {
		cp[k] = v
	}
	m.rules = cp
	applyStmts(m, sc.Decls)
	end, n, err := m.matchTerm(sc.Term, input, pos)
	m.rules = savedRules
	m.wrap = savedWrap
	m.firstRule = savedFirst
	return end, n, err
}

func (m *matcher) matchStack(st grammar.Stack, input string, pos int) (int, Node, error) {
	if len(st.Levels) == 0 {
		return pos, Node{}, fmt.Errorf("empty stack")
	}
	var matchLevel func(int, int) (int, Node, error)
	matchLevel = func(i, p int) (int, Node, error) {
		if i == len(st.Levels)-1 {
			saved := m.tighter
			m.tighter = nil
			end, n, err := m.matchTerm(st.Levels[i], input, p)
			m.tighter = saved
			return end, n, err
		}
		saved := m.tighter
		m.tighter = func(in string, tp int) (int, Node, error) {
			return matchLevel(i+1, tp)
		}
		end, n, err := m.matchTerm(st.Levels[i], input, p)
		m.tighter = saved
		return end, n, err
	}
	return matchLevel(0, pos)
}

func hasPrefix(s string, pos int, pre string) bool {
	return pos+len(pre) <= len(s) && s[pos:pos+len(pre)] == pre
}

func classMatch(c grammar.CharClass, r rune) bool {
	ok := false
	for _, e := range c.Elems {
		lo, _ := utf8.DecodeRuneInString(e.Lo)
		if e.Hi == "" {
			if r == lo {
				ok = true
				break
			}
			continue
		}
		hi, _ := utf8.DecodeRuneInString(e.Hi)
		if r >= lo && r <= hi {
			ok = true
			break
		}
	}
	if c.Negated {
		return !ok
	}
	return ok
}

func escapeMatch(code string, r rune) bool {
	if code == "" {
		return false
	}
	switch code {
	case "s":
		return unicode.IsSpace(r)
	case "S":
		return !unicode.IsSpace(r)
	case "d":
		return r >= '0' && r <= '9'
	case "D":
		return r < '0' || r > '9'
	case "w":
		return r == '_' || unicode.IsLetter(r) || unicode.IsDigit(r)
	case "W":
		return !(r == '_' || unicode.IsLetter(r) || unicode.IsDigit(r))
	case "n":
		return r == '\n'
	case "t":
		return r == '\t'
	case "r":
		return r == '\r'
	default:
		ch, _ := utf8.DecodeRuneInString(code)
		return r == ch
	}
}

func leafRegexp(pat string) (*regexp.Regexp, error) {
	re, err := regexp.Compile("\\A(?s:" + pat + ")")
	if err != nil {
		return nil, fmt.Errorf("leaf /%s/: %w", pat, err)
	}
	return re, nil
}
