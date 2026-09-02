// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package fromwbnf

import (
	"fmt"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/marcelocantos/xbnf/grammar"
)

type reParser struct {
	s      string
	i      int
	at     string
	issues *[]Issue
	dotall bool
	fold   bool
}

func (p *reParser) issue(kind, msg string) {
	at := p.at
	if at == "" {
		at = "."
	}
	*p.issues = append(*p.issues, Issue{At: at, Kind: kind, Msg: msg})
}

func (p *reParser) peek() byte {
	if p.i >= len(p.s) {
		return 0
	}
	return p.s[p.i]
}

func (p *reParser) eat(c byte) bool {
	if p.peek() != c {
		return false
	}
	p.i++
	return true
}

func (p *reParser) skip() {}

func (p *reParser) alt() grammar.Term {
	first := p.concat()
	if p.peek() != '|' {
		return first
	}
	terms := []grammar.Term{first}
	for p.eat('|') {
		terms = append(terms, p.concat())
	}
	return grammar.OrderedAlt{Terms: terms}
}

func (p *reParser) concat() grammar.Term {
	var parts []grammar.Term
	var lit strings.Builder
	flush := func() {
		if lit.Len() == 0 {
			return
		}
		parts = append(parts, grammar.String{Text: lit.String()})
		lit.Reset()
	}
	for {
		c := p.peek()
		if c == 0 || c == '|' || c == ')' {
			break
		}
		atom := p.atom()
		if p.fold {
			atom = caseFold(atom)
		}
		q, ok := p.quant()
		if !ok {
			if s, is := atom.(grammar.String); is {
				lit.WriteString(s.Text)
				continue
			}
			flush()
			parts = append(parts, atom)
			continue
		}
		flush()
		parts = append(parts, q(atom))
	}
	flush()
	out := parts[:0]
	for _, t := range parts {
		if _, ok := t.(grammar.Empty); ok {
			continue
		}
		out = append(out, t)
	}
	parts = out
	if len(parts) == 0 {
		return grammar.Empty{}
	}
	if len(parts) == 1 {
		return parts[0]
	}
	return grammar.Seq{Terms: parts}
}

func (p *reParser) atom() grammar.Term {
	c := p.peek()
	switch c {
	case 0:
		p.issue("regex", "expected atom")
		return grammar.Empty{}
	case '.':
		p.i++
		if p.dotall {
			return grammar.AnyChar{}
		}
		return grammar.CharClass{Elems: []grammar.ClassElem{{Lo: "\n"}}, Negated: true}
	case '^':
		p.i++
		// Start of a terminal is implicit.
		return grammar.Empty{}
	case '$':
		p.i++
		// End of line: already encoded by using [^\n] for `.` when not dotall.
		return grammar.Empty{}
	case '[':
		return p.class()
	case '\\':
		return p.escape()
	case '(':
		return p.group()
	case '{':
		p.i++
		return grammar.String{Text: "{"}
	case ']', '}', '?', '*', '+':
		p.i++
		return grammar.String{Text: string(c)}
	default:
		p.i++
		return grammar.String{Text: string(c)}
	}
}

func (p *reParser) group() grammar.Term {
	if !p.eat('(') {
		p.issue("regex", "expected (")
		return grammar.Empty{}
	}
	saveDot, saveFold := p.dotall, p.fold
	if p.peek() == '?' {
		p.i++
		switch p.peek() {
		case '=':
			p.i++
			t := p.alt()
			p.eat(')')
			p.dotall, p.fold = saveDot, saveFold
			return grammar.Lookahead{Term: t}
		case '!':
			p.i++
			t := p.alt()
			p.eat(')')
			p.dotall, p.fold = saveDot, saveFold
			return grammar.NegLookahead{Term: t}
		default:
			scoped, ok := p.groupFlags()
			if !ok {
				p.dotall, p.fold = saveDot, saveFold
				return grammar.Empty{}
			}
			if !scoped {
				return grammar.Empty{}
			}
			t := p.alt()
			if !p.eat(')') {
				p.issue("regex", "expected )")
			}
			p.dotall, p.fold = saveDot, saveFold
			return t
		}
	}
	t := p.alt()
	if !p.eat(')') {
		p.issue("regex", "expected )")
	}
	p.dotall, p.fold = saveDot, saveFold
	return t
}

func (p *reParser) groupFlags() (scoped bool, ok bool) {
	for p.i < len(p.s) {
		switch p.peek() {
		case 'i':
			p.i++
			p.fold = true
		case 's':
			p.i++
			p.dotall = true
		case 'm':
			p.i++
		case ':':
			p.i++
			return true, true
		case ')':
			p.i++
			return false, true
		default:
			p.issue("regex-flag", fmt.Sprintf("unsupported group (?%c…)", p.peek()))
			for p.i < len(p.s) && p.peek() != ')' {
				p.i++
			}
			p.eat(')')
			return false, false
		}
	}
	p.issue("regex-flag", "unterminated (? flags")
	return false, false
}

