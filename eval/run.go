// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

// Package eval is the T22.1 corpus harness: correctness first, then timed
// phases on the uninstrumented Parse path, then a separate diagnostic pass.
package eval

import (
	"encoding/json"
	"fmt"
	"os"
	"runtime"
	"sort"
	"strings"
	"time"

	"github.com/marcelocantos/xbnf/engine"
	"github.com/marcelocantos/xbnf/syntax"
)

// Args is the Evaluate configuration. Zero values select smoke defaults.
type Args struct {
	Manifest    string
	WarmRepeat  int // warm identical-document parses; default 8
	NoisePairs  int // same-binary interleaved pairs; 0 skips
	Oracle      Oracle
	WriteReport string // optional JSON path; empty means no extra file
}

// Oracle is an external reference. It is never the xbnf result.
type Oracle interface {
	Name() string
	Work() string
	Recognize(input []byte) (ok bool, err error)
}

// JSONValid is encoding/json.Valid as a recognition oracle.
type JSONValid struct{}

func (JSONValid) Name() string { return "encoding/json.Valid" }
func (JSONValid) Work() string {
	return "recognition only; no Unmarshal, no tree"
}
func (JSONValid) Recognize(input []byte) (bool, error) {
	return json.Valid(input), nil
}

// FileResult is one fixture after the untimed correctness pass.
type FileResult struct {
	Path     string          `json:"path"`
	Bytes    int             `json:"bytes"`
	Expect   Expect          `json:"expect"`
	Class    Class           `json:"class"`
	XBNFOK   bool            `json:"xbnf_ok"`
	OracleOK *bool           `json:"oracle_ok,omitempty"`
	Packed   int             `json:"packed"`
	Error    string          `json:"error,omitempty"`
	Pass     bool            `json:"pass"`
	Kind     string          `json:"kind"` // ok, miss, overaccept, mismatch, semantic
	WarmNs   int64           `json:"warm_ns,omitempty"`
	Profile  *engine.Profile `json:"profile,omitempty"`
}

// Report is the machine-readable evaluation of one manifest.
type Report struct {
	Language      string       `json:"language"`
	Dialect       string       `json:"dialect"`
	Source        Source       `json:"source"`
	Grammar       string       `json:"grammar"`
	Denominator   int          `json:"denominator"`
	Passed        int          `json:"passed"`
	Failed        int          `json:"failed"`
	Exclusions    []string     `json:"exclusions"`
	Reference     RefSpec      `json:"reference"`
	ColdCompileNs int64        `json:"cold_compile_ns"`
	ColdFirstNs   int64        `json:"cold_first_parse_ns"`
	WarmVariedNs  int64        `json:"warm_varied_ns"`
	WarmIdentNs   int64        `json:"warm_identical_ns"`
	WarmIdentN    int          `json:"warm_identical_n"`
	HeadlineNote  string       `json:"headline_note"`
	Coverage      string       `json:"coverage,omitempty"`
	OracleLimit   string       `json:"oracle_limit,omitempty"`
	Noise         *NoiseResult `json:"noise,omitempty"`
	Files         []FileResult `json:"files"`
}

