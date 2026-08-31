// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package main

import (
	"io"
	"os"
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
	code := run(args)
	_ = outW.Close()
	_ = errW.Close()
	os.Stdout, os.Stderr = oldOut, oldErr
	outB, readErr := io.ReadAll(outR)
	if readErr != nil {
		t.Fatal(readErr)
	}
	errB, readErr := io.ReadAll(errR)
	if readErr != nil {
		t.Fatal(readErr)
	}
	_ = outR.Close()
	_ = errR.Close()
	return code, string(outB), string(errB)
}
