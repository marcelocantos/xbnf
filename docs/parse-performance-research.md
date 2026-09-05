# Eliminating Redundant Work in Dynamic Generalized Parsing

## A research agenda for xbnf without native code generation

**Research position paper · 5 September 2026**

**Status.** This paper combines source analysis, previously recorded measurements, and proposed experiments. It does not
report an implemented replacement architecture or a demonstrated cross-language speedup. The implementation and
evaluation harness are under active development; the evidence boundary is recorded in Appendix A.

### Abstract

xbnf accepts a grammar at runtime and executes it in the same process using a scannerless generalized LL engine with a
DFA terminal layer. Recent work has removed expensive list handling, reduced tree-construction costs, and nearly
eliminated allocations in a repeated JSON benchmark. These improvements do not establish that the parser has reached an
architectural fixed point. Source inspection still shows a substantial amount of scheduling, indexing, completion
propagation, and derivation reconstruction between input recognition and the final tree.

This paper argues that the next substantial improvement is most likely to come from changing the unit of shared work. It
proposes a progression from production-slot execution toward a compact grammar automaton whose runtime shares callee
results, executes straight-line regions without repeatedly scheduling descriptors, and carries derivation evidence
directly. A central complication is xbnf's intended support for layout and data-dependent syntax: sharing must
distinguish relevant context while avoiding fragmentation by irrelevant context. More speculative directions include
batched relational evaluation, grammar-derived structural scans, and context-sensitive reuse of parse summaries.

The proposals preserve unordered alternation, explicit disambiguation, scannerless operation, tree semantics, and
runtime grammar loading. Native code generation and JIT compilation are excluded. A seven-language evaluation programme
is specified to distinguish general improvements from favourable behaviour on JSON. Each hypothesis has a mechanism, a
correctness obligation, and an experiment that could reject it.

## 1. The problem: optimize an execution strategy, preserve a language

The central question is not whether a dynamic parser can be made to resemble a handwritten JSON parser. It is whether
xbnf can execute a broad class of grammar definitions with much less administrative work while preserving their meaning.

The framework's constraints are substantive. The grammar must be read, compiled to in-memory structures, and used in one
running process. The installed executable may contain a fixed interpreter and optimized primitive operations; it may not
invoke a compiler or produce native instructions for the user's grammar. An in-memory automaton, bytecode program,
decision table, or specialized data layout is compatible with this requirement.

The language contract also matters. Unordered `|` cannot become first-success choice. `|>` has its own ordered
semantics. Precedence, associativity, preferences, longest-match rules, labels, lookahead, leaf collapse, and scoped
whitespace must survive optimization. Returning a correct recognition answer while changing the public tree is not
equivalence. Likewise, turning an inconvenient language construct into a handwritten parsing callback would evade the
general problem. These constraints follow the project's [implementation
plan](/Users/marcelo/work/github.com/marcelocantos/xbnf/docs/plan.md) and [grammar
specification](/Users/marcelo/work/github.com/marcelocantos/xbnf/docs/xbnf.xbnf).

“Optimal” therefore needs a workload and an output contract. Let grammar compilation cost be `C(G)`, and let parsing
cost for input `x` with output requirement `O` be `P(G, x, O)`. For a grammar used on `m` inputs, the relevant total is

```text
T(G, x[1] ... x[m], O) = C(G) + sum(P(G, x[k], O), k = 1 ... m).
```

A transformation that improves every warm parse can still lose when a grammar is loaded for one small document. A
recognizer and a parser producing a rich tree have different unavoidable work. Large grammars and large inputs also
stress different resources. The research objective is an improved cost frontier across these cases, not a single
universal fastest representation.

For this paper, a **step change** means a repeatable, substantial improvement associated with eliminating a category of
work or changing its scaling. A proposed twofold improvement would be a useful research ambition, but it is neither a
prediction nor an existing acceptance threshold. Smaller gains remain valuable; their interpretation must remain honest.

## 2. What the existing evidence establishes

### 2.1 The history contains both structural and representational improvements

The [parse-speed log](/Users/marcelo/work/github.com/marcelocantos/xbnf/docs/parse-speed.md) records several distinct
changes. They should not be collapsed into a single speedup curve: inputs, output fidelity, machine conditions, and
measurement procedures changed during development.

| Stage | Mechanism | What can reasonably be inferred |
|---|---|---|
| Repetition and lists, T15 | Left-recursive lowering and FIRST-based pruning removed excessive repeated completion work. | Grammar representation can change scaling, not merely instruction cost. |
| Derivation-based trees, T16 | Trees began following the generalized parse rather than an independent greedy reconstruction. | Correct output can expose costs hidden by a weaker contract. |
| Tree flattening, T18 | List spines were traversed and flattened more efficiently. | A linear recognizer does not guarantee linear tree construction. |
| Pooling and arenas, T19 | Charts and internal storage were reused; nodes and graph records moved into arenas. | Allocation and reclamation had been substantial costs. |
| Later T20 work | Compact keys, generational tables, contiguous step runs, cached lookups, and packed output storage. | The remaining problem increasingly concerns work performed on retained structures. |

For scale, the early `genJSON` probe recorded about 1.34 seconds and 1.29 GB allocated per parse before T15, followed by
about 28.9 milliseconds and 43.2 MB after T15. This is historical evidence of an earlier structural improvement, not a
comparison with today's tree-producing parser. That probe used a different input generator from `nestedJSON`.

In the later `nestedJSON` series, the log records 22.9 milliseconds and 6.85 MB at `7504667`, and subsequent snapshots
around 11–17 milliseconds and 2.35 MB with three allocations. The time values are not comparable across sessions. The
present keep/discard procedure uses interleaved baseline/candidate runs and a same-binary noise check. No new throughput
experiment was performed for this paper.

