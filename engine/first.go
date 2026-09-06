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
//
// The hot fields come first and the atoms slice last: slotFirst is one flat
// array, and admits reads only ascii/nullable/asciiOK on the path that
// matters, so keeping those adjacent lets one load reach them all.
type firstInfo struct {
	// ascii is a bitset of bytes 0–255 that can begin this remainder, filled
	// when every FIRST atom is an ASCII-start terminal or DFA. admits then
	// does not walk atoms or DFAs on the hot path.
	ascii    [asciiWords]uint64
	nullable bool
	asciiOK  bool
	atoms    []firstAtom
}

// The ascii bitset is 256 bits held in 64-bit words, so a byte splits into a
// word index and a bit index.
const (
	asciiBitsetSize = 256
	asciiWords      = asciiBitsetSize / 64
	asciiWordShift  = 6
	asciiWordMask   = 63
)

// admitsASCII reports whether byte b is in the ASCII bitset. It is only
// meaningful when asciiOK; otherwise the bitset was never filled.
func (f *firstInfo) admitsASCII(b byte) bool {
	return f.ascii[b>>asciiWordShift]&(1<<(b&asciiWordMask)) != 0
}

// nonePID / manyPID are bytePred.byByte sentinels.
const (
	nonePID int32 = -1
	manyPID int32 = -2
)

// bytePred maps the first input byte to a unique production of one
// nonterminal, when the alternatives are non-nullable and FIRST-disjoint
// on ASCII. Built at compile time; used to avoid forking GLL at runtime.
type bytePred struct {
	byByte [256]int32
}

// firstAtom is one member of a FIRST set: a character-level terminal, or a
// regular rule whose DFA decides whether a character can begin it.
type firstAtom struct {
	term grammar.Term // String, CharClass, Escape, or AnyChar
	name string       // rule name when dfa is set
	desc string       // interned by computeFirst
	dfa  *dfa
}

func (a firstAtom) starts(r rune) bool {
	if a.dfa != nil {
		return a.dfa.canStart(r)
	}
	switch x := a.term.(type) {
	case grammar.String:
		first, _ := utf8.DecodeRuneInString(x.Text)
		if x.Fold {
			return asciiFoldEq(r, first)
		}
		return r == first
	case grammar.CharClass:
		if x.Fold {
			return classMatchFold(x, r)
		}
		return classMatch(x, r)
	case grammar.Escape:
		return escapeMatch(x.Code, r)
	case grammar.AnyChar:
		return true
	}
	return true
}

func (a firstAtom) describe() string {
	if a.desc != "" {
		return a.desc
	}
	if a.dfa != nil {
		return displayNT(a.name)
	}
	return describeTerm(a.term)
}

func (a *firstAtom) internDesc() {
	if a.dfa != nil {
		a.desc = displayNT(a.name)
		return
	}
	a.desc = describeTerm(a.term)
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
func (f *firstInfo) admits(r rune) bool {
	if f.nullable {
		return true
	}
	if f.asciiOK {
		if r < 0 || r > maxASCIIBitsetRune {
			return false
		}
		return f.admitsASCII(byte(r))
	}
	for _, a := range f.atoms {
		if a.starts(r) {
			return true
		}
	}
	return false
}

// maxASCIIBitsetRune is the largest rune the ascii bitset can represent.
const maxASCIIBitsetRune = 255

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
		case grammar.Empty, grammar.PosProp:
			return firstInfo{nullable: true}
		case grammar.Ref:
			// Bound text consumes input. Treating it as ε leaks FIRST of
			// what follows (e.g. ">" after "</" %n ">").
			return firstInfo{atoms: []firstAtom{{term: grammar.AnyChar{}}}}
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
	slots := 0
	for i := range c.prods {
		c.prods[i].firstBase = int32(slots)
		slots += len(c.prods[i].rhs) + 1
	}
	c.slotFirst = make([]firstInfo, slots)
	for _, pr := range c.prods {
		for ip := 0; ip <= len(pr.rhs); ip++ {
			f := c.seqFirst(pr.rhs[ip:], nt)
			f.buildASCII()
			c.slotFirst[int(pr.firstBase)+ip] = f
		}
	}
	for i := range c.slotFirst {
		atoms := c.slotFirst[i].atoms
		for j := range atoms {
			atoms[j].internDesc()
		}
	}
	c.computeBytePred()
}

func (f *firstInfo) buildASCII() {
	if f.nullable {
		return
	}
	var seen [asciiBitsetSize]bool
	if !fillASCII(f, &seen) {
		return
	}
	f.asciiOK = true
	for b := range seen {
		if seen[b] {
			f.ascii[b>>asciiWordShift] |= 1 << (uint(b) & asciiWordMask)
		}
	}
}