func (p *reParser) quant() (func(grammar.Term) grammar.Term, bool) {
	switch p.peek() {
	case '*':
		p.i++
		p.lazy()
		return func(t grammar.Term) grammar.Term {
			return grammar.Quant{Term: t, Min: 0, Max: grammar.Unbounded}
		}, true
	case '+':
		p.i++
		p.lazy()
		return func(t grammar.Term) grammar.Term {
			return grammar.Quant{Term: t, Min: 1, Max: grammar.Unbounded}
		}, true
	case '?':
		p.i++
		p.lazy()
		return func(t grammar.Term) grammar.Term {
			return grammar.Quant{Term: t, Min: 0, Max: 1}
		}, true
	case '{':
		return p.braceQuant()
	}
	return nil, false
}

func (p *reParser) lazy() {
	p.eat('?')
}

func (p *reParser) braceQuant() (func(grammar.Term) grammar.Term, bool) {
	save := p.i
	if !p.eat('{') {
		return nil, false
	}
	min, hasMin := p.digits()
	comma := p.eat(',')
	max, hasMax := p.digits()
	if !p.eat('}') || (!hasMin && !comma) {
		p.i = save
		return nil, false
	}
	p.lazy()
	if !comma {
		return func(t grammar.Term) grammar.Term {
			return grammar.Quant{Term: t, Min: min, Max: min}
		}, true
	}
	if !hasMax {
		return func(t grammar.Term) grammar.Term {
			return grammar.Quant{Term: t, Min: min, Max: grammar.Unbounded}
		}, true
	}
	return func(t grammar.Term) grammar.Term {
		return grammar.Quant{Term: t, Min: min, Max: max}
	}, true
}

func (p *reParser) digits() (int, bool) {
	start := p.i
	for p.i < len(p.s) && p.s[p.i] >= '0' && p.s[p.i] <= '9' {
		p.i++
	}
	if p.i == start {
		return 0, false
	}
	n, _ := strconv.Atoi(p.s[start:p.i])
	return n, true
}

func (p *reParser) escape() grammar.Term {
	if !p.eat('\\') {
		p.issue("regex", "expected \\")
		return grammar.Empty{}
	}
	if p.i >= len(p.s) {
		p.issue("regex", "dangling \\")
		return grammar.Empty{}
	}
	c := p.s[p.i]
	p.i++
	switch c {
	case 's', 'S', 'd', 'D', 'w', 'W', 'n', 't', 'r':
		return grammar.Escape{Code: string(c)}
	case 'p', 'P':
		name := p.propName()
		key := strings.Trim(name, "{}")
		if unicodeTable(key) == nil {
			p.issue("unicode-property", fmt.Sprintf(`\%c%s`, c, name))
			return grammar.Empty{}
		}
		return grammar.Escape{Code: string(c) + name}
	case 'x':
		return grammar.String{Text: string(rune(p.hex(2)))}
	case 'u':
		return grammar.String{Text: string(rune(p.hex(4)))}
	case 'Q':
		return p.quoted()
	case 'A', 'z':
		return grammar.Empty{}
	case 'b', 'B':
		p.issue("regex-anchor", fmt.Sprintf(`\%c`, c))
		return grammar.Empty{}
	default:
		return grammar.String{Text: string(c)}
	}
}

func (p *reParser) propName() string {
	if p.eat('{') {
		start := p.i
		for p.i < len(p.s) && p.s[p.i] != '}' {
			p.i++
		}
		name := p.s[start:p.i]
		p.eat('}')
		return "{" + name + "}"
	}
	if p.i < len(p.s) && ((p.s[p.i] >= 'a' && p.s[p.i] <= 'z') || (p.s[p.i] >= 'A' && p.s[p.i] <= 'Z')) {
		p.i++
		return string(p.s[p.i-1])
	}
	return ""
}

func (p *reParser) hex(n int) int {
	if p.i+n > len(p.s) {
		p.issue("regex", "short hex escape")
		return 0
	}
	v, err := strconv.ParseInt(p.s[p.i:p.i+n], 16, 32)
	if err != nil {
		p.issue("regex", "bad hex escape")
	}
	p.i += n
	return int(v)
}

