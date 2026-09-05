// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

// Package syntax is the bootstrap parser: xbnf source → grammar.Grammar.
package syntax

import (
	"fmt"
	"unicode"
	"unicode/utf8"

	"github.com/marcelocantos/xbnf/grammar"
)

// Parse converts xbnf source into IR. Comments are skipped, not stored.
func Parse(src []byte) (*grammar.Grammar, error) {
	p := &parser{src: src}
	p.skip()
	var stmts []grammar.Stmt
	for p.pos < len(p.src) {
		st, err := p.parseStmt()
		if err != nil {
			return nil, err
		}
		stmts = append(stmts, st)
		p.skip()
	}
	if len(stmts) == 0 {
		return nil, p.err("empty grammar")
	}
	return &grammar.Grammar{Stmts: stmts}, nil
}

type parser struct {
	src    []byte
	pos    int
	inLeaf int
}

func (p *parser) err(msg string) error {
	line, col := 1, 1
	for i := 0; i < p.pos && i < len(p.src); i++ {
		if p.src[i] == '\n' {
			line++
			col = 1
		} else {
			col++
		}
	}
	return fmt.Errorf("xbnf:%d:%d: %s", line, col, msg)
}

func (p *parser) peek() byte {
	if p.pos >= len(p.src) {
		return 0
	}
	return p.src[p.pos]
}

func (p *parser) at(s string) bool {
	return p.pos+len(s) <= len(p.src) && string(p.src[p.pos:p.pos+len(s)]) == s
}

func (p *parser) eat(s string) bool {
	if !p.at(s) {
		return false
	}
	p.pos += len(s)
	return true
}

func (p *parser) skip() {
	for p.pos < len(p.src) {
		switch p.src[p.pos] {
		case ' ', '\t', '\n', '\r':
			p.pos++
		default:
			if p.at("//") {
				p.pos += 2
				for p.pos < len(p.src) && p.src[p.pos] != '\n' {
					p.pos++
				}
				continue
			}
			if p.at("/*") {
				p.pos += 2
				for p.pos+1 < len(p.src) && !(p.src[p.pos] == '*' && p.src[p.pos+1] == '/') {
					p.pos++
				}
				if p.pos+1 < len(p.src) {
					p.pos += 2
				}
				continue
			}
			return
		}
	}
}

func (p *parser) parseStmt() (grammar.Stmt, error) {
	p.skip()
	if p.peek() == '#' {
		return p.parsePragma()
	}
	return p.parseRule()
}

func (p *parser) parseRule() (grammar.Stmt, error) {
	name, ok := p.ident()
	if !ok {
		return nil, p.err("expected rule name")
	}
	var mods []string
	p.skip()
	for p.peek() == '#' {
		start := p.pos
		p.pos++
		mod, ok := p.ident()
		if !ok {
			p.pos = start
			break
		}
		if mod == "leaf" {
			return nil, p.err("#leaf is /term/; wrap the body in slashes")
		}
		mods = append(mods, mod)
		p.skip()
	}
	p.skip()
	if !p.eat("->") {
		return nil, p.err("expected ->")
	}
	body, err := p.parseTerm()
	if err != nil {
		return nil, err
	}
	p.skip()
	if !p.eat(";") {
		return nil, p.err("expected ; after rule")
	}
	return grammar.Rule{Name: name, Mods: mods, Body: body}, nil
}

func (p *parser) parsePragma() (grammar.Stmt, error) {
	if !p.eat("#") {
		return nil, p.err("expected #")
	}
	name, ok := p.ident()
	if !ok {
		return nil, p.err("expected pragma name")
	}
	p.skip()
	switch name {
	case "import":
		path, err := p.parseString()
		if err != nil {
			return nil, err
		}
		p.skip()
		p.eat(";")
		return grammar.Import{Path: path}, nil
	case "macro":
		mac, ok := p.ident()
		if !ok {
			return nil, p.err("expected macro name")
		}
		p.skip()
		if !p.eat("(") {
			return nil, p.err("expected (")
		}
		var params []string
		p.skip()
		if p.peek() != ')' {
			for {
				id, ok := p.ident()
				if !ok {
					return nil, p.err("expected parameter")
				}
				params = append(params, id)
				p.skip()
				if !p.eat(",") {
					break
				}
				p.skip()
			}
		}
		p.skip()
		if !p.eat(")") {
			return nil, p.err("expected )")
		}
		p.skip()
		if !p.eat("{") {
			return nil, p.err("expected {")
		}
		body, err := p.parseTerm()
		if err != nil {
			return nil, err
		}
		p.skip()
		if !p.eat("}") {
			return nil, p.err("expected }")
		}
		p.skip()
		p.eat(";")
		return grammar.Macro{Name: mac, Params: params, Body: body}, nil
	default:
		if !p.eat("->") {
			return nil, p.err("expected -> after #" + name)
		}
		body, err := p.parseTerm()
		if err != nil {
			return nil, err
		}
		p.skip()
		if !p.eat(";") {
			return nil, p.err("expected ; after pragma")
		}
		if name != "wrap" {
			return nil, p.err("unknown pragma #" + name)
		}
		return grammar.Wrap{Body: body}, nil
	}
}

