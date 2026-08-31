// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package engine_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/marcelocantos/xbnf/engine"
	"github.com/marcelocantos/xbnf/syntax"
)

type clause struct {
	ID      string `json:"id"`
	Slice   string `json:"slice"`
	Grammar string `json:"grammar"`
	Input   string `json:"input"`
	Start   string `json:"start"`
	Title   string `json:"title"`
}

func loadClauses(t *testing.T) []clause {
	t.Helper()
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller")
	}
	p := filepath.Join(filepath.Dir(file), "..", "docs", "syntax-clauses.json")
	b, err := os.ReadFile(p)
	if err != nil {
		t.Fatal(err)
	}
	var cs []clause
	if err := json.Unmarshal(b, &cs); err != nil {
		t.Fatal(err)
	}
	if len(cs) < 20 {
		t.Fatalf("too few clauses: %d", len(cs))
	}
	return cs
}

func TestClausesRun(t *testing.T) {
	t.Parallel()
	for _, c := range loadClauses(t) {
		c := c
		t.Run(c.ID, func(t *testing.T) {
			t.Parallel()
			g, err := syntax.Parse([]byte(c.Grammar))
			if err != nil {
				t.Fatalf("parse grammar: %v\n%s", err, c.Grammar)
			}
			res := engine.Parse(g, c.Start, c.Input)
			if c.Slice == "later" {
				if res.OK {
					t.Fatalf("later clause %s silently succeeded", c.ID)
				}
				if !strings.Contains(res.Error, "first-slice") {
					t.Fatalf("later clause %s: want first-slice skip, got %q", c.ID, res.Error)
				}
				return
			}
			if !res.OK {
				t.Fatalf("%s: %s\ngrammar:\n%s\ninput: %q", c.ID, res.Error, c.Grammar, c.Input)
			}
		})
	}
}

func TestJSONTrue(t *testing.T) {
	t.Parallel()
	_, file, _, _ := runtime.Caller(0)
	p := filepath.Join(filepath.Dir(file), "..", "docs", "examples", "json.xbnf")
	b, err := os.ReadFile(p)
	if err != nil {
		t.Fatal(err)
	}
	g, err := syntax.Parse(b)
	if err != nil {
		t.Fatal(err)
	}
	res := engine.Parse(g, "json", `{"a": true}`)
	if !res.OK {
		t.Fatal(res.Error)
	}
	cases := []struct {
		in   string
		want bool
	}{
		{`{}`, true},
		{`{"a":true,"b":null}`, true},
		{`{"a":true,}`, false},
		{`{"a":true "b":false}`, false},
	}
	for _, c := range cases {
		got := engine.Parse(g, "json", c.in)
		if got.OK != c.want {
			t.Errorf("%s: ok=%v want %v (%s)", c.in, got.OK, c.want, got.Error)
		}
	}
}
