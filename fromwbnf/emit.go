// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package fromwbnf

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/marcelocantos/xbnf/grammar"
)

const (
	precAtom = iota
	precSuffix
	precSeq
	precAlt
	precStack
)

func emitGrammar(g *grammar.Grammar) string {
	var b strings.Builder
	n := 0
	for _, st := range g.Stmts {
		if n > 0 {
			b.WriteString("\n\n")
		}
		emitStmt(&b, st)
		n++
	}
	if n > 0 {
		b.WriteByte('\n')
	}
	return b.String()
}

func emitStmt(b *strings.Builder, st grammar.Stmt) {
	switch s := st.(type) {
	case grammar.Rule:
		b.WriteString(s.Name)
		for _, m := range s.Mods {
			b.WriteString(" #")
			b.WriteString(m)
		}
		b.WriteString(" -> ")
		emitTerm(b, s.Body, precStack, false)
		b.WriteString(";")
	case grammar.Wrap:
		b.WriteString("#wrap -> ")
		emitTerm(b, s.Body, precStack, false)
		b.WriteString(";")
	case grammar.Import:
		b.WriteString("#import ")
		b.WriteString(strconv.Quote(s.Path))
		b.WriteString(";")
	case grammar.Macro:
		b.WriteString("#macro ")
		b.WriteString(s.Name)
		b.WriteByte('(')
		b.WriteString(strings.Join(s.Params, ", "))
		b.WriteString(") { ")
		emitTerm(b, s.Body, precStack, false)
		b.WriteString(" }")
	default:
		fmt.Fprintf(b, "/* unknown stmt %T */", st)
	}
}