func (p *parser) parseTerm() (grammar.Term, error) {
	return p.parseStack()
}

func (p *parser) parseStack() (grammar.Term, error) {
	left, err := p.parseAlt()
	if err != nil {
		return nil, err
	}
	p.skip()
	if p.peek() != '>' {
		return left, nil
	}
	levels := []grammar.Term{left}
	for {
		p.skip()
		if p.peek() != '>' {
			break
		}
		p.pos++
		t, err := p.parseAlt()
		if err != nil {
			return nil, err
		}
		levels = append(levels, t)
	}
	return grammar.Stack{Levels: levels}, nil
}

func (p *parser) parseAlt() (grammar.Term, error) {
	left, err := p.parseSeq()
	if err != nil {
		return nil, err
	}
	p.skip()
	if p.at("|>") {
		return p.parseAltRest(left, true)
	}
	if p.peek() == '|' {
		return p.parseAltRest(left, false)
	}
	return left, nil
}

func (p *parser) parseAltRest(left grammar.Term, ordered bool) (grammar.Term, error) {
	terms := []grammar.Term{left}
	for {
		p.skip()
		nextOrd := p.at("|>")
		nextUn := p.peek() == '|' && !nextOrd
		if !nextOrd && !nextUn {
			break
		}
		if nextOrd != ordered {
			return nil, p.err("cannot mix | and |> in one alternation; parenthesize")
		}
		if ordered {
			p.pos += 2
		} else {
			p.pos++
		}
		t, err := p.parseSeq()
		if err != nil {
			return nil, err
		}
		terms = append(terms, t)
	}
	if ordered {
		return grammar.OrderedAlt{Terms: terms}, nil
	}
	return grammar.Alt{Terms: terms}, nil
}

func (p *parser) parseSeq() (grammar.Term, error) {
	first, err := p.parseNamedQuants()
	if err != nil {
		return nil, err
	}
	terms := []grammar.Term{first}
	for {
		p.skip()
		if p.seqEnds() {
			break
		}
		t, err := p.parseNamedQuants()
		if err != nil {
			return nil, err
		}
		terms = append(terms, t)
	}
	var dirs []grammar.Directive
	for {
		p.skip()
		if p.peek() != '#' {
			break
		}
		start := p.pos
		p.pos++
		name, ok := p.ident()
		if !ok {
			p.pos = start
			break
		}
		p.skip()
		if p.at("->") {
			p.pos = start
			break
		}
		d := grammar.Directive{Name: name}
		if p.eat("=") {
			p.skip()
			if v, ok := p.ident(); ok {
				d.Value = v
			} else if v, ok := p.intLit(); ok {
				d.Value = v
			} else {
				return nil, p.err("expected directive value")
			}
		}
		dirs = append(dirs, d)
	}
	if len(terms) == 1 && len(dirs) == 0 {
		return first, nil
	}
	return grammar.Seq{Terms: terms, Directives: dirs}, nil
}

func (p *parser) seqEnds() bool {
	if p.pos >= len(p.src) {
		return true
	}
	c := p.src[p.pos]
	switch c {
	case '|', '>', ';', '}', ')', ',':
		return true
	case '#':
		return true
	case '/':
		return p.inLeaf > 0
	}
	return false
}

func (p *parser) parseNamedQuants() (grammar.Term, error) {
	term, err := p.parseNamed()
	if err != nil {
		return nil, err
	}
	for {
		q, ok, err := p.parseQuant(term)
		if err != nil {
			return nil, err
		}
		if !ok {
			break
		}
		term = q
	}
	return term, nil
}

