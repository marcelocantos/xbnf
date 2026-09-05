// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package eval

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
)

// Expect is the independently recorded outcome for one fixture.
type Expect string

const (
	ExpectAccept Expect = "accept"
	ExpectReject Expect = "reject"
)

// Class is the comparison contract for a fixture. Syntax is well-formedness
// against the grammar. Semantic is meaning the reference may reject after a
// successful parse; it must not be reported as an xbnf syntax error.
type Class string

const (
	ClassSyntax   Class = "syntax"
	ClassSemantic Class = "semantic"
	ClassTree     Class = "tree"
)

// Manifest is a versioned corpus description. Paths are relative to the
// manifest file. Changing the file list or hashes is a deliberate corpus
// revision; subsets cannot silently shrink to fit xbnf.
type Manifest struct {
	Version        int      `json:"version"`
	Role           string   `json:"role,omitempty"` // "historical" is not in the live T22 set
	Language       string   `json:"language"`
	Dialect        string   `json:"dialect"`
	DialectVersion string   `json:"dialect_version"`
	Source         Source   `json:"source"`
	Grammar        string   `json:"grammar"`
	Start          string   `json:"start"`
	Selection      string   `json:"selection"`
	Coverage       string   `json:"coverage"`
	Exclusions     []string `json:"exclusions"`
	Reference      RefSpec  `json:"reference"`
	OracleCmd      []string `json:"oracle_cmd,omitempty"`
	Files          []File   `json:"files"`
	Dir            string   `json:"-"`
	GrammarAbs     string   `json:"-"`
}

// Source pins the upstream corpus identity.
type Source struct {
	Name     string `json:"name"`
	Revision string `json:"revision"`
	License  string `json:"license"`
	URL      string `json:"url"`
}

// RefSpec names the oracle and what work it performs.
type RefSpec struct {
	Name string `json:"name"`
	Work string `json:"work"`
}

// File is one fixture. SHA-256 is hex of the exact bytes on disk.
type File struct {
	Path    string `json:"path"`
	SHA256  string `json:"sha256"`
	Expect  Expect `json:"expect"`
	Class   Class  `json:"class"`
	Reason  string `json:"reason,omitempty"`
	AbsPath string `json:"-"`
}

// LoadManifest reads path and checks every file hash. It does not parse input.
func LoadManifest(path string) (*Manifest, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var m Manifest
	if err := json.Unmarshal(raw, &m); err != nil {
		return nil, err
	}
	if m.Version != 1 {
		return nil, fmt.Errorf("manifest version %d (want 1)", m.Version)
	}
	if m.Grammar == "" || len(m.Files) == 0 {
		return nil, fmt.Errorf("manifest needs grammar and at least one file")
	}
	if m.Exclusions == nil {
		m.Exclusions = []string{}
	}
	m.Dir = filepath.Dir(path)
	m.GrammarAbs = filepath.Join(m.Dir, m.Grammar)
	for i := range m.Files {
		f := &m.Files[i]
		if f.Expect != ExpectAccept && f.Expect != ExpectReject {
			return nil, fmt.Errorf("%s: expect %q (want accept|reject)", f.Path, f.Expect)
		}
		if f.Class == "" {
			f.Class = ClassSyntax
		}
		f.AbsPath = filepath.Join(m.Dir, f.Path)
		sum, err := hashFile(f.AbsPath)
		if err != nil {
			return nil, err
		}
		if f.SHA256 != "" && f.SHA256 != sum {
			return nil, fmt.Errorf("%s: sha256 %s != manifest %s", f.Path, sum, f.SHA256)
		}
		if f.SHA256 == "" {
			f.SHA256 = sum
		}
	}
	return &m, nil
}

// Live reports whether this manifest is in the T22 live language set.
// role=historical keeps C++ on disk without counting it as a live track.
func (m *Manifest) Live() bool {
	return m.Role != "historical"
}

func hashFile(path string) (string, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:]), nil
}