func emitTerm(b *strings.Builder, t grammar.Term, prec int, compact bool) {
	tp := termPrec(t)
	paren := tp > prec
	if paren {
		b.WriteByte('(')
	}
	switch x := t.(type) {
	case grammar.Stack:
		for i, lv := range x.Levels {
			if i > 0 {
				if compact {
					b.WriteByte('>')
				} else {
					b.WriteString(" > ")
				}
			}
			emitTerm(b, lv, precAlt, compact)
		}
	case grammar.Alt:
		for i, u := range x.Terms {
			if i > 0 {
				if compact {
					b.WriteByte('|')
				} else {
					b.WriteString(" | ")
				}
			}
			emitTerm(b, u, precSeq, compact)
		}
	case grammar.OrderedAlt:
		for i, u := range x.Terms {
			if i > 0 {
				switch {
				case compact:
					b.WriteString("|>")
				case prec >= precStack:
					b.WriteString("\n    |> ")
				default:
					b.WriteString(" |> ")
				}
			}
			emitTerm(b, u, precSeq, compact)
		}
	case grammar.Seq:
		for i, u := range x.Terms {
			if i > 0 && !compact {
				b.WriteByte(' ')
			}
			emitTerm(b, u, precSuffix, compact)
		}
		for _, d := range x.Directives {
			b.WriteString(" #")
			b.WriteString(d.Name)
			if d.Value != "" {
				b.WriteByte('=')
				b.WriteString(d.Value)
			}
		}
	case grammar.Named:
		b.WriteString(x.Name)
		b.WriteByte('=')
		emitTerm(b, x.Term, precAtom, compact)
	case grammar.Quant:
		emitTerm(b, x.Term, precAtom, compact)
		b.WriteString(quantMark(x.Min, x.Max))
	case grammar.Delim:
		emitTerm(b, x.Term, precAtom, compact)
		b.WriteByte(':')
		if x.Leading {
			b.WriteByte(',')
		}
		emitTerm(b, x.Sep, precAtom, compact)
		if x.Trailing {
			b.WriteByte(',')
		}
	case grammar.Scope:
		b.WriteByte('{')
		for _, d := range x.Decls {
			b.WriteByte(' ')
			emitStmt(b, d)
		}
		if _, ok := x.Term.(grammar.Empty); !ok {
			b.WriteByte(' ')
			emitTerm(b, x.Term, precStack, compact)
		}
		b.WriteString(" }")
	case grammar.Ident:
		b.WriteString(x.Name)
		if x.Label != "" {
			b.WriteString("::")
			b.WriteString(x.Label)
		}
	case grammar.String:
		b.WriteString(quoteString(x.Text))
	case grammar.CharClass:
		b.WriteByte('[')
		if x.Negated {
			b.WriteByte('^')
		}
		for _, e := range x.Elems {
			b.WriteString(classAtom(e.Lo))
			if e.Hi != "" {
				b.WriteByte('-')
				b.WriteString(classAtom(e.Hi))
			}
		}
		b.WriteByte(']')
	case grammar.Escape:
		b.WriteByte('\\')
		b.WriteString(x.Code)
	case grammar.Leaf:
		b.WriteByte('/')
		emitTerm(b, x.Term, precSeq, true)
		b.WriteByte('/')
	case grammar.AnyChar:
		b.WriteByte('.')
	case grammar.Ref:
		b.WriteByte('%')
		b.WriteString(x.Name)
		if x.HasDefault {
			b.WriteByte('=')
			b.WriteString(quoteString(x.Default))
		}
	case grammar.ExtRef:
		b.WriteString("%%")
		b.WriteString(x.Name)
	case grammar.MacroCall:
		b.WriteString("%!")
		b.WriteString(x.Name)
		b.WriteByte('(')
		for i, a := range x.Args {
			if i > 0 {
				b.WriteString(", ")
			}
			emitTerm(b, a, precStack, compact)
		}
		b.WriteByte(')')
	case grammar.Lookahead:
		b.WriteString("(?=")
		emitTerm(b, x.Term, precStack, compact)
		b.WriteByte(')')
	case grammar.NegLookahead:
		b.WriteString("(?!")
		emitTerm(b, x.Term, precStack, compact)
		b.WriteByte(')')
	case grammar.CaseFold:
		if x.On {
			b.WriteString("(?i:")
		} else {
			b.WriteString("(?~i:")
		}
		emitTerm(b, x.Term, precStack, compact)
		b.WriteByte(')')
	case grammar.Self:
		b.WriteByte('@')
	case grammar.Empty:
		b.WriteString("()")
	default:
		fmt.Fprintf(b, "/* %T */", t)
	}
	if paren {
		b.WriteByte(')')
	}
}

func termPrec(t grammar.Term) int {
	switch t.(type) {
	case grammar.Stack:
		return precStack
	case grammar.Alt, grammar.OrderedAlt:
		return precAlt
	case grammar.Seq:
		return precSeq
	case grammar.Named, grammar.Quant, grammar.Delim:
		return precSuffix
	default:
		return precAtom
	}
}

func quantMark(min, max int) string {
	if min == 0 && max == grammar.Unbounded {
		return "*"
	}
	if min == 1 && max == grammar.Unbounded {
		return "+"
	}
	if min == 0 && max == 1 {
		return "?"
	}
	if min == max && max >= 0 {
		return "{" + strconv.Itoa(min) + "}"
	}
	if max == grammar.Unbounded {
		return "{" + strconv.Itoa(min) + ",}"
	}
	return fmt.Sprintf("{%d,%d}", min, max)
}

func quoteString(s string) string {
	if strings.Contains(s, `"`) && !strings.Contains(s, "'") {
		return "'" + strings.ReplaceAll(strings.ReplaceAll(s, `\`, `\\`), "'", `\'`) + "'"
	}
	return strconv.Quote(s)
}

func classAtom(s string) string {
	if s == "" {
		return ""
	}
	switch s {
	case `\`, `]`, `-`, `^`:
		return `\` + s
	}
	if s == "\n" {
		return `\n`
	}
	if s == "\t" {
		return `\t`
	}
	if s == "\r" {
		return `\r`
	}
	return s
}
