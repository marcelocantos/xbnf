# Corpus evaluation contract (🎯T22.1)

Shared measurement rules for the live T22 tracks: SQL, XML, Go, Python, YAML,
JavaScript, and CommonMark. C++ under `eval/testdata/cpp` is historical
(`role: historical`) and is not a live language. JSON is a standing fixture
and an auxiliary regression, not a substitute for those languages.

Language-track achievement requires a **present** real-world parser oracle and
`xbnf eval` **Failed=0** on the pinned manifest. A missing binary is an
`oracle_limit`, not a pass. Extending xbnf to close grammar gaps is in scope.
Dropping pinned fixtures after seeing misses to mint 100% is not.

## What this measures

One process: parse the `.xbnf` grammar, compile it to in-memory data, parse
input with `engine.Parse` / `Compiled.Parse` (tree produced). No native
parser codegen, JIT, or language-specific bypass. External parsers are
oracles only.

## Manifest

`eval.Manifest` (JSON, version 1) pins:

- language, dialect, dialect version
- upstream name, revision, license, URL
- grammar path and start rule
- deterministic selection text and an explicit exclusions list
- every fixture path, SHA-256, `expect` (`accept`|`reject`), `class`, reason

The file list is the denominator. Reports keep every fixture and every
failure. Do not drop files or raise thresholds to make xbnf look better.

Hashes are of the exact bytes on disk. `eval.LoadManifest` rejects drift.

## Comparison classes

| class | Meaning | Oracle disagreement |
|---|---|---|
| `syntax` | Well-formedness / grammar membership | xbnf and the reference must agree with `expect` |
| `tree` | Structure after a successful parse | Compare a documented projection, not raw dumps. If the structural oracle is missing, an xbnf accept is `unverified` (not a pass): leftover-text consumption is not success. |
| `semantic` | Meaning the reference may reject after parse | xbnf accept + oracle reject is **not** an xbnf syntax error |

Deliberately malformed fixtures have an independently stated `reason`.
Unknown or semantically invalid input is `semantic`, never relabeled as a
syntax miss.

## Phases (timed separately)

Headline times use uninstrumented `Compiled.Parse`. Diagnostics are a later pass.

1. **Cold compile** — `syntax.Parse` + `engine.Compile`.
2. **Cold first parse** — first `Parse` on that `Compiled`, **before** the
   untimed correctness loop (a sample after `checkFile` is pool-warm).
3. **Correctness** — every fixture, not timed, tree built.
4. **Warm varied** — one `Parse` per fixture on the live `Compiled`.
5. **Warm identical** — repeated `Parse` of one document (pool reuse).
6. **Diagnostics** — `Compiled.ParseProfile` after timing.

Account for reference I/O, process startup, preprocessing, semantic
analysis, and output serialization in the language-track report; do not
fold them into the xbnf headline.

## Diagnostics

`engine.Profile` meanings:

| Field | Meaning |
|---|---|
| `Descriptors` | GLL descriptors processed (`work`) |
| `Packed` | Competing packed derivations (forest ambiguity) |
| `GSSNodes` / `GSSEdges` | GSS size. **Edges are not ambiguity.** |
| `Completions` | Pop/completion records |
| `Steps` | Derivation steps |
| `DFAStart` | Start rule ran as a DFA |
| `ChartBytes` | Approximate backing capacity, not `B/op` |
| `CalleeReuse`, `ContnLifetimes`, `DFATransitions` | Not measured (0) |

## Noise

Keep/discard of parse-speed changes on JSON 64 KB still uses
`make bench-stable` (`scripts/bench-json-stable.sh`). Do not overwrite
that script.

Corpus runs may pass `-self` for a same-binary interleaved check on the
evaluated grammar. `SELF-FAIL` means do not claim a speedup. Per-file
`warm_ns` is sorted descending so outliers are first.

## Commands

```sh
make eval-corpus                          # standing json-smoke
xbnf eval eval/testdata/json-smoke/manifest.json
xbnf eval -o var/eval/report.json <manifest>
```

Write profiles and bulky reports under `var/eval/` (gitignored). Checked-in
reports for T22 live under `docs/eval/` and cite the manifest hashes.

## Standing checks

- `go test ./eval/` — hash pin, json-smoke correctness, 4× descriptor growth
- `engine.TestPooledWrapAcrossGrammars` — cross-grammar pool (T21)
- `engine.TestListDescriptorsLinear` — structural growth on JSON
- `make bench-stable SELF=1` — JSON 64 KB noise gate

Live language corpora are the files listed in each live
`eval/testdata/*/manifest.json` (provenance in `eval/testdata/SOURCES.md`).
`make eval-languages` runs the seven live tracks. Historical manifests
(`role: historical`) stay on disk and load in `go test ./eval/` but are not
live-set evidence. Failures stay in the report; the file list is the
denominator. A language track is not achieved while Failed>0 or the oracle is
absent.

JSON smoke remains `make eval-corpus`.
