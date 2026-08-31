# AGENTS.md

xbnf is a scannerless CFG parser generator. The language spec is
`docs/xbnf.xbnf`. The engine is GLL with a layered DFA fast path.
This is a new product, not an in-place rewrite of `arr-ai/wbnf`.

## Delivery

merged to master

## Build

```sh
make          # bin/xbnf
make test
make vet
make bullseye # standing invariants for /cv
```

Never pass `-j` to make; `MAKEFLAGS` is set in the Makefile.

```sh
make test
go test ./cmd/xbnf/ -run TestName   # set GOWORK=off; a parent go.work does not include this module
```

The Makefile exports `GOWORK=off`. Bare `go test ./...` from this directory fails if a parent `go.work` is visible.

## Architecture

Packages (create when the code that fills them lands):

```
cmd/xbnf/     CLI
grammar/      IR (Rule, Term) — exists
syntax/       bootstrap parser: xbnf source → IR — exists
engine/       GLL + DFA terminal layer — exists
fromwbnf/     parse old .wbnf into an IR of meaning — exists
ast/          committed parse tree
```

Today: CLI (`--version`, `--help`, `--help-agent`, `sandbox`, `-from-wbnf`)
plus packages `grammar` (IR), `syntax` (bootstrap parser), `engine` (GLL
with a DFA fast path), and `fromwbnf` (parse `.wbnf` into meaning and
emit xbnf). `xbnf sandbox` hosts the cheat sheet; `POST /run` matches
editable examples. Compile-time disambiguation (T6), self-host (T7), and
`xbnf parse` (T8) are not this slice.

Locked decisions, work graph, and non-goals: [`docs/plan.md`](docs/plan.md).
Language spec: [`docs/xbnf.xbnf`](docs/xbnf.xbnf).

## Conventions

- Go. Read `~/.claude/go.md` before writing Go. No functional-options pattern.
- Apache-2.0. SPDX on source files:
  `// Copyright 2026 Marcelo Cantos` / `// SPDX-License-Identifier: Apache-2.0`
- Line length 120.
- Tests: `testify` is fine once a real engine exists; stdlib is enough for
  the stub.
- Default branch is `master`.
- Do not introduce TOML.

## Agent guide

[`agents-guide.md`](agents-guide.md) is also printed by `xbnf --help-agent`.
