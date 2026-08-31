// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package main

import (
	"fmt"
	"os"

	"github.com/marcelocantos/xbnf/fromwbnf"
)

func runFromWbnf(args []string) int {
	if len(args) != 1 {
		fmt.Fprintln(os.Stderr, "usage: xbnf from-wbnf file.wbnf")
		return 2
	}
	src, err := os.ReadFile(args[0])
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	out, err := fromwbnf.Convert(src)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	fmt.Print(out)
	return 0
}
