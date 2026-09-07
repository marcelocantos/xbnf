# Parse speed log

Standing comparison of `engine.Parse` on `docs/examples/json.xbnf` against
Go’s `encoding/json` and arr-ai/wbnf on the same input. Update this file
when a parse-speed change lands.

## How to measure

Keep or discard a parse-speed change with the interleaved gate, not a lone
`go test -bench` median. Absolute ns/op wanders with package temperature and
other P-core load on this laptop; B/op and allocs/op do not.

```sh
make bench-stable              # working tree vs HEAD
make bench-stable BASE=a9c96f5
make bench-stable SELF=1       # same binary both sides; must be SELF-OK
```

That compiles two `engine` test binaries (worktree for `BASE`), waits until
1-minute load is ≤ half of the logical CPU count (`LOAD_FRAC=0.50`;
`LOAD_MAX` is an absolute override) and transient processes are below 40% CPU
(always-on AV such as Bitdefender is warned, not waited out), then alternates
old/new for 10 × 2 s with `GOMAXPROCS=1`. Load average is a runnable-thread
count (≈ busy cores), not a utilisation fraction: the old fixed 2.5 was ~2.5
cores on any machine, which is 63% of a 4-core host and 16% of this 16-core
one. Pair ratios cancel common-mode drift. The script prints `TIME` / `MEM` /
`VERDICT`:

| VERDICT | Meaning |
|---|---|
| `KEEP` | New is ≥3% faster on ≥80% of pairs, or time is a tie and B/op or allocs dropped |
| `DISCARD` | Slower, or a tie with flat memory — do not keep the change for speed |
| `NOISY` | Pair ratios still wander, or the machine never went idle. **Do not decide** |
| `SELF-OK` / `SELF-FAIL` | Harness check; `SELF-FAIL` means the machine is too busy to trust A/B |

Exit 2 on `NOISY` or `SELF-FAIL`. Cite the `VERDICT` line and the pair
speedups in the history note. ns/op in the table is a snapshot only — do not
compare it across sessions.

`make bench-json` is the older snapshot: `BenchmarkJSON64K` plus wbnf and
stdlib, 2 s × 3 on `nestedJSON(64<<10)` (~65 553 bytes). Use it for the
side-by-side latest table, not for keep/discard.

