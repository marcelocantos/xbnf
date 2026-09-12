# Parse output as an event stream

🎯T34 records this design before the API change. It supersedes the
`Text` invariants in [tree-positions.md](tree-positions.md); the position
choices there (half-open byte spans, wrap-skipped left edge) stand.

## Why

Measured on `docs/examples/json.xbnf` over a 537 KB nested JSON file
(119,281 nodes, depth 42): `Text` summed over every node was 30× the
input, and the JSON tree was 48× the input, 218 bytes per node. Text
repeats once per ancestor, so it scales as input × depth. Positions had
already made it redundant. A stream whose leaf lengths tile the input
makes positions redundant as well, and gives `#wrap` text a place: the
tree dropped it, so a formatter could not recover comments.

Prior art: rust-analyzer's parser emits Start / Token / Finish events and
builds its rowan tree from them afterwards; rowan and Roslyn keep only
lengths on the immutable tree and compute absolute offsets on demand.

## Choice

| Question | Selected | Rejected |
|---|---|---|
| Canonical output | `Result.Events []Event`, preorder | Nested `Node` tree; both at once |
| Event ops | `open`, `close`, `leaf`, `skip` | Open/close for leaves too; a token-only stream |
| Positions | `Len` on `leaf` and `skip`; none on `open`/`close` | `Start`/`End` on every event |
| Wrap | `skip` events for every gap in `[0, End)` | Wrap stays invisible; trivia attached to leaves |
| Node text | `input[Start:End]`, via `Node.Text(input)` | `Text` field; grammar spelling for case-folded literals |
| Tree | `Result.Tree()` decodes on demand into `Node` (no `Text`) | Tree field populated eagerly |
| Kind | `Kind`, a one-byte enum shared with `Node`, JSON as its name | Strings; a symbol table with integer ids for names too |
| JSON | One object per event, `omitempty` | Positional arrays; a binary encoding |
| Golden identity | Hash of the decoded tree, as before | Hash of the stream |

**Reason.** A preorder stream is what the builder's arena walk already
produces; the only extra state is a byte cursor for `skip`. Leaves need
one event, not three. Lengths are the delta coding of positions, and the
decoder recovers absolute spans by prefix sum in the same pass that links
children. Every gap gets a `skip` so that concatenating the spans of
`leaf` and `skip` events reproduces `input[0:End]`; a consumer that wants
whitespace and comments has them, and one that does not can ignore
`skip`. Case-folded string literals used to report the grammar spelling
instead of the input; the stream has no text to carry it in, and the
grammar term is where a consumer finds the canonical spelling. `Kind` as
a byte and `Len` as an int32 make an `Event` 24 bytes against the 88 of the
old `Node`; names stay strings (static data, no copy), and a symbol table
for them is a wire-format optimisation for later. The golden fingerprint keeps hashing kind, name,
text and child count over the decoded tree so that the 🎯T25.1 SHA-256s
are unchanged by this change: same hash means same tree.

## Stream

```
Event{Op, Kind, Name, Len}
```

| Op | Fields | Meaning |
|---|---|---|
| `open` | Kind, Name | A node with children begins |
| `close` | | The innermost open node ends |
| `leaf` | Kind, Name, Len | A node without children covering the next Len bytes |
| `skip` | Len | The next Len bytes belong to no node (`#wrap`) |

Invariants:

- Events are a well-formed preorder walk: `open` and `close` balance, and
  there is at most one root.
- The lengths of `leaf` and `skip` events sum to `Result.End`, and every
  byte of `input[0:End)` is covered by exactly one of them.
- A `skip` appears only before a node or before the end: leading wrap of
  the root, wrap between siblings, and trailing wrap after the root. A
  parent's span therefore ends where its last child's does, and begins
  where its first child's does, unless the parent is itself a `leaf`.
- A childless node is a `leaf` whatever its kind; `rule` and `seq` occur
  as leaves when zero-width or when a derivation could not be recovered.
- `Result.Tree()` returns the tree those events describe. `Start` is the
  cursor at the node's first event and `End` the cursor at its last, so
  every invariant in tree-positions.md still holds, with
  `input[Start:End]` in place of `Text`.

JSON:

```json
{"ok":true,"end":5,
 "events":[
  {"op":"open","kind":"rule","name":"s"},
  {"op":"leaf","kind":"string","len":1},
  {"op":"skip","len":2},
  {"op":"leaf","kind":"leaf","len":2,"name":"n"},
  {"op":"close"}]}
```

## Out of scope

- A binary or symbol-table encoding of the stream.
- Streaming during the parse. GLL knows the chosen derivation only after
  the parse completes, so the stream begins when the parse ends.
- Per-event positions, line/column, or source maps.
- Trivia ownership (leading versus trailing) as Roslyn defines it.
- The `ast/` package.

## Cost

`materialize` copied 88 bytes per node into one array; the emitter counts
first and writes exactly one 24-byte event per node, per interior node's
close, and per gap. `Tree()` allocates on demand. Keep or discard on
`make bench-stable`, recorded in `parse-speed.md`.
