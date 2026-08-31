// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package fromwbnf

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"
)

var (
	whitespaceRE   = regexp.MustCompile(`\s`)
	escapedSpaceRE = regexp.MustCompile(`((?:\A|[^\\])(?:\\\\)*)\\_`)
)

// Parse reads .wbnf source into an IR of what the grammar means. Macros stay
// as definitions and calls; cut-points are not inserted. The result is not a
// runnable parser.
func Parse(src []byte) (*File, error) {
	p := &parser{src: src}
	f, err := p.parseGrammar(0)
	if err != nil {
		return nil, err
	}
	p.skip()
	p.skipComments()
	p.skip()
	if p.pos < len(p.src) {
		return nil, p.err("unexpected input")
	}
	if len(f.Stmts) == 0 {
		return nil, p.err("empty grammar")
	}
	return f, nil
}

type parser struct {
	src []byte
	pos int
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
	return fmt.Errorf("fromwbnf:%d:%d: %s", line, col, msg)
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
		c := p.src[p.pos]
		if c == ' ' || c == '\t' || c == '\n' || c == '\r' {
			p.pos++
			continue
		}
		return
	}
}

func (p *parser) skipComments() {
	for {
		p.skip()
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

func (p *parser) parseGrammar(stop byte) (*File, error) {
	f := &File{}
	for {
		p.skip()
		p.skipComments()
		p.skip()
		if p.pos >= len(p.src) {
			break
		}
		if stop != 0 && p.peek() == stop {
			break
		}
		st, err := p.parseStmt()
		if err != nil {
			return nil, err
		}
		f.Stmts = append(f.Stmts, st)
	}
	return f, nil
}

func (p *parser) parseStmt() (Stmt, error) {
	p.skip()
	name, ok := p.ident()
	if !ok {
		return nil, p.err("expected statement")
	}
	p.skip()
	if name == ".import" && !p.at("->") {
		return p.parseImport()
	}
	if name == ".macro" && !p.at("->") {
		return p.parseMacro()
	}
	if !p.eat("->") {
		return nil, p.err("expected ->")
	}
	body, err := p.parseProdBody()
	if err != nil {
		return nil, err
	}
	p.skip()
	if !p.eat(";") {
		return nil, p.err("expected ; after production")
	}
	return Prod{Name: name, Body: body}, nil
}

func (p *parser) parseImport() (Stmt, error) {
	p.skip()
	if p.at("//") || p.at("/*") {
		return nil, p.err("expected import path")
	}
	start := p.pos
	end := p.pos
	if p.peek() == '/' && !p.at("//") {
		p.eat("/")
		end = p.pos
		p.skip()
	}
	for {
		if _, ok := p.pathPart(); !ok {
			break
		}
		end = p.pos
		save := p.pos
		p.skip()
		if !p.eat("/") {
			break
		}
		p.skip()
		if _, ok := p.pathPart(); !ok {
			p.pos = save
			break
		}
		end = p.pos
	}
	if end == start {
		return nil, p.err("expected import path")
	}
	path := string(p.src[start:end])
	p.skip()
	p.eat(";")
	return Import{Path: path}, nil
}

func (p *parser) pathPart() (string, bool) {
	if p.at("..") && (p.pos+2 >= len(p.src) || !isPathChar(p.src[p.pos+2])) {
		p.pos += 2
		return "..", true
	}
	if p.peek() == '.' && (p.pos+1 >= len(p.src) || !isPathChar(p.src[p.pos+1])) {
		p.pos++
		return ".", true
	}
	start := p.pos
	for p.pos < len(p.src) && isPathChar(p.src[p.pos]) {
		p.pos++
	}
	if p.pos == start {
		return "", false
	}
	return string(p.src[start:p.pos]), true
}

func isPathChar(c byte) bool {
	return c == '.' || c == ':' || (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') || (c >= '0' && c <= '9')
}

func (p *parser) parseMacro() (Stmt, error) {
	p.skip()
	name, ok := p.ident()
	if !ok {
		return nil, p.err("expected macro name")
	}
	p.skip()
	if !p.eat("(") {
		return nil, p.err("expected (")
	}
	var args []string
	for {
		p.skip()
		if p.peek() == ')' {
			break
		}
		id, ok := p.ident()
		if !ok {
			return nil, p.err("expected macro argument")
		}
		args = append(args, id)
		p.skip()
		if !p.eat(",") {
			break
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
	return MacroDef{Name: name, Args: args, Body: body}, nil
}

func (p *parser) parseProdBody() (Term, error) {
	first, err := p.parseTerm()
	if err != nil {
		return nil, err
	}
	terms := []Term{first}
	for {
		p.skip()
		if p.peek() == ';' || p.peek() == 0 || p.peek() == '}' {
			break
		}
		if !p.termStart() {
			break
		}
		t, err := p.parseTerm()
		if err != nil {
			return nil, err
		}
		terms = append(terms, t)
	}
	if len(terms) == 1 {
		return first, nil
	}
	return Seq(terms), nil
}

func (p *parser) termStart() bool {
	p.skip()
	c := p.peek()
	if c == 0 {
		return false
	}
	if c == '"' || c == '\'' || c == '`' || c == '%' || c == '(' || c == '[' || c == '\\' ||
		c == '^' || c == '$' || c == '/' {
		return true
	}
	if c == '.' || c == '@' || isIdentStart(c) {
		return true
	}
	return false
}

func (p *parser) parseTerm() (Term, error) {
	return p.parseStack()
}

func (p *parser) parseStack() (Term, error) {
	t, body, err := p.parseStackElem()
	if err != nil {
		return nil, err
	}
	p.skip()
	if p.peek() != '>' {
		return scoped(t, body), nil
	}
	levels := []Term{scoped(t, body)}
	for {
		p.skip()
		if p.peek() != '>' {
			break
		}
		p.pos++
		t, body, err = p.parseStackElem()
		if err != nil {
			return nil, err
		}
		levels = append(levels, scoped(t, body))
	}
	return Stack(levels), nil
}

func scoped(t Term, body *File) Term {
	if body == nil {
		return t
	}
	return Scoped{Term: t, Body: body}
}

func (p *parser) parseStackElem() (Term, *File, error) {
	t, err := p.parseAlt()
	if err != nil {
		return nil, nil, err
	}
	p.skip()
	if p.peek() != '{' || p.isRangeQuant() {
		return t, nil, nil
	}
	p.pos++
	g, err := p.parseGrammar('}')
	if err != nil {
		return nil, nil, err
	}
	p.skip()
	if !p.eat("}") {
		return nil, nil, p.err("expected }")
	}
	return t, g, nil
}

func (p *parser) parseAlt() (Term, error) {
	first, err := p.parseSeq()
	if err != nil {
		return nil, err
	}
	p.skip()
	if p.peek() != '|' {
		return first, nil
	}
	terms := []Term{first}
	for {
		p.skip()
		if p.peek() != '|' {
			break
		}
		p.pos++
		t, err := p.parseSeq()
		if err != nil {
			return nil, err
		}
		terms = append(terms, t)
	}
	return Oneof(terms), nil
}

func (p *parser) parseSeq() (Term, error) {
	first, err := p.parseNamedQuants()
	if err != nil {
		return nil, err
	}
	terms := []Term{first}
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
	if len(terms) == 1 {
		return first, nil
	}
	return Seq(terms), nil
}

func (p *parser) seqEnds() bool {
	if p.pos >= len(p.src) {
		return true
	}
	switch p.src[p.pos] {
	case '|', '>', ';', '}', ')', ',':
		return true
	case '{':
		return true
	}
	return false
}

func (p *parser) parseNamedQuants() (Term, error) {
	term, err := p.parseNamed()
	if err != nil {
		return nil, err
	}
	var ops []quant
	for {
		q, ok, err := p.parseQuant()
		if err != nil {
			return nil, err
		}
		if !ok {
			break
		}
		ops = append(ops, q)
	}
	// wbnf applies quants right-to-left: `term:","?` is Delim(Opt(term), ",").
	for i := len(ops) - 1; i >= 0; i-- {
		term = ops[i].apply(term)
	}
	return term, nil
}

type quant struct {
	star, plus, opt bool
	min, max        int
	minmax          bool
	delim           *Delim
}

func (q quant) apply(inner Term) Term {
	switch {
	case q.star:
		return Quant{Term: inner, Min: 0, Max: 0}
	case q.plus:
		return Quant{Term: inner, Min: 1, Max: 0}
	case q.opt:
		return Quant{Term: inner, Min: 0, Max: 1}
	case q.minmax:
		return Quant{Term: inner, Min: q.min, Max: q.max}
	case q.delim != nil:
		d := *q.delim
		d.Term = inner
		return d
	}
	return inner
}

func (p *parser) parseNamed() (Term, error) {
	p.skip()
	save := p.pos
	if id, ok := p.ident(); ok {
		p.skip()
		if p.eat("=") {
			atom, err := p.parseAtom()
			if err != nil {
				return nil, err
			}
			return Named{Name: id, Term: atom}, nil
		}
		p.pos = save
	}
	return p.parseAtom()
}

func (p *parser) parseQuant() (quant, bool, error) {
	p.skip()
	switch {
	case p.eat("?"):
		return quant{opt: true}, true, nil
	case p.eat("*"):
		return quant{star: true}, true, nil
	case p.eat("+"):
		return quant{plus: true}, true, nil
	case p.at("<:"):
		p.pos += 2
		return p.parseDelimQuant("<:")
	case p.at(":>"):
		p.pos += 2
		return p.parseDelimQuant(":>")
	case p.peek() == ':' && !p.at("::"):
		p.pos++
		return p.parseDelimQuant(":")
	case p.peek() == '{' && p.isRangeQuant():
		return p.parseRangeQuant()
	}
	return quant{}, false, nil
}

func (p *parser) parseDelimQuant(assoc string) (quant, bool, error) {
	p.skip()
	leading := p.eat(",")
	p.skip()
	sep, err := p.parseNamed()
	if err != nil {
		return quant{}, false, err
	}
	p.skip()
	trailing := p.eat(",")
	return quant{delim: &Delim{Sep: sep, Assoc: assoc, Leading: leading, Trailing: trailing}}, true, nil
}

func (p *parser) isRangeQuant() bool {
	save := p.pos
	defer func() { p.pos = save }()
	if !p.eat("{") {
		return false
	}
	p.skip()
	p.intLit()
	p.skip()
	if !p.eat(",") {
		return false
	}
	p.skip()
	p.intLit()
	p.skip()
	return p.eat("}")
}

func (p *parser) parseRangeQuant() (quant, bool, error) {
	if !p.eat("{") {
		return quant{}, false, p.err("expected {")
	}
	p.skip()
	min := 0
	max := 0
	if n, ok := p.intLit(); ok {
		min, _ = strconv.Atoi(n)
	}
	p.skip()
	if !p.eat(",") {
		return quant{}, false, p.err("expected , in {min,max}")
	}
	p.skip()
	if n, ok := p.intLit(); ok {
		max, _ = strconv.Atoi(n)
	}
	p.skip()
	if !p.eat("}") {
		return quant{}, false, p.err("expected }")
	}
	return quant{minmax: true, min: min, max: max}, true, nil
}

func (p *parser) parseAtom() (Term, error) {
	p.skip()
	if p.pos >= len(p.src) {
		return nil, p.err("expected atom")
	}
	switch {
	case p.at("(?="):
		p.pos += 3
		t, err := p.parseTerm()
		if err != nil {
			return nil, err
		}
		p.skip()
		if !p.eat(")") {
			return nil, p.err("expected )")
		}
		return Lookahead{Term: t}, nil
	case p.peek() == '(':
		p.pos++
		p.skip()
		if p.eat(")") {
			return Empty{}, nil
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
	case p.peek() == '"' || p.peek() == '\'' || p.peek() == '`':
		s, err := p.parseString()
		if err != nil {
			return nil, err
		}
		return String(s), nil
	case p.at("%!"):
		return p.parseMacroCall()
	case p.at("%%"):
		p.pos += 2
		name, ok := p.ident()
		if !ok {
			return nil, p.err("expected extref name")
		}
		return ExtRef(name), nil
	case p.peek() == '%':
		p.pos++
		name, ok := p.ident()
		if !ok {
			return nil, p.err("expected ref name")
		}
		r := Ref{Ident: name}
		p.skip()
		if p.eat("=") {
			p.skip()
			s, err := p.parseString()
			if err != nil {
				return nil, err
			}
			str := String(s)
			r.Default = &str
		}
		return r, nil
	}
	if id, ok := p.ident(); ok {
		return Ident(id), nil
	}
	if re, ok := p.scanRE(); ok {
		return RE(cleanRE(re)), nil
	}
	return nil, p.err("expected atom")
}

func (p *parser) parseMacroCall() (Term, error) {
	if !p.eat("%!") {
		return nil, p.err("expected %!")
	}
	name, ok := p.ident()
	if !ok {
		return nil, p.err("expected macro name")
	}
	p.skip()
	if !p.eat("(") {
		return nil, p.err("expected (")
	}
	var args []Term
	for {
		p.skip()
		if p.peek() == ')' {
			break
		}
		t, err := p.parseTerm()
		if err != nil {
			return nil, err
		}
		args = append(args, t)
		p.skip()
		if !p.eat(",") {
			break
		}
	}
	p.skip()
	if !p.eat(")") {
		return nil, p.err("expected )")
	}
	return MacroCall{Name: name, Args: args}, nil
}

func (p *parser) ident() (string, bool) {
	if p.peek() == '@' {
		p.pos++
		return "@", true
	}
	start := p.pos
	if p.peek() == '.' {
		if p.pos+1 >= len(p.src) || !isIdentStart(p.src[p.pos+1]) {
			return "", false
		}
		p.pos++
	}
	if p.pos >= len(p.src) || !isIdentStart(p.src[p.pos]) {
		p.pos = start
		return "", false
	}
	p.pos++
	for p.pos < len(p.src) && isIdentCont(p.src[p.pos]) {
		p.pos++
	}
	return string(p.src[start:p.pos]), true
}

func isIdentStart(c byte) bool {
	return c == '_' || (c >= 'A' && c <= 'Z') || (c >= 'a' && c <= 'z')
}

func isIdentCont(c byte) bool {
	return isIdentStart(c) || (c >= '0' && c <= '9')
}

func (p *parser) intLit() (string, bool) {
	start := p.pos
	for p.pos < len(p.src) && p.src[p.pos] >= '0' && p.src[p.pos] <= '9' {
		p.pos++
	}
	if p.pos == start {
		return "", false
	}
	return string(p.src[start:p.pos]), true
}

func (p *parser) scanRE() (string, bool) {
	if p.at("/{") {
		return p.scanREBrace()
	}
	return p.scanRESimple()
}

func (p *parser) scanREBrace() (string, bool) {
	start := p.pos
	p.pos += 2
	for p.pos < len(p.src) {
		c := p.src[p.pos]
		if c == '\\' && p.pos+1 < len(p.src) {
			p.pos += 2
			continue
		}
		if c == '[' {
			if !p.scanClass() {
				p.pos = start
				return "", false
			}
			continue
		}
		if c == '{' {
			p.pos++
			p.tryREBraceQuant()
			continue
		}
		if c == '}' {
			p.pos++
			return string(p.src[start:p.pos]), true
		}
		p.pos++
	}
	p.pos = start
	return "", false
}

func (p *parser) tryREBraceQuant() {
	save := p.pos
	if p.digits() {
		if p.eat(",") {
			for p.pos < len(p.src) && p.src[p.pos] >= '0' && p.src[p.pos] <= '9' {
				p.pos++
			}
		}
		if p.peek() == '}' {
			p.pos++
			return
		}
		p.pos = save
		return
	}
	if p.eat(",") && p.digits() && p.peek() == '}' {
		p.pos++
		return
	}
	p.pos = save
}

func (p *parser) digits() bool {
	if p.pos >= len(p.src) || p.src[p.pos] < '0' || p.src[p.pos] > '9' {
		return false
	}
	for p.pos < len(p.src) && p.src[p.pos] >= '0' && p.src[p.pos] <= '9' {
		p.pos++
	}
	return true
}

func (p *parser) scanRESimple() (string, bool) {
	start := p.pos
	n := 0
	for {
		save := p.pos
		if !p.simpleREAtom() {
			p.pos = save
			break
		}
		p.simpleREQuant()
		n++
	}
	if n == 0 {
		return "", false
	}
	return string(p.src[start:p.pos]), true
}

func (p *parser) simpleREAtom() bool {
	if p.peek() == '[' {
		return p.scanClass()
	}
	if p.peek() == '\\' {
		if p.pos+1 >= len(p.src) {
			return false
		}
		n := p.src[p.pos+1]
		if n == 'p' || n == 'P' {
			p.pos += 2
			if p.pos < len(p.src) && p.src[p.pos] >= 'a' && p.src[p.pos] <= 'z' {
				p.pos++
				return true
			}
			if p.eat("{") {
				if p.pos >= len(p.src) {
					return false
				}
				c := p.src[p.pos]
				if !((c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') || c == '_') {
					return false
				}
				p.pos++
				for p.pos < len(p.src) {
					c := p.src[p.pos]
					if !((c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') || (c >= '0' && c <= '9') || c == '_') {
						break
					}
					p.pos++
				}
				return p.eat("}")
			}
			return false
		}
		if (n >= 'a' && n <= 'z') || (n >= 'A' && n <= 'Z') {
			p.pos += 2
			return true
		}
		return false
	}
	c := p.peek()
	if c == '.' || c == '^' || c == '$' {
		p.pos++
		return true
	}
	return false
}

func (p *parser) simpleREQuant() {
	c := p.peek()
	if c == '+' || c == '*' || c == '?' {
		p.pos++
		if p.peek() == '?' {
			p.pos++
		}
		return
	}
	if c != '{' {
		return
	}
	save := p.pos
	p.pos++
	if !p.digits() {
		p.pos = save
		return
	}
	p.eat(",")
	if p.pos < len(p.src) && p.src[p.pos] >= '0' && p.src[p.pos] <= '9' {
		p.pos++
	}
	if p.peek() != '}' {
		p.pos = save
		return
	}
	p.pos++
	if p.peek() == '?' {
		p.pos++
	}
}

func (p *parser) scanClass() bool {
	if p.peek() != '[' {
		return false
	}
	p.pos++
	n := 0
	for p.pos < len(p.src) && p.src[p.pos] != ']' {
		if p.src[p.pos] == '\\' && p.pos+1 < len(p.src) {
			p.pos += 2
			n++
			continue
		}
		if p.at("[:") {
			q := p.pos
			p.pos += 2
			p.eat("^")
			ok := false
			for p.pos < len(p.src) && p.src[p.pos] >= 'a' && p.src[p.pos] <= 'z' {
				p.pos++
				ok = true
			}
			if ok && p.at(":]") {
				p.pos += 2
				n++
				continue
			}
			p.pos = q
		}
		p.pos++
		n++
	}
	if n == 0 || p.peek() != ']' {
		return false
	}
	p.pos++
	return true
}

func (p *parser) parseString() (string, error) {
	if p.pos >= len(p.src) {
		return "", p.err("expected string")
	}
	quote := p.src[p.pos]
	if quote != '"' && quote != '\'' && quote != '`' {
		return "", p.err("expected string")
	}
	p.pos++
	start := p.pos
	if quote == '`' {
		for p.pos < len(p.src) {
			if p.src[p.pos] == '`' {
				if p.pos+1 < len(p.src) && p.src[p.pos+1] == '`' {
					p.pos += 2
					continue
				}
				s := strings.ReplaceAll(string(p.src[start:p.pos]), "``", "`")
				p.pos++
				return s, nil
			}
			p.pos++
		}
		return "", p.err("unclosed raw string")
	}
	s := string(p.src[start:])
	var sb strings.Builder
	i := 0
	for i < len(s) {
		c := s[i]
		if c == quote {
			p.pos += i + 1
			return sb.String(), nil
		}
		if c != '\\' {
			sb.WriteByte(c)
			i++
			continue
		}
		i++
		if i >= len(s) {
			break
		}
		switch s[i] {
		case 'x':
			if i+3 > len(s) {
				return "", p.err("bad \\x escape")
			}
			n, err := strconv.ParseInt(s[i+1:i+3], 16, 8)
			if err != nil {
				return "", p.err("bad \\x escape")
			}
			sb.WriteByte(uint8(n))
			i += 2
		case 'u':
			if i+5 > len(s) {
				return "", p.err("bad \\u escape")
			}
			n, err := strconv.ParseInt(s[i+1:i+5], 16, 16)
			if err != nil {
				return "", p.err("bad \\u escape")
			}
			sb.WriteByte(uint8(n))
			i += 4
		case 'U':
			if i+9 > len(s) {
				return "", p.err("bad \\U escape")
			}
			n, err := strconv.ParseInt(s[i+1:i+9], 16, 32)
			if err != nil {
				return "", p.err("bad \\U escape")
			}
			sb.WriteByte(uint8(n))
			i += 8
		case '0', '1', '2', '3', '4', '5', '6', '7':
			if i+3 > len(s) {
				return "", p.err("bad octal escape")
			}
			n, err := strconv.ParseInt(s[i:i+3], 8, 8)
			if err != nil {
				return "", p.err("bad octal escape")
			}
			sb.WriteByte(uint8(n))
			i += 2
		case 'a':
			sb.WriteByte('\a')
		case 'b':
			sb.WriteByte('\b')
		case 'f':
			sb.WriteByte('\f')
		case 'n':
			sb.WriteByte('\n')
		case 'r':
			sb.WriteByte('\r')
		case 't':
			sb.WriteByte('\t')
		case 'v':
			sb.WriteByte('\v')
		case '\\':
			sb.WriteByte('\\')
		case '\'':
			sb.WriteByte('\'')
		case quote:
			sb.WriteByte(quote)
		default:
			return "", p.err(fmt.Sprintf("unrecognized \\-escape: %q", s[i]))
		}
		i++
	}
	return "", p.err("unclosed string")
}

func cleanRE(s string) string {
	s = whitespaceRE.ReplaceAllString(s, "")
	s = escapedSpaceRE.ReplaceAllString(s, "$1 ")
	s = escapedSpaceRE.ReplaceAllString(s, "$1 ")
	if strings.HasPrefix(s, "/{") {
		s = s[2 : len(s)-1]
	}
	return s
}
