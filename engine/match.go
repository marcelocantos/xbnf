// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

// Package engine matches input against a grammar.Grammar using GLL with a DFA
// terminal layer for regular-fragment rules.
package engine

import (
	"fmt"

	"github.com/marcelocantos/xbnf/grammar"
)

// Result is the outcome of Parse.
type Result struct {
	OK     bool   `json:"ok"`
	Error  string `json:"error,omitempty"`
	End    int    `json:"end,omitempty"`
	Tree   Node   `json:"tree,omitempty"`
	Packed int    `json:"packed,omitempty"`
}

// Node is a sandbox-facing parse tree.
type Node struct {
	Kind     string `json:"kind"`
	Name     string `json:"name,omitempty"`
	Text     string `json:"text,omitempty"`
	Children []Node `json:"children,omitempty"`
}

func rejectLater(stmts []grammar.Stmt) error {
	var walkTerm func(grammar.Term) error
	var walkStmts func([]grammar.Stmt) error
	walkTerm = func(t grammar.Term) error {
		switch x := t.(type) {
		case grammar.Stack:
			for _, l := range x.Levels {
				if err := walkTerm(l); err != nil {
					return err
				}
			}
		case grammar.Alt:
			for _, c := range x.Terms {
				if err := walkTerm(c); err != nil {
					return err
				}
			}
		case grammar.Seq:
			for _, c := range x.Terms {
				if err := walkTerm(c); err != nil {
					return err
				}
			}
		case grammar.Named:
			return walkTerm(x.Term)
		case grammar.Leaf:
			return walkTerm(x.Term)
		case grammar.Quant:
			return walkTerm(x.Term)
		case grammar.Delim:
			if err := walkTerm(x.Term); err != nil {
				return err
			}
			return walkTerm(x.Sep)
		case grammar.Scope:
			if err := walkStmts(x.Decls); err != nil {
				return err
			}
			return walkTerm(x.Term)
		case grammar.Lookahead:
			return walkTerm(x.Term)
		case grammar.NegLookahead:
			return walkTerm(x.Term)
		case grammar.Ref:
			return fmt.Errorf("first-slice runner: %% ref is not executed")
		case grammar.ExtRef:
			return fmt.Errorf("first-slice runner: %%%% extref is not executed")
		case grammar.MacroCall:
			return fmt.Errorf("first-slice runner: macro call is not executed")
		case grammar.PosProp:
			return fmt.Errorf("first-slice runner: @%s is not executed", x.Name)
		}
		return nil
	}
	walkStmts = func(stmts []grammar.Stmt) error {
		for _, st := range stmts {
			switch s := st.(type) {
			case grammar.Import:
				return fmt.Errorf("first-slice runner: #import is not executed")
			case grammar.Macro:
				return fmt.Errorf("first-slice runner: #macro is not executed")
			case grammar.Rule:
				if err := walkTerm(s.Body); err != nil {
					return err
				}
			case grammar.Wrap:
				if err := walkTerm(s.Body); err != nil {
					return err
				}
			}
		}
		return nil
	}
	return walkStmts(stmts)
}
