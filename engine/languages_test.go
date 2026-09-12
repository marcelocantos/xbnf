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

type languagePage struct {
	ID    string `json:"id"`
	Title string `json:"title"`
	Dir   string `json:"dir"`
	File  string `json:"file"`
}

type languageManifest struct {
	Language string `json:"language"`
	Role     string `json:"role"`
	Grammar  string `json:"grammar"`
	Start    string `json:"start"`
}

func loadLanguagePages(t *testing.T) (string, []languagePage) {
	t.Helper()
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller")
	}
	root := filepath.Join(filepath.Dir(file), "..")
	b, err := os.ReadFile(filepath.Join(root, "docs", "languages.json"))
	if err != nil {
		t.Fatal(err)
	}
	var xs []languagePage
	if err := json.Unmarshal(b, &xs); err != nil {
		t.Fatal(err)
	}
	if len(xs) != 7 {
		t.Fatalf("languages.json: %d entries, want 7 live tracks", len(xs))
	}
	return root, xs
}

func TestLanguagePageDefaultsRun(t *testing.T) {
	t.Parallel()
	root, xs := loadLanguagePages(t)
	for _, lang := range xs {
		lang := lang
		t.Run(lang.ID, func(t *testing.T) {
			t.Parallel()
			dir := filepath.Join(root, "eval", "testdata", lang.Dir)
			manRaw, err := os.ReadFile(filepath.Join(dir, "manifest.json"))
			if err != nil {
				t.Fatal(err)
			}
			var man languageManifest
			if err := json.Unmarshal(manRaw, &man); err != nil {
				t.Fatal(err)
			}
			if man.Role == "historical" {
				t.Fatalf("%s is historical; languages.json is the live set", lang.ID)
			}
			if man.Language != lang.ID && man.Language != lang.Dir {
				t.Fatalf("manifest language %q, page id %q", man.Language, lang.ID)
			}
			gpath := filepath.Join(dir, man.Grammar)
			if man.Grammar == "" {
				gpath = filepath.Join(dir, "grammar.xbnf")
			}
			src, err := os.ReadFile(gpath)
			if err != nil {
				t.Fatal(err)
			}
			in, err := os.ReadFile(filepath.Join(dir, lang.File))
			if err != nil {
				t.Fatal(err)
			}
			g, err := syntax.Parse(src)
			if err != nil {
				t.Fatalf("parse grammar: %v", err)
			}
			res := engine.Parse(g, man.Start, string(in))
			if !res.OK {
				t.Fatalf("%s %s: %s", lang.ID, lang.File, res.Error)
			}
		})
	}
}
