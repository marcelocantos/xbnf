// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package engine

import (
	"fmt"
	"math/rand"
	"os"
	"strings"
	"testing"

	"github.com/marcelocantos/xbnf/syntax"
)

// descriptorGrowthBound is the maximum ratio of descriptors processed when the
// input grows 4x. Linear work gives 4; this leaves room for constant setup.
const descriptorGrowthBound = 5.0

func compileDoc(t testing.TB, rel string) *Compiled {
	t.Helper()
	src, err := os.ReadFile("../" + rel)
	if err != nil {
		t.Fatal(err)
	}
	g, err := syntax.Parse(src)
	if err != nil {
		t.Fatal(err)
	}
	c, err := Compile(g)
	if err != nil {
		t.Fatal(err)
	}
	return c
}

// flatArray is a JSON array of integers of roughly n bytes.
func flatArray(n int) string {
	r := rand.New(rand.NewSource(1))
	var b strings.Builder
	b.WriteString("[")
	for b.Len() < n {
		if b.Len() > 1 {
			b.WriteString(", ")
		}
		fmt.Fprintf(&b, "%d", r.Intn(1000))
	}
	b.WriteString("]")
	return b.String()
}

// nestedJSON is a deterministic JSON document of roughly n bytes mixing
// objects, arrays, strings, numbers, and literals.
func nestedJSON(n int) string {
	r := rand.New(rand.NewSource(1))
	var b strings.Builder
	var val func(depth int)
	val = func(depth int) {
		switch k := r.Intn(7); {
		case k == 0 && depth < 4:
			b.WriteString("{")
			for i, m := 0, 1+r.Intn(4); i < m; i++ {
				if i > 0 {
					b.WriteString(", ")
				}
				fmt.Fprintf(&b, "\"k%d\": ", r.Intn(100))
				val(depth + 1)
			}
			b.WriteString("}")
		case k == 1 && depth < 4:
			b.WriteString("[")
			for i, m := 0, 1+r.Intn(5); i < m; i++ {
				if i > 0 {
					b.WriteString(", ")
				}
				val(depth + 1)
			}
			b.WriteString("]")
		case k == 2:
			fmt.Fprintf(&b, "\"s%d\\n\"", r.Intn(1000))
		case k == 3:
			fmt.Fprintf(&b, "%d", r.Intn(100000)-50000)
		case k == 4:
			fmt.Fprintf(&b, "%d.%de%d", r.Intn(100), r.Intn(1000), r.Intn(9)-4)
		case k == 5:
			b.WriteString("true")
		default:
			b.WriteString("null")
		}
	}
	b.WriteString("[\n")
	for first := true; b.Len() < n; first = false {
		if !first {
			b.WriteString(",\n")
		}
		b.WriteString("  ")
		val(1)
	}
	b.WriteString("\n]\n")
	return b.String()
}

func TestListDescriptorsLinear(t *testing.T) {
	t.Parallel()
	c := compileDoc(t, "docs/examples/json.xbnf")
	for _, gen := range []struct {
		name string
		f    func(int) string
	}{{"flat", flatArray}, {"nested", nestedJSON}} {
		prev := 0
		for _, n := range []int{1 << 10, 4 << 10, 16 << 10} {
			in := gen.f(n)
			res, p := c.run("json", in)
			if !res.OK {
				t.Fatalf("%s %d: %s", gen.name, n, res.Error)
			}
			if prev > 0 {
				ratio := float64(p.steps) / float64(prev)
				if ratio > descriptorGrowthBound {
					t.Fatalf("%s: %d bytes took %d descriptors, %.1fx the previous 4x-smaller input (bound %.0fx)",
						gen.name, len(in), p.steps, ratio, descriptorGrowthBound)
				}
			}
			prev = p.steps
		}
	}
}

func BenchmarkJSON64K(b *testing.B) {
	c := compileDoc(b, "docs/examples/json.xbnf")
	in := nestedJSON(64 << 10)
	if res := c.Parse("json", in); !res.OK {
		b.Fatal(res.Error)
	}
	b.SetBytes(int64(len(in)))
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		c.Parse("json", in)
	}
}
