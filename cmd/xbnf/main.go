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
	if len(args) > 0 && args[0] == "from-wbnf" {
		return runFromWbnf(args[1:])
	}
	if len(args) > 0 && args[0] == "parse" {
		return runParse(args[1:])
	}
	sandboxCmd := false
	if len(args) > 0 && args[0] == "sandbox" {
		sandboxCmd = true
		args = args[1:]
	}

	fs := flag.NewFlagSet("xbnf", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	fs.Usage = func() {
		out := fs.Output()
		fmt.Fprintf(out, "Usage of xbnf:\n")
		fmt.Fprintf(out, "  xbnf -version\n")
		fmt.Fprintf(out, "  xbnf -help-agent\n")
		fmt.Fprintf(out, "  xbnf sandbox [-bind %s] [-port %d]\n", sandboxBindDefault, sandboxPortDefault)
		fmt.Fprintf(out, "  xbnf -sandbox [-bind %s] [-port %d]\n", sandboxBindDefault, sandboxPortDefault)
		fmt.Fprintf(out, "  xbnf from-wbnf file.wbnf\n")
		fmt.Fprintf(out, "  xbnf -from-wbnf file.wbnf\n")
		fmt.Fprintf(out, "  xbnf parse grammar.xbnf input\n")
		fmt.Fprintf(out, "  xbnf --explain grammar.xbnf\n\n")
		fs.PrintDefaults()
	}
	showVersion := fs.Bool("version", false, "print version")
	helpAgent := fs.Bool("help-agent", false, "print CLI help and the agent guide")
	sandboxFlag := fs.Bool("sandbox", false, "host the language sandbox")
	fromWbnf := fs.String("from-wbnf", "", "convert a .wbnf grammar to xbnf on stdout")
	explain := fs.Bool("explain", false, "print DFA vs GLL promotion for a grammar")
	bind := fs.String("bind", sandboxBindDefault, "sandbox listen address")
	port := fs.Int("port", sandboxPortDefault, "sandbox listen port")
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
	if sandboxCmd || *sandboxFlag {
		if fs.NArg() != 0 {
			fmt.Fprintln(os.Stderr, "xbnf sandbox: unexpected argument")
			return 2
		}
		return runSandbox(*bind, *port)
	}
	if *fromWbnf != "" {
		if fs.NArg() != 0 {
			fmt.Fprintln(os.Stderr, "xbnf -from-wbnf: unexpected argument")
			return 2
		}
		return runFromWbnf([]string{*fromWbnf})
	}
	if *explain {
		if fs.NArg() != 1 {
			fmt.Fprintln(os.Stderr, "usage: xbnf --explain <grammar.xbnf>")
			return 2
		}
		return runExplain(fs.Arg(0))
	}
	if fs.NArg() == 0 {
		fs.Usage()
		return 2
	}
	fmt.Fprintln(os.Stderr, "usage: xbnf parse <grammar.xbnf> <input>")
	return 2
}
