// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package eval

import "testing"

func TestLookOracleMissing(t *testing.T) {
	o, limit := LookOracle([]string{"xbnf-oracle-not-installed-xyz"}, "x", "y")
	if o != nil {
		t.Fatal("missing binary must not yield an oracle")
	}
	if limit == "" {
		t.Fatal("want a recorded limit")
	}
}

func TestCmdOracleEcho(t *testing.T) {
	o, limit := LookOracle([]string{"true"}, "true", "exit 0")
	if limit != "" || o == nil {
		t.Fatalf("true: %s", limit)
	}
	ok, err := o.Recognize([]byte("unused"))
	if err != nil || !ok {
		t.Fatalf("true: ok=%v err=%v", ok, err)
	}
	o, limit = LookOracle([]string{"false"}, "false", "exit 1")
	if limit != "" || o == nil {
		t.Fatalf("false: %s", limit)
	}
	ok, err = o.Recognize([]byte("unused"))
	if err != nil || ok {
		t.Fatalf("false: ok=%v err=%v", ok, err)
	}
}
