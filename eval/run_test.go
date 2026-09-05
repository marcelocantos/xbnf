// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package eval

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/marcelocantos/xbnf/engine"
	"github.com/marcelocantos/xbnf/syntax"
)

func TestLoadManifestHash(t *testing.T) {
	m, err := LoadManifest("testdata/json-smoke/manifest.json")
	if err != nil {
		t.Fatal(err)
	}
	if len(m.Files) != 2 {
		t.Fatalf("files=%d", len(m.Files))
	}
	bad := filepath.Join(t.TempDir(), "manifest.json")
	raw, err := os.ReadFile("testdata/json-smoke/manifest.json")
	if err != nil {
		t.Fatal(err)
	}
	tampered := strings.Replace(string(raw), m.Files[0].SHA256, strings.Repeat("0", 64), 1)
	if err := os.WriteFile(bad, []byte(tampered), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadManifest(bad); err == nil {
		t.Fatal("tampered hash accepted")
	}
}

func TestEvaluateJSONSmoke(t *testing.T) {
	rep, err := Evaluate(&Args{
		Manifest:   "testdata/json-smoke/manifest.json",
		WarmRepeat: 4,
		Oracle:     JSONValid{},
	})
	if err != nil {
		t.Fatal(err)
	}
	if rep.Denominator != 2 || rep.Passed != 2 || rep.Failed != 0 {
		t.Fatalf("denom=%d pass=%d fail=%d", rep.Denominator, rep.Passed, rep.Failed)
	}
	var sawAccept, sawReject bool
	for _, f := range rep.Files {
		if !f.Pass {
			t.Fatalf("%s: %s %s", f.Path, f.Kind, f.Error)
		}
		if f.Profile == nil {
			t.Fatalf("%s missing diagnostic profile", f.Path)
		}
		if f.Expect == ExpectAccept {
			sawAccept = true
			if f.Profile.Descriptors <= 0 && !f.Profile.DFAStart {
				t.Fatalf("accept file recorded no GLL work: %+v", f.Profile)
			}
		}
		if f.Expect == ExpectReject {
			sawReject = true
		}
	}
	if !sawAccept || !sawReject {
		t.Fatal("fixture must retain both accept and reject (full denominator)")
	}
	if rep.ColdCompileNs <= 0 || rep.WarmIdentN != 4 {
		t.Fatalf("phases: compile=%d identN=%d", rep.ColdCompileNs, rep.WarmIdentN)
	}
}

func TestEvaluateGrowth(t *testing.T) {
	src, err := os.ReadFile("../docs/examples/json.xbnf")
	if err != nil {
		t.Fatal(err)
	}
	g, err := syntax.Parse(src)
	if err != nil {
		t.Fatal(err)
	}
	c, err := engine.Compile(g)
	if err != nil {
		t.Fatal(err)
	}
	small := jsonArray(1 << 10)
	big := jsonArray(4 << 10)
	_, p1 := c.ParseProfile("json", small)
	_, p4 := c.ParseProfile("json", big)
	if p1.Descriptors <= 0 || p4.Descriptors <= 0 {
		t.Fatalf("work %d -> %d", p1.Descriptors, p4.Descriptors)
	}
	ratio := float64(p4.Descriptors) / float64(p1.Descriptors)
	if ratio > 6 {
		t.Fatalf("descriptor growth %.1fx for 4x input (bound 6)", ratio)
	}
}

func TestNoiseSelfOnLargeJSON(t *testing.T) {
	src, err := os.ReadFile("../docs/examples/json.xbnf")
	if err != nil {
		t.Fatal(err)
	}
	g, err := syntax.Parse(src)
	if err != nil {
		t.Fatal(err)
	}
	c, err := engine.Compile(g)
	if err != nil {
		t.Fatal(err)
	}
	in := jsonArray(16 << 10)
	n := noiseSelf(c, "json", in, 8)
	if n.Verdict != "SELF-OK" {
		t.Skipf("machine too noisy for same-binary check: %+v", n)
	}
}

func jsonArray(n int) string {
	var b strings.Builder
	b.WriteByte('[')
	for b.Len() < n {
		if b.Len() > 1 {
			b.WriteString(",")
		}
		b.WriteString("1")
	}
	b.WriteByte(']')
	return b.String()
}

func TestLanguageManifests(t *testing.T) {
	ents, err := os.ReadDir("testdata")
	if err != nil {
		t.Fatal(err)
	}
	n := 0
	for _, e := range ents {
		if !e.IsDir() || e.Name() == "json-smoke" {
			continue
		}
		man := filepath.Join("testdata", e.Name(), "manifest.json")
		if _, err := os.Stat(man); err != nil {
			continue
		}
		n++
		t.Run(e.Name(), func(t *testing.T) {
			rep, err := Evaluate(&Args{Manifest: man, WarmRepeat: 2})
			if err != nil {
				t.Fatal(err)
			}
			if rep.Denominator < 2 || len(rep.Files) != rep.Denominator {
				t.Fatalf("denom %d files %d", rep.Denominator, len(rep.Files))
			}
			if rep.Failed != 0 {
				t.Fatalf("failed %d: %+v", rep.Failed, rep.Files)
			}
			if rep.Coverage == "" {
				t.Fatal("coverage missing")
			}
			if rep.ColdCompileNs <= 0 || rep.WarmIdentN != 2 {
				t.Fatalf("phases compile=%d identN=%d", rep.ColdCompileNs, rep.WarmIdentN)
			}
			var sawAccept, sawReject bool
			for _, f := range rep.Files {
				if f.Profile == nil {
					t.Fatalf("%s missing profile", f.Path)
				}
				if f.Expect == ExpectAccept {
					sawAccept = true
				}
				if f.Expect == ExpectReject {
					sawReject = true
				}
			}
			if !sawAccept || !sawReject {
				t.Fatal("need accept and reject in the denominator")
			}
		})
	}
	if n < 7 {
		t.Fatalf("language manifests %d, want 7", n)
	}
}

func TestReportJSONRoundTrip(t *testing.T) {
	rep, err := Evaluate(&Args{Manifest: "testdata/json-smoke/manifest.json", Oracle: JSONValid{}})
	if err != nil {
		t.Fatal(err)
	}
	raw, err := json.Marshal(rep)
	if err != nil {
		t.Fatal(err)
	}
	var back Report
	if err := json.Unmarshal(raw, &back); err != nil {
		t.Fatal(err)
	}
	if back.Passed != 2 {
		t.Fatalf("round-trip passed=%d", back.Passed)
	}
}
