// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package eval

import (
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// CmdOracle runs an external recognizer with the fixture on stdin.
// Exit 0 is accept; any other exit is reject. A missing binary is an error
// so Evaluate can record an oracle limit instead of inventing results.
type CmdOracle struct {
	Argv []string
	N    string
	W    string
}

func (o CmdOracle) Name() string { return o.N }
func (o CmdOracle) Work() string { return o.W }

func (o CmdOracle) Recognize(input []byte) (bool, error) {
	if len(o.Argv) == 0 {
		return false, fmt.Errorf("empty oracle command")
	}
	cmd := exec.Command(o.Argv[0], o.Argv[1:]...)
	cmd.Stdin = bytes.NewReader(input)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	err := cmd.Run()
	if err == nil {
		return true, nil
	}
	if _, ok := err.(*exec.ExitError); ok {
		return false, nil
	}
	return false, fmt.Errorf("%s: %w (%s)", o.Argv[0], err, stderr.String())
}

// resolveOracleCmd makes relative script arguments findable from a language
// manifest directory or the repository root (two levels above testdata/lang).
func resolveOracleCmd(manifestDir string, argv []string) []string {
	out := append([]string{}, argv...)
	var roots []string
	dir := manifestDir
	for i := 0; i < 8; i++ {
		roots = append(roots, dir)
		parent := filepath.Clean(filepath.Join(dir, ".."))
		if parent == dir {
			break
		}
		dir = parent
	}
	roots = append(roots, ".")
	for i, a := range out {
		if i == 0 || a == "" || strings.HasPrefix(a, "-") || filepath.IsAbs(a) {
			continue
		}
		found := a
		for _, root := range roots {
			cand := filepath.Join(root, a)
			if st, err := os.Stat(cand); err == nil && !st.IsDir() {
				found = cand
				break
			}
			cand = filepath.Join(root, filepath.Base(a))
			if st, err := os.Stat(cand); err == nil && !st.IsDir() {
				found = cand
				break
			}
		}
		out[i] = found
	}
	return out
}

// LookOracle returns a CmdOracle if argv[0] is on PATH, or a reason the
// oracle cannot run.
func LookOracle(argv []string, name, work string) (Oracle, string) {
	if len(argv) == 0 {
		return nil, ""
	}
	if _, err := exec.LookPath(argv[0]); err != nil {
		return nil, argv[0] + " not on PATH; oracle skipped (limit, not a mock)"
	}
	if name == "" {
		name = argv[0]
	}
	if work == "" {
		work = "external command; stdin=fixture; exit 0 accept, else reject"
	}
	return CmdOracle{Argv: argv, N: name, W: work}, ""
}
