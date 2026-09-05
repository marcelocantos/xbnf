// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package engine

import (
	"unicode"
	"unicode/utf8"

	"github.com/marcelocantos/xbnf/grammar"
)

func hasMod(mods []string, name string) bool {
	for _, m := range mods {
		if m == name {
			return true
		}
	}
	return false
}

func matchTerminal(t grammar.Term, input string, pos int) (int, bool) {
	switch x := t.(type) {
	case grammar.String:
		if pos+len(x.Text) <= len(input) && input[pos:pos+len(x.Text)] == x.Text {
			return pos + len(x.Text), true
		}
	case grammar.CharClass:
		if pos >= len(input) {
			return pos, false
		}
		r, n := decodeRune(input, pos)
		if classMatch(x, r) {
			return pos + n, true
		}
	case grammar.Escape:
		if pos >= len(input) {
			return pos, false
		}
		r, n := decodeRune(input, pos)
		if escapeMatch(x.Code, r) {
			return pos + n, true
		}
	case grammar.AnyChar:
		if pos >= len(input) {
			return pos, false
		}
		_, n := decodeRune(input, pos)
		return pos + n, true
	case grammar.Empty:
		return pos, true
	}
	return pos, false
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
		if tab, neg, ok := unicodeEscape(code); ok {
			return unicode.Is(tab, r) != neg
		}
		ch, _ := utf8.DecodeRuneInString(code)
		return r == ch
	}
}

func unicodeEscape(code string) (*unicode.RangeTable, bool, bool) {
	if code == "" {
		return nil, false, false
	}
	neg := false
	switch code[0] {
	case 'p':
	case 'P':
		neg = true
	default:
		return nil, false, false
	}
	name := code[1:]
	if len(name) >= 2 && name[0] == '{' && name[len(name)-1] == '}' {
		name = name[1 : len(name)-1]
	}
	if name == "" {
		return nil, false, false
	}
	if tab := unicode.Categories[name]; tab != nil {
		return tab, neg, true
	}
	if tab := unicode.Scripts[name]; tab != nil {
		return tab, neg, true
	}
	if tab := unicode.Properties[name]; tab != nil {
		return tab, neg, true
	}
	return nil, false, false
}

func runePred(t grammar.Term) func(rune) bool {
	switch x := t.(type) {
	case grammar.CharClass:
		return func(r rune) bool { return classMatch(x, r) }
	case grammar.Escape:
		return func(r rune) bool { return escapeMatch(x.Code, r) }
	case grammar.AnyChar:
		return func(rune) bool { return true }
	}
	return nil
}
