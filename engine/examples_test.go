// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package engine_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/marcelocantos/xbnf/engine"
	"github.com/marcelocantos/xbnf/syntax"
)

type workedExample struct {
	ID      string `json:"id"`
	Slice   string `json:"slice"`
	File    string `json:"file"`
	Grammar string `json:"grammar"`
	Input   string `json:"input"`
	Start   string `json:"start"`
}

func loadWorkedExamples(t *testing.T) (string, []workedExample) {
	t.Helper()
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller")
	}
	docs := filepath.Join(filepath.Dir(file), "..", "docs")
	b, err := os.ReadFile(filepath.Join(docs, "examples.json"))
	if err != nil {
		t.Fatal(err)
	}
	var xs []workedExample
	if err := json.Unmarshal(b, &xs); err != nil {
		t.Fatal(err)
	}
	if len(xs) < 4 {
		t.Fatalf("too few worked examples: %d", len(xs))
	}
	return docs, xs
}

func TestWorkedExamplesRun(t *testing.T) {
	t.Parallel()
	docs, xs := loadWorkedExamples(t)
	core := 0
	for _, ex := range xs {
		ex := ex
		if ex.Slice == "later" {
			continue
		}
		core++
		t.Run(ex.ID, func(t *testing.T) {
			t.Parallel()
			src := []byte(ex.Grammar)
			if ex.File != "" {
				b, err := os.ReadFile(filepath.Join(docs, ex.File))
				if err != nil {
					t.Fatal(err)
				}
				src = b
			}
			if len(src) == 0 {
				t.Fatal("empty grammar")
			}
			g, err := syntax.Parse(src)
			if err != nil {
				t.Fatalf("parse grammar: %v\n%s", err, src)
			}
			res := engine.Parse(g, ex.Start, ex.Input)
			if !res.OK {
				t.Fatalf("%s: %s\ninput: %q", ex.ID, res.Error, ex.Input)
			}
		})
	}
	if core < 4 {
		t.Fatalf("want at least 4 first-slice examples, got %d", core)
	}
}
