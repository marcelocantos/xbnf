// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package engine

import (
	"encoding/json"
	"fmt"
	"math/rand"
	"os"
	"strings"
	"testing"

	wparser "github.com/arr-ai/wbnf/parser"
	"github.com/arr-ai/wbnf/wbnf"
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
				ratio := float64(p.work) / float64(prev)
				if ratio > descriptorGrowthBound {
					t.Fatalf("%s: %d bytes took %d descriptors, %.1fx the previous 4x-smaller input (bound %.0fx)",
						gen.name, len(in), p.work, ratio, descriptorGrowthBound)
				}
			}
			prev = p.work
		}
	}
}

func TestJSONValueBytePred(t *testing.T) {
	c := compileDoc(t, "docs/examples/json.xbnf")
	bp := c.pred["value"]
	if bp == nil {
		t.Fatal("json value should have a FIRST-disjoint byte predictor")
	}
	if bp.byByte['{'] < 0 || bp.byByte['['] < 0 || bp.byByte['"'] < 0 || bp.byByte['t'] < 0 {
		t.Fatalf("value predictor missing a JSON atom: {=%d [=%d \"=%d t=%d",
			bp.byByte['{'], bp.byByte['['], bp.byByte['"'], bp.byByte['t'])
	}
	if bp.byByte['{'] == bp.byByte['['] {
		t.Fatal("object and array collided in the value predictor")
	}
	if bp.byByte['x'] != nonePID {
		t.Fatalf("x should be nonePID, got %d", bp.byByte['x'])
	}
}

func TestJSON16KParseCost(t *testing.T) {
	c := compileDoc(t, "docs/examples/json.xbnf")
	in := nestedJSON(16 << 10)
	res, p := c.run("json", in)
	if !res.OK {
		t.Fatal(res.Error)
	}
	const workBound = 30000
	if p.work > workBound {
		t.Fatalf("16 KB nested JSON work=%d, bound %d", p.work, workBound)
	}
	allocs := testing.AllocsPerRun(10, func() { c.Parse("json", in) })
	const allocBound = 85000
	if allocs > allocBound {
		t.Fatalf("16 KB nested JSON allocs=%.0f, bound %d", allocs, allocBound)
	}
}

func TestTreeBuildLinearAlloc(t *testing.T) {
	c := compileDoc(t, "docs/examples/json.xbnf")
	in4 := nestedJSON(4 << 10)
	in16 := nestedJSON(16 << 10)
	if res := c.Parse("json", in4); !res.OK {
		t.Fatal(res.Error)
	}
	if res := c.Parse("json", in16); !res.OK {
		t.Fatal(res.Error)
	}
	a4 := testing.AllocsPerRun(10, func() { c.Parse("json", in4) })
	a16 := testing.AllocsPerRun(10, func() { c.Parse("json", in16) })
	if a4 < 1 {
		t.Fatalf("4 KB parsed with no allocations")
	}
	ratio := a16 / a4
	if ratio > 6 {
		t.Fatalf("nested JSON allocs: %.0f at %d bytes, %.0f at %d bytes (%.1fx, bound 6x for 4x input)",
			a4, len(in4), a16, len(in16), ratio)
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

func BenchmarkJSON64K_stdlibUnmarshal(b *testing.B) {
	in := []byte(nestedJSON(64 << 10))
	b.SetBytes(int64(len(in)))
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		var v any
		if err := json.Unmarshal(in, &v); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkJSON64K_stdlibValid(b *testing.B) {
	in := []byte(nestedJSON(64 << 10))
	b.SetBytes(int64(len(in)))
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if !json.Valid(in) {
			b.Fatal("invalid")
		}
	}
}

func BenchmarkJSON64K_wbnf(b *testing.B) {
	src, err := os.ReadFile("testdata/json.wbnf")
	if err != nil {
		b.Fatal(err)
	}
	p, err := wbnf.Compile(string(src), nil)
	if err != nil {
		b.Fatal(err)
	}
	in := nestedJSON(64 << 10)
	if _, err := p.Parse(wparser.Rule("json"), wparser.NewScanner(in)); err != nil {
		b.Fatal(err)
	}
	b.SetBytes(int64(len(in)))
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, err := p.Parse(wparser.Rule("json"), wparser.NewScanner(in)); err != nil {
			b.Fatal(err)
		}
	}
}
