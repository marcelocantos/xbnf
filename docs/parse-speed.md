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
a different `genJSON` helper, not `nestedJSON`.

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

## Probe harness (`genJSON`, 64 KB)

2026-09-02 quality evaluation, Apple M4 Max. Scratch module compiled
`json.wbnf` (now `engine/testdata/json.wbnf`) with wbnf v0.41.0 and the
same `genJSON` as the xbnf/pigeon/stdlib benches in that harness. **Not**
`nestedJSON`. Do not merge these ns/op values into the table above.

| When | xbnf ns/op | xbnf B/op | wbnf ns/op | wbnf B/op | Note |
|---|---:|---:|---:|---:|---|
| Before 🎯T15 | 1341e6 | 1293e6 | 24.4e6 | 22.71e6 | xbnf quadratic; wbnf 2.69 MB/s, 598426 allocs. pigeon 13.5e6 ns, 9.58e6 B; stdlib Unmarshal ~0.65e6 ns |
| After 🎯T15 (`9b65539` era) | 28.9e6 | 43.2e6 | 25.4e6 | — | xbnf 2.27 MB/s, 198074 allocs; wbnf 25.383e6 ns (B/op not kept) |
| After 🎯T16 trees | 120.5e6 | 580.9e6 | 36.0e6 | 22.81e6 | xbnf 0.54 MB/s, 544152 allocs; wbnf 1.82 MB/s, 598566 allocs. pigeon 18.8e6 ns / 9.59e6 B |

wbnf stayed ~24–36 ms on this input while xbnf moved from 1.3 s → 29 ms →
120 ms (tree-builder copy) and later back to ~25–32 ms on `nestedJSON`.