// Evaluate loads the grammar once, checks every fixture (untimed), then
// measures cold/warm phases with Compiled.Parse. Diagnostics use ParseProfile
// after timing.
func Evaluate(a *Args) (*Report, error) {
	if a == nil || a.Manifest == "" {
		return nil, fmt.Errorf("eval: manifest path required")
	}
	if a.WarmRepeat <= 0 {
		a.WarmRepeat = 8
	}
	m, err := LoadManifest(a.Manifest)
	if err != nil {
		return nil, err
	}
	src, err := os.ReadFile(m.GrammarAbs)
	if err != nil {
		return nil, err
	}
	t0 := time.Now()
	g, err := syntax.Parse(src)
	if err != nil {
		return nil, fmt.Errorf("grammar parse: %w", err)
	}
	c, err := engine.Compile(g)
	if err != nil {
		return nil, fmt.Errorf("compile: %w", err)
	}
	coldCompile := time.Since(t0)

	rep := &Report{
		Language:      m.Language,
		Dialect:       m.Dialect,
		Source:        m.Source,
		Grammar:       m.GrammarAbs,
		Denominator:   len(m.Files),
		Exclusions:    m.Exclusions,
		Reference:     m.Reference,
		ColdCompileNs: coldCompile.Nanoseconds(),
		HeadlineNote:  "times are Compiled.Parse on the shipped path; diagnostics are a later pass",
		Coverage:      m.Coverage,
		WarmIdentN:    a.WarmRepeat,
	}
	if a.Oracle == nil && len(m.OracleCmd) > 0 {
		o, limit := LookOracle(resolveOracleCmd(m.Dir, m.OracleCmd), m.Reference.Name, m.Reference.Work)
		a.Oracle = o
		rep.OracleLimit = limit
	}
	if a.Oracle == nil && strings.EqualFold(m.Language, "json") {
		a.Oracle = JSONValid{}
	}
	if a.Oracle != nil {
		rep.Reference.Name = a.Oracle.Name()
		rep.Reference.Work = a.Oracle.Work()
	}

	type loaded struct {
		file File
		data []byte
	}
	items := make([]loaded, 0, len(m.Files))
	for _, f := range m.Files {
		data, err := os.ReadFile(f.AbsPath)
		if err != nil {
			return nil, err
		}
		items = append(items, loaded{file: f, data: data})
	}

	// Cold first parse on this Compiled before any other Parse (not after
	// the correctness loop, which would report a pool-warm sample).
	first := items[0]
	t1 := time.Now()
	_ = c.Parse(m.Start, string(first.data))
	rep.ColdFirstNs = time.Since(t1).Nanoseconds()

	// Untimed correctness. Tree is produced (Parse always builds it).
	for _, it := range items {
		fr := checkFile(c, m.Start, it.file, it.data, a.Oracle)
		if fr.Pass {
			rep.Passed++
		} else {
			rep.Failed++
		}
		rep.Files = append(rep.Files, fr)
	}

	// Warm varied: one Parse per file on the live Compiled.
	t2 := time.Now()
	for _, it := range items {
		_ = c.Parse(m.Start, string(it.data))
	}
	rep.WarmVariedNs = time.Since(t2).Nanoseconds()

	// Warm identical.
	in := string(first.data)
	t3 := time.Now()
	for i := 0; i < a.WarmRepeat; i++ {
		_ = c.Parse(m.Start, in)
	}
	rep.WarmIdentNs = time.Since(t3).Nanoseconds()

	// Per-file warm + diagnostics (not headline).
	for i, it := range items {
		t := time.Now()
		_ = c.Parse(m.Start, string(it.data))
		rep.Files[i].WarmNs = time.Since(t).Nanoseconds()
		_, prof := c.ParseProfile(m.Start, string(it.data))
		rep.Files[i].Profile = prof
	}

	if a.NoisePairs > 0 {
		rep.Noise = noiseSelf(c, m.Start, in, a.NoisePairs)
	}

	sort.SliceStable(rep.Files, func(i, j int) bool {
		return rep.Files[i].WarmNs > rep.Files[j].WarmNs
	})

	if a.WriteReport != "" {
		b, err := json.MarshalIndent(rep, "", "  ")
		if err != nil {
			return nil, err
		}
		if err := os.WriteFile(a.WriteReport, b, 0o644); err != nil {
			return nil, err
		}
	}
	return rep, nil
}

func checkFile(c *engine.Compiled, start string, f File, data []byte, o Oracle) FileResult {
	res := c.Parse(start, string(data))
	fr := FileResult{
		Path:   f.Path,
		Bytes:  len(data),
		Expect: f.Expect,
		Class:  f.Class,
		XBNFOK: res.OK,
		Packed: res.Packed,
		Error:  res.Error,
	}
	want := f.Expect == ExpectAccept
	if o == nil && f.Class == ClassTree && res.OK && want {
		fr.Pass = false
		fr.Kind = "unverified"
		fr.Error = "structural oracle missing; leftover-text consumption is not success"
		return fr
	}
	if o != nil && f.Class != ClassSemantic {
		ok, err := o.Recognize(data)
		if err != nil {
			fr.Pass = false
			fr.Kind = "oracle-error"
			fr.Error = err.Error()
			return fr
		}
		fr.OracleOK = &ok
		if ok != want && f.Class == ClassSyntax {
			fr.Pass = false
			fr.Kind = "oracle-disagree"
			return fr
		}
	}
	switch {
	case res.OK == want:
		fr.Pass = true
		fr.Kind = "ok"
	case want && !res.OK:
		fr.Pass = false
		fr.Kind = "miss"
	case !want && res.OK && f.Class == ClassSemantic:
		fr.Pass = true
		fr.Kind = "semantic"
		fr.Error = "xbnf accepted; semantic reject is the oracle's job"
	default:
		fr.Pass = false
		fr.Kind = "overaccept"
	}
	return fr
}

// NoiseResult is a same-binary interleaved check (docs/parse-speed.md).
type NoiseResult struct {
	Pairs       int     `json:"pairs"`
	MedianRatio float64 `json:"median_ratio"`
	Verdict     string  `json:"verdict"`
}

func noiseSelf(c *engine.Compiled, start, input string, pairs int) *NoiseResult {
	if pairs < 4 {
		pairs = 4
	}
	ratios := make([]float64, 0, pairs)
	for i := 0; i < pairs; i++ {
		runtime.GC()
		var a, b time.Duration
		if i%2 == 0 {
			a = oneParse(c, start, input)
			b = oneParse(c, start, input)
		} else {
			b = oneParse(c, start, input)
			a = oneParse(c, start, input)
		}
		if b <= 0 {
			b = 1
		}
		ratios = append(ratios, float64(a)/float64(b))
	}
	sort.Float64s(ratios)
	med := ratios[len(ratios)/2]
	v := "SELF-OK"
	if med < 0.97 || med > 1.03 {
		v = "SELF-FAIL"
	}
	return &NoiseResult{Pairs: pairs, MedianRatio: med, Verdict: v}
}

func oneParse(c *engine.Compiled, start, input string) time.Duration {
	t := time.Now()
	_ = c.Parse(start, input)
	return time.Since(t)
}
