// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package eval

import (
	"os"
	"testing"
)

func TestLookOracleMissing(t *testing.T) {
	o, limit := LookOracle([]string{"xbnf-oracle-not-installed-xyz"}, "x", "y")
	if o != nil {
		t.Fatal("missing binary must not yield an oracle")
	}
	if limit == "" {
		t.Fatal("want a recorded limit")
	}
}

func TestCmdOracleEcho(t *testing.T) {
	o, limit := LookOracle([]string{"true"}, "true", "exit 0")
	if limit != "" || o == nil {
		t.Fatalf("true: %s", limit)
	}
	ok, err := o.Recognize([]byte("unused"))
	if err != nil || !ok {
		t.Fatalf("true: ok=%v err=%v", ok, err)
	}
	o, limit = LookOracle([]string{"false"}, "false", "exit 1")
	if limit != "" || o == nil {
		t.Fatalf("false: %s", limit)
	}
	ok, err = o.Recognize([]byte("unused"))
	if err != nil || ok {
		t.Fatalf("false: ok=%v err=%v", ok, err)
	}
}

func TestGoParserRecognize(t *testing.T) {
	ok, err := (GoParser{}).Recognize([]byte("package p\nfunc F() {}\n"))
	if err != nil || !ok {
		t.Fatalf("valid: ok=%v err=%v", ok, err)
	}
	ok, err = (GoParser{}).Recognize([]byte("package p\nfunc F( {\n"))
	if err != nil || ok {
		t.Fatalf("invalid: ok=%v err=%v", ok, err)
	}
	src, err := os.ReadFile("testdata/go/corpus/errors.go")
	if err != nil {
		t.Fatal(err)
	}
	ok, err = (GoParser{}).Recognize(src)
	if err != nil || !ok {
		t.Fatalf("errors.go: ok=%v err=%v", ok, err)
	}
	src, err = os.ReadFile("testdata/go/corpus/issue11377.src")
	if err != nil {
		t.Fatal(err)
	}
	ok, err = (GoParser{}).Recognize(src)
	if err != nil || ok {
		t.Fatalf("issue11377.src: ok=%v err=%v", ok, err)
	}
}
