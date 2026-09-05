// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package eval

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"

	"github.com/marcelocantos/xbnf/engine"
	"github.com/marcelocantos/xbnf/syntax"
)

// Golden maps "<manifest dir>/<fixture path>" to the fingerprint of parsing
// that fixture with the manifest's grammar. It is the 🎯T25.1 ratchet: an
// engine change that alters any tree, end, packed count or error text must
// update the golden in the same commit with a reason.
type Golden map[string]engine.Fingerprint

// ManifestPaths lists every manifest.json under root, sorted.
func ManifestPaths(root string) ([]string, error) {
	ents, err := os.ReadDir(root)
	if err != nil {
		return nil, err
	}
	var out []string
	for _, e := range ents {
		if !e.IsDir() {
			continue
		}
		p := filepath.Join(root, e.Name(), "manifest.json")
		if _, err := os.Stat(p); err == nil {
			out = append(out, p)
		}
	}
	sort.Strings(out)
	return out, nil
}

// ComputeGolden parses every fixture of every manifest under root.
func ComputeGolden(root string) (Golden, error) {
	paths, err := ManifestPaths(root)
	if err != nil {
		return nil, err
	}
	g := Golden{}
	for _, mp := range paths {
		m, err := LoadManifest(mp)
		if err != nil {
			return nil, err
		}
		src, err := os.ReadFile(m.GrammarAbs)
		if err != nil {
			return nil, err
		}
		gr, err := syntax.Parse(src)
		if err != nil {
			return nil, fmt.Errorf("%s: grammar parse: %w", mp, err)
		}
		c, err := engine.Compile(gr)
		if err != nil {
			return nil, fmt.Errorf("%s: compile: %w", mp, err)
		}
		dir := filepath.Base(m.Dir)
		for _, f := range m.Files {
			data, err := os.ReadFile(f.AbsPath)
			if err != nil {
				return nil, err
			}
			g[dir+"/"+f.Path] = engine.FingerprintResult(c.Parse(m.Start, string(data)))
		}
	}
	return g, nil
}