func (p *parser) parseNamed() (grammar.Term, error) {
	p.skip()
	var name string
	save := p.pos
	if id, ok := p.ident(); ok {
		p.skip()
		if p.eat("=") {
			name = id
			p.skip()
		} else {
			p.pos = save
		}
	}
	atom, err := p.parseAtom()
	if err != nil {
		return nil, err
	}
	if name != "" {
		return grammar.Named{Name: name, Term: atom}, nil
	}
	return atom, nil
}

func (p *parser) parseQuant(inner grammar.Term) (grammar.Term, bool, error) {
	p.skip()
	switch p.peek() {
	case '?':
		p.pos++
		return grammar.Quant{Term: inner, Min: 0, Max: 1}, true, nil
	case '*':
		p.pos++
		return grammar.Quant{Term: inner, Min: 0, Max: grammar.Unbounded}, true, nil
	case '+':
		p.pos++
		return grammar.Quant{Term: inner, Min: 1, Max: grammar.Unbounded}, true, nil
	case '{':
		// Scope is an atom, not a quant, if this `{` starts a scope in parseAtom.
		// Here we are after an atom, so `{` is `{n}` or `{m,n}`.
		p.pos++
		p.skip()
		min := 0
		max := grammar.Unbounded
		exact := true
		if n, ok := p.intLit(); ok {
			min = atoi(n)
			max = min
		}
		p.skip()
		if p.eat(",") {
			exact = false
			p.skip()
			if n, ok := p.intLit(); ok {
				max = atoi(n)
			} else {
				max = grammar.Unbounded
			}
			if p.peek() == '}' && p.pos > 0 {
				// `{,3}` min stays 0
			}
		}
		p.skip()
		if !p.eat("}") {
			return nil, false, p.err("expected }")
		}
		if exact {
			return grammar.Quant{Term: inner, Min: min, Max: max}, true, nil
		}
		return grammar.Quant{Term: inner, Min: min, Max: max}, true, nil
	case ':':
		if p.at("::") {
			return nil, false, nil
		}
		p.pos++
		p.skip()
		leading := p.eat(",")
		p.skip()
		sep, err := p.parseNamed()
		if err != nil {
			return nil, false, err
		}
		p.skip()
		trailing := p.eat(",")
		return grammar.Delim{Term: inner, Sep: sep, Leading: leading, Trailing: trailing}, true, nil
	}
	return nil, false, nil
}

func (p *parser) parseAtom() (grammar.Term, error) {
	p.skip()
	if p.pos >= len(p.src) {
		return nil, p.err("expected atom")
	}
	switch {
	case p.at("(?"):
		on, off := p.at("(?i:"), p.at("(?~i:")
		neg := p.at("(?!")
		pos := p.at("(?=")
		switch {
		case off:
			p.pos += 5
		case on:
			p.pos += 4
		case neg:
			p.pos += 3
		case pos:
			p.pos += 3
		default:
			return nil, p.err("unknown (? flag")
		}
		t, err := p.parseTerm()
		if err != nil {
			return nil, err
		}
		p.skip()
		if !p.eat(")") {
			return nil, p.err("expected )")
		}
		switch {
		case off:
			return grammar.CaseFold{On: false, Term: t}, nil
		case on:
			return grammar.CaseFold{On: true, Term: t}, nil
		case neg:
			return grammar.NegLookahead{Term: t}, nil
		default:
			return grammar.Lookahead{Term: t}, nil
		}
	case p.peek() == '(':
		return p.parseGroup()
	case p.peek() == '{':
		return p.parseScope()
	case p.peek() == '"' || p.peek() == '\'' || p.peek() == '`':
		s, err := p.parseString()
		if err != nil {
			return nil, err
		}
		return grammar.String{Text: s}, nil
	case p.peek() == '[':
		return p.parseClass()
	case p.peek() == '\\':
		p.pos++
		if p.pos >= len(p.src) {
			return nil, p.err("expected escape")
		}
		r, n := utf8.DecodeRune(p.src[p.pos:])
		p.pos += n
		code := string(r)
		if (r == 'p' || r == 'P') && p.peek() == '{' {
			p.pos++
			start := p.pos
			for p.pos < len(p.src) {
				rr, nn := utf8.DecodeRune(p.src[p.pos:])
				if rr != '_' && !unicode.IsLetter(rr) && !unicode.IsDigit(rr) {
					break
				}
				p.pos += nn
			}
			if p.peek() != '}' {
				return nil, p.err("expected } after \\p{")
			}
			name := string(p.src[start:p.pos])
			p.pos++
			code = code + "{" + name + "}"
		}
		return grammar.Escape{Code: code}, nil
	case p.peek() == '/':
		return p.parseLeaf()
	case p.peek() == '.':
		p.pos++
		return grammar.AnyChar{}, nil
	case p.at("%!"):
		return p.parseMacroCall()
	case p.at("%%"):
		p.pos += 2
		name, ok := p.ident()
		if !ok {
			return nil, p.err("expected extref name")
		}
		return grammar.ExtRef{Name: name}, nil
	case p.peek() == '%':
		p.pos++
		name, ok := p.ident()
		if !ok {
			return nil, p.err("expected ref name")
		}
		def := ""
		has := false
		p.skip()
		if p.eat("=") {
			p.skip()
			s, err := p.parseString()
			if err != nil {
				return nil, err
			}
			def = s
			has = true
		}
		return grammar.Ref{Name: name, Default: def, HasDefault: has}, nil
	case p.peek() == '@':
		return p.parseAt()
	}
	id, ok := p.ident()
	if !ok {
		return nil, p.err("expected atom")
	}
	label := ""
	if p.at("::") {
		p.pos += 2
		lab, ok := p.ident()
		if !ok {
			return nil, p.err("expected label")
		}
		label = lab
	}
	return grammar.Ident{Name: id, Label: label}, nil
}