Keep [Probe harness](#probe-harness-genjson-64-kb) separate: those rows used
a different `genJSON` helper, not `nestedJSON`. Pigeon numbers in that
section are a frozen record from the evaluation, not something to re-run.

## Golden ratchet (🎯T25.1)

Speed changes must not change trees. `make test` runs `TestGolden` in
`engine` (nestedJSON 64 KB on `docs/examples/json.xbnf`) and in `eval`
(every fixture of every manifest under `eval/testdata`, live, historical and
json-smoke). Each fixture's `engine.Fingerprint` — ok, end, packed, error
text, node count and a preorder SHA-256 of kind/name/text — must equal
`engine/testdata/golden.json` / `eval/testdata/golden.json`.
Public partial trees returned on failure are included; an absent failure tree retains the empty fingerprint.
The engine also locks `s -> "a"; #wrap -> ();` on `ab` as `partial-tree-ab`.

The T30 coverage correction deliberately rebaselined ten failed corpus tree hashes and node counts, with all other
fields and all existing successful fingerprints unchanged. It preceded the T29 engine correction. See
[capture context and ratchet evidence](capture-context.md) for the mechanism, cases and remaining limits.

A deliberate tree change (grammar fix, new corpus pin, changed error text)
runs `make golden-update` and commits the new golden **in the same commit**
with the reason in the message. An optimisation candidate that needs a
golden update is not an optimisation; it is a tree change and is judged as
one. `TestLanguageManifests` also locks `Failed == 0` for every live track.

## T29/T30 correctness follow-up (2026-09-06)

The [capture-context correction](capture-context.md) fixes order-dependent reference matching and invalid selected
bindings while preserving shared callees. The fresh full suite, vet, standing build checks, real CLI capture journey and
42/42 live language comparisons pass. All existing successful golden fingerprints remain unchanged; T30's deliberate
failed-tree fingerprint update is described above.

Earlier the same day, the normal corpus gate against `9c47802` stopped at the idle check after 90 seconds: load1 was
24.15 against a 2.5 maximum. Two diagnostic runs used `--skip-idle --pairs 4 --benchtime 500ms` to inspect allocations
on the same shipped `Compiled.Parse` benchmark and unchanged fixtures. The first was NOISY. It exposed
capture-descriptor map reconstruction; reusable chart indexing removed most of that allocation:

| Workload | Pre-fix baseline B/op | First correction B/op | Final pooled correction B/op |
|---|---:|---:|---:|
| XML corpus | 1.29 MB | 4.56 MB | 1.38 MB |
| Corpus aggregate | 3.20 MB | 6.48 MB | 3.30 MB |

The other six languages' B/op remain unchanged at the harness's displayed precision. Pooled descriptor storage counts
toward the chart capacity bound; this trades repeated allocation for retained storage. Per-parse binding and completion
maps still allocate. These figures do not measure total retained memory or compilation cost.

The final diagnostic reported an aggregate old/new timing ratio of 0.974×, with one candidate win in four pairs.
Python and YAML reported 0.942× and 0.947× respectively; XML's timing row was NOISY. Its overall script verdict was
DISCARD (TIME: TIE, MEM: LOSE), an optimization keep/discard classification rather than a reason to restore incorrect
capture semantics. The idle check was deliberately skipped and the run was short, so those are cost signals for
investigation, not a stable latency estimate. No claim that T25's exact speedup survives this correction has been made.

Raw diagnostic logs: `/tmp/xbnf-t2930-corpus-gate.txt`, `/tmp/xbnf-t2930-corpus-allocation-probe.txt`, and
`/tmp/xbnf-t2930-pooled-allocation-probe.txt`.

## T31 stable gates vs `9c47802` (2026-09-08)

HEAD `86e10f1` (T29/T30 engine from `41ffcd3`, idle bar `LOAD_FRAC=0.50`) versus `9c47802`, default **10 × 2 s**,
`GOMAXPROCS=1`, idle and pair-ratio noise checks **not** skipped. Benchmark definitions and fixtures were not changed.
The seven-language corpus is primary; JSON 64 KB is auxiliary. Earlier `VERDICT: NOISY` runs are not a speed baseline.

### Corpus (primary)

One full-discipline run, idle load1=5.13 (max 8.00 = 0.50 × 16 cpus), end load1=2.84. Valid (not `NOISY`):

| | old `9c47802` | new `86e10f1` |
|---|---:|---:|
| aggregate speedup | 0.907× (2/10 new-faster) | |
| B/op | 3.20 MB | 3.30 MB |
| TIME / MEM / VERDICT | LOSE / LOSE / **DISCARD** | |

Per-language rows (B/op is median of the ten pairs):

| Language | speedup | wins | tag | B/op old | B/op new |
|---|---:|---:|---|---:|---:|
| commonmark | 1.013× | 7/10 | DISCARD | 0.26 MB | 0.26 MB |
| go | 0.979× | 1/10 | DISCARD | 0.13 MB | 0.13 MB |
| javascript | 0.980× | 0/10 | DISCARD | 0.05 MB | 0.05 MB |
| python | 0.939× | 0/10 | LOSE | 0.19 MB | 0.19 MB |
| sql | 0.865× | 2/10 | LOSE | 1.26 MB | 1.26 MB |
| xml | 0.932× | 2/10 | LOSE | 1.29 MB | 1.38 MB |
| yaml | 0.984× | 1/10 | DISCARD | 0.03 MB | 0.03 MB |
| **aggregate** | **0.907×** | **2/10** | **DISCARD** | **3.20 MB** | **3.30 MB** |

Pair totals (ms): old 23.32 23.90 23.07 24.00 25.71 23.17 23.08 25.08 23.87 23.70; new 27.45 26.03 23.88 23.92 28.67 33.78 29.16 26.00 23.80 28.04.

**Allocation vs retained memory.** B/op is repeated-parse allocation on `Compiled.Parse`, not RSS and not compile-time
retained size. XML +0.09 MB and aggregate +0.10 MB match the pooled-correction diagnostic. That extra B/op is the
capture-descriptor chart index participating in the existing chart-pool capacity bound: it is retained storage reused
across parses, not a per-parse map rebuild. The other six languages' B/op are unchanged at the harness's displayed
precision. Binding-intern and completed-context maps remain per-parse allocations (`docs/capture-context.md`). These
B/op figures do not measure total retained process memory.

Python (0.939×, 0/10), SQL (0.865×, 2/10) and XML (0.932×, 2/10) are language-level `LOSE` rows under an aggregate
`TIME: LOSE`. That is a measured cost of the T29/T30 correction, not a reason to restore pre-T29 capture semantics.
`DISCARD` is the optimisation keep/discard label for a valid slower-or-tied run.

### JSON 64 KB (auxiliary)

One full-discipline run, idle load1=7.86, end load1=4.60. Valid (not `NOISY`):

| | old `9c47802` | new `86e10f1` |
|---|---:|---:|
| median ns/op | 3.53 ms | 3.62 ms |
| pair speedup median | 0.974× (2/10 new-faster, ratio cv 12.2%) | |
| B/op | 2.35 MB | 2.35 MB |
| allocs/op | 2 | 2 |
| TIME / MEM / VERDICT | TIE / TIE / **DISCARD** | |

JSON allocation is unchanged. The DISCARD is the optimisation keep/discard label, not a reason to revert T29.

Raw logs of the valid pair: implementer scratch `corpus-gate.txt` and `json-gate.txt` from 2026-09-08 (idle bar 8.00,
`VERDICT: DISCARD` both). Prior `NOISY` attempts the same night are not this baseline. An independent `/vcheck T31`
re-run at `86e10f1` also printed `VERDICT: DISCARD` for both gates (corpus TIME LOSE / MEM LOSE, aggregate 0.968×,
B/op 3.20→3.30 MB; JSON TIME LOSE / MEM TIE, B/op 2.35 MB).

## Corpus gate (🎯T25.2)

`BenchmarkCorpus` in `eval` parses every accept fixture of each live
manifest per iteration (warm varied) as one sub-benchmark per language.
Bytes are the summed fixture sizes. It is the second half of keep/discard:
a JSON win that loses a language corpus needs a written justification.

`scripts/bench-json-stable.sh --bench corpus` (or `make bench-corpus` / `make
bench-stable BENCH=corpus`) runs this gate with the same interleaved-pairs
harness as the JSON gate: same idle wait, same alternating old/new at
`GOMAXPROCS=1`, same pair-ratio statistics — but built with
`go test -c -o ... ./eval` and run with cwd `eval/` so `testdata` resolves,
parsing every `BenchmarkCorpus/<language>` line per run instead of one
`BenchmarkJSON64K` line.

```sh
make bench-corpus                        # dirty tree vs HEAD
make bench-stable BENCH=corpus BASE=a9c96f5
make bench-stable BENCH=corpus SELF=1    # harness sanity, same binary both sides
```

It prints one pair-ratio row per language (median speedup, wins/pairs, B/op
old/new, verdict) plus an **aggregate** row: the same statistics computed on
the sum of per-language ns/op (and B/op, allocs/op) for each run. Final
`TIME:` / `MEM:` / `VERDICT:` reflect the aggregate. `VERDICT: KEEP` requires
the aggregate to be `KEEP` *and* no language individually `LOSE` (real
regression, not just a flat tie); a language `LOSE` under an aggregate
`KEEP` prints `VERDICT: MIXED` (exit 0) instead — cite it in the history
note and justify the regressed language(s) explicitly. Aggregate `NOISY` or
`SELF-FAIL` behave exactly as in the JSON gate (exit 2; do not decide).
`--bench json` (the default) is unchanged.

## 🎯T25 optimisation programme (2026-09-06)

Every documented opportunity in `docs/parse-performance-research.md` has a
verdict here. Mechanism counts are deterministic (`xbnf eval` profiles and
`c.run` on nestedJSON 64K); timings are from the interleaved gates in the
table at the end of this section. Trees were golden-identical for every
kept change (no `make golden-update` in this programme).

| Opportunity | Verdict | Mechanism evidence |
|---|---|---|
| H2 straight-line fusion (§5.2) | KEEP | Descriptors JSON 102810→78745 (−23%), xml −12%, commonmark −12%, sql −3%. Intermediate slots still deduped via U (`claim` without push). Steps/completions/packed unchanged. |
| H3 predecessor-linked evidence (§5.3) | KEEP | Descriptors carry evidence cells; `path` is a link walk; packSteps/matchSteps/sort/stepReach deleted (−198 lines). Instrumented: 119,781 matched steps, 0 unreachable, so `stepReach` was a tautology. Agent interleaved A/B on JSON: median 1.3×. |
| Unit-production shortcut (§5.2/§7.3) | KEEP | `X?` lowers to one NT; unit prods (`$q→$qs`, `$d→$dl`, stack fallbacks) fork the target in place and pop through the chain. Descriptors JSON 78745→40112, sql 353343→144176, python 21701→8357, go 8564→4671; GSS sql 131198→101668. |
| DFA ASCII tables + allocation-free twalk (§7.2) | KEEP | `dfaState.ascii[128]` before the rune map; twalk uses a scratch stack + arena, `describeElem` interned (was `strconv.Quote` per failure). Allocs/parse commonmark 32264→1469, xml 19692→3101; commonmark ns/op −69% (indicative). |
| Root-end tracking | KEEP | `endAt` map written per completion, read only for the root: replaced by three ints (`rootAt`). `noteEnd` was 44% of `uMap.get` time on JSON. |
| Allocation-free span selection | KEEP | `pick` scratch buffer, `symMore` as a pooled slab. Allocs/parse sql 16706→12, commonmark 1469→49. |
| H1 callee sharing (§5.1) | KEEP | Counter first: duplicated callee descriptors 3.3% (JSON), 8.0% (sql), 10.1% (go), 14.2% (python). Implemented Afroozeh-style: GSS keyed by (nonterminal, position), return slot + caller + cell on the edge, productions forked once per node. Descriptors JSON 40112→27491, xml 58971→39147, python 8357→5656; edges −33%; CalleeReuse 0 everywhere. Agent's interleaved run: JSON 1.078× (5/6), corpus aggregate 1.18× (6/6). |
| Chart pool across varied inputs | KEEP (xml MIXED) | Diagnosis: `gllPool` was a `sync.Pool`, emptied at every GC, so 0.2% of `newGLL` calls rebuilt multi-MB tables (~30% of bytes). Now a bounded free list (8 charts, 16M cells) and `uMap` windows sized per hint bucket with a spare array. B/op sql 6.30→2.70 MB, python 1.37→0.19 MB, commonmark 0.76→0.34 MB; sql −15%, python −13%, xml +7–10% (indicative). |
| Builder on nonterminal ids | KEEP | Precomputed `ntInfo` (kind, class, name), `spineProd`, per-element tree kind/name; `splices` and `IsDFA` string lookups gone from the per-node path; `materialize` is a flat BFS. Builder subtree 34%→14% of JSON samples; indicative JSON −20%, corpus −7–13%. |
| Frontier expectations without a map | KEEP (tie) | `failInfo.want` is a deduplicated slice; error text byte-identical (formatExpect sorts). Timing within noise; removes `mapassign_faststr` (2.7% corpus). |
| §7.1 duplicate terminal questions | DISCARD | Census on t25-opt: DFA questions repeated 0% (json), 14% (sql), 54% (yaml), 33% (go); but `dfa.match` is ≤4% of time, so a memo's ceiling is 2.2% (yaml) at the cost of a `uMap` probe per question. Not implemented. |
| §7.1 batched joins | DEFERRED | Join fanout is bounded by GSS edges per node; after unit shortcut the GSS is the sharing point already. Reopen if a language shows a node with hundreds of edges in `ParseProfile`. |
| H4 context projection (§6.1) | DEFERRED | The engine has no environment: `@col` is a function of position, `%name` resolves inside the production instance (now via evidence links). Reopen when a grammar needs a parent's indentation or lexical mode in the invocation key. |
| §7.3 deterministic regions | DEFERRED | Fusion and the unit shortcut already run straight-line and chain regions without scheduling. A separate LL mode needs a soundness certificate the compiler does not have. |
| §7.4 transfer summaries | REJECTED | Incremental parsing under another name; requires external-read tracking. Out of scope for this engine. |
| Flat `slotFirst` + admits by pointer | KEEP | One flat `[]firstInfo` indexed by `prod.firstBase+ip`, hot fields first, ASCII byte decided from one bitset word without decoding. Agent min-of-N 1.041×; `admits` 0.56→0.24 s per 600 parses. |
| 16-byte `uMap` slots | KEEP | `uMap` generic over the id type; `uset`/`gssAt`/`moreAt` use int32 (U set for 64 KB JSON 1.5→1 MB); `sym` keeps int64 (packComp). Agent: 1.064× 11/12 KEEP; both micro changes together 1.133× 11/12 KEEP, corpus aggregate 1.126× all languages KEEP. |
| Builder: skip packed probe, int32 arena, inline leaf elements | KEEP (marginal) | `compsAt` skips the `moreAt` probe when nothing packed this parse; `inode` 64→48 bytes with input spans instead of text headers; leaves built inline. Builder share 27%→24.5%; agent gates 1.03–1.05× with 75–95% of pairs, formally NOISY under load. |
| `wrapEnd` as int32 | see final gate | Halves the 8-bytes-per-position skip memo read at random by every element and builder span. |
| Per-slot 128-byte ASCII admit table | DISCARD | 0.967× (a second cache line for what the bitset already answers). |
| Dense `firstBase []int32` | DISCARD | 1.002× tie. |
| `treeKind`-first element dispatch + span memo | DISCARD | Builder 1.52→1.57 ms per parse. |
| Final cycle: `noteFail` on the admits rejection path; inlining `admits` into `claim` | not attempted (ceiling < gate) | Profile after round four attributes 80 ms and 50 ms of 3.1 s (2.6% and 1.6%) to these; each is below the gate's 3% KEEP bar on its own. The exploring agent was cut off by the account spend limit before measuring; recorded as the open tail. |
| §7.2 bulk homogeneous scans | not attempted | After ASCII tables `dfa.match` is 17% of commonmark and ≤5% elsewhere; a run-scan loop has ~5% ceiling on one language. |

### Gate results, t25-opt vs master (8dfa60a)

Interleaved, 10 × 2 s pairs, `GOMAXPROCS=1`, idle-checked (load1 ≤ 2.5).

| Gate | Candidate | old median | new median | pair speedup median | new faster | B/op | VERDICT |
|---|---|---:|---:|---:|---:|---|---|
| JSON 64K | 3aa5c02 (through H1) | 12.26 ms | 4.84 ms | 2.520 | 10/10 | 2.35 → 2.35 MB, allocs 3 → 2 | KEEP |
| JSON 64K | ab03b6e (all rounds) | 11.78 ms | 4.06 ms | 2.882 | 10/10 | 2.35 → 2.35 MB, allocs 3 → 2 | KEEP |
| Corpus aggregate | 681d1fb (all rounds) | — | — | 3.043 | 10/10 | 18.89 → 3.20 MB | KEEP |
| Corpus commonmark | 681d1fb | | | 4.070 | 10/10 | 12.61 → 0.26 MB | KEEP |
| Corpus xml | 681d1fb | | | 3.541 | 10/10 | 2.51 → 1.29 MB | KEEP |
| Corpus sql | 681d1fb | | | 2.787 | 10/10 | 3.17 → 1.26 MB | KEEP |
| Corpus python | 681d1fb | | | 2.532 | 10/10 | 0.25 → 0.19 MB | KEEP |
| Corpus javascript | 681d1fb | | | 2.493 | 10/10 | 0.05 → 0.05 MB | KEEP |
| Corpus go | 681d1fb | | | 2.449 | 10/10 | 0.22 → 0.13 MB | KEEP |
| Corpus yaml | 681d1fb | | | 2.205 | 10/10 | 0.07 → 0.03 MB | KEEP |

The pool change's indicative xml loss did not survive integration: xml is
3.5× faster at the gate. Per-change attribution comes from the agents'
interleaved runs in the table above, not from re-gating each commit.

**Fixed point.** After round four every remaining candidate in the profile
is below the gate's own KEEP bar (3% median, 80% of pairs): the largest is
the `noteFail` rejection path at 2.6%. The public tree (`materialize`,
2 allocs, 2.35 MB on JSON) and its collection are the API floor; changing
that is a `Node` layout decision (see 🎯T23 for positions), not an
optimisation.

## Latest (2026-09-05, Apple M4 Max)

`nestedJSON(64<<10)`. xbnf and stdlib: `c077348`, count=2 side-by-side.
wbnf: same machine and input, `BenchmarkJSON64K_wbnf` count=2 (25.7 ms).

| Parser | ns/op | MB/s | B/op | allocs/op |
|---|---:|---:|---:|---:|
| xbnf `engine.Parse` + tree | 31.7e6 | 2.07 | 48.07e6 | 294501 |
| wbnf 0.41.0 (`testdata/json.wbnf`) | 25.7e6 | 2.55 | 22.83e6 | 598593 |
| `encoding/json.Unmarshal` into `any` | 0.667e6 | 98.2 | 0.580e6 | 14039 |
| `json.Valid` | 0.172e6 | 381 | 0 | 0 |

xbnf is ~1.2× wbnf, ~47× `Unmarshal`, ~180× `Valid`. A dedicated xbnf-only
run at `c077348` was ~23–25 ms/op / 48 MB.

Current xbnf-only (post-🎯T25, final gate 2026-09-06, 10 × 2 s interleaved
vs master on the same machine): median **4.06 ms** / **2.35 MB** / 2 allocs
(master 11.78 ms in the same run). Same-session
`a9c96f5` was median **19.8 ms** on a hot machine (quiet T20++ was 11.0 ms).
`sym` and `reach` are `uMap`; `endAt` tracks max right without iterating
`sym`; all step runs are sorted and binary-searched; tables grow at load
1/4. Remaining B/op is the public `Node` tree.

## History

`nestedJSON` / `BenchmarkJSON64K` unless noted. Times are median of the
recorded run.

| Date | Commit | xbnf ns/op | xbnf B/op | wbnf ns/op | Unmarshal ns/op | Valid ns/op | Note |
|---|---|---:|---:|---:|---:|---:|---|
| 2026-09-02 | `9b65539` | 27.8e6 | 43e6 | — | — | — | 🎯T15; greedy tree still, not GLL-derived |
| 2026-09-02 | `6c0ba27` | ~100e6 | 581e6 | — | — | — | 🎯T16 trees; T18 later measured 73–120 ms / 581 MB |
| 2026-09-02 | `351a4a8` | 53e6 | 95e6 | — | — | — | 🎯T18 linear tree flatten; attested 51–55 ms. Same-day re-run 42–45 ms / 95 MB |
| 2026-09-02 | `6d9786d` | 24e6 | 48e6 | — | — | — | FIRST jump table, chart reuse, packed U/GSS, alloc cuts |
| 2026-09-03 | `c077348` | 31.7e6 | 48.07e6 | 25.7e6 | 0.667e6 | 0.172e6 | Unicode FIRST fix; first stdlib+wbnf side-by-side on `nestedJSON` |
| 2026-09-03 | `dbe1b6a` | 35.3e6 | 44.29e6 | — | — | — | 3s×5 same-session vs pool: 32.1/32.3/35.3/35.9/36.8 ms, 228223 allocs. Earlier 2s×3 21.0/23.1/25.4 was underpowered. First prod per span, stack path buf, pre-sized maps. |
| 2026-09-03 | `498a9b3` | 27.2e6 | 22.04e6 | — | — | — | 3s×5: 24.5/25.1/27.2/27.5/30.5 ms, 226920 allocs. ~23% faster than same-session no-pool, not a regression. CPU `runtime.madvise` 17.6%→8.6% (GC returning ~44 MB charts). `newGLL` 49% of alloc_space before, gone after. B/op is repeated-parse; one-shot still ~44 MB. Inline first GSS edge discarded earlier (time up, B/op flat). |
| 2026-09-03 | `7504667` | 22.9e6 | 6.85e6 | — | — | — | 3s×5: 20.0/21.3/22.9/23.6/24.0 ms, 137314 allocs. Node arena (int kids, materialize once) + bump GSS edges/pops/steps. Chart slice allocs gone from pprof (advance/pop ~6 objects). |
| 2026-09-03 | T20 | 19.0e6 | 4.00e6 | — | — | — | 3s×5: 20.4/19.0/19.2/17.1/17.5 ms, 23338 allocs. Kept: contiguous per-instKey steps + stack cands (pred-map index dropped), derive scratch, packed Node array, describe intern, pooled reach memo. Discarded: custom U set (2.2 s/op), further skip (already memoised). |
| 2026-09-03 | T20+ | 13.4e6 | 2.51e6 | — | — | — | 3s×5: 13.2/14.2/13.3/13.4/14.1 ms, 12 allocs. Kept: candBuf on stack (was heap via orderCands), linear matchSteps + no sort for n≤32, wrapEnd reuse on same input. Left materialize as []Node API floor. |
| 2026-09-04 | T20++ | 11.0e6 | 2.35e6 | — | — | — | 3s×5: 10.88/11.04/10.91/11.76/11.67 ms, 3 allocs. Same-session 075a8d9 was 12.78/13.09/13.95/14.12/14.22 ms, 2.51 MB, 12 allocs. Kept: generational maps, slimmer steps, last-stepAt cache, flatten-all + radix large runs, pooled spines, packed sym, compile-time dfa/nid, ASCII decodeRune. Discarded: admits-before-U (noise). Left materialize as []Node API floor. |
| 2026-09-05 | T20+++ | 13.6e6 | 2.35e6 | — | — | — | 3s×5: 13.55/14.13/13.61/16.12/12.84 ms, 3 allocs. Same-session Go map U was ~15.3 ms (this session was hotter than the 11.0 T20++ run). Kept: splitmix64 uSet/uMap for U, gssAt, stepAt (probe+place, not k&mask); packed uint64 reach. Discarded: position-indexed U lists (tie). Isolated microbench ~2× std map (`uSet` later folded into `uMap`; the benchmark is now `BenchmarkMapVsUMap`). Left materialize. |
| 2026-09-05 | T20++++ | 16.6e6 | 2.35e6 | — | — | — | 3s×5: 19.34/17.41/16.03/16.64/16.54 ms, 3 allocs. Same-session a9c96f5 was 16.65/19.05/21.54/25.43/19.82 ms (hot). Kept: sym+reach as uMap, endAt max-right, always-sort + matchSteps, load 1/4, packSteps pre-size. Discarded: in-parse step regions (copy still needed; pre-size is enough). Left materialize. |

## Probe harness (`genJSON`, 64 KB)

2026-09-02 quality evaluation, Apple M4 Max. Scratch module: xbnf, wbnf
v0.41.0 on `json.wbnf` (now `engine/testdata/json.wbnf`), mna/pigeon v1.3.0
generated JSON PEG (`Parse`; `Memoize(true)` is the memo column), and
`encoding/json`. **Not** `nestedJSON`. Do not merge these ns/op values into
the table above.

| When | xbnf ns/op | xbnf B/op | wbnf ns/op | wbnf B/op | pigeon ns/op | pigeon B/op | pigeon+memo ns/op |
|---|---:|---:|---:|---:|---:|---:|---:|
| Before 🎯T15 | 1341e6 | 1293e6 | 24.4e6 | 22.71e6 | 13.5e6 | 9.58e6 | 48.9e6 |
| After 🎯T15 (`9b65539` era) | 28.9e6 | 43.2e6 | 25.4e6 | — | — | — | — |
| After 🎯T16 trees | 120.5e6 | 580.9e6 | 36.0e6 | 22.81e6 | 18.8e6 | 9.59e6 | 58.0e6 |

Allocs on the pre-T15 64 KB run: xbnf 3.80M, wbnf 598k, pigeon 282k,
pigeon+memo 439k. After T16: xbnf 544k, wbnf 599k, pigeon 282k, pigeon+memo
439k. Stdlib Unmarshal on this input was ~0.65e6 ns before T15 and 0.78e6 ns
after T16; `json.Valid` after T16 was 0.19e6 ns.

Pigeon (no memo) was the fastest generic parser in that harness: ~13–19 ms
vs wbnf ~24–36 ms vs xbnf 1.3 s → 29 ms → 120 ms. Memoize made pigeon
slower and fatter on this grammar. Frozen snapshot; do not re-measure.
