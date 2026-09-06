// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package engine

import "testing"

// TestPickPriorityPreferAvoidFallback pins builder.pick's selection rules on a
// hand-built candidate list before the span-selection allocation refactor:
// priority wins outright; #prefer beats plain, #avoid, and fallback
// alternatives; #avoid/fallback lose to a plain alternative; when every
// candidate is #avoid or fallback (or several are equally #prefer'd), the
// span stays ambiguous (packed++) and the smallest pid wins the tie. pick
// operates on packComp values (production plus evidence cell); the cell is
// irrelevant to selection, so every candidate here uses cell 0.
func TestPickPriorityPreferAvoidFallback(t *testing.T) {
	t.Parallel()
	prods := []prod{
		0: {dirs: prodDirs{priority: 0}},                 // plain
		1: {dirs: prodDirs{priority: 0, prefer: true}},   // preferred
		2: {dirs: prodDirs{priority: 0, avoid: true}},    // avoided
		3: {dirs: prodDirs{priority: 1}},                 // higher priority, otherwise plain
		4: {dirs: prodDirs{priority: 0}, fallback: true}, // stack fallback
		5: {dirs: prodDirs{priority: 0, prefer: true}},   // second preferred, ties with 1
		6: {dirs: prodDirs{priority: 0}},                 // plain, ties with 7
		7: {dirs: prodDirs{priority: 0}},                 // plain, ties with 6
	}
	c := &Compiled{prods: prods}

	tests := []struct {
		name   string
		pids   []int
		want   int
		packed int
	}{
		{"single candidate short-circuits", []int{2}, 2, 0},
		{"priority wins outright over prefer", []int{1, 3}, 3, 0},
		{"prefer beats plain and avoid", []int{0, 1, 2}, 1, 0},
		{"avoid and fallback lose to a plain alternative", []int{0, 2, 4}, 0, 0},
		{"all avoid/fallback: falls back to full set, smallest pid wins", []int{2, 4}, 2, 1},
		{"two equally preferred stay ambiguous, smallest pid wins", []int{1, 5}, 1, 1},
		{"unsorted plain tie: smallest pid wins, still ambiguous", []int{7, 6}, 6, 1},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			b := &builder{p: &gll{c: c}}
			comps := make([]int, len(tc.pids))
			for i, pid := range tc.pids {
				comps[i] = packComp(pid, 0)
			}
			got := compPID(b.pick(comps, 0, 0))
			if got != tc.want {
				t.Fatalf("pick(%v) = %d, want %d", tc.pids, got, tc.want)
			}
			if b.packed != tc.packed {
				t.Fatalf("pick(%v) packed = %d, want %d", tc.pids, b.packed, tc.packed)
			}
		})
	}
}
