# Review of the T25 optimization programme

**6 September 2026.** Reviewed engine revision `fda3284` against `8dfa60a`, the baseline after the golden ratchet and
corpus benchmark were installed. This review combines source inspection, the mnemo-recorded implementation session,
retained benchmark output, and fresh correctness checks. It does not report a new timing run.

The programme delivered the step change proposed in the [research paper](parse-performance-research.md): approximately
three times faster warm parsing across the current language corpus, with unchanged golden outputs. It validates the
paper's central mechanism—eliminating repeated work and carrying evidence directly—within the existing GLL/DFA runtime.
It does not establish an architectural fixed point for generalized parsing.

## 1. What the session history establishes

The primary mnemo session is `38804713-d366-47dc-ba1d-a96e7f9fed58`. The recorded conversation first reviewed the paper and
found that the original language harness had not successfully parsed its positive fixtures. By the next review, the
grammar work had landed separately: all 42 live fixtures passed their current checks, and Go had replaced C++ in the
live portfolio. C++ remains historical. This review does not attribute that earlier scope change to T25.

The session explicitly distinguished two verification obligations: preserving existing engine output during optimization,
and establishing that the language grammars produce the correct structures. It built the golden ratchet before changing
the engine, while filing independent structural comparisons and larger corpora as deferred targets, now 🎯T26 and 🎯T27.
That was a reasonable way to unblock bounded optimization; it limits what the resulting evidence establishes.

Four rounds followed: H2 fusion and H3 evidence links; DFA and pooling work; lowering and tree-allocation changes; H1
callee sharing and further representation changes. The history records useful corrections during integration. In
particular, the initial fusion experiment was replaced by a version that retained intermediate descriptor deduplication.
H3 was rebased onto that implementation, and competing-frame evidence received a dedicated regression test.

The final exploration contained two unmeasured leads, explicitly disclosed in the completion report. Keep two events
separate: the session recorded an earlier spend interruption at 12:43 Melbourne time, then declared completion at 13:26;
the later spend-limit response at 14:15 happened **after** completion. That later response is not evidence that the
optimization goal was unfinished. The reservations below concern the experiments and claims themselves.

## 2. The measured improvement is substantial

The retained `gate-json-final.txt` and `gate-corpus.txt` outputs agree with the [speed log](parse-speed.md). Both compare
against `8dfa60a`, alternating baseline and candidate across ten pairs with `GOMAXPROCS=1`. Every language improved in
every pair. Benchmark definitions, corpus pins, and golden files did not change between the baseline and reviewed head.
Only documentation and target state changed after the final corpus candidate, `681d1fb`.

| Workload | Recorded median pair speedup |
|---|---:|
| CommonMark | 4.07× |
| XML | 3.54× |
| SQL | 2.79× |
| Python | 2.53× |
| JavaScript | 2.49× |
| Go | 2.45× |
| YAML | 2.21× |
| Corpus aggregate | 3.04× |
| JSON 64K, auxiliary benchmark | 2.88× |

The corpus aggregate sums per-language iteration times; it is not the average language speedup. Allocated bytes per
aggregate iteration fell from 18.89 MB to 3.20 MB. JSON fell from 11.78 ms to 4.06 ms while retaining approximately
2.35 MB of allocation, with allocations falling from three to two.

These are convincing large improvements, not precision estimates to three decimal places. The final JSON pair-ratio
coefficient of variation was 6.6%. The idle check passed at startup; load rose during both runs. The harness accepts
strong directional agreement despite this variation. The evidence supports an approximately threefold gain much more
strongly than it supports fine comparisons among marginal changes.

Allocation is also not retained memory. Replacing `sync.Pool` with a bounded free list intentionally keeps charts alive
across garbage collections. That can improve throughput and B/op while increasing persistent memory. Compilation,
truly cold parsing, rejection throughput, and retained memory did not receive equivalent interleaved gates.

One attribution in the log should be weakened: the complete series making XML 3.54× faster does not prove the pooling
change's individual 7–10% XML regression disappeared. The other improvements could outweigh it. A final-branch ablation
with and without that change would answer the narrower question. This does not undermine the integrated speedup.

## 3. What changed architecturally

**H1 changed the identity of shared work.** GSS nodes now represent a callee and input position; edges carry return slots
and caller evidence. A callee expands once, and subsequent callers subscribe to its completions. This is the substantive
sharing change proposed by the paper, including propagation to late subscribers.

