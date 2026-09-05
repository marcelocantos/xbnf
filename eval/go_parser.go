// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package eval

import (
	"go/parser"
	"go/token"
)

// GoParser is go/parser.ParseFile as a syntax oracle. It does not run
// go/types, the compiler, or the program.
type GoParser struct{}

func (GoParser) Name() string { return "go/parser.ParseFile" }

func (GoParser) Work() string {
	return "syntax only (parser.AllErrors|SkipObjectResolution); not go/types, not compile"
}

func (GoParser) Recognize(input []byte) (bool, error) {
	fset := token.NewFileSet()
	_, err := parser.ParseFile(fset, "stdin.go", input, parser.AllErrors|parser.SkipObjectResolution)
	if err != nil {
		return false, nil
	}
	return true, nil
}