func (p *parser) parseGroup() (grammar.Term, error) {
	if !p.eat("(") {
		return nil, p.err("expected (")
	}
	p.skip()
	if p.eat(")") {
		return grammar.Empty{}, nil
	}
	t, err := p.parseTerm()
	if err != nil {
		return nil, err
	}
	p.skip()
	if !p.eat(")") {
		return nil, p.err("expected )")
	}
	return t, nil
}

func (p *parser) parseScope() (grammar.Term, error) {
	if !p.eat("{") {
		return nil, p.err("expected {")
	}
	var decls []grammar.Stmt
	var body grammar.Term
	for {
		p.skip()
		if p.peek() == '}' {
			break
		}
		if p.peek() == '#' {
			st, err := p.parsePragma()
			if err != nil {
				return nil, err
			}
			decls = append(decls, st)
			continue
		}
		if p.looksLikeRule() {
			st, err := p.parseRule()
			if err != nil {
				return nil, err
			}
			decls = append(decls, st)
			continue
		}
		if body != nil {
			return nil, p.err("scope has more than one body")
		}
		t, err := p.parseTerm()
		if err != nil {
			return nil, err
		}
		body = t
		p.skip()
		p.eat(";")
	}
	if !p.eat("}") {
		return nil, p.err("expected }")
	}
	if body == nil {
		body = grammar.Empty{}
	}
	return grammar.Scope{Decls: decls, Term: body}, nil
}

func (p *parser) looksLikeRule() bool {
	save := p.pos
	defer func() { p.pos = save }()
	p.skip()
	if _, ok := p.ident(); !ok {
		return false
	}
	p.skip()
	for p.peek() == '#' {
		p.pos++
		if _, ok := p.ident(); !ok {
			return false
		}
		p.skip()
	}
	return p.at("->")
}

func (p *parser) parseAt() (grammar.Term, error) {
	p.pos++ // @
	if id, ok := p.ident(); ok {
		op, arg := "", ""
		p.skip()
		if p.eat("(") {
			p.skip()
			switch {
			case p.eat("<="):
				op = "<="
			case p.eat(">="):
				op = ">="
			case p.eat("!="):
				op = "!="
			case p.eat("="):
				op = "="
			case p.eat("<"):
				op = "<"
			case p.eat(">"):
				op = ">"
			default:
				return nil, p.err("expected comparison")
			}
			p.skip()
			if v, ok := p.ident(); ok {
				arg = v
			} else if v, ok := p.intLit(); ok {
				arg = v
			} else {
				return nil, p.err("expected comparison argument")
			}
			p.skip()
			if !p.eat(")") {
				return nil, p.err("expected )")
			}
		}
		return grammar.PosProp{Name: id, Op: op, Arg: arg}, nil
	}
	return grammar.Self{}, nil
}

func (p *parser) parseMacroCall() (grammar.Term, error) {
	p.pos += 2
	name, ok := p.ident()
	if !ok {
		return nil, p.err("expected macro name")
	}
	p.skip()
	if !p.eat("(") {
		return nil, p.err("expected (")
	}
	var args []grammar.Term
	p.skip()
	if p.peek() != ')' {
		for {
			t, err := p.parseTerm()
			if err != nil {
				return nil, err
			}
			args = append(args, t)
			p.skip()
			if !p.eat(",") {
				break
			}
			p.skip()
		}
	}
	p.skip()
	if !p.eat(")") {
		return nil, p.err("expected )")
	}
	return grammar.MacroCall{Name: name, Args: args}, nil
}