The progression is significant: “three allocations” is not evidence that little work remains. Pooling removes repeated
allocation while preserving the cost of probing, updating, scanning, and retaining the pooled structures.

### 2.2 An exploratory structural census

An earlier isolated snapshot based on `a9c96f5` plus then-current working changes was instrumented on `nestedJSON(64 <<
10)`, a 65,553-byte input. The run succeeded and reported the following counts:

| Quantity | Observed value |
|---|---:|
| Processed descriptors | 102,810 |
| GSS nodes | 19,585 |
| GSS edges | 28,464 |
| Completion records | 50,280 |
| Derivation steps | 88,679 |
| Steps copied into the packed representation | 88,679 |
| Public tree nodes | 32,641 |
| Reported `Packed` result | 0 |

On this host, a public `Node` occupied 72 bytes. The node payload alone was consequently 2,350,152 bytes, before
accounting for input retention or other storage. The six inspected hash-table backing arrays occupied 26 MiB in
aggregate. That figure is allocated capacity calculated from element sizes and capacities; it is neither RSS nor
per-operation allocation. Appendix A records the calculation and provenance.

This census establishes the existence of substantial intermediate machinery on a successful input with `Packed=0`. It
does not establish which records are redundant. Multiple GSS edges are not synonymous with ambiguity, completion records
need not have distinct endpoints, and the public `Packed` count is not a certificate of global determinism. The census
also says nothing yet about other languages.

A retained CPU profile from the same investigation places substantial cumulative time under `gll.process`, tree-path
reconstruction, and step packing. Reported cumulative shares include 53.28% for `process`, 27.60% for `rhsNodesInto`,
6.01% for `packSteps`, and 4.37% for `dfa.match`. These are overlapping call-tree measurements, not an additive phase
partition. They motivate investigation; they cannot be summed into an exact budget or used directly for a speedup
forecast.

### 2.3 The parser is already an interpreter

In the source snapshot, a grammar becomes arrays of productions and elements. A slot is a production ID and an element
index. A descriptor adds a GSS node and input position. `process` dispatches among terminals, DFA calls, nonterminal
calls, and lookahead. `advance` records an element match and invokes `add`; `add` performs deduplication and FIRST
admission before scheduling the next descriptor. This is already a small virtual machine, even though its instructions
are not called opcodes. See [compile.go](/Users/marcelo/work/github.com/marcelocantos/xbnf/engine/compile.go) and
[gll.go](/Users/marcelo/work/github.com/marcelocantos/xbnf/engine/gll.go).

Two further details shape the research agenda. GSS nodes are keyed by a return slot and input position. Separately,
recognition records productions over spans and per-element steps; the tree builder then packs, sorts, and searches those
steps to recover paths. Thus both the identity of shared work and the representation of its evidence are open to
improvement. Renaming the current operations `CALL`, `MATCH`, and `RETURN` would leave these costs largely intact.

## 3. A semantic and cost model

### 3.1 Parsing as a relation with evidence

For a fixed grammar and input, let a context-free nonterminal denote a relation over input positions:

```text
R_A ⊆ Position × Position.
R_A(i, j) means that A recognizes the span [i, j).
```

Sequence composes relations; unordered alternatives union them; positive recursive definitions are evaluated to a least
fixed point. Tree-producing parsing additionally records derivation evidence. Recognition may merge two proofs of the
same span, while tree construction or disambiguation may need to distinguish them.

For data-dependent syntax, a more adequate model is

```text
R_A ⊆ Position × Environment × Position × Environment × Evidence.
```

The environment can include an indentation reference, captured delimiter, lexical mode, or other explicitly supported
syntactic state. This notation is a design model, not a claim that every xbnf construct currently has a completed formal
semantics. Negative lookahead and committed choice require additional evaluation rules: “no success found yet” is not
failure while relevant recursive work is still pending. Arbitrary data-dependent predicates also do not inherit the
complexity guarantees of the context-free subset.

