// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package main

import (
	"fmt"
	"os"

	"github.com/marcelocantos/xbnf/engine"
	"github.com/marcelocantos/xbnf/grammar"
	"github.com/marcelocantos/xbnf/syntax"
)

func runParse(args []string) int {
	if len(args) != 2 {
		fmt.Fprintln(os.Stderr, "usage: xbnf parse <grammar.xbnf> <input>")
		return 2
	}
	g, err := loadGrammar(args[0])
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	in, err := os.ReadFile(args[1])
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	res := engine.Parse(g, "", string(in))
	if !res.OK {
		fmt.Fprintln(os.Stderr, res.Error)
		return 1
	}
	return 0
}

func runExplain(path string) int {
	g, err := loadGrammar(path)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	c, err := engine.Compile(g)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	fmt.Print(c.Promotion().String())
	return 0
}

func loadGrammar(path string) (*grammar.Grammar, error) {
	src, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	return syntax.Parse(src)
}
