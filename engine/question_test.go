// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package engine

import "testing"

// TestQuestionCensus is the research §7.1 duplicate-terminal-question probe:
// how many DFA and literal matches process asks for, and how many repeat a
// (question, position) pair already asked in the same parse. Run with -v to
// read the numbers; the assertions only lock the counters' shape.
func TestQuestionCensus(t *testing.T) {
	c := compileDoc(t, "docs/examples/json.xbnf")
	in := nestedJSON(64 << 10)
	res, prof := c.ParseProfile("json", in)
	if !res.OK {
		t.Fatalf("parse: %s", res.Error)
	}
	t.Logf("nestedJSON(64K): %d bytes, %d descriptors", len(in), prof.Descriptors)
	t.Logf("  DFA   asked %d repeat %d (%.1f%%)", prof.DFAQuestions, prof.DFARepeats,
		pct(prof.DFARepeats, prof.DFAQuestions))
	t.Logf("  term  asked %d repeat %d (%.1f%%)", prof.TermQuestions, prof.TermRepeats,
		pct(prof.TermRepeats, prof.TermQuestions))
	if prof.DFAQuestions == 0 && prof.TermQuestions == 0 {
		t.Fatal("no terminal questions counted")
	}
	if prof.DFARepeats > prof.DFAQuestions || prof.TermRepeats > prof.TermQuestions {
		t.Fatalf("repeats exceed questions: %+v", prof)
	}
}

// TestQuestionCensusOffOutsideProfile keeps the census off the shipped path:
// Parse must not pay for it, and a pooled gll reused by Parse after
// ParseProfile must not still be counting.
func TestQuestionCensusOffOutsideProfile(t *testing.T) {
	c := compileDoc(t, "docs/examples/json.xbnf")
	in := nestedJSON(4 << 10)
	if _, prof := c.ParseProfile("json", in); prof.DFAQuestions+prof.TermQuestions == 0 {
		t.Fatal("ParseProfile counted nothing")
	}
	res, p := c.run("json", in)
	if !res.OK {
		t.Fatalf("parse: %s", res.Error)
	}
	if p.diag {
		t.Fatal("run left the census armed")
	}
	if p.qc != (questionCounts{}) {
		t.Fatalf("run counted questions: %+v", p.qc)
	}
	p.release()
}

func pct(part, whole int) float64 {
	if whole == 0 {
		return 0
	}
	return 100 * float64(part) / float64(whole)
}