func (p *reParser) class() grammar.Term {
	if !p.eat('[') {
		p.issue("regex", "expected [")
		return grammar.Empty{}
	}
	neg := p.eat('^')
	var elems, inverted []grammar.ClassElem
	first := true
	for p.i < len(p.s) && (p.peek() != ']' || first) {
		first = false
		if p.atPOSIX() {
			el, inv := p.posixElems()
			if inv {
				inverted = append(inverted, el...)
			} else {
				elems = append(elems, el...)
			}
			continue
		}
		if p.atPropEscape() {
			el, inv, ok := p.propClassElems()
			if !ok {
				continue
			}
			if inv {
				inverted = append(inverted, el...)
			} else {
				elems = append(elems, el...)
			}
			continue
		}
		lo := p.classAtom()
		if p.peek() == '-' && p.i+1 < len(p.s) && p.s[p.i+1] != ']' {
			p.i++
			hi := p.classAtom()
			elems = append(elems, grammar.ClassElem{Lo: lo, Hi: hi})
			continue
		}
		elems = append(elems, grammar.ClassElem{Lo: lo})
	}
	if !p.eat(']') {
		p.issue("regex", "expected ]")
	}
	if len(inverted) > 0 {
		if len(elems) > 0 {
			p.issue("posix-class", "[:^class:] or \\P mixed with other class atoms")
			elems = append(elems, inverted...)
		} else if neg {
			return grammar.CharClass{Elems: inverted}
		} else {
			return grammar.CharClass{Negated: true, Elems: inverted}
		}
	}
	return grammar.CharClass{Negated: neg, Elems: elems}
}

func (p *reParser) atPropEscape() bool {
	return p.i+1 < len(p.s) && p.s[p.i] == '\\' && (p.s[p.i+1] == 'p' || p.s[p.i+1] == 'P')
}

func (p *reParser) propClassElems() ([]grammar.ClassElem, bool, bool) {
	if !p.eat('\\') {
		return nil, false, false
	}
	c := p.s[p.i]
	p.i++
	name := p.propName()
	key := strings.Trim(name, "{}")
	tab := unicodeTable(key)
	if tab == nil {
		p.issue("unicode-property", fmt.Sprintf(`\%c%s`, c, name))
		return nil, false, false
	}
	return rangeElems(tab), c == 'P', true
}

func (p *reParser) atPOSIX() bool {
	return p.i+1 < len(p.s) && p.s[p.i] == '[' && p.s[p.i+1] == ':'
}

func (p *reParser) posixElems() ([]grammar.ClassElem, bool) {
	if !p.eat('[') || !p.eat(':') {
		return nil, false
	}
	inv := p.eat('^')
	start := p.i
	for p.i < len(p.s) && p.s[p.i] >= 'a' && p.s[p.i] <= 'z' {
		p.i++
	}
	name := p.s[start:p.i]
	if !p.eat(':') || !p.eat(']') {
		p.issue("posix-class", "malformed [:"+name)
		return nil, false
	}
	elems, ok := posixClass(name)
	if !ok {
		p.issue("posix-class", "[:"+name+":]")
		return nil, false
	}
	return elems, inv
}

func posixClass(name string) ([]grammar.ClassElem, bool) {
	switch name {
	case "digit":
		return []grammar.ClassElem{{Lo: "0", Hi: "9"}}, true
	case "lower":
		return []grammar.ClassElem{{Lo: "a", Hi: "z"}}, true
	case "upper":
		return []grammar.ClassElem{{Lo: "A", Hi: "Z"}}, true
	case "alpha":
		return []grammar.ClassElem{{Lo: "A", Hi: "Z"}, {Lo: "a", Hi: "z"}}, true
	case "alnum":
		return []grammar.ClassElem{{Lo: "0", Hi: "9"}, {Lo: "A", Hi: "Z"}, {Lo: "a", Hi: "z"}}, true
	case "xdigit":
		return []grammar.ClassElem{{Lo: "0", Hi: "9"}, {Lo: "A", Hi: "F"}, {Lo: "a", Hi: "f"}}, true
	case "blank":
		return []grammar.ClassElem{{Lo: " "}, {Lo: "\t"}}, true
	case "space":
		return []grammar.ClassElem{{Lo: " "}, {Lo: "\t"}, {Lo: "\n"}, {Lo: "\r"}, {Lo: "\f"}, {Lo: "\v"}}, true
	case "word":
		return []grammar.ClassElem{{Lo: "0", Hi: "9"}, {Lo: "A", Hi: "Z"}, {Lo: "a", Hi: "z"}, {Lo: "_"}}, true
	default:
		return nil, false
	}
}

