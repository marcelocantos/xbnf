// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package eval

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/marcelocantos/xbnf/engine"
	"github.com/marcelocantos/xbnf/syntax"
)

// BenchmarkCorpus is the cross-language half of the keep/discard gate
// (🎯T25.2). Each sub-benchmark parses every accept fixture of one live
// manifest once per iteration on a warm Compiled (warm varied). Bytes are the
// summed fixture sizes so ns/op and MB/s are comparable across languages.
func BenchmarkCorpus(b *testing.B) {
	paths, err := ManifestPaths("testdata")
	if err != nil {
		b.Fatal(err)
	}
	for _, mp := range paths {
		m, err := LoadManifest(mp)
		if err != nil {
			b.Fatal(err)
		}
		if !m.Live() || filepath.Base(m.Dir) == "json-smoke" {
			continue
		}
		src, err := os.ReadFile(m.GrammarAbs)
		if err != nil {
			b.Fatal(err)
		}
		g, err := syntax.Parse(src)
		if err != nil {
			b.Fatal(err)
		}
		c, err := engine.Compile(g)
		if err != nil {
			b.Fatal(err)
		}
		var inputs []string
		var total int64
		for _, f := range m.Files {
			if f.Expect != ExpectAccept {
				continue
			}
			data, err := os.ReadFile(f.AbsPath)
			if err != nil {
				b.Fatal(err)
			}
			if res := c.Parse(m.Start, string(data)); !res.OK {
				b.Fatalf("%s: %s", f.Path, res.Error)
			}
			inputs = append(inputs, string(data))
			total += int64(len(data))
		}
		b.Run(filepath.Base(m.Dir), func(b *testing.B) {
			b.SetBytes(total)
			b.ReportAllocs()
			for i := 0; i < b.N; i++ {
				for _, in := range inputs {
					c.Parse(m.Start, in)
				}
			}
		})
	}
}
