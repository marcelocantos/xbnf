# Corpus evaluation contract (🎯T22.1)

Shared measurement rules for SQL, XML, C++, Python, YAML, JavaScript, and
CommonMark tracks. JSON is a standing fixture and an auxiliary regression, not
a substitute for those languages.

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
| `tree` | Structure after a successful parse | Compare a documented projection, not raw dumps |
| `semantic` | Meaning the reference may reject after parse | xbnf accept + oracle reject is **not** an xbnf syntax error |

Deliberately malformed fixtures have an independently stated `reason`.
Unknown or semantically invalid input is `semantic`, never relabeled as a
syntax miss.

## Phases (timed separately)

Headline times use uninstrumented `Compiled.Parse` after correctness.

1. **Correctness** — every fixture, not timed, tree built.
2. **Cold compile** — `syntax.Parse` + `engine.Compile`.
3. **Cold first parse** — first `Parse` after compile.
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

Full language corpora are later T22.2–T22.4 commands, not this smoke.