**H2 changed the scheduling unit.** Straight-line matches run together, retaining intermediate descriptor claims. The
unit-production shortcut also removes scheduling around synthetic wrappers and precedence fallback chains. These are
meaningful interpreter improvements without requiring a separate bytecode format or native compilation.

**H3 changed how the parser carries its justification.** Descriptors refer to evidence cells, including competing
predecessors. Tree reconstruction follows those links rather than packing, sorting and searching the old step log.
Mutable cells retain later CFG predecessors even after their shared descriptor has run. The removal of reconstruction
work is a stronger result than merely making the old searches faster.

The DFA and builder changes complement those transformations: ASCII transitions avoid rune-map work, tree construction
uses precomputed nonterminal information, and scratch storage avoids repeated allocation. The non-JSON profiles helped
identify these costs. Broadening the workload therefore already paid off, despite the corpus's remaining limitations.

The three primary hypotheses are supported as implemented mechanisms and as part of the measured package. The final
aggregate gate is not a controlled ranking of each individual hypothesis's contribution.

## 4. Correctness findings beyond the existing fixtures

### 4.1 Capture bindings remain dependent on alternative order — 🎯T29

This minimal grammar compiles on both the baseline and reviewed head:

```xbnf
s -> n=A B "!" %n ;
A -> (?="a") "a" #prefer | (?="a") "aa" ;
B -> (?="a") "a" #prefer | (?="a") "aa" ;
#wrap -> () ;
```

Both reject `aaa!a`, although the complete derivation `n="a", B="aa", %n="a"` satisfies it. Reverse only A's unordered
alternatives, preserving the preference on the same short alternative:

```xbnf
A -> (?="a") "aa" | (?="a") "a" #prefer ;
```

Both now accept, but return a tree containing `n.Text="aa"` and `ref.Text="a"`. Independently of how a preference chooses
between successful derivations, unordered source order must not change acceptance, and the returned binding and
reference must agree.

The mechanism is visible in [boundText](../engine/gll.go): it walks only the head predecessor through merged evidence
cells. Descriptor identity does not distinguish relevant capture bindings, and tree selection can subsequently select
another predecessor. Sharing recognition state is insufficient when future recognition depends on which proof reached
that state. The existing competing-capture test does not cover this reconvergence through another nonterminal before
the reference.

This defect **predates T25**; it is not evidence that H1 or H3 introduced a regression. It does establish that the H4
question is already relevant. A capture local to a production is still semantic state if two derivations reach the same
slot and position with different captured values. Correct handling could distinguish the relevant binding in execution
state or propagate newly arriving binding alternatives through dependent operations. It need not introduce a general
environment on every context-free call. Whichever representation is chosen, recognition and the selected tree must
retain the same evidence relationship.

### 4.2 Failed parses can return trees that the golden ratchet ignores — 🎯T30

For `s -> "a"; #wrap -> ();` on input `ab`, shipped `Compiled.Parse` returns `OK=false`, `End=1`, and a partial rule tree
whose text is `"a"`. [FingerprintResult](../engine/fingerprint.go) hashes trees only when `OK` is true; this result gets
`Nodes=0` and the hash of empty input. A change to its partial tree can therefore pass the golden check.

The ratchet remains useful for successful outputs. Its claim to preserve all public trees needs this gap closed, with a
deliberate update of the failed-fixture fingerprints. This is distinct from 🎯T26, which compares against independent
language parsers rather than yesterday's xbnf output.

### 4.3 Bounded differential review

A supplementary review compared 722 grammar/input combinations across the two revisions, covering plain CFG and
capture cases. It established no new recognition or successful-tree regression. It did find three changes limited to
the rule name in an error message. For example:

```xbnf
s -> A "!" ;
A -> (A "a" | "a") #assoc=left ;
#wrap -> () ;
```

On `aa`, both report the same expectations and end-of-input location, but the baseline names rule `A` and the new engine
names rule `s`. Thus byte-identical diagnostics are established for the golden fixtures, not for arbitrary grammars.
These finite checks are regression evidence, not a proof of engine equivalence.

The known shared-Compiled concurrency issue remains recorded separately as 🎯T28. This review did not rerun its race
reproduction or claim it resolved.

## 5. The corpus supports broader experiments, not generalized saturation

