// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package fromwbnf

import (
	"fmt"
	"strconv"
	"strings"
)

// Dump is a canonical print of a parsed grammar. Tests use it as the
// isomorphism oracle: same names, combinators, terminals, and wrapRE as
// wbnf's grammar AST (not cut-points, not Go types).
func Dump(f *File) string {
	if f == nil {
		return ""
	}
	var b strings.Builder
	for i, st := range f.Stmts {
		if i > 0 {
			b.WriteByte('\n')
		}
		switch s := st.(type) {
		case Prod:
			b.WriteString(s.Name)
			b.WriteString(" = ")
			b.WriteString(dumpTerm(s.Body))
		case Import:
			b.WriteString(".import ")
			b.WriteString(s.Path)
		case MacroDef:
			b.WriteString(".macro ")
			b.WriteString(s.Name)
			b.WriteByte('(')
			b.WriteString(strings.Join(s.Args, ", "))
			b.WriteString(") { ")
			b.WriteString(dumpTerm(s.Body))
			b.WriteString(" }")
		default:
			fmt.Fprintf(&b, "<%T>", st)
		}
	}
	return b.String()
}

func dumpTerm(t Term) string {
	switch x := t.(type) {
	case Seq:
		return "seq(" + dumpList([]Term(x)) + ")"
	case Oneof:
		return "oneof(" + dumpList([]Term(x)) + ")"
	case Stack:
		return "stack(" + dumpList([]Term(x)) + ")"
	case Quant:
		return fmt.Sprintf("{%s}(%s)", dumpBounds(x.Min, x.Max), dumpTerm(x.Term))
	case Delim:
		flags := x.Assoc
		if x.Leading {
			flags += "L"
		}
		if x.Trailing {
			flags += "T"
		}
		return fmt.Sprintf("delim[%s](%s, %s)", flags, dumpTerm(x.Term), dumpTerm(x.Sep))
	case Named:
		return x.Name + "=" + dumpTerm(x.Term)
	case Ident:
		return string(x)
	case String:
		return strconv.Quote(string(x))
	case RE:
		return "re(" + string(x) + ")"
	case Ref:
		s := "%" + x.Ident
		if x.Default != nil {
			s += "=" + strconv.Quote(string(*x.Default))
		}
		return s
	case ExtRef:
		return "%%" + string(x)
	case Lookahead:
		return "lookahead(" + dumpTerm(x.Term) + ")"
	case Scoped:
		body := ""
		if x.Body != nil {
			body = Dump(x.Body)
		}
		return "scoped(" + dumpTerm(x.Term) + "){ " + body + " }"
	case MacroCall:
		return "%!" + x.Name + "(" + dumpList(x.Args) + ")"
	case Empty:
		return "()"
	default:
		return fmt.Sprintf("<%T>", t)
	}
}

func dumpList(terms []Term) string {
	var b strings.Builder
	for i, t := range terms {
		if i > 0 {
			b.WriteString(", ")
		}
		b.WriteString(dumpTerm(t))
	}
	return b.String()
}

func dumpBounds(min, max int) string {
	if min == 0 && max == 0 {
		return "*"
	}
	if min == 0 && max == 1 {
		return "?"
	}
	if min == 1 && max == 0 {
		return "+"
	}
	if max == 0 {
		return fmt.Sprintf("%d,", min)
	}
	return fmt.Sprintf("%d,%d", min, max)
}
