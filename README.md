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
- [`docs/xbnf.xbnf`](docs/xbnf.xbnf) — the language, defined in itself
- [`docs/examples/`](docs/examples/) — JSON, calc, and larger sketches
- [`docs/plan.md`](docs/plan.md) — locked decisions and work graph
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
xbnf sandbox          # http://127.0.0.1:7373/docs/cheatsheet.html
```

## License

Apache License 2.0. Copyright 2026 Marcelo Cantos.
