// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package engine_test

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/marcelocantos/xbnf/engine"
	"github.com/marcelocantos/xbnf/syntax"
)

func docsFile(t *testing.T, rel string) []byte {
	t.Helper()
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller")
	}
	p := filepath.Join(filepath.Dir(file), "..", rel)
	b, err := os.ReadFile(p)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func TestSelfHostXBNF(t *testing.T) {
	t.Parallel()
	src := docsFile(t, "docs/xbnf.xbnf")
	g, err := syntax.Parse(src)
	if err != nil {
		t.Fatal(err)
	}
	res := engine.Parse(g, "grammar", string(src))
	if !res.OK {
		t.Fatalf("xbnf.xbnf: %s", res.Error)
	}
}

func TestSelfHostJSON(t *testing.T) {
	t.Parallel()
	src := docsFile(t, "docs/examples/json.xbnf")
	g, err := syntax.Parse(src)
	if err != nil {
		t.Fatal(err)
	}
	sample := `{"a": [true, false, null], "b": {"n": -1.5e+2, "s": "x"}}`
	res := engine.Parse(g, "json", sample)
	if !res.OK {
		t.Fatalf("json sample: %s", res.Error)
	}
}

func TestSelfHostCalc(t *testing.T) {
	t.Parallel()
	src := docsFile(t, "docs/examples/calc.xbnf")
	g, err := syntax.Parse(src)
	if err != nil {
		t.Fatal(err)
	}
	for _, sample := range []string{`1+2*3`, `(1+2)*3`, `let x = 1+2; x`} {
		res := engine.Parse(g, "expr", sample)
		if !res.OK {
			t.Fatalf("calc %q: %s", sample, res.Error)
		}
	}
}
