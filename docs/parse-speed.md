# Parse speed log

Standing comparison of `engine.Parse` on `docs/examples/json.xbnf` against
Go’s `encoding/json` and arr-ai/wbnf on the same input. Update this file
when a parse-speed change lands.

## How to measure

```sh
make bench-json
```

That runs `BenchmarkJSON64K`, `BenchmarkJSON64K_wbnf`,
`BenchmarkJSON64K_stdlibUnmarshal`, and `BenchmarkJSON64K_stdlibValid`
(`engine/scaling_test.go`) for 2 s × 3 on `nestedJSON(64<<10)` (~65 553
bytes). Append a row under [History](#history) with the median ns/op and
B/op, the commit SHA, machine, and a one-line note.

Keep [Probe harness](#probe-harness-genjson-64-kb) separate: those rows used
a different `genJSON` helper, not `nestedJSON`. Pigeon numbers in that
section are a frozen record from the evaluation, not something to re-run.

## Latest (2026-09-03, Apple M4 Max)

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

Current xbnf-only (🎯T19, 3 s × 5 on the same machine): median **22.9 ms** /
**6.85 MB** / 137314 allocs. Same-session pool-only baseline (`498a9b3`) was
median **27.2 ms** / **22.0 MB** / 226920 allocs. Node arena + bump-allocated
GLL charts cut repeated-parse B/op ~3× and allocs ~40%. One-shot still fills
the maps and charts (~44 MB TotalAlloc on a cold `sync.Pool`); B/op is the
repeated-`Parse` path. Remaining heap is the public `Node` tree (`materialize`)
and `instIndex`.

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
| 2026-09-03 | T19 | 22.9e6 | 6.85e6 | — | — | — | 3s×5: 20.0/21.3/22.9/23.6/24.0 ms, 137314 allocs. Node arena (int kids, materialize once) + bump GSS edges/pops/steps. Chart slice allocs gone from pprof (advance/pop ~6 objects). SHA filled on the follow-up stamp commit. |

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
