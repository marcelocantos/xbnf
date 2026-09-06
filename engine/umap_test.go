// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package engine

import "testing"

func TestUMapGenAndGrow(t *testing.T) {
	var m uMap
	m.init(8)
	const gen uint32 = 1
	for i := uint64(0); i < 100; i++ {
		k := i<<20 | i // packed-style: low bits repeat
		if _, hit := m.get(k, gen); hit {
			t.Fatalf("unexpected hit %d", i)
		}
		m.put(k, gen, int(i))
		if id, hit := m.get(k, gen); !hit || id != int(i) {
			t.Fatalf("missing after put %d: %d %v", i, id, hit)
		}
	}
	if id, hit := m.get(3<<20|3, gen); !hit || id != 3 {
		t.Fatalf("lost key across grow: %d %v", id, hit)
	}
	m.reset()
	const gen2 uint32 = 2
	if _, hit := m.get(3<<20|3, gen2); hit {
		t.Fatal("stale gen hit")
	}
	m.put(99, gen2, 7)
	if _, hit := m.get(99, gen); hit {
		t.Fatal("gen isolation")
	}
	if id, hit := m.get(99, gen2); !hit || id != 7 {
		t.Fatalf("got %d %v", id, hit)
	}
}

// TestUMapProbePlace covers the one-probe path the parser takes: probe once,
// then write into the slot the miss reported.
func TestUMapProbePlace(t *testing.T) {
	var m uMap
	m.init(4)
	const gen uint32 = 1
	for i := uint64(0); i < 40; i++ {
		k := i<<20 | i
		idx, hit := m.probe(k, gen)
		if hit {
			t.Fatalf("unexpected hit %d", i)
		}
		m.placeAt(idx, k, gen, int(i)+1)
	}
	for i := uint64(0); i < 40; i++ {
		k := i<<20 | i
		idx, hit := m.probe(k, gen)
		if !hit {
			t.Fatalf("missing %d", i)
		}
		if id := m.idAt(idx); id != int(i)+1 {
			t.Fatalf("key %d has id %d", i, id)
		}
	}
}

func BenchmarkMapVsUMap(b *testing.B) {
	const n = 80000
	keys := make([]uint64, n)
	for i := range keys {
		keys[i] = uint64(i)<<20 | uint64(i%251) // packed-style low bits
	}
	b.Run("std", func(b *testing.B) {
		m := make(map[uint64]uint32, n)
		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			gen := uint32(i + 1)
			for _, k := range keys {
				if m[k] != gen {
					m[k] = gen
				}
			}
			hits := 0
			for _, k := range keys {
				if m[k] == gen {
					hits++
				}
			}
			if hits != n {
				b.Fatal(hits)
			}
		}
	})
	b.Run("umap", func(b *testing.B) {
		var m uMap
		m.init(n)
		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			gen := uint32(i + 1)
			m.reset()
			for _, k := range keys {
				if idx, hit := m.probe(k, gen); !hit {
					m.placeAt(idx, k, gen, 0)
				}
			}
			hits := 0
			for _, k := range keys {
				if _, hit := m.probe(k, gen); hit {
					hits++
				}
			}
			if hits != n {
				b.Fatal(hits)
			}
		}
	})
}
