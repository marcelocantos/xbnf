// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package main

import (
	"flag"
	"fmt"
	"os"

	"github.com/marcelocantos/xbnf"
)

func main() {
	os.Exit(run(os.Args[1:]))
}

func run(args []string) int {
	fs := flag.NewFlagSet("xbnf", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	showVersion := fs.Bool("version", false, "print version")
	helpAgent := fs.Bool("help-agent", false, "print CLI help and the agent guide")
	if err := fs.Parse(args); err != nil {
		if err == flag.ErrHelp {
			return 0
		}
		return 2
	}
	if *showVersion {
		fmt.Println(xbnf.Version)
		return 0
	}
	if *helpAgent {
		fs.SetOutput(os.Stdout)
		fs.Usage()
		fmt.Fprint(os.Stdout, "\n")
		fmt.Fprint(os.Stdout, xbnf.AgentGuide)
		return 0
	}
	if fs.NArg() == 0 {
		fs.Usage()
		return 2
	}
	fmt.Fprintln(os.Stderr, "xbnf: engine not implemented yet")
	return 2
}
