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
1-minute load is ≤ 2.5 and transient processes are below 40% CPU (always-on
AV such as Bitdefender is warned, not waited out), then alternates old/new
for 10 × 2 s with `GOMAXPROCS=1`. Pair ratios cancel common-mode drift. The
script prints `TIME` / `MEM` / `VERDICT`:

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

A deliberate tree change (grammar fix, new corpus pin, changed error text)
runs `make golden-update` and commits the new golden **in the same commit**
with the reason in the message. An optimisation candidate that needs a
golden update is not an optimisation; it is a tree change and is judged as
one. `TestLanguageManifests` also locks `Failed == 0` for every live track.

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

Current xbnf-only (post-🎯T20+++ `sym`/`reach` as `uMap`, 3 s × 5 on the
same machine): median **16.6 ms** / **2.35 MB** / 3 allocs. Same-session
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