func (p *parser) parseLeaf() (grammar.Term, error) {
	p.pos++ // opening /
	p.inLeaf++
	defer func() { p.inLeaf-- }()
	p.skip()
	t, err := p.parseTerm()
	if err != nil {
		return nil, err
	}
	p.skip()
	if !p.eat("/") {
		return nil, p.err("expected /")
	}
	return grammar.Leaf{Term: t}, nil
}

func (p *parser) parseClass() (grammar.Term, error) {
	p.pos++ // [
	neg := p.eat("^")
	var elems []grammar.ClassElem
	for p.pos < len(p.src) && p.peek() != ']' {
		lo, err := p.classAtom()
		if err != nil {
			return nil, err
		}
		if p.peek() == '-' && p.pos+1 < len(p.src) && p.src[p.pos+1] != ']' {
			p.pos++
			hi, err := p.classAtom()
			if err != nil {
				return nil, err
			}
			elems = append(elems, grammar.ClassElem{Lo: lo, Hi: hi})
			continue
		}
		elems = append(elems, grammar.ClassElem{Lo: lo})
	}
	if !p.eat("]") {
		return nil, p.err("unterminated character class")
	}
	return grammar.CharClass{Negated: neg, Elems: elems}, nil
}

func (p *parser) classAtom() (string, error) {
	if p.pos >= len(p.src) {
		return "", p.err("expected class atom")
	}
	if p.src[p.pos] == '\\' && p.pos+1 < len(p.src) {
		p.pos++
		r, n := utf8.DecodeRune(p.src[p.pos:])
		p.pos += n
		switch r {
		case 'n':
			return "\n", nil
		case 't':
			return "\t", nil
		case 'r':
			return "\r", nil
		default:
			return string(r), nil
		}
	}
	r, n := utf8.DecodeRune(p.src[p.pos:])
	p.pos += n
	return string(r), nil
}

func (p *parser) parseString() (string, error) {
	if p.pos >= len(p.src) {
		return "", p.err("expected string")
	}
	q := p.src[p.pos]
	if q != '"' && q != '\'' && q != '`' {
		return "", p.err("expected string")
	}
	p.pos++
	var b []byte
	if q == '`' {
		for p.pos < len(p.src) {
			if p.at("``") {
				b = append(b, '`')
				p.pos += 2
				continue
			}
			if p.src[p.pos] == '`' {
				p.pos++
				return string(b), nil
			}
			b = append(b, p.src[p.pos])
			p.pos++
		}
		return "", p.err("unterminated string")
	}
	for p.pos < len(p.src) {
		c := p.src[p.pos]
		if c == q {
			p.pos++
			return string(b), nil
		}
		if c == '\\' && p.pos+1 < len(p.src) {
			p.pos++
			esc := p.src[p.pos]
			p.pos++
			switch esc {
			case 'n':
				b = append(b, '\n')
			case 't':
				b = append(b, '\t')
			case 'r':
				b = append(b, '\r')
			default:
				b = append(b, esc)
			}
			continue
		}
		b = append(b, c)
		p.pos++
	}
	return "", p.err("unterminated string")
}

func (p *parser) ident() (string, bool) {
	if p.pos >= len(p.src) {
		return "", false
	}
	r, n := utf8.DecodeRune(p.src[p.pos:])
	if r != '_' && !unicode.IsLetter(r) {
		return "", false
	}
	start := p.pos
	p.pos += n
	for p.pos < len(p.src) {
		r, n = utf8.DecodeRune(p.src[p.pos:])
		if r != '_' && !unicode.IsLetter(r) && !unicode.IsDigit(r) {
			break
		}
		p.pos += n
	}
	return string(p.src[start:p.pos]), true
}

func (p *parser) intLit() (string, bool) {
	if p.pos >= len(p.src) || p.src[p.pos] < '0' || p.src[p.pos] > '9' {
		return "", false
	}
	start := p.pos
	for p.pos < len(p.src) && p.src[p.pos] >= '0' && p.src[p.pos] <= '9' {
		p.pos++
	}
	return string(p.src[start:p.pos]), true
}

func atoi(s string) int {
	n := 0
	for i := 0; i < len(s); i++ {
		n = n*10 + int(s[i]-'0')
	}
	return n
}
