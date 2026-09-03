# Parse speed log

Standing comparison of `engine.Parse` on `docs/examples/json.xbnf` against
Go’s `encoding/json` on the same input. Update this file when a parse-speed
change lands.

## How to measure

```sh
make bench-json
```

That runs `BenchmarkJSON64K`, `BenchmarkJSON64K_stdlibUnmarshal`, and
`BenchmarkJSON64K_stdlibValid` (`engine/scaling_test.go`) for 2 s × 3 on
`nestedJSON(64<<10)` (~65 553 bytes). Append a row under [History](#history)
with the median ns/op and B/op, the commit SHA, machine, and a one-line note.

Do not mix this log with the old scratch harness that used a different
`genJSON` helper.

## Latest (2026-09-03, `c077348`, Apple M4 Max)

Same process, `nestedJSON(64<<10)`, `go test -bench BenchmarkJSON64K -benchtime=2s -count=2`:

| Parser | ns/op | MB/s | B/op | allocs/op |
|---|---:|---:|---:|---:|
| xbnf `engine.Parse` + tree | 31.7e6 | 2.07 | 48.07e6 | 294501 |
| `encoding/json.Unmarshal` into `any` | 0.667e6 | 98.2 | 0.580e6 | 14039 |
| `json.Valid` | 0.172e6 | 381 | 0 | 0 |

xbnf is ~47× slower than `Unmarshal`, ~83× fatter, ~21× more allocations, and
~180× slower than `Valid`. A dedicated xbnf-only run on this commit was ~23–25
ms/op / 48 MB; the side-by-side run is the one to compare against stdlib.

## History

xbnf `BenchmarkJSON64K` only, unless a stdlib column is filled. Times are
median of the recorded run.

| Date | Commit | xbnf ns/op | xbnf B/op | Unmarshal ns/op | Valid ns/op | Note |
|---|---|---:|---:|---:|---:|---|
| 2026-09-02 | `9b65539` | 27.8e6 | 43e6 | — | — | 🎯T15; greedy tree still, not GLL-derived |
| 2026-09-02 | `6c0ba27` | ~100e6 | 581e6 | — | — | 🎯T16 trees; T18 later measured 73–120 ms / 581 MB |
| 2026-09-02 | `351a4a8` | 53e6 | 95e6 | — | — | 🎯T18 linear tree flatten; attested 51–55 ms. Same-day re-run 42–45 ms / 95 MB |
| 2026-09-02 | `6d9786d` | 24e6 | 48e6 | — | — | FIRST jump table, chart reuse, packed U/GSS, alloc cuts |
| 2026-09-03 | `c077348` | 31.7e6 | 48.07e6 | 0.667e6 | 0.172e6 | Unicode FIRST fix; first stdlib side-by-side on this input |
