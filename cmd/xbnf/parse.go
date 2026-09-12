// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package main

import (
	"bufio"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strconv"

	"github.com/marcelocantos/xbnf/engine"
	"github.com/marcelocantos/xbnf/grammar"
	"github.com/marcelocantos/xbnf/syntax"
)

func runParse(args []string) int {
	fs := flag.NewFlagSet("parse", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	fs.Usage = func() {
		fmt.Fprintln(fs.Output(), "usage: xbnf parse [-json] <grammar.xbnf> <input>")
		fs.PrintDefaults()
	}
	asJSON := fs.Bool("json", false, "print the engine.Result as JSON instead of a listing")
	if err := fs.Parse(args); err != nil {
		if err == flag.ErrHelp {
			return 0
		}
		return 2
	}
	if fs.NArg() != 2 {
		fs.Usage()
		return 2
	}
	g, err := loadGrammar(fs.Arg(0))
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	in, err := os.ReadFile(fs.Arg(1))
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	opts := &engine.CompileOpts{
		ReadImport: func(p string) ([]byte, error) {
			if !filepath.IsAbs(p) {
				p = filepath.Join(filepath.Dir(fs.Arg(0)), p)
			}
			return os.ReadFile(p)
		},
	}
	res := engine.ParseWith(g, "", string(in), opts)
	// A failed parse still shows what it recognised; the error goes to stderr.
	if *asJSON {
		enc := json.NewEncoder(os.Stdout)
		if err := enc.Encode(res); err != nil {
			fmt.Fprintln(os.Stderr, err)
			return 1
		}
	} else {
		w := bufio.NewWriter(os.Stdout)
		writeListing(w, res)
		if err := w.Flush(); err != nil {
			fmt.Fprintln(os.Stderr, err)
			return 1
		}
	}
	if !res.OK {
		fmt.Fprintln(os.Stderr, res.Error)
		return 1
	}
	return 0
}

// writeListing prints the event stream one node per line, indented by depth:
// "kind name" for a node with children, "kind name "text"" for a leaf, and
// "· "text"" for skipped wrap. It is the same view docs/syntax.html renders.
func writeListing(w *bufio.Writer, res *engine.Result) {
	depth, cur := 0, 0
	for _, e := range res.Events {
		switch e.Op {
		case engine.OpClose:
			depth--
			continue
		case engine.OpOpen:
			writeIndent(w, depth)
			w.WriteString(e.Kind.String())
			if e.Name != "" {
				w.WriteByte(' ')
				w.WriteString(e.Name)
			}
			w.WriteByte('\n')
			depth++
			continue
		}
		n := max(int(e.Len), 0)
		text := res.Input[cur:min(cur+n, len(res.Input))]
		cur += n
		writeIndent(w, depth)
		if e.Op == engine.OpSkip {
			w.WriteString("·")
		} else {
			w.WriteString(e.Kind.String())
			if e.Name != "" {
				w.WriteByte(' ')
				w.WriteString(e.Name)
			}
		}
		w.WriteByte(' ')
		w.WriteString(strconv.Quote(text))
		w.WriteByte('\n')
	}
}

func writeIndent(w *bufio.Writer, depth int) {
	for i := 0; i < depth; i++ {
		w.WriteString("  ")
	}
}

func runExplain(path string) int {
	g, err := loadGrammar(path)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	c, err := engine.CompileWith(g, &engine.CompileOpts{
		ReadImport: func(p string) ([]byte, error) {
			if !filepath.IsAbs(p) {
				p = filepath.Join(filepath.Dir(path), p)
			}
			return os.ReadFile(p)
		},
	})
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
