// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package engine

import (
	"fmt"
	"sync"
	"testing"
)

// TestConcurrentParse pins 🎯T28: one Compiled serves Parse from many
// goroutines. The DFA transition caches fill lazily during matching and
// the tree walker's scratch lives on the chart, so a fresh Compiled is
// compiled for the concurrent run (its caches are cold, which is where a
// race would be) and every result is checked against a sequential
// baseline from a second instance. Run with -race (make race).
func TestConcurrentParse(t *testing.T) {
	t.Parallel()
	inputs := []string{
		nestedJSON(1 << 10),
		nestedJSON(4 << 10),
		`{"a": [1, 2.5e3, {"b": null, "c": "héllo wörld ☃"}], "d": true}`,
		`[true, false, "日本語", -0.5]`,
		`{"k": "v"`,
		`"unterminated`,
	}
	for i := 0; i < 4; i++ {
		inputs = append(inputs, fmt.Sprintf(`{"n%d": [%d, "%d"]}`, i, i*7, i))
	}
	base := compileDoc(t, "docs/examples/json.xbnf")
	want := make([]Fingerprint, len(inputs))
	for i, in := range inputs {
		want[i] = FingerprintResult(base.Parse("json", in))
	}
	c := compileDoc(t, "docs/examples/json.xbnf")
	const workers, rounds = 8, 12
	var wg sync.WaitGroup
	for w := 0; w < workers; w++ {
		wg.Add(1)
		go func(w int) {
			defer wg.Done()
			for k := 0; k < rounds; k++ {
				i := (w + k) % len(inputs)
				if got := FingerprintResult(c.Parse("json", inputs[i])); got != want[i] {
					t.Errorf("worker %d input %d: %+v, want %+v", w, i, got, want[i])
				}
			}
		}(w)
	}
	wg.Wait()
}
