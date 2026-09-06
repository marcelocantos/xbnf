// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package engine

import "testing"

func TestUMapGenAndGrow(t *testing.T) {
	var m uMap[int32]
	m.size(8)
	const gen uint32 = 1
	for i := uint64(0); i < 100; i++ {
		k := i<<20 | i // packed-style: low bits repeat
		if _, hit := m.get(k, gen); hit {
			t.Fatalf("unexpected hit %d", i)
		}
		m.put(k, gen, int32(i))
		if id, hit := m.get(k, gen); !hit || id != int32(i) {
			t.Fatalf("missing after put %d: %d %v", i, id, hit)
		}
	}
	if id, hit := m.get(3<<20|3, gen); !hit || id != 3 {
		t.Fatalf("lost key across grow: %d %v", id, hit)
	}
	grown := cap(m.slots)
	if grown <= minTableSlots {
		t.Fatalf("table never grew: cap %d", grown)
	}
	m.size(8)
	if cap(m.slots) != grown {
		t.Fatalf("new generation dropped the grown array: cap %d, was %d", cap(m.slots), grown)
	}
	if len(m.slots) != grown {
		t.Fatalf("same hint should reopen at the size it grew to: len %d, grew to %d", len(m.slots), grown)
	}
	m.size(1)
	if cap(m.slots) != grown {
		t.Fatalf("smaller hint dropped the grown array: cap %d, was %d", cap(m.slots), grown)
	}
	if len(m.slots) >= grown {
		t.Fatalf("an eighth of the hint kept the whole window: len %d, grew to %d", len(m.slots), grown)
	}
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

// TestUMapSpareReuse covers the path a varied corpus takes: a generation
// starts on a small window of a big retained array, then grows twice, so the
// second grow reuses a spare that still holds this generation's keys.
func TestUMapSpareReuse(t *testing.T) {
	const keys = 1000
	var m uMap[int32]
	m.size(1 << 12)
	const gen1 uint32 = 1
	for i := uint64(0); i < keys; i++ {
		m.put(i<<20|i, gen1, int32(i))
	}
	big := cap(m.slots)
	m.size(0) // next generation, a tiny input
	const gen2 uint32 = 2
	for i := uint64(0); i < keys; i++ {
		k := i<<20 | i
		if _, hit := m.get(k, gen2); hit {
			t.Fatalf("stale key %d visible in the new generation", i)
		}
		m.put(k, gen2, int32(i)+1)
	}
	for i := uint64(0); i < keys; i++ {
		id, hit := m.get(i<<20|i, gen2)
		if !hit || id != int32(i)+1 {
			t.Fatalf("key %d after regrow: id %d hit %v", i, id, hit)
		}
	}
	if cap(m.slots) < big && cap(m.spare) < big {
		t.Fatalf("both retained arrays shrank: %d/%d, was %d", cap(m.slots), cap(m.spare), big)
	}
}

// TestUMapProbePlace covers the one-probe path the parser takes: probe once,
// then write into the slot the miss reported.
func TestUMapProbePlace(t *testing.T) {
	var m uMap[int32]
	m.size(4)
	const gen uint32 = 1
	for i := uint64(0); i < 40; i++ {
		k := i<<20 | i
		idx, hit := m.probe(k, gen)
		if hit {
			t.Fatalf("unexpected hit %d", i)
		}
		m.placeAt(idx, k, gen, int32(i)+1)
	}
	for i := uint64(0); i < 40; i++ {
		k := i<<20 | i
		idx, hit := m.probe(k, gen)
		if !hit {
			t.Fatalf("missing %d", i)
		}
		if id := m.idAt(idx); id != int32(i)+1 {
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
		var m uMap[int32]
		m.size(n)
		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			gen := uint32(i + 1)
			m.size(n)
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
