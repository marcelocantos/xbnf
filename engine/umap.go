// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package engine

// uSet is an open-addressing set of uint64 keys stamped with a generation so
// reset is O(1). Hash is splitmix64 — packed GLL keys put position in the low
// bits, so a raw k&mask table clusters and was measured ~100× slower.
type uSet struct {
	slots []uSlot
	n     int
	mask  uint64
}

type uSlot struct {
	key uint64
	gen uint32
}

// uMap is the same table with an int value, for gssAt / stepAt.
type uMap struct {
	slots []umSlot
	n     int
	mask  uint64
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

func nextPow2(n int) int {
	if n < 16 {
		return 16
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

func (s *uSet) init(hint int) {
	n := nextPow2(hint)
	s.slots = make([]uSlot, n)
	s.mask = uint64(n - 1)
	s.n = 0
}

func (s *uSet) reset() {
	s.n = 0
}

func (s *uSet) clear() {
	clear(s.slots)
	s.n = 0
}

func (s *uSet) has(k uint64, gen uint32) bool {
	_, hit := s.probe(k, gen)
	return hit
}

func (s *uSet) probe(k uint64, gen uint32) (uint64, bool) {
	slots := s.slots
	if len(slots) == 0 {
		return 0, false
	}
	mask := s.mask
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

const uLoad = 4 // grow when n*uLoad >= cap (load 1/4; 1/2 clustered on packed keys)

func (s *uSet) crowded() bool { return s.n*uLoad >= len(s.slots) }

func (s *uSet) add(k uint64, gen uint32) {
	if s.crowded() {
		s.grow(gen)
	}
	s.insert(k, gen)
}

// placeAt writes a key known to be absent. idx is from a prior probe miss.
// If the table must grow, idx is ignored and the key is re-inserted.
func (s *uSet) placeAt(idx uint64, k uint64, gen uint32) {
	if s.crowded() {
		s.grow(gen)
		s.insert(k, gen)
		return
	}
	e := &s.slots[idx]
	e.key = k
	e.gen = gen
	s.n++
}

func (s *uSet) insert(k uint64, gen uint32) {
	slots := s.slots
	mask := s.mask
	i := mix64(k) & mask
	for {
		e := &slots[i]
		if e.gen != gen {
			e.key = k
			e.gen = gen
			s.n++
			return
		}
		if e.key == k {
			return
		}
		i = (i + 1) & mask
	}
}

func (s *uSet) grow(gen uint32) {
	old := s.slots
	n := len(old) * 2
	if n == 0 {
		n = 16
	}
	s.slots = make([]uSlot, n)
	s.mask = uint64(n - 1)
	s.n = 0
	for i := range old {
		if old[i].gen == gen {
			s.insert(old[i].key, gen)
		}
	}
}

func (m *uMap) init(hint int) {
	n := nextPow2(hint)
	m.slots = make([]umSlot, n)
	m.mask = uint64(n - 1)
	m.n = 0
}

func (m *uMap) reset() {
	m.n = 0
}

func (m *uMap) clear() {
	clear(m.slots)
	m.n = 0
}

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
		n = 16
	}
	m.slots = make([]umSlot, n)
	m.mask = uint64(n - 1)
	m.n = 0
	for i := range old {
		if old[i].gen == gen {
			m.insert(old[i].key, gen, old[i].id)
		}
	}
}
