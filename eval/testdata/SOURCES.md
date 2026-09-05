# Corpus provenance (🎯T22)

Pinned third-party files under `eval/testdata/*/corpus/`. Hashes in each
`manifest.json` are SHA-256 of the exact bytes on disk. Selection is the
listed files only; it cannot shrink at eval time.

Live T22 set: SQL, XML, **Go**, Python, YAML, JavaScript, CommonMark.
C++ remains on disk (`role: historical`) and is not a live language.

| Language | Upstream | Revision | License | What was taken |
|---|---|---|---|---|
| SQL | [postgres/postgres](https://github.com/postgres/postgres) | `1f47e7b59b92` (tag REL_16_6) | PostgreSQL (`corpus/COPYRIGHT`) | `src/test/regress/sql/{select,select_distinct,case,union}.sql` |
| XML | [junit4 r4.13.2](https://github.com/junit-team/junit4), [unicode-org/cldr release-46](https://github.com/unicode-org/cldr), [apache/ant rel/1.10.14](https://github.com/apache/ant), [W3C xmlts20080205](https://www.w3.org/XML/Test/) | see paths | EPL-1.0; Unicode; Apache-2.0; W3C | `junit4.pom.xml`, `windowsZones.xml`, `xmlproperty.xml`, `xml.xsd`, xmltest valid/not-wf sa/001–002 |
| Go | [golang/go](https://github.com/golang/go) | tag `go1.26.4` | BSD-3-Clause (`corpus/LICENSE`) | `src/errors/errors.go`, `src/errors/join.go`, `src/sync/once.go`; `src/go/parser/testdata/{issue11377,commas}.src` (ParseFile rejects). |
| C++ (historical) | [gcc-mirror/gcc](https://github.com/gcc-mirror/gcc) `releases/gcc-14.2.0` | tag `releases/gcc-14.2.0` | GPL-2.0 (`corpus/COPYING`) | Not live. `gcc/testsuite/g++.dg/parse/{defarg1,ambig3,error1,crash1}.C` preprocessed with `clang -E -P -x c++` to `.ii`. |
| Python | [python/cpython](https://github.com/python/cpython) | `60403a5409ff` (tag v3.13.0) | PSF (`corpus/LICENSE`) | `Lib/keyword.py`, `Lib/token.py`, `Lib/this.py`, `Lib/test/tokenizedata/{bad_coding,badsyntax_3131,badsyntax_pep3120}.py` |
| YAML | [yaml/yaml-test-suite](https://github.com/yaml/yaml-test-suite) | `6e6c296ae9c9` (tag data-2022-01-17) | MIT (`corpus/LICENSE` from main) | `in.yaml` for ids 229Q, 26DV, 27NA, 33X3, 35KP, 4ABK, 4CQQ, 4FJ6 (suite-valid), 2JQS (suite-valid; PyYAML events reject — expect follows the named oracle), and 236B, 2CMS, 3HFZ (suite `error` tests) |
| JavaScript | [tc39/test262](https://github.com/tc39/test262) | `419d3e0a2273` (main) | BSD (`corpus/LICENSE`) | `test/language/literals/{boolean,null}/…`, `statements/empty/S12.3_A1.js`, `statements/block/12.1-1.js` (negative), `statements/function/early-params-super-call.js` (negative) |
| CommonMark | [commonmark/commonmark-spec](https://github.com/commonmark/commonmark-spec) | `9103e341a973` (tag 0.31.2) | CC-BY-SA (`corpus/LICENSE`) | `README.md`, `changelog.txt` |

Expect `accept` vs `reject` is taken from the named oracle when it disagrees with the upstream suite (YAML 2JQS is suite-valid and PyYAML-event-invalid). Otherwise it follows the suite (postgres regress inputs are valid; yaml-test-suite `error` tests and test262 `negative.phase: parse` are invalid; W3C xmltest not-wf vs valid; gcc `{ dg-error }` tests are reject; CPython tokenizedata `bad_*` / `badsyntax_*` are encoding/syntax rejects). Files are not dropped after a miss.

The SQL oracle is `pg_query`, built from `eval/oracles/pgquery` against `libpg_query` (`//go:build pgquery`, so `go build ./...` on CI does not need the C library):

```sh
CGO_ENABLED=1 go build -tags pgquery -o pg_query ./eval/oracles/pgquery
```