The warm benchmark contains 29 accepted fixtures totaling 177,148 bytes across seven tracks. That total is a sequence of
small documents, not one 177 KB representative input. The entire accepted YAML track is 583 bytes; JavaScript is 1,451
bytes; Python is 4,533 bytes. SQL and XML provide more substantial inputs. Rejected fixtures participate in correctness
checks but not the warm benchmark.

The [coverage report](eval/T22.md) is explicit about missing constructs: JavaScript has no regular-expression lexical
goal or automatic semicolon insertion, Python omits significant language features, and CommonMark treats inline content
as line text. C++'s context-sensitive challenges are absent from the live set. Go contributes useful syntax but does not
replace those particular challenges.

External oracles currently return acceptance booleans, including for tree-class inputs. CommonMark's green result does
not establish a cmark-equivalent tree. The golden ratchet freezes current behavior; it cannot establish that this behavior
is correct. These limitations justify the existing 🎯T26 and 🎯T27 targets rather than invalidating the measured work.

Fresh `make eval-languages` passed 42/42 fixtures during this review with reference results present. The standing syntax
checks can fall back to pinned expectations when an external oracle is unavailable, so that fact should accompany a
claimed live comparison; a green test exit alone is weaker evidence.

## 6. Revised research conclusion

The immediate structural programme was successful. There is no evidence here that a wholesale VM rewrite is the next
best investment. The current runtime now embodies important parts of the proposed execution strategy. Native code
generation remains unnecessary and outside the framework's contract.

The stronger fixed-point claim is unsupported for four reasons:

1. The 3% keep threshold is a measurement policy, not a bound on remaining architectural opportunity. Small profile
   leaves do not bound transformations that eliminate work across several leaves or change locality. Even the two
   explicitly unmeasured leaves could have interactions; their individual sample shares do not decide a combined change.
2. The same log lists an unattempted bulk-scan idea with an estimated 5% ceiling on CommonMark. That estimate is not a
   promised gain, but it contradicts a literal assertion that every remaining candidate is below 3%.
3. Deterministic regions, batched joins, richer context handling and other paper proposals were deferred or scoped out,
   not experimentally refuted. Narrow corpus coverage also limits their current cost estimates.
4. Transfer summaries were dismissed as incremental parsing, but paper section 7.4 explicitly proposes repetition
   **within one parse** first and separates editor-style incremental reuse. The difficulty of external-read tracking is
   real; the dismissal does not evaluate that proposal. There is still no evidence that it would be profitable.

Likewise, the required output node count supplies a lower bound on allocation and output work, not proof that today's
materialization and garbage-collection time are minimal. Further traversal or lifetime improvements could preserve the
same public API. A compact or lazy replacement API would be a separate experiment.

The next research priority is to improve what the measurements can distinguish: correct branch-dependent captures,
independent tree checks, larger and more representative language inputs, and separate warm, cold, compilation and
retained-memory measurements. The new capture counterexample supplies a concrete semantic requirement for that work.
It also motivates selective context-sensitive sharing rather than abandoning the successful context-free fast path.

A defensible completion statement is: **the programme achieved a substantial, measured improvement and reached a useful
stopping point for the implemented experiments on the current suite. Further architectural gains remain an open
empirical question.**

## Evidence and reproducibility

- Reviewed baseline: `8dfa60a`; engine head: `fda3284`; final corpus candidate: `681d1fb`.
- Primary history: mnemo session `38804713-d366-47dc-ba1d-a96e7f9fed58`, including the final reports and experiment
  dispositions. The later spend-limit response is separate from the completed programme.
- Historical timing evidence: that session's scratchpad `gate-json-final.txt` and `gate-corpus.txt`; summarized in
  [parse-speed.md](parse-speed.md). No new benchmark run was used for this review.
- Fresh validation: `go test ./... -count=1` with `GOWORK=off`, `make vet`, and `make eval-languages`; all passed. The
  uncached HTTP tests required permission to bind loopback ports beyond the initial filesystem sandbox.
- New counterexamples above were independently rerun through `Compiled.Parse`; the capture case was checked on both
  revisions. The complete small grammars and inputs are included here so temporary review runners are unnecessary.
- Follow-up targets: 🎯T29 for capture/evidence consistency; 🎯T30 for failed-tree fingerprints. Existing 🎯T26, 🎯T27 and
  🎯T28 retain their independent meanings. No parser implementation or benchmark data changed in this review.
