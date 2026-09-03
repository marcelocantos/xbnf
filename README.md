# xbnf

Scannerless CFG parser generator for Go. Grammars and regexes are the same
notation. Regular fragments compile to DFAs; the rest run as GLL.

xbnf succeeds [wbnf](https://github.com/arr-ai/wbnf) as a new product, not
a compatible upgrade. Alternation is unordered. Ambiguity is a compile
error unless the grammar says how to resolve it.

**Status:** language spec, IR types, and a local sandbox for the cheat sheet.
The parse engine is not written yet.

## Spec

- [`docs/cheatsheet.html`](docs/cheatsheet.html) — interactive language cheat sheet
- [`docs/syntax.html`](docs/syntax.html) — full syntax reference with runnable examples
- [`docs/xbnf.xbnf`](docs/xbnf.xbnf) — the language, defined in itself
- [`docs/examples/`](docs/examples/) — JSON, calc, and larger sketches
- [`docs/plan.md`](docs/plan.md) — locked decisions and work graph
- [`docs/parse-speed.md`](docs/parse-speed.md) — JSON 64 KB parse speed vs `encoding/json`
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
xbnf --explain docs/examples/json.xbnf
```

## License

Apache License 2.0. Copyright 2026 Marcelo Cantos.
