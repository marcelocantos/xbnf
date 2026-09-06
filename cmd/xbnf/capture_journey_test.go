// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package main

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"testing"
	"time"
)

// TestCaptureJourney verifies that the shipped CLI accepts a matching capture
// under either unordered alternative order, and rejects an impossible copy.
func TestCaptureJourney(t *testing.T) {
	dir := t.TempDir()
	binary := filepath.Join(dir, "xbnf")
	if runtime.GOOS == "windows" {
		binary += ".exe"
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	build := exec.CommandContext(ctx, "go", "build", "-o", binary, ".")
	build.Env = append(os.Environ(), "GOWORK=off")
	if out, err := build.CombinedOutput(); err != nil {
		t.Fatalf("build shipped CLI: %v\n%s", err, out)
	}
	for _, order := range []struct {
		name string
		rule string
	}{
		{"short first", `A -> (?="a") "a" #prefer | (?="a") "aa" ;`},
		{"long first", `A -> (?="a") "aa" | (?="a") "a" #prefer ;`},
	} {
		t.Run(order.name, func(t *testing.T) {
			grammarPath := filepath.Join(dir, "capture.xbnf")
			src := "s -> n=A B \"!\" %n ;\n" + order.rule + "\n" +
				"B -> (?=\"a\") \"a\" #prefer | (?=\"a\") \"aa\" ;\n#wrap -> () ;\n"
			if err := os.WriteFile(grammarPath, []byte(src), 0o600); err != nil {
				t.Fatal(err)
			}
			for _, input := range []struct {
				text string
				ok   bool
			}{
				{"aaa!a", true},
				{"aaa!aaa", false},
			} {
				t.Run(input.text, func(t *testing.T) {
					inputPath := filepath.Join(dir, "input.txt")
					if err := os.WriteFile(inputPath, []byte(input.text), 0o600); err != nil {
						t.Fatal(err)
					}
					cmd := exec.CommandContext(ctx, binary, "parse", grammarPath, inputPath)
					out, err := cmd.CombinedOutput()
					if input.ok {
						if err != nil {
							t.Fatalf("CLI rejected matching capture: %v\n%s", err, out)
						}
						return
					}
					if exit, ok := err.(*exec.ExitError); !ok || exit.ExitCode() != 1 || len(out) == 0 {
						t.Fatalf("want parser rejection with exit 1, got %v\n%s", err, out)
					}
				})
			}
		})
	}
}
