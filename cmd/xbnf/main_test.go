// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package main

import (
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/marcelocantos/xbnf"
)

var captureMu sync.Mutex

func TestRunVersion(t *testing.T) {
	code, stdout, stderr := capture(t, []string{"-version"})
	if code != 0 {
		t.Fatalf("version exit: %d", code)
	}
	if got := strings.TrimSpace(stdout); got != xbnf.Version {
		t.Fatalf("version: got %q want %q", got, xbnf.Version)
	}
	if stderr != "" {
		t.Fatalf("version stderr: %q", stderr)
	}
}

func TestRunHelpAgent(t *testing.T) {
	code, stdout, stderr := capture(t, []string{"-help-agent"})
	if code != 0 {
		t.Fatalf("help-agent exit: %d", code)
	}
	if stderr != "" {
		t.Fatalf("help-agent stderr: %q", stderr)
	}
	if !strings.Contains(stdout, "Usage of xbnf") {
		t.Fatalf("help-agent missing usage: %q", stdout)
	}
	if !strings.Contains(stdout, xbnf.AgentGuide) {
		t.Fatal("help-agent missing agent guide")
	}
}

func TestRunUnknownFlag(t *testing.T) {
	code, _, stderr := capture(t, []string{"-nope"})
	if code != 2 {
		t.Fatalf("unknown flag exit: %d", code)
	}
	if !strings.Contains(stderr, "nope") {
		t.Fatalf("unknown flag stderr: %q", stderr)
	}
}

func TestRunHelp(t *testing.T) {
	code, _, stderr := capture(t, []string{"-h"})
	if code != 0 {
		t.Fatalf("help exit: %d", code)
	}
	if !strings.Contains(stderr, "Usage of xbnf") {
		t.Fatalf("help stderr: %q", stderr)
	}
}

func TestRunFromWbnf(t *testing.T) {
	xml := "../../fromwbnf/testdata/xml.wbnf"
	for _, args := range [][]string{
		{"from-wbnf", xml},
		{"-from-wbnf", xml},
	} {
		code, stdout, stderr := capture(t, args)
		if code != 0 {
			t.Fatalf("%v exit %d stderr %q", args, code, stderr)
		}
		if !strings.Contains(stdout, "#wrap") || !strings.Contains(stdout, "|>") {
			t.Fatalf("%v output: %q", args, stdout)
		}
		if stderr != "" {
			t.Fatalf("%v stderr: %q", args, stderr)
		}
	}
}

func parseFixture(t *testing.T) (grammar, okIn, badIn string) {
	t.Helper()
	dir := t.TempDir()
	g := filepath.Join(dir, "g.xbnf")
	ok := filepath.Join(dir, "ok.txt")
	bad := filepath.Join(dir, "bad.txt")
	src := "S -> \"(\" S \")\" | A ;\nA -> /[a-z]+/ ;\n"
	if err := os.WriteFile(g, []byte(src), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(ok, []byte("(hi)"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(bad, []byte("1"), 0o644); err != nil {
		t.Fatal(err)
	}
	return g, ok, bad
}

func TestRunParseOK(t *testing.T) {
	g, in, _ := parseFixture(t)
	code, stdout, stderr := capture(t, []string{"parse", g, in})
	if code != 0 {
		t.Fatalf("parse exit %d stderr %q stdout %q", code, stderr, stdout)
	}
	if strings.Contains(stderr, "engine not implemented") {
		t.Fatalf("stub still in stderr: %q", stderr)
	}
}

func TestRunParseFail(t *testing.T) {
	g, _, bad := parseFixture(t)
	code, _, stderr := capture(t, []string{"parse", g, bad})
	if code == 0 {
		t.Fatal("want non-zero")
	}
	if !strings.Contains(stderr, ":") {
		t.Fatalf("want position: %q", stderr)
	}
	if !strings.Contains(stderr, "expected") && !strings.Contains(stderr, "S") && !strings.Contains(stderr, "A") {
		t.Fatalf("want expected terminals or rule: %q", stderr)
	}
}

func TestRunExplain(t *testing.T) {
	g, _, _ := parseFixture(t)
	code, stdout, stderr := capture(t, []string{"-explain", g})
	if code != 0 {
		t.Fatalf("explain exit %d stderr %q", code, stderr)
	}
	if !strings.Contains(stdout, "DFA") || !strings.Contains(stdout, "A") {
		t.Fatalf("want DFA rule A: %q", stdout)
	}
	if !strings.Contains(stdout, "GLL") || !strings.Contains(stdout, "S") {
		t.Fatalf("want GLL forking S: %q", stdout)
	}
	if !strings.Contains(stdout, "forking") {
		t.Fatalf("want forking: %q", stdout)
	}
}

func TestRunHelpMentionsParse(t *testing.T) {
	code, _, stderr := capture(t, []string{"-h"})
	if code != 0 {
		t.Fatalf("help exit: %d", code)
	}
	if !strings.Contains(stderr, "parse") || !strings.Contains(stderr, "explain") {
		t.Fatalf("help missing parse/explain: %q", stderr)
	}
}

func TestRunHelpMentionsFromWbnf(t *testing.T) {
	code, _, stderr := capture(t, []string{"-h"})
	if code != 0 {
		t.Fatalf("help exit: %d", code)
	}
	if !strings.Contains(stderr, "-from-wbnf") {
		t.Fatalf("help missing -from-wbnf: %q", stderr)
	}
}

func TestRunNoArgs(t *testing.T) {
	code, _, stderr := capture(t, nil)
	if code != 2 {
		t.Fatalf("no-args exit: %d", code)
	}
	if !strings.Contains(stderr, "Usage of xbnf") {
		t.Fatalf("no-args stderr: %q", stderr)
	}
}

func capture(t *testing.T, args []string) (int, string, string) {
	t.Helper()
	captureMu.Lock()
	defer captureMu.Unlock()
	oldOut, oldErr := os.Stdout, os.Stderr
	outR, outW, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	errR, errW, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	os.Stdout, os.Stderr = outW, errW
	var outB, errB []byte
	var wg sync.WaitGroup
	wg.Add(2)
	go func() { defer wg.Done(); outB, _ = io.ReadAll(outR) }()
	go func() { defer wg.Done(); errB, _ = io.ReadAll(errR) }()
	code := run(args)
	_ = outW.Close()
	_ = errW.Close()
	wg.Wait()
	os.Stdout, os.Stderr = oldOut, oldErr
	_ = outR.Close()
	_ = errR.Close()
	return code, string(outB), string(errB)
}
