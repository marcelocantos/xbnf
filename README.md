# xbnf

Scannerless CFG parser generator for Go. Grammars and regexes are the same
notation. Regular fragments compile to DFAs; the rest run as GLL.

xbnf succeeds [wbnf](https://github.com/arr-ai/wbnf) as a new product, not
a compatible upgrade. Alternation is unordered. Ambiguity is a compile
error unless the grammar says how to resolve it.

**Status:** GLL+DFA engine, self-host of `docs/xbnf.xbnf`, CLI (`parse`,
`--explain`, `sandbox`, `from-wbnf`, `eval`). Seven live language tracks
are Failed=0 against present oracles. Open: source positions (🎯T23),
tree-vs-oracle (🎯T26), larger corpora (🎯T27), concurrent `Parse` (🎯T28).

## Spec

- [`docs/cheatsheet.html`](docs/cheatsheet.html) — interactive language cheat sheet
- [`docs/syntax.html`](docs/syntax.html) — full syntax reference with runnable examples
- [`docs/xbnf.xbnf`](docs/xbnf.xbnf) — the language, defined in itself
- [`docs/examples/`](docs/examples/) — JSON, calc, and larger sketches
- [`docs/plan.md`](docs/plan.md) — locked decisions and work graph
- [`docs/parse-speed.md`](docs/parse-speed.md) — JSON 64 KB parse speed vs wbnf and `encoding/json`
- [`docs/eval/contract.md`](docs/eval/contract.md) — T22 corpus evaluation contract
- [`docs/eval/T22.md`](docs/eval/T22.md) — seven-language evaluation report (SQL, XML, Go, Python, YAML, JS, CommonMark)
- [`docs/toward-a-universal-grammar.md`](docs/toward-a-universal-grammar.md) —
  design origin (written against wbnf; see the lineage note at the top)

## Build

```sh
make          # bin/xbnf
make test
make vet
```

```sh
xbnf --version
xbnf --help-agent
xbnf sandbox          # http://127.0.0.1:7373/docs/index.html
xbnf -from-wbnf old.wbnf > old.xbnf
xbnf parse docs/examples/json.xbnf input.json
xbnf eval eval/testdata/json-smoke/manifest.json
xbnf eval eval/testdata/sql/manifest.json
xbnf --explain docs/examples/json.xbnf
```

## License

Apache License 2.0. Copyright 2026 Marcelo Cantos.
