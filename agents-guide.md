# xbnf — Agent Usage Guide

*Also printed by `xbnf --help-agent`, prefixed with CLI flag help.*

xbnf is a scannerless CFG parser generator. Grammars are written in xbnf
notation (`docs/xbnf.xbnf`). Regular rules become DFAs; the rest parse as
GLL. Alternation `|` is unordered; `|>` is committed choice (do not mix).
Ambiguous grammars do not compile unless
they declare a disambiguator (`#prefer`, `#avoid`, `#assoc`, `#priority`,
`#longest`).

## Status

The engine is GLL with a DFA terminal layer for regular-fragment rules.
The CLI supports `--version`, `--help`, `--help-agent`, `sandbox`
(also `-sandbox`) to host the cheat sheet and a syntax reference,
`from-wbnf` (also `-from-wbnf`) to emit xbnf for an old wbnf grammar,
`parse <grammar.xbnf> <input>` to parse with the GLL+DFA engine,
`eval <manifest.json>` for the T22 corpus harness, and
`--explain <grammar.xbnf>` to list DFA-promoted rules vs GLL forking.
Sandbox `POST /run` parses xbnf with the bootstrap parser and matches
input with that engine. Compile-time disambiguation (🎯T6) and self-host
(🎯T7) have landed.

## Spec and plan

| File | Role |
|---|---|
| `docs/cheatsheet.html` | Interactive language cheat sheet |
| `docs/syntax.html` | Syntax reference with runnable examples |
| `docs/xbnf.xbnf` | Language, defined in itself |
| `docs/examples/*.xbnf` | Example grammars; `#macro` execution is later |
| `docs/plan.md` | Locked decisions and work graph |
| `docs/toward-a-universal-grammar.md` | Design origin (wbnf-era; see lineage note) |

## Relation to wbnf

[wbnf](https://github.com/arr-ai/wbnf) is a PEG backtracker. xbnf does not
preserve its ordered-choice semantics and is not a drop-in replacement.
Do not add compatibility shims. `fromwbnf.Parse` reads `.wbnf` into an IR of
what the grammar meant (macros kept, no cut-points, not executed).
`fromwbnf.Convert` / `xbnf -from-wbnf` emit xbnf. Leftovers with no xbnf
spelling are ConvertError kinds (unicode-property, regex-anchor, regex-flag,
lazy-quant, posix-class, malformed regex) — never silent; see 🎯T14.

## If you are implementing

1. Read `docs/plan.md` and the current bullseye frontier.
2. Read `~/.claude/go.md` before writing Go.
3. Construct grammar IR in tests for engine work; do not block GLL on the
   bootstrap parser.
4. A test that depends on alternative order is wrong.
5. Keep or discard a `BenchmarkJSON64K` change with `make bench-stable`, not a
   same-session median of sequential 3 s × 5 runs. See `docs/parse-speed.md`.
