// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package engine

import (
	"unicode/utf8"

	"github.com/marcelocantos/xbnf/grammar"
)

// firstInfo is the FIRST set of the remainder of a production: the characters
// that can begin it, and whether it can match the empty string.
//
// GLL uses it as the test before enqueueing a descriptor. A descriptor whose
// remaining right-hand side cannot start with the next input character will
// fail on its first terminal, so it is never created. This is the test-set
// pruning from Scott & Johnstone's GLL; without it the parser explores every
// alternative of every nonterminal at every position it reaches.
type firstInfo struct {
	nullable bool
	atoms    []firstAtom
}

// firstAtom is one member of a FIRST set: a character-level terminal, or a
// regular rule whose DFA decides whether a character can begin it.
type firstAtom struct {
	term grammar.Term // String, CharClass, Escape, or AnyChar
	name string       // rule name when dfa is set
	dfa  *dfa
}

func (a firstAtom) starts(r rune) bool {
	if a.dfa != nil {
		return a.dfa.canStart(r)
	}
	switch x := a.term.(type) {
	case grammar.String:
		first, _ := utf8.DecodeRuneInString(x.Text)
		return r == first
	case grammar.CharClass:
		return classMatch(x, r)
	case grammar.Escape:
		return escapeMatch(x.Code, r)
	case grammar.AnyChar:
		return true
	}
	return true
}

func (a firstAtom) describe() string {
	if a.dfa != nil {
		return displayNT(a.name)
	}
	return describeTerm(a.term)
}

func (a firstAtom) eq(b firstAtom) bool {
	if a.dfa != nil || b.dfa != nil {
		return a.dfa == b.dfa
	}
	switch x := a.term.(type) {
	case grammar.String, grammar.Escape, grammar.AnyChar:
		return a.term == b.term
	case grammar.CharClass:
		y, ok := b.term.(grammar.CharClass)
		return ok && classText(x) == classText(y)
	}
	return false
}

// admits reports whether r can begin the remainder. A nullable remainder
// admits everything: the decision belongs to whatever follows.
func (f firstInfo) admits(r rune) bool {
	if f.nullable {
		return true
	}
	for _, a := range f.atoms {
		if a.starts(r) {
			return true
		}
	}
	return false
}

// union merges o into f and reports whether f grew.
func (f *firstInfo) union(o firstInfo) bool {
	grew := false
	if o.nullable && !f.nullable {
		f.nullable = true
		grew = true
	}
outer:
	for _, a := range o.atoms {
		for _, b := range f.atoms {
			if a.eq(b) {
				continue outer
			}
		}
		f.atoms = append(f.atoms, a)
		grew = true
	}
	return grew
}

// elemFirst is the FIRST set of one right-hand-side element.
func (c *Compiled) elemFirst(e elem, nt map[string]firstInfo) firstInfo {
	switch e.kind {
	case ekTerm:
		switch x := e.term.(type) {
		case grammar.Empty:
			return firstInfo{nullable: true}
		case grammar.String:
			if x.Text == "" {
				return firstInfo{nullable: true}
			}
		}
		return firstInfo{atoms: []firstAtom{{term: e.term}}}
	case ekDFA, ekNT:
		if d := c.dfa[e.nt]; d != nil {
			return firstInfo{nullable: d.nullable(), atoms: []firstAtom{{name: e.nt, dfa: d}}}
		}
		if e.kind == ekDFA {
			// Missing DFA: never prune, the runtime reports the gap.
			return firstInfo{nullable: true}
		}
		return nt[e.nt]
	default:
		// Lookaheads consume nothing.
		return firstInfo{nullable: true}
	}
}

// seqFirst is the FIRST set of a sequence of elements.
func (c *Compiled) seqFirst(rhs []elem, nt map[string]firstInfo) firstInfo {
	var out firstInfo
	for _, e := range rhs {
		f := c.elemFirst(e, nt)
		out.union(firstInfo{atoms: f.atoms})
		if !f.nullable {
			return out
		}
	}
	out.nullable = true
	return out
}

// computeFirst fills slotFirst with the FIRST set of every slot (pid, ip),
// where ip == len(rhs) is the completed production.
func (c *Compiled) computeFirst() {
	nt := map[string]firstInfo{}
	for changed := true; changed; {
		changed = false
		for _, pr := range c.prods {
			f := c.seqFirst(pr.rhs, nt)
			cur := nt[pr.nt]
			if cur.union(f) {
				nt[pr.nt] = cur
				changed = true
			}
		}
	}
	c.slotFirst = make([][]firstInfo, len(c.prods))
	for i, pr := range c.prods {
		c.slotFirst[i] = make([]firstInfo, len(pr.rhs)+1)
		for ip := range c.slotFirst[i] {
			c.slotFirst[i][ip] = c.seqFirst(pr.rhs[ip:], nt)
		}
	}
}
