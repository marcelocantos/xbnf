// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package fromwbnf

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/marcelocantos/xbnf/grammar"
)

type reParser struct {
	s      string
	i      int
	at     string
	issues *[]Issue
	dotall bool
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
	saveDot := p.dotall
	if p.peek() == '?' {
		p.i++
		switch p.peek() {
		case ':':
			p.i++
		case 's':
			p.i++
			if p.eat(':') {
				p.dotall = true
			} else if p.eat(')') {
				p.dotall = true
				return grammar.Empty{}
			} else {
				p.issue("regex-flag", "unsupported group (?s"+string(p.peek())+")")
			}
		case '=':
			p.i++
			p.issue("regex-lookahead", "(?=…) in a regex terminal")
			t := p.alt()
			p.eat(')')
			p.dotall = saveDot
			return grammar.Lookahead{Term: t}
		case '!':
			p.i++
			p.issue("regex-lookahead", "(?!…) in a regex terminal")
			t := p.alt()
			p.eat(')')
			p.dotall = saveDot
			return grammar.NegLookahead{Term: t}
		default:
			p.issue("regex-flag", fmt.Sprintf("unsupported group (?%c…)", p.peek()))
			for p.i < len(p.s) && p.peek() != ')' {
				p.i++
			}
			p.eat(')')
			p.dotall = saveDot
			return grammar.Empty{}
		}
	}
	t := p.alt()
	if !p.eat(')') {
		p.issue("regex", "expected )")
	}
	p.dotall = saveDot
	return t
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
	if p.eat('?') {
		p.issue("lazy-quant", "reluctant quantifier; emitted as greedy")
	}
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
		p.issue("unicode-property", fmt.Sprintf(`\%c%s`, c, name))
		return grammar.Escape{Code: string(c)}
	case 'x':
		return grammar.String{Text: string(rune(p.hex(2)))}
	case 'u':
		return grammar.String{Text: string(rune(p.hex(4)))}
	case 'Q':
		p.issue("regex", `\Q…\E is not an xbnf construct`)
		return grammar.Empty{}
	case 'A', 'z', 'b', 'B':
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
	var elems []grammar.ClassElem
	first := true
	for p.i < len(p.s) && (p.peek() != ']' || first) {
		first = false
		if p.atPOSIX() {
			p.issue("posix-class", p.s[p.i:min(p.i+8, len(p.s))])
			for p.i < len(p.s) && p.peek() != ']' {
				p.i++
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
	return grammar.CharClass{Negated: neg, Elems: elems}
}

func (p *reParser) atPOSIX() bool {
	return p.i+1 < len(p.s) && p.s[p.i] == '[' && p.s[p.i+1] == ':'
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
