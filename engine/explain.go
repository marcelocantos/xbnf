// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package engine

import (
	"fmt"
	"sort"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/marcelocantos/xbnf/grammar"
)

// Promotion is compile-time DFA vs GLL assignment of named rules.
type Promotion struct {
	DFA []string
	GLL []string
}

// Promotion reports which named rules compiled to DFAs and which require GLL.
func (c *Compiled) Promotion() Promotion {
	if c == nil {
		return Promotion{}
	}
	var dfa, gll []string
	for name := range c.rules {
		if strings.HasPrefix(name, "$") {
			continue
		}
		if c.IsDFA(name) {
			dfa = append(dfa, name)
		} else {
			gll = append(gll, name)
		}
	}
	sort.Strings(dfa)
	sort.Strings(gll)
	return Promotion{DFA: dfa, GLL: gll}
}

func (p Promotion) String() string {
	var b strings.Builder
	b.WriteString("DFA:\n")
	if len(p.DFA) == 0 {
		b.WriteString("  (none)\n")
	} else {
		for _, n := range p.DFA {
			b.WriteString("  ")
			b.WriteString(n)
			b.WriteByte('\n')
		}
	}
	b.WriteString("GLL (runtime forking):\n")
	if len(p.GLL) == 0 {
		b.WriteString("  (none)\n")
	} else {
		for _, n := range p.GLL {
			b.WriteString("  ")
			b.WriteString(n)
			b.WriteByte('\n')
		}
	}
	return b.String()
}

func lineCol(input string, pos int) (line, col int) {
	line, col = 1, 1
	if pos < 0 {
		pos = 0
	}
	if pos > len(input) {
		pos = len(input)
	}
	for i := 0; i < pos; i++ {
		if input[i] == '\n' {
			line++
			col = 1
		} else {
			col++
		}
	}
	return line, col
}

func describeTerm(t grammar.Term) string {
	switch x := t.(type) {
	case grammar.String:
		return strconv.Quote(x.Text)
	case grammar.Escape:
		return `\` + x.Code
	case grammar.AnyChar:
		return "."
	case grammar.CharClass:
		return classText(x)
	case grammar.Ident:
		return x.Name
	case grammar.Leaf:
		return "/" + describeTerm(x.Term) + "/"
	case grammar.Empty:
		return "()"
	default:
		return x.Kind().String()
	}
}

func classText(c grammar.CharClass) string {
	var b strings.Builder
	b.WriteByte('[')
	if c.Negated {
		b.WriteByte('^')
	}
	for _, e := range c.Elems {
		b.WriteString(e.Lo)
		if e.Hi != "" {
			b.WriteByte('-')
			b.WriteString(e.Hi)
		}
	}
	b.WriteByte(']')
	return b.String()
}

func displayNT(nt string) string {
	if strings.HasPrefix(nt, "$lf") {
		return "leaf"
	}
	if strings.HasPrefix(nt, "$") {
		return "group"
	}
	return nt
}

func describeElem(e elem) string {
	switch e.kind {
	case ekTerm:
		return describeTerm(e.term)
	case ekDFA, ekNT:
		return displayNT(e.nt)
	case ekLook:
		return "(?=…)"
	case ekNegLook:
		return "(?!…)"
	default:
		return "?"
	}
}

func formatExpect(input string, pos int, want []string, rule string, dfa bool) string {
	line, col := lineCol(input, pos)
	got := "end of input"
	if pos >= 0 && pos < len(input) {
		_, n := utf8.DecodeRuneInString(input[pos:])
		got = strconv.Quote(input[pos : pos+n])
	}
	sort.Strings(want)
	list := strings.Join(want, ", ")
	if dfa {
		return fmt.Sprintf("expected one of %s at %d:%d; got %s", list, line, col, got)
	}
	if rule == "" {
		rule = "start"
	}
	return fmt.Sprintf("in rule %s, expected %s at %d:%d; got %s", displayNT(rule), list, line, col, got)
}