Data-dependent parsing is established work. YAKKER and Iguana demonstrate grammar formalisms with bindings and
constraints; Iguana implements such ideas over GLL. The opportunity here is to use dependency information to control the
cost of sharing within xbnf's narrower runtime contract, rather than treating unrestricted computation as an
optimization strategy. [Jim, Mandelbaum, and Walker,
2010](https://collaborate.princeton.edu/en/publications/semantics-and-algorithms-for-data-dependent-grammars), [Afroozeh
and Izmaylova, 2016](https://ir.cwi.nl/pub/25126/).

### 3.2 Account for work and representation separately

A useful empirical model is

```text
P ≈ c_scan·Q + c_schedule·D + c_join·J + c_evidence·F + c_output·O + overhead,
```

where `Q` counts scanning work, `D` scheduled states, `J` caller/completion combinations, `F` evidence construction and
traversal, and `O` public output size. The coefficients depend on locality, representation, and hardware; the expression
is an explanatory model, not an asserted linear fit.

Recent table and arena work largely reduces coefficients. A structural improvement reduces counts, avoids revisiting the
same relation fact, or changes how a family of operations is evaluated. Both matter. A smaller chart can also improve
cache behaviour, so the effects need not be independent.

The output term is unavoidable under a fixed tree API. Emitting every required node costs at least work proportional to
the number of nodes. A compact or lazy tree could lower that cost, but it changes the output contract and must be
evaluated separately. A recognition-only result cannot be advertised as a faster replacement for `Parse` with trees.

## 4. What related work suggests—and what it does not prove

The most relevant precedent is algorithmic restructuring within generalized parsing. Afroozeh and Izmaylova move return
positions from GSS nodes to edges, allowing calls of the same nonterminal at the same input position to share a node.
Their evaluation reports language-dependent improvements, including factors of 1.5, 1.7, and 5.2 on Java, C#, and OCaml.
Those are results for their implementation and grammars, not estimates for xbnf. [Faster, Practical GLL
Parsing](https://ir.cwi.nl/pub/24026/24026B.pdf).

Scott and Johnstone's FGLL and RGLL work studies factorized grammars, reduced descriptor sets, and scheduling order. It
reinforces that GLL's execution structure remains an optimization variable after choosing the algorithm family. The
discussion here relies on the authors' published abstract for that work, rather than an independent reproduction.
[Structuring the GLL parsing algorithm for
performance](https://pure.royalholloway.ac.uk/en/publications/structuring-the-gll-parsing-algorithm-for-performance/).

Opedal and colleagues describe Earley deduction over a compact finite-state representation of a grammar, reducing the
grammar-size factor when structure can be shared. This is evidence for investigating grammar representation, not a
reason to replace xbnf with an Earley implementation. Goodman's semiring framework separately demonstrates how one
deduction structure can support different forms of result aggregation. Neither result makes xbnf's complete
disambiguation vocabulary an automatically valid semiring. [Efficient Semiring-Weighted Earley
Parsing](https://aclanthology.org/2023.acl-long.204/), [Semiring Parsing](https://aclanthology.org/J99-4004.pdf).

Two other precedents apply to narrower opportunities. LPeg demonstrates a parsing VM and specialized operations, but its
PEG choice semantics cannot substitute for unordered CFG choice. Elkhound switches between deterministic and generalized
execution, showing the value of paying for generality only when required. Its LR/GLR machinery is a precedent for
adaptive representation, not a drop-in scannerless GLL design. [A Text Pattern-Matching Tool based on Parsing Expression
Grammars](https://www.inf.puc-rio.br/~roberto/docs/peg.pdf),
[Elkhound](https://people.eecs.berkeley.edu/~necula/Papers/elkhound_cc04.pdf).

The proposals below are a synthesis and an xbnf-specific research agenda. No claim of priority for these broad ideas is
made.

## 5. Three principal hypotheses

### 5.1 H1: share a callee's work independently of its callers

The current return-slot key distinguishes calls that may ask the same underlying parsing question. The first experiment
should determine how often different call sites request the same nonterminal at the same position and compatible
context. A high count would identify an opportunity that a faster hash function cannot remove.

A conceptual runtime separates three things:

```text
Invocation:     the parsing question being evaluated
Subscription:   a caller waiting for answers, with its return slot and evidence
Completion:     an answer that can be delivered to every applicable subscriber
```

For the context-free subset, an invocation can use `(nonterminal, start)` as its semantic key within one grammar/input
evaluation. Return slots live in subscriptions. Existing completions must be delivered to newly registered subscribers,
and new completions must reach existing subscribers. Left recursion requires registration before recursive expansion.
Deduplication must preserve both recognition facts and distinct derivation evidence.

The current implementation already shares some work and has separate span records. Consequently, the experiment must
measure the *additional* reduction available; it must not count all existing calls as duplicated computation. Useful
measurements include distinct call-site requests per semantic invocation, repeated callee expansion, repeated endpoint
publication, and subscriber-delivery work that remains necessary after sharing.

**Correctness obligation.** The new runtime must produce the same supported span results and derivations under
recursive, nullable, and late-subscriber cases. For contextual rules, the invocation key must be strengthened as
described in Section 6.

**Reject or defer H1** if compatible repeated invocations are rare, or if reduced expansion is offset by more expensive
subscriptions, larger keys, or forest maintenance. This remains a useful negative result because it identifies where the
existing GSS already shares enough.

### 5.2 H2: schedule semantic boundaries instead of every grammar element

The second hypothesis concerns dispatch frequency. After an ordinary terminal succeeds, the source snapshot records the
match and schedules the next slot through `add`. A compiled straight-line region could instead keep the current
position, continuation, and evidence in local interpreter state until reaching a point that needs shared scheduling.

Consider a production containing a literal, a lexical rule, another literal, and then a nonterminal call. A
runtime-compiled block can perform the first three operations in one interpreter visit while preserving their individual
tree contributions. The shared scheduler is entered at the call, a join, a recursive boundary, or another point whose
semantics require it. This does not require committing to one alternative: several alternatives can each execute a block
before returning to the same generalized machinery.

The compiler should retain useful structure before lowering everything into synthetic nonterminals. A compact automaton
can share common prefixes while attaching the production identities, capture operations, and output recipes needed to
recover the original grammar's trees. Common suffixes may offer further sharing, but their different incoming evidence
must remain distinguishable. Language equivalence alone is an insufficient criterion for merging automaton states.

A possible fixed instruction vocabulary is:

| Operation | Purpose |
|---|---|
| `MATCH` / `SCAN` | Recognize a literal or regular fragment and record its semantic result. |
| `CALL` | Subscribe to a shared nonterminal invocation. |
| `PUBLISH` | Add a completed result and notify relevant subscribers. |
| `TEST` | Evaluate a supported predicate under explicit context rules. |
| `CAPTURE` | Record branch-local evidence or an environment update. |
| `CHOICE` | Register the alternatives required by the declared choice semantics. |

These names are illustrative. The experiment succeeds only if the resulting program performs fewer scheduling and
indexing operations, or improves their locality enough to outweigh compilation cost. A larger `switch` statement is not
itself a contribution.

Loops deserve explicit attention. A list operation may avoid repeated synthetic-rule traffic, but a greedy loop is not a
general replacement for relational repetition. The operation must publish every endpoint the surrounding grammar is
entitled to use, unless a stronger restriction is established. Empty matches and nullable cycles require explicit
handling. Likewise, terminal fusion must reproduce failure positions and expected-symbol information under the chosen
diagnostic contract.

**Correctness obligation.** Block execution must simulate the original slot transitions, including evidence, predicates,
recursion, and diagnostics. Deduplication can move to block entries only where no omitted internal join changes that
simulation.

**Reject or defer H2** if blocks are consistently too short, compilation expands the grammar excessively, or the saved
descriptor operations are dominated by work that still has to occur inside each block.

### 5.3 H3: construct reusable derivation evidence while parsing

The source snapshot records GLL matching steps and later recovers a path through them. `newBuilder` first packs and
sorts steps; `path` then finds matching boundaries and tests reachability. Non-leaf DFA rules have a different replay
path: `dfaNode` uses `twalk` to recover structure from the recognized span, with a leaf fallback when replay does not
reach that endpoint. Both paths matter when evaluating reconstruction cost and tree fidelity. They must not be
conflated into one operation, or assumed correct merely because recognition succeeds.

The proposed alternative gives each partial production result an explicit handle to its derivation evidence. Extending a
prefix creates or retrieves a compact evidence node connecting that prefix to the newly recognized child. Completion
publishes an evidence handle as well as an endpoint. Multiple derivations remain shared or packed until the declared
disambiguation semantics allow selection.

The attractive case is one derivation: the representation can store a single link inline and expand to a packed
collection only when a competing derivation arrives. The difficult case is many competing derivations: interning and
maintaining evidence might cost more than the present step log. This proposal must therefore be tested as a
representation tradeoff, not presumed superior because it resembles a conventional shared packed parse forest.

Compilation can attach an output recipe to each grammar operation: retain a named node, flatten an internal list spine,
collapse a leaf, or omit a purely internal wrapper. These recipes should preserve the public tree while avoiding
repeated rediscovery of the relationship between lowered productions and source constructs.

An additional opportunity is demand-driven evidence. Lookahead already avoids public-tree construction, and collapsed
DFA leaves already avoid internal public nodes. The remaining opportunity is to suppress unnecessary generalized step
logging, share predicate evaluation, or avoid DFA structural replay when the declared result does not require it. The
compiler could propagate these requirements and select lighter evidence modes where sound. Captured values used by
later constraints must still be retained even when their corresponding tree nodes are hidden.

**Correctness obligation.** Evidence updates must preserve distinct derivations when they affect later constraints or
selection. Equal endpoints are not enough to establish interchangeable results. Early preference selection requires a
proof that discarded evidence cannot become relevant under any enclosing continuation; otherwise selection remains
deferred.

**Reject or defer H3** if evidence construction adds more hashing and allocation than reconstruction removes, or if the
benefit disappears when the same public output and ambiguity behaviour are required.

## 6. Context determines the limits of sharing

The three hypotheses interact most strongly at the invocation key. For an ordinary context-free rule, the input position
and rule identity determine its possible results. With indentation, captured text, lexical modes, or external syntactic
state, that is no longer sufficient.

A naive extension adds the complete environment to every key. This is safe only if the environment contains every
relevant dependency, and can be disastrously expensive even then. Two XML element parses may carry different outer names
while an internal lexical rule reads neither. Keying that lexical rule by the entire enclosing environment would destroy
sharing without improving correctness.

### 6.1 H4: project context to the dependencies of a rule

The proposed compiler computes a conservative summary of the environment components that each rule reads and writes,
including transitive dependencies through called rules and predicates. It then uses a projection of the incoming
context:

```text
InvocationKey(A, i, env) = (A, i, project(ReadDependencies(A), env)).
```

Grammar identity and input identity are implicit only inside one evaluation. Any cache spanning evaluations must include
their identities or otherwise prove safe reuse.

The corresponding result cannot simply return a complete cached environment from another caller. It should return the
relevant changes or a persistent result whose application preserves unrelated caller state. Evidence keys must likewise
retain contextual distinctions that affect interpretation. Reads include output behaviour where the output contract
depends on context; they are not limited to acceptance predicates.

This gives a concrete route to sharing despite context. A name rule can be context-free, a closing delimiter can depend
on a captured string, and a block can depend on an indentation reference. Rules with unknown dependencies use a
conservative key or forgo reuse. Arbitrary impure callbacks cannot participate in this optimization without an explicit
effect contract.

The research question is whether a small relevant projection usually exists. Measure distinct complete environments
versus distinct projections for the same `(rule, position)`, the cost of comparing those keys, and the reduction in
repeated work. **Reject H4 as an optimization** if projections remain large or context rarely repeats; retain explicit
context as a correctness requirement regardless of the performance result.

### 6.2 A practical warning from the existing pool

The earlier exploratory snapshot reused whitespace results on equal input without also checking the compiled grammar.
Forced pool reuse made a no-whitespace grammar accept `" a"` after a whitespace-accepting grammar, and caused the
reverse order to reject incorrectly. The later source snapshot examined for this paper adds `wrapC` to the reuse
condition. This paper does not claim that the historical bug is still present, or that all cache correctness is now
established.

The example illustrates a general rule: every omitted key component is a semantic assertion. Optimizations should make
that assertion explicit and test it under grammar changes, branch changes, nested context, and repeated calls. The same
principle governs callee sharing, DFA caching, predicate memoization, and future structural-summary reuse.

## 7. More ambitious directions

### 7.1 Batched relational evaluation

The most unconventional useful perspective is to treat parsing as incremental evaluation of a small relational program.
A nonterminal call joins waiting callers with completed results. Instead of evaluating every pair through an individual
descriptor, the runtime could group pending work by grammar state, position, and compatible context, then propagate only
new facts through the relevant groups.

This suggests adaptive representations: a singleton for one continuation, a small array for a few, and a bitmap or
indexed set for dense groups. Terminal matching can be shared across continuations that pose the same recognition
question. Each continuation must still receive its distinct evidence where required; batching does not make genuine
output work vanish.

Scheduling might alternate between depth-first execution for locality and batches for high-fanout joins. Changes must
preserve fixed-point completion and the handling of nullable recursion. Reclaiming an old input region is safe only when
no live continuation, lookahead computation, or retained evidence can require it; monotonic input positions alone do not
prove that condition.

**Experiment.** Measure join fanout, duplicate terminal questions, group density, and queue locality. Compare singleton,
sparse, and dense representations independently before introducing an adaptive policy. The hypothesis fails when groups
are too small or too heterogeneous to amortize batching. GPU execution is not an initial priority: the required density,
transfer amortization, and branch uniformity have not been demonstrated.

### 7.2 Grammar-derived scans and structural summaries

The current DFA layer advances through decoded characters and cached rune transitions. Possible improvements include
ASCII tables, Unicode equivalence classes, shared recognition of labelled alternatives, and bulk scanning of character
runs. All must preserve longest-match and label semantics. Tagged automata offer a precedent for carrying submatch
information through automaton execution, although their disambiguation policy must be reconciled with xbnf's trees.
[Trofimovich, 2017](https://re2c.org/2017_trofimovich_tagged_deterministic_finite_automata_with_lookahead.pdf).

There is also a conservative compile-time opportunity: the current regularity analysis rejects recursive rule cycles.
Some recursive components, such as suitably restricted right-linear definitions, still describe regular languages.
Recognizing proven eligible components could enlarge the automaton region. This is a sufficient-condition analysis, not
an attempt to decide regularity for arbitrary CFGs. Tree, capture, and endpoint semantics remain part of eligibility:
the current DFA convention returns the longest accepted prefix, while a recursive GLL rule can offer several endpoints
to its continuations. Promotion must preserve those endpoints or prove that the original semantics already select the
longest. Recognizing the same set of complete strings is insufficient.

A more speculative extension compiles cheap structural scans from a grammar: candidate delimiters, quote boundaries,
newlines, or homogeneous text runs. A scannerless parser can consume such an auxiliary index without introducing a
mandatory lexer, provided the index is a sound accelerator and exact grammar evaluation retains authority.

simdjson demonstrates the value of separating structural discovery from later interpretation for JSON. It supplies
inspiration for bulk work, not a portable grammar-independent algorithm. Contextual strings, escapes, comments, and
embedded languages make generalization a research problem. [Langdale and Lemire,
2019](https://arxiv.org/abs/1902.08318).

**Experiment.** Begin with grammar-proven long lexical runs and measure scanning as a fraction of total parse work. Do
not use the JSON profile's small DFA share to infer the share for XML text, SQL strings, or C++ identifiers. Reject
pre-indexing when its extra full-input pass costs more than the parser work it removes.

### 7.3 Deterministic regions inside a generalized runtime

A grammar fragment whose next action is proven unique may run with an ordinary compact stack, expanding into shared
continuations when necessary. Visibly pushdown languages provide a formal example of structured stack behaviour where
input classes determine push and pop actions, but not every nested programming-language fragment meets that definition.
[Alur and Madhusudan, 2004](https://www.cis.upenn.edu/~alur/Stoc04.pdf).

This is a useful specialization, not the organizing thesis of the project. The principal hypotheses also improve
execution when several alternatives remain live. A strategy valuable only on JSON-like deterministic nesting would leave
much of xbnf's intended problem untouched.

A safety condition is essential: one currently queued descriptor does not establish determinism. A suspended caller,
nullable cycle, or later completion may create additional work. Either the compiler supplies a sound eligibility
argument, or the runtime retains enough state to expand without losing alternatives. The current ambiguity diagnostics,
which include sample-based checks, must not be repurposed as a general optimization certificate.

### 7.4 Reusable summaries of repeated syntax

The furthest-reaching proposal is to cache a grammar fragment's behaviour as a transfer summary: how a span transforms
an admissible entry state into exits, context changes, and evidence. Repeated input fragments could then reuse that
work. A summary keyed only by byte shape is unsound: equal delimiters or equal lengths do not imply equal parsing
behaviour.

The necessary key may include exact content, grammar fragment, lexical mode, relevant context, and input-version
information. It also needs an account of the input read outside the returned span. For example, a rule matching `"x"`
followed by lookahead for `"y"` consumes the same one-character span at two occurrences but can succeed at only one.
Longest-match stopping decisions can also inspect following input. Reuse therefore requires either a proven independent
boundary or recorded external read dependencies whose validity is checked. Exact content can be checked after hashing;
captures and output spans must be rebound correctly. A parameterized summary for varying identifiers would need an
additional proof that those bytes affect only specified outputs.

This direction is most credible after invocation identity and evidence semantics are explicit. It should first target
repetition within a parse, then repeated documents, with editor-style incremental parsing treated as a separate
workload. Changing the benchmark to repeated identical input would not demonstrate a general improvement.

## 8. The evaluation portfolio

The seven language tracks in 🎯T22 supply complementary pressure on the hypotheses. They are intended workloads, not
seven completed grammars or seven available performance results.

| Track | Primary questions | Reference and comparison boundary |
|---|---|---|
| SQL, 🎯T22.2 | Shared prefixes, expression structure, optional clauses, callee reuse. | A pinned PostgreSQL raw parser; separate syntax from binding and execution. |
| XML, 🎯T22.3 | Long text runs, nesting, captured name agreement, contextual reuse. | libxml2 under an explicit well-formedness profile; align entity, DTD, and namespace treatment. |
| C++, 🎯T22.4 | Large grammar, declaration/expression interactions, templates, expensive continuations. | Pinned Clang on identical preprocessed inputs; account for semantic work in syntax-only mode. |
| Python, 🎯T22.5 | Indentation, logical lines, contextual keywords, embedded expressions. | CPython AST parsing; distinguish parsing from later compiler checks. |
| YAML, 🎯T22.6 | Indentation/context parameters and scalar interpretation. | A pinned YAML test suite and compatible parser; separate syntax/events from value construction. |
| JavaScript, 🎯T22.7 | Lexical goals, template boundaries, newline-sensitive syntax. | Pinned Acorn with matching edition and Script/Module settings. |
| CommonMark, 🎯T22.8 | Block/inline interactions, competing delimiters, deferred references, literal fallback. | Pinned cmark and specification examples; compare structure or rendering, not acceptance alone. |

The feature choices are grounded in the [PostgreSQL parser
description](https://www.postgresql.org/docs/17/parser-stage.html), [XML specification](https://www.w3.org/TR/xml/),
[Clang tooling documentation](https://clang.llvm.org/docs/LibTooling.html), [Python lexical
specification](https://docs.python.org/3.13/reference/lexical_analysis.html), [YAML production
parameters](https://yaml.org/spec/1.2.2/#42-production-parameters), [ECMAScript lexical
grammar](https://tc39.es/ecma262/2024/multipage/ecmascript-language-lexical-grammar.html), and [CommonMark
specification](https://spec.commonmark.org/0.31.2/).

Whole-language names are insufficient experimental definitions. Every track needs a frozen dialect, reference version,
corpus revision, licence, selection rule, exclusions, and output contract. C++ name/type-dependent decisions and
CommonMark reference interpretation deserve explicit boundaries; silently accepting a CFG superset is not full-language
conformance. Source input for xbnf remains characters, including for Python and JavaScript. External token streams must
not do the hard part on xbnf's behalf.

JSON and xbnf self-hosting remain useful auxiliary regressions. arr.ai is outside this portfolio. The earlier Pigeon
measurements remain a frozen historical record, not a benchmark programme to restart.

Real-language corpora must be complemented by small grammar-driven tests. These isolate shared callees, nullable chains,
left recursion, competing endpoints, ordered versus unordered choice, label ties, context differences, and ambiguous
derivations admitted by the declared disambiguation contract. Additional cases should cross compact-key limits and
Unicode boundaries. Mainstream-language throughput cannot substitute for these semantic tests.

## 9. Experimental method and falsification

### 9.1 Establish two independent comparisons

One comparison checks the optimizer against xbnf's semantics. Retain a deliberately simple evaluator for a bounded,
well-defined grammar subset, enumerate small inputs, and compare accepted spans and derivation structure. The old engine
is a useful regression reference, but known defects mean it cannot be the sole semantic authority.

The other comparison checks the language grammar against its external reference. A perfectly optimized execution of an
incorrect SQL grammar remains incorrect. Conversely, a reference compiler's semantic rejection must not automatically be
classified as an xbnf syntax error. These are distinct verification problems.

Metamorphic checks provide additional pressure: reorder unordered alternatives, rename local rules consistently, vary
irrelevant context, and compare fresh execution with pooled execution. Such transformations must preserve the declared
observable result; they do not apply indiscriminately to ordered alternatives, labels, or observable rule names.
Mutation tests should deliberately omit a context key, lose a completion, or discard a competing derivation and confirm
that the checks detect the resulting error.

### 9.2 Measure the shipped path and explain it separately

Headline results use the uninstrumented public `Compiled.Parse` path with the required tree. Diagnostic runs collect
mechanism counters separately. At minimum, record:

1. Grammar parsing and compilation, including automaton size and retained compiled state.
2. First parse in a fresh process or explicitly isolated runtime, including lazy automaton construction and fresh pools.
3. Warm parsing of varied documents under one compiled grammar.
4. Warm parsing of identical documents, reported as a distinct cache-sensitive case.
5. Public output cost, additional interpretation, and retained input ownership.
6. Failure behaviour and resource growth on independently specified malformed inputs.

A newly compiled grammar does not by itself guarantee a cold runtime: global pools can survive compilation. Likewise,
process RSS, live heap, backing capacity, total allocated bytes, and `B/op` answer different questions. Report them
separately. The diagnostic prototype present in the source snapshot leaves several counters unmeasured and estimates
only selected chart storage; its zero values must not be interpreted as measured absence of work.

For timing decisions, reuse the interleaved discipline in the parse-speed log. Freeze the correctness-qualified
baseline, alternate baseline and candidate, run a same-binary noise check, and retain per-file distributions and slow
outliers. The existing JSON gate's decision thresholds are a local keep/discard policy, not proof of a cross-language
effect. Correctness and corpus coverage must not drift when the baseline changes.

### 9.3 Attribute improvements through ablation

| Experiment | Primary mechanism measure | What would weaken the hypothesis |
|---|---|---|
| H1: callee sharing | Repeated compatible expansions and completion publications removed. | Almost no duplicated callee work, or subscriber overhead cancels the gain. |
| H2: execution blocks | Scheduler entries and table probes per semantic operation. | Short blocks, code/data expansion, or unchanged administrative work. |
| H3: direct evidence | Packing/reachability work removed versus new evidence-maintenance work. | More total hashing, memory traffic, or retained evidence under equal output. |
| H4: context projection | Complete-context diversity versus relevant-context diversity. | Little reusable context or costly projection/equality checks. |
| Batched joins | Work-group size, density, and repeated terminal questions. | Sparse groups and evidence scatter dominate. |
| Bulk lexical scans | Bytes scanned per operation and fraction of total time. | Extra passes outweigh saved scanner work. |

Run each change independently before combining them, then measure interactions. H1 may create larger batches; H3 may
increase per-state size; H2 may make the remaining hash-table cost more visible. Multiplying isolated speedups would
assume independence that has not been established.

Before making a headline claim, define the aggregation rule and the treatment of regressions. Report each language
alongside any aggregate so one large or favourable corpus cannot hide losses elsewhere. No files may disappear from the
denominator because the candidate fails to parse them or runs out of resources.

### 9.4 Verification cost and residual uncertainty

The largest research risk is misattributing a favourable number to a sound architectural improvement. A shared harness
and executable semantic checks reduce repeated verification work across all hypotheses. Existing tree, recursion,
disambiguation, pooling, and scaling tests are useful starting points; missing span/evidence comparisons and contextual
equivalence checks remain necessary extensions.

This follows the oracle-first distinction between conformance to a declared model and validity of the model itself.
Finite tests can check implementations and expose mistakes; they cannot prove that the selected languages fully
represent future use, or that an informal transformation is universally sound. Those residual questions require explicit
argument and review. Artifact counts, grammar counts, or a harness that has not run on the product are not performance
evidence.

The target graph already provides the main dependency: 🎯T22.1 is the shared evaluation foundation, and the seven
language tracks supply evidence to 🎯T22. Known compiler and pool prerequisites remain tracked as 🎯T17 and 🎯T21. This
paper is research groundwork for that programme, not evidence that any of those targets is achieved. Architectural
hypotheses should become implementation experiments only as their required measurements and semantic contracts become
available.

## 10. Recommended research order

The first priority is to measure compatible callee duplication and derivation reconstruction on the language portfolio.
Those measurements directly test whether the two largest conceptual changes have room to pay. Complete context semantics
where a track requires them; an unsound context cache invalidates every later optimization result.

Next, prototype callee sharing and direct evidence as separable experiments. Add block execution around the resulting
semantic boundaries, rather than freezing an instruction set before understanding which operations should disappear.
Preserve structured grammar information in the compiler so successful experiments do not depend on reverse-engineering
its own desugaring.

After this foundation, investigate context projection, adaptive continuation representations, and bulk scans where
profiles justify them. Deterministic regions are one specialization among these. Transfer-summary caching is a later,
higher-risk experiment because it depends on a precise account of context, evidence, and reuse.

This ordering makes the VM emerge from the semantic and cost model. It also permits useful outcomes short of a wholesale
rewrite: callee keys, evidence storage, block scheduling, and DFA layout can each be evaluated within the GLL+DFA
family.

## 11. Threats to validity

**Evidence is narrow.** The numerical observations concern JSON and historical snapshots. No seven-language result,
general speedup, or new asymptotic bound is claimed.

**The source is moving.** The frozen source inspection includes uncommitted changes, including early support for
position constraints and captured references. Their presence is not proof of full Python layout, XML conformance, or
branch-local data-dependent semantics. Earlier statements about unsupported constructs must not be mistaken for current
capability tests.

**Measurement is incomplete.** Historical CPU profiles are exploratory and contain runtime activity outside the parse
itself. Storage counts omit some ownership and allocator effects. The raw exploratory artifacts are local, not a
published reproducibility package. They must be regenerated from a pinned candidate before supporting a quantitative
research claim.

**The proposed semantics have limits.** Negative predicates, nullable cycles, contextual effects, and disambiguation can
invalidate naive fixed-point or aggregation arguments. The context-free subset's bounds cannot simply be transferred to
arbitrary extensions.

**Comparators perform different work.** AST building, preprocessing, validation, semantic checks, rendering, and runtime
warmup can dominate a misleading ratio. Output equivalence and phase boundaries are part of the experiment, not
footnotes.

## 12. Conclusion

There is no evidence that xbnf has reached a fixed point. The evidence instead shows that substantial administrative
work remains after allocation improvements, while providing too little language diversity to identify a universal
winner.

The strongest research direction is a runtime-compiled generalized parser that shares the same semantic question across
callers, executes useful stretches of grammar before returning to the scheduler, and retains derivation evidence in a
form suited to the required output. Context must be made explicit so this sharing remains sound and can be limited to
the dependencies that matter.

A state machine and a VM are plausible vehicles for that design. Their value comes from eliminating redundant execution
and representation, not from their names. The seven-language programme can determine which opportunities survive contact
with general parsing, and which attractive ideas should be discarded.

## References

1. Afroozeh, A., and Izmaylova, A. (2015). *Faster, Practical GLL Parsing*. Compiler Construction, LNCS 9031, 89–108.
   [Author manuscript](https://ir.cwi.nl/pub/24026/24026B.pdf).
2. Scott, E., and Johnstone, A. (2016). *Structuring the GLL parsing algorithm for performance*.
   Science of Computer Programming 125, 1–22.
   [Publication and abstract](https://pure.royalholloway.ac.uk/en/publications/structuring-the-gll-parsing-algorithm-for-performance/).
3. Opedal, A., Zmigrod, R., Vieira, T., Cotterell, R., and Eisner, J. (2023). *Efficient Semiring-Weighted Earley Parsing*.
   ACL, 3687–3713. [Paper](https://aclanthology.org/2023.acl-long.204/).
4. Goodman, J. (1999). *Semiring Parsing*. Computational Linguistics 25(4), 573–606.
   [Paper](https://aclanthology.org/J99-4004.pdf).
5. Jim, T., Mandelbaum, Y., and Walker, D. (2010). *Semantics and algorithms for data-dependent grammars*.
   POPL, 417–430.
   [Publication](https://collaborate.princeton.edu/en/publications/semantics-and-algorithms-for-data-dependent-grammars).
6. Afroozeh, A., and Izmaylova, A. (2016). *Iguana: A Practical Data-Dependent Parsing Framework*.
   Compiler Construction. [Publication](https://ir.cwi.nl/pub/25126/).
7. McPeak, S., and Necula, G. C. (2004). *Elkhound: A Fast, Practical GLR Parser Generator*.
   Compiler Construction, 73–88. [Paper](https://people.eecs.berkeley.edu/~necula/Papers/elkhound_cc04.pdf).
8. Ierusalimschy, R. (2009). *A Text Pattern-Matching Tool based on Parsing Expression Grammars*.
   Software: Practice and Experience 39(3), 221–258.
   [Author manuscript](https://www.inf.puc-rio.br/~roberto/docs/peg.pdf).
9. Trofimovich, U. (2017). *Tagged Deterministic Finite Automata with Lookahead*.
   [Manuscript](https://re2c.org/2017_trofimovich_tagged_deterministic_finite_automata_with_lookahead.pdf).
10. Langdale, G., and Lemire, D. (2019). *Parsing Gigabytes of JSON per Second*.
    [Paper](https://arxiv.org/abs/1902.08318).
11. Alur, R., and Madhusudan, P. (2004). *Visibly Pushdown Languages*.
    [Paper](https://www.cis.upenn.edu/~alur/Stoc04.pdf).

## Appendix A. Evidence boundary and reproduction notes

The committed base inspected is `a9c96f580fd6ba177224deeb2083ca6da782d350`. Two working snapshots must be distinguished:

- **Exploratory census:** `/tmp/xbnf-structural-probe.8IA3Bn`, based on that commit plus earlier working changes.
  The local diagnostic test is `engine/structural_probe_test.go`; its retained output is
  `/tmp/xbnf-structural-probe.8IA3Bn-results.txt`. The successful run counted structures and deliberately reproduced the
  historical cross-grammar whitespace-cache mismatch. It was not a throughput measurement.
- **Paper source inspection:** `/tmp/xbnf-performance-paper.sn41S7`, a later copy of `engine`, `grammar`, `syntax`, and
  selected documents. It contains the `wrapC` reuse check, early positional/reference code, and `engine/profile.go`.
  The saved tracked-file patch has SHA-256
  `851a44df237ac59cdf6bd6e091c7025ca961721fbdebd06b770f044db575deba`.
  This patch checksum is not a complete snapshot identity: the copy also includes then-untracked files.

The retained CPU profile is `/tmp/xbnf-a9c96f5-cpu.pprof`, timestamped 2026-09-05 21:10:16 AEST. It records 3.66 seconds
of samples over a 4.34-second capture. Its filename does not identify the complete working-tree state. Re-reading it
with `go tool pprof -top -cum` reproduces the cited cumulative entries, not a new benchmark result.

The structural census's table-capacity calculation is:

| Table | Backing slots | Bytes per slot | Backing bytes |
|---|---:|---:|---:|
| Descriptor set | 524,288 | 16 | 8,388,608 |
| GSS index | 131,072 | 24 | 3,145,728 |
| Span index | 262,144 | 24 | 6,291,456 |
| Maximum-end index | 131,072 | 24 | 3,145,728 |
| Step-instance index | 131,072 | 24 | 3,145,728 |
| Reachability index | 131,072 | 24 | 3,145,728 |
| **Total** | | | **27,262,976 = 26 MiB** |

The main source anchors for reinspection are:

| File | Relevant structures and operations |
|---|---|
| [compile.go](/Users/marcelo/work/github.com/marcelocantos/xbnf/engine/compile.go) | `Compiled`, `elem`, `prod`, `analyzeRegular`, `flatten`, `quantNT`, `delimNT`. |
| [gll.go](/Users/marcelo/work/github.com/marcelocantos/xbnf/engine/gll.go) | `desc`, `gssNode`, `bind`, `add`, `advance`, `create`, `pop`, `process`, `succeeds`. |
| [tree.go](/Users/marcelo/work/github.com/marcelocantos/xbnf/engine/tree.go) | `packSteps`, `path`, `stepReach`, `pickAt`, `materialize`. |
| [dfa.go](/Users/marcelo/work/github.com/marcelocantos/xbnf/engine/dfa.go) | `match`, `step`, `matchAlts`, `matchCalls`. |
| [disambig.go](/Users/marcelo/work/github.com/marcelocantos/xbnf/engine/disambig.go) | Compile-time overlap diagnostics and sample-based checks. |
| [profile.go](/Users/marcelo/work/github.com/marcelocantos/xbnf/engine/profile.go) | Prototype diagnostics; unmeasured fields and selected chart-capacity estimate. |
| [parse-speed.md](/Users/marcelo/work/github.com/marcelocantos/xbnf/docs/parse-speed.md) | Historical observations, input distinctions, and interleaved measurement policy. |

These links point to the live checkout and may move beyond the inspected snapshot. A publishable experimental follow-up
must archive complete source identities, manifests, commands, toolchain and host details, raw results, and exclusions.
The present paper deliberately stops at an evidence-backed research agenda.
