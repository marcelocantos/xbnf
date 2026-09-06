# Capture-dependent parser state

The T29 correction keeps the GLL/DFA architecture and its shared callees. It distinguishes execution states only when
an earlier capture can affect a later back-reference. The motivating counterexample and historical baseline are in the
[T25 review](parse-performance-review.md#41-capture-bindings-remain-dependent-on-alternative-order--t29).

## Binding scope and execution identity

Back-references keep their existing scope: an earlier named element of the same lowered production, with the first
matching name winning. This change does not introduce inherited bindings across calls. Defaults apply when that name is
unbound; an empty bound capture remains a binding and takes precedence over a default.

Compilation marks only named elements read by a subsequent reference. Matching such an element extends an immutable,
interned context with its element index and matched input span, after leading wrap text. The descriptor key becomes
`(production, slot, caller frame, input position, capture context)`. Context zero follows the existing packed descriptor
map; a grammar without these references does not allocate capture maps. Unreferenced names never split execution.

Captured descriptors use a separate pooled chart index. Its hash combines the ordinary packed descriptor key with the
context ID; a collision chain compares both complete components before sharing a cell. The index and records reuse
storage across parses, participate in the existing chart-pool capacity limit, and reset through generation stamps.
Descriptors that exceed the packed key's ranges retain the full-key overflow map. Binding interning and completed-context
maps remain per-parse allocations. Lower warm allocation here comes with retained chart storage, not free memory.

Span identity deliberately retains more information than string equality. Equal text captured from different positions
must not let tree selection substitute a different occurrence. The context chain is local to the current production;
sharing interned chains across productions is safe because the production remains part of descriptor identity.

A callee remains keyed by `(nonterminal, input position)`: its recognition does not depend on the caller's local captures.
Each subscription carries the caller's context, and edge identity includes it alongside the caller and return slot.
Both immediate completions and later replay resume that context. A callee starts with its own empty context.

## Evidence and selected trees

Every evidence cell now agrees on the captures that future execution can read. Additional context-free predecessors may
arrive after its descriptor runs, but cannot change those bindings. A completed production retains its context too:
completion deduplication merges the same production and context, while retaining different contexts for the same span.
Dropping the distinction immediately after the last reference would reintroduce the invalid-tree defect.

Production priority, preference and avoidance still choose between productions. If one production has several completed
capture contexts, the builder compares complete valid paths from their last internal boundary backwards, using the same
left/right boundary policy as its existing path walk. It never combines a prefix from one context with a reference from
another. `#assoc=none` diagnoses competing paths; resolved associativity does not count them as packed ambiguity.

A reference can match the empty string, including at end of input. FIRST information therefore conservatively admits
both empty matches and any initial rune. The actual binding or default decides the match at runtime.

That correction also exposes zero-width recursive trees previously hidden by incorrect pruning. Compilation builds a
conservative graph of nonterminal calls whose siblings are all nullable. Removing nodes with zero incoming degree leaves
cycles and their descendants. Only those nonterminals need an active-completion guard during tree construction.
Re-entering a span excludes completed production/cell pairs already being unfolded, then applies the usual selection
policy to the remaining completions. If that subtree cannot finish without a cycle, its branching ancestor retries
another completion, restoring the discarded attempt's arena and diagnostics. This lets the recursive empty-default,
empty-capture and unit-wrapper regressions select their finite base alternatives. Independent sibling occurrences are
allowed after the earlier occurrence finishes. Transparent recursive spines retain their existing requirement that each
iteration shorten the span.

**Limit:** this is not a complete search of an infinitely ambiguous forest. If no unvisited completion remains, the
builder returns a `zero-width recursion` diagnostic; it does not search alternative predecessor paths inside an active
completion. Such a forest can still contain a finite witness. Consuming recursion and ordinary empty captures work.

## Standing evidence

`make test` runs the following checks through the shipped implementation:

- `TestBindingOracleReconvergence` independently enumerates a finite language and its valid witnesses, then checks
  16,128 grammar/input combinations against `Compiled.Parse`. It varies capture and padding choices, copied suffixes,
  unordered alternative order, delayed completion, repeated/multiple captures, shared callers and recursive callers.
  The pre-fix baseline failed 192 checks. The oracle validates the selected tree as well as acceptance.
- Nested scopes, association across completed contexts, duplicate recursive completions, empty captures, defaults,
  repeated names and Unicode receive separate public API checks. Nullable recursive cases run in subprocesses with a
  deadline and reduced stack, so a regression fails without crashing the whole suite.
- `TestCaptureIndexCollisionAndGeneration` forces full-hash collisions and table growth, then checks exact identity,
  deduplication, rebinds, intervening capture-free grammars and generation rollover. Hash equality alone never shares a cell.
- `TestCaptureJourney` builds the real CLI and exercises grammar-file/input-file parsing. Both alternative orders accept
  `aaa!a`; the impossible copy `aaa!aaa` is rejected.
- `TestFingerprintPartialTree` checks the public tree returned for `s -> "a"; #wrap -> ();` on `ab`, and verifies that
  changing any tree field changes its fingerprint while result metadata stays fixed. `TestGolden` retains this case.

The CLI exposes acceptance and diagnostics, not trees. Direct `Compiled.Parse` checks are the product boundary for the
tree contract; a CLI acceptance check cannot substitute for them. These bounded checks do not prove correctness for all
grammars or independently validate the language corpus's ASTs; the latter remains T26.

## Partial-tree ratchet correction

T30 changes fingerprint coverage, not failed-parse behavior. A nonzero public tree is hashed on both success and failure;
an absent tree on failure keeps the empty fingerprint. Before the T29 engine changes, the deliberate golden update
changed only node counts and tree hashes for ten existing failed corpus cases: Go (2), JavaScript (2), Python (1), XML
(2), and YAML (3). Their acceptance, end positions, packed counts and diagnostics stayed unchanged. All 31 existing
successful fingerprints and eight absent-tree failure fingerprints stayed unchanged. The new engine fixture is
`partial-tree-ab`. No additional golden update was needed for the T29 implementation.
