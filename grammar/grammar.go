// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

// Package grammar holds the xbnf grammar IR.
//
// Types are constructed as composite literals. This package does not parse
// xbnf source; a bootstrap parser (T3) and the GLL engine (T4) both consume
// these values.
package grammar

import "strconv"

// Kind classifies a statement or term. Every construct in docs/xbnf.xbnf has
// a distinct kind except grouping, which is nested terms with no extra node.
type Kind int

const (
	KindRule Kind = iota + 1
	KindWrap
	KindImport
	KindMacro

	KindStack
	KindAlt
	KindSeq
	KindNamed
	KindQuant
	KindDelim
	KindScope

	KindIdent
	KindString
	KindCharClass
	KindEscape
	KindLeaf
	KindAnyChar
	KindRef
	KindExtRef
	KindMacroCall
	KindLookahead
	KindNegLookahead
	KindSelf
	KindPosProp
	KindEmpty
)

func (k Kind) String() string {
	names := [...]string{
		KindRule:         "Rule",
		KindWrap:         "Wrap",
		KindImport:       "Import",
		KindMacro:        "Macro",
		KindStack:        "Stack",
		KindAlt:          "Alt",
		KindSeq:          "Seq",
		KindNamed:        "Named",
		KindQuant:        "Quant",
		KindDelim:        "Delim",
		KindScope:        "Scope",
		KindIdent:        "Ident",
		KindString:       "String",
		KindCharClass:    "CharClass",
		KindEscape:       "Escape",
		KindLeaf:         "Leaf",
		KindAnyChar:      "AnyChar",
		KindRef:          "Ref",
		KindExtRef:       "ExtRef",
		KindMacroCall:    "MacroCall",
		KindLookahead:    "Lookahead",
		KindNegLookahead: "NegLookahead",
		KindSelf:         "Self",
		KindPosProp:      "PosProp",
		KindEmpty:        "Empty",
	}
	if int(k) < 0 || int(k) >= len(names) || names[k] == "" {
		return "Kind(" + strconv.Itoa(int(k)) + ")"
	}
	return names[k]
}

// Grammar is an ordered list of rules and pragmas.
type Grammar struct {
	Stmts []Stmt
}

// Stmt is a rule or pragma.
type Stmt interface {
	Kind() Kind
	stmt()
}

// Rule is `name mod* -> term ;`.
// Mods are identifier names without the `#` sigil (`lex`).
type Rule struct {
	Name string
	Mods []string
	Body Term
}

func (Rule) stmt()      {}
func (Rule) Kind() Kind { return KindRule }

// Wrap is `#wrap -> term ;`.
type Wrap struct {
	Body Term
}

func (Wrap) stmt()      {}
func (Wrap) Kind() Kind { return KindWrap }

// Import is `#import "path" ;`.
type Import struct {
	Path string
}

func (Import) stmt()      {}
func (Import) Kind() Kind { return KindImport }

// Macro is `#macro name(params) { body }`.
type Macro struct {
	Name   string
	Params []string
	Body   Term
}

func (Macro) stmt()      {}
func (Macro) Kind() Kind { return KindMacro }
