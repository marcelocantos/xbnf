// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package engine

import "testing"

func TestUSetGenAndGrow(t *testing.T) {
	var s uSet
	s.init(8)
	const gen uint32 = 1
	for i := uint64(0); i < 100; i++ {
		k := i<<20 | i // packed-style: low bits repeat
		if s.has(k, gen) {
			t.Fatalf("unexpected hit %d", i)
		}
		s.add(k, gen)
		if !s.has(k, gen) {
			t.Fatalf("missing after add %d", i)
		}
	}
	if !s.has(3<<20|3, gen) {
		t.Fatal("lost key across grow")
	}
	s.reset()
	const gen2 uint32 = 2
	if s.has(3<<20|3, gen2) {
		t.Fatal("stale gen hit")
	}
	s.add(99, gen2)
	if !s.has(99, gen2) || s.has(99, gen) {
		t.Fatal("gen isolation")
	}
}

func TestUMapGetPut(t *testing.T) {
	var m uMap
	m.init(4)
	const gen uint32 = 1
	m.put(10, gen, 42)
	id, ok := m.get(10, gen)
	if !ok || id != 42 {
		t.Fatalf("got %d %v", id, ok)
	}
	if _, ok := m.get(10, 2); ok {
		t.Fatal("wrong gen")
	}
	for i := 0; i < 50; i++ {
		m.put(uint64(i), gen, i+1)
	}
	id, ok = m.get(10, gen)
	if !ok || id != 11 {
		t.Fatalf("after grow got %d %v", id, ok)
	}
}

func BenchmarkMapVsUSet(b *testing.B) {
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
	b.Run("uset", func(b *testing.B) {
		var s uSet
		s.init(n)
		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			gen := uint32(i + 1)
			s.reset()
			for _, k := range keys {
				if !s.has(k, gen) {
					s.add(k, gen)
				}
			}
			hits := 0
			for _, k := range keys {
				if s.has(k, gen) {
					hits++
				}
			}
			if hits != n {
				b.Fatal(hits)
			}
		}
	})
}