func (p *reParser) classAtom() string {
	if p.eat('\\') {
		if p.i >= len(p.s) {
			return `\`
		}
		c := p.s[p.i]
		p.i++
		switch c {
		case 'n':
			return "\n"
		case 't':
			return "\t"
		case 'r':
			return "\r"
		default:
			return string(c)
		}
	}
	if p.i >= len(p.s) {
		return ""
	}
	c := p.s[p.i]
	p.i++
	return string(c)
}

func (p *reParser) quoted() grammar.Term {
	start := p.i
	for p.i < len(p.s) {
		if p.s[p.i] == '\\' && p.i+1 < len(p.s) && p.s[p.i+1] == 'E' {
			lit := p.s[start:p.i]
			p.i += 2
			return grammar.String{Text: lit}
		}
		p.i++
	}
	return grammar.String{Text: p.s[start:]}
}

func unicodeTable(name string) *unicode.RangeTable {
	if tab := unicode.Categories[name]; tab != nil {
		return tab
	}
	if tab := unicode.Scripts[name]; tab != nil {
		return tab
	}
	if tab := unicode.Properties[name]; tab != nil {
		return tab
	}
	return nil
}

func rangeElems(tab *unicode.RangeTable) []grammar.ClassElem {
	var out []grammar.ClassElem
	add := func(lo, hi rune, stride int) {
		if stride <= 1 {
			e := grammar.ClassElem{Lo: string(lo)}
			if hi != lo {
				e.Hi = string(hi)
			}
			out = append(out, e)
			return
		}
		for r := lo; r <= hi; r += rune(stride) {
			out = append(out, grammar.ClassElem{Lo: string(r)})
		}
	}
	for _, r := range tab.R16 {
		add(rune(r.Lo), rune(r.Hi), int(r.Stride))
	}
	for _, r := range tab.R32 {
		add(rune(r.Lo), rune(r.Hi), int(r.Stride))
	}
	return out
}

func caseFold(t grammar.Term) grammar.Term {
	switch x := t.(type) {
	case grammar.String:
		return foldString(x.Text)
	case grammar.CharClass:
		return foldClass(x)
	case grammar.Seq:
		ts := make([]grammar.Term, len(x.Terms))
		for i, u := range x.Terms {
			ts[i] = caseFold(u)
		}
		x.Terms = ts
		return x
	case grammar.Alt:
		ts := make([]grammar.Term, len(x.Terms))
		for i, u := range x.Terms {
			ts[i] = caseFold(u)
		}
		x.Terms = ts
		return x
	case grammar.OrderedAlt:
		ts := make([]grammar.Term, len(x.Terms))
		for i, u := range x.Terms {
			ts[i] = caseFold(u)
		}
		x.Terms = ts
		return x
	case grammar.Quant:
		x.Term = caseFold(x.Term)
		return x
	case grammar.Named:
		x.Term = caseFold(x.Term)
		return x
	case grammar.Lookahead:
		x.Term = caseFold(x.Term)
		return x
	case grammar.NegLookahead:
		x.Term = caseFold(x.Term)
		return x
	default:
		return t
	}
}

func foldString(s string) grammar.Term {
	var parts []grammar.Term
	for _, r := range s {
		fs := foldsOf(r)
		if len(fs) == 1 {
			parts = append(parts, grammar.String{Text: string(r)})
			continue
		}
		el := make([]grammar.ClassElem, len(fs))
		for i, f := range fs {
			el[i] = grammar.ClassElem{Lo: string(f)}
		}
		parts = append(parts, grammar.CharClass{Elems: el})
	}
	if len(parts) == 0 {
		return grammar.Empty{}
	}
	if len(parts) == 1 {
		return parts[0]
	}
	return grammar.Seq{Terms: parts}
}

func foldClass(c grammar.CharClass) grammar.CharClass {
	var elems []grammar.ClassElem
	seen := map[string]bool{}
	add := func(lo, hi string) {
		key := lo + "\x00" + hi
		if seen[key] {
			return
		}
		seen[key] = true
		e := grammar.ClassElem{Lo: lo}
		if hi != "" && hi != lo {
			e.Hi = hi
		}
		elems = append(elems, e)
	}
	for _, e := range c.Elems {
		add(e.Lo, e.Hi)
		r, _ := utf8.DecodeRuneInString(e.Lo)
		for _, f := range foldsOf(r) {
			if f != r {
				add(string(f), "")
			}
		}
		if e.Hi == "" {
			continue
		}
		h, _ := utf8.DecodeRuneInString(e.Hi)
		for _, f := range foldsOf(h) {
			if f != h {
				add(string(f), "")
			}
		}
		if r == 'a' && h == 'z' {
			add("A", "Z")
		}
		if r == 'A' && h == 'Z' {
			add("a", "z")
		}
	}
	c.Elems = elems
	return c
}

func foldsOf(r rune) []rune {
	out := []rune{r}
	for f := unicode.SimpleFold(r); f != r; f = unicode.SimpleFold(f) {
		out = append(out, f)
	}
	return out
}
