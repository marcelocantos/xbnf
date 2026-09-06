// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package engine

import "math/bits"

// uMap is an open-addressing map from uint64 key to int, stamped with a
// generation so reset is O(1). Hash is splitmix64 — packed GLL keys put
// position in the low bits, so a raw k&mask table clusters and was measured
// ~100× slower. It backs U, gssAt, sym and moreAt.
//
// slots is the active table and spare the array it last grew out of. Both are
// retained across parses so a pooled chart never allocates a table again, but
// only slots is probed, and size re-points it at a window sized for the input
// at hand — see size. demand is what makes that window the right size: the
// largest table a grow has ever demanded, per magnitude bucket of the sizing
// hint. It is a sizing heuristic only, so a chart that carries it from one
// grammar to another stays correct.
type uMap struct {
	slots  []umSlot
	spare  []umSlot
	n      int
	mask   uint64
	bucket int
	demand [demandBuckets]int32
}

type umSlot struct {
	key uint64
	gen uint32
	id  int
}

func mix64(k uint64) uint64 {
	k ^= k >> 30
	k *= 0xbf58476d1ce4e5b9
	k ^= k >> 27
	k *= 0x94d049bb133111eb
	k ^= k >> 31
	return k
}

// minTableSlots is the floor for any uMap table; below it the probe sequence
// is shorter than a cache line and the mask stops discriminating.
const minTableSlots = 16

// demandBuckets covers every hint a 64-bit int can express, bucketed by
// bits.Len, so a bucket spans one doubling of the hint.
const demandBuckets = 64

func nextPow2(n int) int {
	if n < minTableSlots {
		return minTableSlots
	}
	n--
	n |= n >> 1
	n |= n >> 2
	n |= n >> 4
	n |= n >> 8
	n |= n >> 16
	n |= n >> 32
	return n + 1
}

const uLoad = 4 // grow when n*uLoad >= cap (load 1/4; 1/2 clustered on packed keys)

// size starts a generation on a table sized for hint, reusing a retained
// array whenever one is big enough. The caller must have bumped the
// generation first: whichever array is chosen still holds older generations'
// keys, which probe as empty.
//
// It is the only place the table changes shape, and on a corpus of mixed
// input sizes two opposing pressures meet here. Opening at the static hint
// pays a doubling series of rehashes on every parse, because the hint is a
// guess about a grammar's density that is usually several doublings low.
// Leaving the table wherever the last parse left it hash-scatters a 60-byte
// input's handful of probes over the megabytes a 49 KB input grew, and that
// costs more than the rehashes did. So size opens on what a parse of this
// magnitude actually demanded last time — a measurement, and one that does
// not extrapolate from a 49 KB fixture to a 60-byte one, because demand for
// each is recorded in its own bucket.
func (m *uMap) size(hint int) {
	if hint < 1 {
		hint = 1
	}
	m.bucket = bits.Len(uint(hint))
	want := hint
	if d := int(m.demand[m.bucket]); d > want {
		want = d
	}
	n := nextPow2(want)
	switch {
	case cap(m.slots) >= n:
		m.slots = m.slots[:n]
	case cap(m.spare) >= n:
		m.slots, m.spare = m.spare[:n], m.slots
	default:
		m.slots = make([]umSlot, n)
	}
	m.mask = uint64(n - 1)
	m.n = 0
}

// noteDemand records a size a grow demanded for the current hint bucket. Only
// actual grows feed it: seeding it from size's own output would compound its
// rounding bucket by bucket.
func (m *uMap) noteDemand(n int) {
	if int32(n) > m.demand[m.bucket] {
		m.demand[m.bucket] = int32(n)
	}
}

func (m *uMap) clear() {
	clear(m.slots[:cap(m.slots)])
	clear(m.spare[:cap(m.spare)])
	m.n = 0
}

// cells is the table's retained capacity, both arrays, for the pool bound.
func (m *uMap) cells() int { return cap(m.slots) + cap(m.spare) }

func (m *uMap) get(k uint64, gen uint32) (int, bool) {
	slots := m.slots
	if len(slots) == 0 {
		return 0, false
	}
	mask := m.mask
	i := mix64(k) & mask
	for {
		e := &slots[i]
		if e.gen != gen {
			return 0, false
		}
		if e.key == k {
			return e.id, true
		}
		i = (i + 1) & mask
	}
}

// probe returns the slot index for k. hit says whether the key is present;
// on a miss the index is where placeAt writes it.
func (m *uMap) probe(k uint64, gen uint32) (uint64, bool) {
	slots := m.slots
	if len(slots) == 0 {
		return 0, false
	}
	mask := m.mask
	i := mix64(k) & mask
	for {
		e := &slots[i]
		if e.gen != gen {
			return i, false
		}
		if e.key == k {
			return i, true
		}
		i = (i + 1) & mask
	}
}

func (m *uMap) idAt(idx uint64) int { return m.slots[idx].id }

// placeAt writes a key known to be absent. idx is from a prior probe miss.
// If the table must grow, idx is ignored and the key is re-inserted.
func (m *uMap) placeAt(idx uint64, k uint64, gen uint32, id int) {
	if m.crowded() {
		m.grow(gen)
		m.insert(k, gen, id)
		return
	}
	e := &m.slots[idx]
	e.key = k
	e.gen = gen
	e.id = id
	m.n++
}

func (m *uMap) crowded() bool { return m.n*uLoad >= len(m.slots) }

func (m *uMap) put(k uint64, gen uint32, id int) {
	if m.crowded() {
		m.grow(gen)
	}
	m.insert(k, gen, id)
}

func (m *uMap) insert(k uint64, gen uint32, id int) {
	slots := m.slots
	mask := m.mask
	i := mix64(k) & mask
	for {
		e := &slots[i]
		if e.gen != gen {
			e.key = k
			e.gen = gen
			e.id = id
			m.n++
			return
		}
		if e.key == k {
			e.id = id
			return
		}
		i = (i + 1) & mask
	}
}

func (m *uMap) grow(gen uint32) {
	old := m.slots
	n := len(old) * 2
	if n == 0 {
		n = minTableSlots
	}
	m.noteDemand(n)
	if cap(m.spare) >= n {
		m.slots = m.spare[:n]
		// An earlier grow this generation may have left live keys behind.
		clear(m.slots)
	} else {
		m.slots = make([]umSlot, n)
	}
	m.spare = old
	m.mask = uint64(n - 1)
	m.n = 0
	for i := range old {
		if old[i].gen == gen {
			m.insert(old[i].key, gen, old[i].id)
		}
	}
}
