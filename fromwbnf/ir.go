// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

// Package fromwbnf holds the meaning of an old .wbnf grammar: productions
// and terms as written (macros not expanded, no cut-points). Convert emits
// equivalent xbnf source. It is not a parser engine.
package fromwbnf

// File is a parsed .wbnf grammar.
type File struct {
	Stmts []Stmt
}

// Stmt is a production, import, or macro definition.
type Stmt interface {
	stmt()
}

// Prod is `NAME -> term ;`.
type Prod struct {
	Name string
	Body Term
}

func (Prod) stmt() {}

// Import is `.import path`.
type Import struct {
	Path string
}

func (Import) stmt() {}

// MacroDef is `.macro name(args) { term }`.
type MacroDef struct {
	Name string
	Args []string
	Body Term
}

func (MacroDef) stmt() {}

// Term is a wbnf expression.
type Term interface {
	term()
}

// Seq is juxtaposition.
type Seq []Term

func (Seq) term() {}

// Oneof is ordered choice (`|` in wbnf).
type Oneof []Term

func (Oneof) term() {}

// Stack is precedence (`>`).
type Stack []Term

func (Stack) term() {}

// Quant is repetition. Max 0 means unbounded, matching wbnf.
type Quant struct {
	Term Term
	Min  int
	Max  int
}

func (Quant) term() {}

// Delim is `term assoc sep` with optional leading/trailing sep.
type Delim struct {
	Term     Term
	Sep      Term
	Assoc    string // ":", ":>", or "<:"
	Leading  bool
	Trailing bool
}

func (Delim) term() {}

// Named is `name=term`.
type Named struct {
	Name string
	Term Term
}

func (Named) term() {}

// Ident is a rule name, including `@` and `.wrapRE`.
type Ident string

func (Ident) term() {}

// String is an unquoted string value.
type String string

func (String) term() {}

// RE is a regex terminal after wbnf's whitespace stripping.
type RE string

func (RE) term() {}

// Ref is `%IDENT` with optional default string.
type Ref struct {
	Ident   string
	Default *String
}

func (Ref) term() {}

// ExtRef is `%%IDENT`.
type ExtRef string

func (ExtRef) term() {}

// Lookahead is `(?=term)`.
type Lookahead struct {
	Term Term
}

func (Lookahead) term() {}

// Scoped nests a grammar around a term.
type Scoped struct {
	Term Term
	Body *File
}

func (Scoped) term() {}

// MacroCall is `%!name(args)`.
type MacroCall struct {
	Name string
	Args []Term
}

func (MacroCall) term() {}

// Empty is `()`.
type Empty struct{}

func (Empty) term() {}
