// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package engine

import (
	"encoding/json"
	"os"
	"testing"
)

// TestGolden is the engine half of the 🎯T25.1 ratchet: nestedJSON(64<<10)
// on docs/examples/json.xbnf must fingerprint exactly as recorded.
// XBNF_GOLDEN=update rewrites testdata/golden.json.
func TestGolden(t *testing.T) {
	const path = "testdata/golden.json"
	c := compileDoc(t, "docs/examples/json.xbnf")
	in := nestedJSON(64 << 10)
	got := map[string]Fingerprint{"nestedJSON-64K": FingerprintResult(c.Parse("json", in))}
	if os.Getenv("XBNF_GOLDEN") == "update" {
		b, err := json.MarshalIndent(got, "", "  ")
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, append(b, '\n'), 0o644); err != nil {
			t.Fatal(err)
		}
		t.Logf("wrote %s", path)
		return
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("%v (run XBNF_GOLDEN=update go test ./engine -run TestGolden)", err)
	}
	var want map[string]Fingerprint
	if err := json.Unmarshal(raw, &want); err != nil {
		t.Fatal(err)
	}
	for k, g := range got {
		w, ok := want[k]
		if !ok {
			t.Errorf("%s: missing from golden", k)
			continue
		}
		if g != w {
			t.Errorf("%s: fingerprint changed\n  want %+v\n  got  %+v", k, w, g)
		}
	}
}
