// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package eval

import (
	"encoding/json"
	"os"
	"sort"
	"testing"
)

// TestGolden is the corpus half of the 🎯T25.1 ratchet. Every fixture of
// every manifest under testdata (live, historical and json-smoke) must
// fingerprint exactly as recorded. XBNF_GOLDEN=update rewrites the file.
func TestGolden(t *testing.T) {
	const path = "testdata/golden.json"
	got, err := ComputeGolden("testdata")
	if err != nil {
		t.Fatal(err)
	}
	if os.Getenv("XBNF_GOLDEN") == "update" {
		b, err := json.MarshalIndent(got, "", "  ")
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, append(b, '\n'), 0o644); err != nil {
			t.Fatal(err)
		}
		t.Logf("wrote %s with %d fixtures", path, len(got))
		return
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("%v (run make golden-update)", err)
	}
	var want Golden
	if err := json.Unmarshal(raw, &want); err != nil {
		t.Fatal(err)
	}
	keys := make([]string, 0, len(got))
	for k := range got {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		w, ok := want[k]
		if !ok {
			t.Errorf("%s: missing from golden (make golden-update after checking the tree)", k)
			continue
		}
		if g := got[k]; g != w {
			t.Errorf("%s: fingerprint changed\n  want %+v\n  got  %+v", k, w, g)
		}
	}
	for k := range want {
		if _, ok := got[k]; !ok {
			t.Errorf("%s: in golden but no longer in any manifest", k)
		}
	}
}
