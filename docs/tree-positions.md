# Parse-tree source positions

**Superseded in part by [tree-stream.md](tree-stream.md) (🎯T34):** `Node`
no longer has a `Text` field; its text is `input[Start:End]` via
`Node.Text(input)`, and `Result.Tree()` decodes the spans below from the
event stream. Case-folded string literals no longer report the grammar
spelling. The span rules here still hold.

🎯T23 records this design before the API change. Positions exist so callers
who used wbnf byte offsets can migrate (🎯T32) and so every public node
names the input it came from.

## Choice

| Question | Selected | Rejected |
|---|---|---|
| Representation | Byte offsets `Start`/`End` on `engine.Node` | Line/col on every node; a nested `Span` object; a parallel table |
| Interval | Half-open `[Start, End)` | Inclusive end |
| Coordinates | Wrap-skipped left edge, raw right edge | Raw GLL `[l,r)` including this node's leading `#wrap`; wrap as tree nodes |
| Which nodes | Every public `Node`, including DFA-collapsed leaves and leaf-collapsed `/term/` | Named-only; leaves-only |
| JSON | `start`, `end` (always present; not `omitempty`) | `lo`/`hi`; nested `span` |
| Home | Stay on `engine.Node` | Move to the planned `ast` package now |

**Reason.** Tree construction already has `[l,r)` when it calls `span` and
`addNode`, then drops the bounds at `materialize`. Those offsets are the
GLL/DFA match, not a post-hoc search. Go slices, `Result.End`, and wbnf
offsets are half-open bytes. `lineCol` already exists for diagnostics and
is O(n) from the start of the input; putting line/col on every node is
derived payload that feeds `BenchmarkJSON64K`. A nested `Span` type and an
`ast` package are extra API with no caller yet. `#wrap` text belongs to the
gap before a node, which is what `builder.span` already does, so
`input[Start:End] == Text` when `Text` is a verbatim input slice.

## Invariants

- `0 <= Start <= End <= len(input)` on every public node of a tree that
  covers that input.
- When `Text` is taken from the input, `Text == input[Start:End]`. The
  exception is a node whose text is not a span of the input (a case-folded
  string literal keeps the grammar spelling). Its `Start`/`End` are still
  the matched input, derived from the same walk that produced the node.
- Leading `#wrap` of a node is not in its span. Internal wrap between
  children is inside the parent's span. Wrap itself is not a node.
- Zero-width public nodes (if any) are `[i, i)` after skip. Empty,
  `PosProp`, and lookahead terms that already make no node still make no
  node.
- `Parse` and `Compiled.Parse` share this tree; both expose the fields.
- Positions are copied from the builder/DFA walk. They are not recovered
  by searching `Text` in the input.

## Out of scope

- Line/column on `Node` (diagnostics keep `lineCol`).
- Including positions in the T25.1 golden fingerprint (kind/name/text/
  children stay the identity; dedicated tests pin spans).
- Creating `ast/`.
- Wrap nodes, inclusive ends, source maps, multi-file inputs.
- `@col` / `@line` as grammar predicates (plan non-goal for this slice).
- Concurrent `Parse` (🎯T28).

## Cost

Two `int` fields on `Node` grow the public materialize array. That is a
layout change 🎯T23 requires, not a speed hypothesis. If
`BenchmarkJSON64K` B/op or allocs/op moves, keep the fields and record the
delta with `make bench-stable` in `parse-speed.md`.
