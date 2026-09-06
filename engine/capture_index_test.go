// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package engine

import "testing"

// These are collisions of the entire capture hash, not merely its table index.
// claimCaptured receives a real grammar slot and in-range input position, but
// opaque packed keys are chosen algebraically to force the collision chain.
func TestCaptureIndexCollisionAndGeneration(t *testing.T) {
	c := mustCompileXBNF(t, `s -> n="a" %n ; #wrap -> () ;`)
	plain := mustCompileXBNF(t, `plain -> "a" ; #wrap -> () ;`)
	const input = "aaaa"
	pid := c.ntProds["s"][0]
	sl := slot{pid: pid, ip: len(c.prods[pid].rhs)}
	const pos = 2
	key, ok := packDesc(sl, 0, pos)
	if !ok {
		t.Fatal("test slot does not pack")
	}
	for _, transition := range []string{"rebind", "release and rebind", "plain grammar", "generation rollover"} {
		t.Run(transition, func(t *testing.T) {
			p := &gll{}
			p.bind(c, input, "s")
			// Make real capture contexts. The index treats their identities as
			// opaque; each refers to a different span of the same input.
			captureSlot := slot{pid: pid, ip: 1}
			envs := []int32{
				p.capture(captureSlot, 0, 1, 0),
				p.capture(captureSlot, 0, 2, 0),
				p.capture(captureSlot, 0, 3, 0),
			}
			keys := []uint64{key, key ^ mix64(uint64(envs[0])) ^ mix64(uint64(envs[1])),
				key ^ mix64(uint64(envs[0])) ^ mix64(uint64(envs[2]))}
			hash := keys[0] ^ mix64(uint64(envs[0]))
			for i := range keys {
				if keys[i]^mix64(uint64(envs[i])) != hash {
					t.Fatal("test keys do not collide")
				}
				for j := 0; j < i; j++ {
					if keys[i] == keys[j] || envs[i] == envs[j] {
						t.Fatal("test identities are not distinct")
					}
				}
			}

			check := func() {
				t.Helper()
				cells := make([]int32, len(keys))
				before := len(p.cells)
				for i := range keys {
					cell, fresh, ok := p.claimCaptured(sl, pos, keys[i], envs[i])
					if !ok || !fresh || cell != int32(before+i) {
						t.Fatalf("claim %d: cell=%d fresh=%t ok=%t, want new cell %d", i, cell, fresh, ok, before+i)
					}
					cells[i] = cell
				}
				cellCount, descCount := len(p.cells), len(p.captureDescs)
				// Revisit tail, middle, and head after all collided entries exist.
				for _, i := range []int{0, 1, 2, 1, 0} {
					cell, fresh, ok := p.claimCaptured(sl, pos, keys[i], envs[i])
					if !ok || fresh || cell != cells[i] {
						t.Fatalf("reclaim %d: cell=%d fresh=%t ok=%t, want existing cell %d", i, cell, fresh, ok, cells[i])
					}
				}
				if len(p.cells) != cellCount || len(p.captureDescs) != descCount {
					t.Fatal("reclaim allocated another cell or capture descriptor")
				}
			}
			check()

			// Force growth so rollover must clear retained/spare table arrays,
			// and prove the collision chain survives a table relocation.
			const extraKeys = 40
			oldSlots := len(p.captureAt.slots)
			for offset := uint64(1); offset <= extraKeys; offset++ {
				if _, fresh, ok := p.claimCaptured(sl, pos, key+offset, envs[0]); !ok || !fresh {
					t.Fatalf("extra key %d was not fresh", offset)
				}
			}
			if len(p.captureAt.slots) <= oldSlots {
				t.Fatal("test did not grow the capture index")
			}
			for i := range keys {
				if cell, fresh, ok := p.claimCaptured(sl, pos, keys[i], envs[i]); !ok || fresh || cell != int32(i+1) {
					t.Fatalf("collision chain lost entry %d after growth: %d %t %t", i, cell, fresh, ok)
				}
			}

			switch transition {
			case "release and rebind":
				p.release()
				p = newGLL(c, input, "s") // Reacquire ownership before touching a pooled chart.
			case "plain grammar":
				p.bind(plain, "a", "plain")
			case "generation rollover":
				p.gen = ^uint32(0) // Old generation-one entries must not become live again.
			}
			p.bind(c, input, "s")
			if transition == "generation rollover" && p.gen != 1 {
				t.Fatalf("rollover generation=%d, want 1", p.gen)
			}
			if len(p.captureDescs) != 1 {
				t.Fatalf("rebind retained %d live descriptors, want dummy only", len(p.captureDescs))
			}
			// Numeric IDs are intentionally reusable after bind; freshness and
			// allocation from the current slab distinguish them from stale hits.
			// Recreate the contexts and reserve a cell so stale old IDs also differ.
			for i, old := range envs {
				if env := p.capture(captureSlot, 0, i+1, 0); env != old {
					t.Fatalf("rebuilt context=%d, want reusable ID %d", env, old)
				}
			}
			p.newCell(sl.ip)
			check()
		})
	}
}
