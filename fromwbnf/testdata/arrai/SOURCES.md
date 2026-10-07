# arr.ai corpus

Pinned from https://github.com/arr-ai/arrai, branch `master`, commit
`e53fee1a532c8dad2a81e51d3b155c940a8358f5` (2026-09-15). Apache-2.0.

| File | Origin |
|---|---|
| `stdlib/flag.arrai` | `syntax/stdlib/flag.arrai` |
| `stdlib/flag_test.arrai` | `syntax/stdlib/flag_test.arrai` |
| `stdlib/stdlib-safe.arrai` | `syntax/stdlib/stdlib-safe.arrai` |
| `stdlib/stdlib-unsafe.arrai` | `syntax/stdlib/stdlib-unsafe.arrai` |
| `stdlib/util.arrai` | `syntax/stdlib/util.arrai` |

The grammar they are parsed with is `../arrai.wbnf` (the same commit's
`syntax/arrai.wbnf` with `‵` restored to a backquote). `TestConvertArraiTree`
parses every file with wbnf and with the converted xbnf grammar and compares
the adapted trees; `BenchmarkArrai` times both engines over the set.
