// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package main

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"

	"github.com/marcelocantos/xbnf/eval"
)

func runEval(args []string) int {
	out := ""
	self := 0
	var rest []string
	for i := 0; i < len(args); i++ {
		switch {
		case args[i] == "-o" && i+1 < len(args):
			out = args[i+1]
			i++
		case strings.HasPrefix(args[i], "-o="):
			out = strings.TrimPrefix(args[i], "-o=")
		case args[i] == "-self":
			self = 8
		default:
			rest = append(rest, args[i])
		}
	}
	if len(rest) != 1 {
		fmt.Fprintln(os.Stderr, "usage: xbnf eval [-o report.json] [-self] <manifest.json>")
		return 2
	}
	rep, err := eval.Evaluate(&eval.Args{
		Manifest:    rest[0],
		WriteReport: out,
		NoisePairs:  self,
	})
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	if err := enc.Encode(rep); err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	if rep.Failed > 0 {
		return 1
	}
	if rep.Noise != nil && rep.Noise.Verdict == "SELF-FAIL" {
		return 2
	}
	return 0
}