func fillASCII(f *firstInfo, seen *[asciiBitsetSize]bool) bool {
	for _, a := range f.atoms {
		if atomBeyondASCII(a) {
			return false
		}
		if a.dfa != nil {
			for b := 0; b < asciiBitsetSize; b++ {
				if a.dfa.canStart(rune(b)) {
					seen[b] = true
				}
			}
			continue
		}
		switch x := a.term.(type) {
		case grammar.String:
			if x.Text == "" {
				return false
			}
			r, size := utf8.DecodeRuneInString(x.Text)
			if size != 1 || r > 255 {
				return false
			}
			seen[byte(r)] = true
			if x.Fold {
				if r >= 'A' && r <= 'Z' {
					seen[byte(r+32)] = true
				}
				if r >= 'a' && r <= 'z' {
					seen[byte(r-32)] = true
				}
			}
		case grammar.CharClass:
			for b := 0; b < asciiBitsetSize; b++ {
				ok := classMatch(x, rune(b))
				if !ok && x.Fold {
					ok = classMatchFold(x, rune(b))
				}
				if ok {
					seen[b] = true
				}
			}
		case grammar.Escape:
			for b := 0; b < asciiBitsetSize; b++ {
				if escapeMatch(x.Code, rune(b)) {
					seen[b] = true
				}
			}
		default:
			return false
		}
	}
	return true
}

// atomBeyondASCII reports whether a can begin with a rune > 255. The ASCII
// bitset in admits must not be used in that case: it would reject those runes.
func atomBeyondASCII(a firstAtom) bool {
	if a.dfa != nil {
		return dfaBeyondASCII(a.dfa)
	}
	switch x := a.term.(type) {
	case grammar.AnyChar:
		return true
	case grammar.String:
		r, _ := utf8.DecodeRuneInString(x.Text)
		return r > 255
	case grammar.Escape:
		return escapeBeyondASCII(x.Code)
	case grammar.CharClass:
		return classBeyondASCII(x)
	default:
		return true
	}
}

func escapeBeyondASCII(code string) bool {
	switch code {
	case "d", "n", "t", "r":
		return false
	case "s", "S", "w", "W", "D":
		return true
	}
	if _, _, ok := unicodeEscape(code); ok {
		return true
	}
	ch, _ := utf8.DecodeRuneInString(code)
	return ch > 255
}

func classBeyondASCII(c grammar.CharClass) bool {
	if c.Negated {
		return true
	}
	for _, e := range c.Elems {
		lo, _ := utf8.DecodeRuneInString(e.Lo)
		if lo > 255 {
			return true
		}
		if e.Hi != "" {
			hi, _ := utf8.DecodeRuneInString(e.Hi)
			if hi > 255 {
				return true
			}
		}
	}
	return false
}

func dfaBeyondASCII(d *dfa) bool {
	if d == nil {
		return false
	}
	for _, o := range d.ordered {
		if dfaBeyondASCII(o) {
			return true
		}
	}
	for _, a := range d.alts {
		if dfaBeyondASCII(a.d) {
			return true
		}
	}
	if d.nfa == nil {
		return false
	}
	// FIRST only: epsilon-closure of the start set, not later body states
	// (JSON STR's [^"\\] is unicode but does not begin the rule).
	for _, si := range d.start {
		if si < 0 || si >= len(d.nfa.states) {
			continue
		}
		st := d.nfa.states[si]
		for _, tr := range st.trans {
			if tr.call != "" {
				if d.owner != nil {
					if cd := d.owner.dfa[tr.call]; cd != nil {
						if dfaBeyondASCII(cd) {
							return true
						}
						continue
					}
				}
				return true
			}
			if tr.beyondASCII {
				return true
			}
		}
	}
	return false
}

func termBeyondASCII(t grammar.Term) bool {
	switch x := t.(type) {
	case grammar.AnyChar:
		return true
	case grammar.Escape:
		return escapeBeyondASCII(x.Code)
	case grammar.CharClass:
		return classBeyondASCII(x)
	case grammar.String:
		r, _ := utf8.DecodeRuneInString(x.Text)
		return r > 255
	default:
		return false
	}
}

func (c *Compiled) computeBytePred() {
	c.pred = map[string]*bytePred{}
	for nt, pids := range c.ntProds {
		if len(pids) < 2 {
			continue
		}
		bp := &bytePred{}
		for i := range bp.byByte {
			bp.byByte[i] = nonePID
		}
		ok := true
		for _, pid := range pids {
			f := &c.slotFirst[c.prods[pid].firstBase]
			if f.nullable {
				ok = false
				break
			}
			var seen [asciiBitsetSize]bool
			if !fillASCII(f, &seen) {
				ok = false
				break
			}
			for b := 0; b < asciiBitsetSize; b++ {
				if !seen[b] {
					continue
				}
				switch bp.byByte[b] {
				case nonePID:
					bp.byByte[b] = int32(pid)
				case int32(pid):
				default:
					bp.byByte[b] = manyPID
				}
			}
		}
		if ok {
			c.pred[nt] = bp
		}
	}
}
