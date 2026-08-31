# xbnf — Agent Usage Guide

*Also printed by `xbnf --help-agent`, prefixed with CLI flag help.*

xbnf is a scannerless CFG parser generator. Grammars are written in xbnf
notation (`docs/xbnf.xbnf`). Regular rules become DFAs; the rest parse as
GLL. Alternation is unordered. Ambiguous grammars do not compile unless
they declare a disambiguator (`#prefer`, `#avoid`, `#assoc`, `#priority`,
`#longest`).

## Status

The engine is not implemented. The CLI currently supports `--version`,
`--help`, and `--help-agent` only. Do not invent a parse API or a
`.xbnf` runner.

## Spec and plan

| File | Role |
|---|---|
| `docs/xbnf.xbnf` | Language, defined in itself |
| `docs/examples/*.xbnf` | Example grammars; `@col` and macros are later |
| `docs/plan.md` | Locked decisions and work graph |
| `docs/toward-a-universal-grammar.md` | Design origin (wbnf-era; see lineage note) |

## Relation to wbnf

[wbnf](https://github.com/arr-ai/wbnf) is a PEG backtracker. xbnf does not
preserve its ordered-choice semantics and is not a drop-in replacement.
Do not add compatibility shims.

## If you are implementing

1. Read `docs/plan.md` and the current bullseye frontier.
2. Read `~/.claude/go.md` before writing Go.
3. Construct grammar IR in tests for engine work; do not block GLL on the
   bootstrap parser.
4. A test that depends on alternative order is wrong.
