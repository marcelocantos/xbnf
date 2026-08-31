// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package grammar

// Term is a grammar expression. Combinators and atoms are distinct types.
type Term interface {
	Kind() Kind
	term()
}

// Unbounded is Quant.Max when there is no upper bound (`*`, `+`, `{n,}`).
const Unbounded = -1

// Stack is precedence levels separated by `>`. A level may itself be a Scope.
type Stack struct {
	Levels []Term
}

func (Stack) term()      {}
func (Stack) Kind() Kind { return KindStack }

// Alt is unordered alternation (`a | b | c`).
type Alt struct {
	Terms []Term
}

func (Alt) term()      {}
func (Alt) Kind() Kind { return KindAlt }

// Seq is juxtaposition (`a b c`) with optional trailing directives.
type Seq struct {
	Terms      []Term
	Directives []Directive
}

func (Seq) term()      {}
func (Seq) Kind() Kind { return KindSeq }

// Directive is `#name` or `#name=value` trailing a sequence.
type Directive struct {
	Name  string
	Value string
}

// Named is `name=term`.
type Named struct {
	Name string
	Term Term
}

func (Named) term()      {}
func (Named) Kind() Kind { return KindNamed }

// Quant is repetition of Term. Max is inclusive; Unbounded means no upper bound.
type Quant struct {
	Term Term
	Min  int
	Max  int
}

func (Quant) term()      {}
func (Quant) Kind() Kind { return KindQuant }

// Delim is delimited repetition: Term separated by Sep (`x:","`).
// Leading and Trailing allow a separator before the first / after the last item.
type Delim struct {
	Term     Term
	Sep      Term
	Leading  bool
	Trailing bool
}

func (Delim) term()      {}
func (Delim) Kind() Kind { return KindDelim }

// Scope is `{ decls term }`: nested rules and pragmas around a body.
// It is the `{ scope }` attached to a stack level, and a rule body that
// introduces a local wrap or local rules.
type Scope struct {
	Decls []Stmt
	Term  Term
}

func (Scope) term()      {}
func (Scope) Kind() Kind { return KindScope }

// Ident is a rule reference, optionally filtered by `::label`.
type Ident struct {
	Name  string
	Label string
}

func (Ident) term()      {}
func (Ident) Kind() Kind { return KindIdent }

// String is a quoted literal. Text is the intended match, not the quotes.
type String struct {
	Text string
}

func (String) term()      {}
func (String) Kind() Kind { return KindString }

// CharClass is `[...]`. Elems are single atoms or ranges.
type CharClass struct {
	Negated bool
	Elems   []ClassElem
}

func (CharClass) term()      {}
func (CharClass) Kind() Kind { return KindCharClass }

// ClassElem is one character-class item. Hi is empty when the item is not a range.
type ClassElem struct {
	Lo string
	Hi string
}

// Escape is a backslash class such as `\d`. Code is the character after `\`.
type Escape struct {
	Code string
}

func (Escape) term()      {}
func (Escape) Kind() Kind { return KindEscape }

// Leaf is `/term/`: match Term (the same language) and emit one string.
type Leaf struct {
	Term Term
}

func (Leaf) term()      {}
func (Leaf) Kind() Kind { return KindLeaf }

// AnyChar is `.`.
type AnyChar struct{}

func (AnyChar) term()      {}
func (AnyChar) Kind() Kind { return KindAnyChar }

// Ref is `%name` or `%name="default"`.
type Ref struct {
	Name    string
	Default string
}

func (Ref) term()      {}
func (Ref) Kind() Kind { return KindRef }

// ExtRef is `%%name`.
type ExtRef struct {
	Name string
}

func (ExtRef) term()      {}
func (ExtRef) Kind() Kind { return KindExtRef }

// MacroCall is `%!name(args)`.
type MacroCall struct {
	Name string
	Args []Term
}

func (MacroCall) term()      {}
func (MacroCall) Kind() Kind { return KindMacroCall }

// Lookahead is `(?=term)`.
type Lookahead struct {
	Term Term
}

func (Lookahead) term()      {}
func (Lookahead) Kind() Kind { return KindLookahead }

// NegLookahead is `(?!term)`.
type NegLookahead struct {
	Term Term
}

func (NegLookahead) term()      {}
func (NegLookahead) Kind() Kind { return KindNegLookahead }

// Self is `@`, a stack self-reference.
type Self struct{}

func (Self) term()      {}
func (Self) Kind() Kind { return KindSelf }

// PosProp is `@name` or `@name(op arg)`, e.g. `@col` and `@col(=level)`.
type PosProp struct {
	Name string
	Op   string
	Arg  string
}

func (PosProp) term()      {}
func (PosProp) Kind() Kind { return KindPosProp }

// Empty is `()`.
type Empty struct{}

func (Empty) term()      {}
func (Empty) Kind() Kind { return KindEmpty }
