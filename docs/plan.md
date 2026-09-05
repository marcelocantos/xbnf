# xbnf implementation plan

Desired end state: xbnf compiles an `xbnf.xbnf`-shaped grammar and parses
input with a GLL-family CFG engine, using DFAs automatically for regular
fragments. The engine eats its own cooking: `docs/xbnf.xbnf` is parsed by
xbnf.

This is a new product (`github.com/marcelocantos/xbnf`), not an in-place
rewrite of `arr-ai/wbnf`. wbnf stays a PEG backtracker; xbnf does not
preserve ordered-choice semantics.

## Locked decisions

1. **Engine: GLL + layered DFA.** GLL (Scott & Johnstone, 2010) for the
   general case: scannerless, native left recursion, top-down, data-dependent
   extensions. Rules whose bodies lie in the regular fragment compile to
   character-class DFAs at grammar-compile time. LL(*)-style regular
   lookahead can layer on GLL later as prediction, not as the core engine.
   ALL(*) is rejected: its prediction DFA is token-keyed and assumes a
   context-free lexer.

2. **Alternation is unordered.** Source order of `|` has no meaning. A decision
   that is neither provably deterministic (bounded lookahead at compile
   time) nor covered by an explicit disambiguator is a compile error.
   Unnecessary disambiguation is a warning. Ordered choice is a different
   combinator (`|>`); mixing `|` and `|>` in one alternation is an error.

3. **Disambiguation vocabulary** (first-class syntax, `#` sigil):
   `#prefer` / `#avoid`, `#assoc=left|right|none`, `#priority`, `#longest`
   (default for regular-fragment rules).

4. **No lexer concept.** There are only rules. Regularity is detected;
   `#lex` is an optional strictness lock, not the optimisation switch.
   `rule::label` is filtered longest-match over named alternatives.

5. **Scannerless, scoped whitespace.** `#wrap` replaces wbnf `.wrapRE`.
   `/term/` matches term (the same language) and emits one string.
   Terminals that are character sequences use a wrap-off scope inside
   the slashes: `IDENT -> /{ #wrap -> () ; [A-Za-z_] [A-Za-z0-9_]* }/`.

6. **Bootstrap, then self-host.** A hand-written parser of `xbnf.xbnf`
   produces the grammar IR. The GLL engine consumes that IR. Once the
   engine parses `xbnf.xbnf`, the bootstrap parser is a compatibility
   path, not the specification.

7. **Optimizer thesis is compilation, not a second engine.** A parse is
   a relation over spans. Interpreting that spec as a naive fixpoint is
   correct and slow. DFA extraction, chart/SPPF reuse, and position
   indexing are compilation strategies over the same spec. They do not
   fork the implementation; they are how the GLL+DFA engine is justified.
   Relation-valued attributes (indentation as an attribute, not a
   primitive) stay out of the first engine slice.

## Non-goals for the first engine slice

- PEG compatibility or ordered-choice migration from existing `.wbnf` files.
- In-place evolution of `arr-ai/wbnf`.
- Algebraic grammar composition (`+`, `|=`, override) in full generality.
- `@col` / `@line` positional constraints (designed; not first slice).
- Relation-valued attributes / closing the parse-as-relation algebra.
- Code generation of Go types and visitors (wbnf had this; defer).

## Package shape (as packages appear)

```
cmd/xbnf/     CLI (--version, --help, --help-agent, parse, --explain)
grammar/      IR: Rule, Term (seq, alt, quant, stack, named, …)
syntax/       bootstrap parser: xbnf source → grammar.Grammar
engine/       GLL parse functions + DFA runner + SPPF/tree construction
fromwbnf/     parse old .wbnf into an IR of meaning; Convert emits xbnf
eval/         corpus harness: manifests, correctness, cold/warm, diagnostics
ast/          committed parse tree (after disambiguation)
```

Do not create empty packages ahead of the code that fills them.

## Work graph

Frontier moves left to right; T3, T4, and T5 fan out after T2.

```
T1 bootstrap
    → T2 grammar IR
         → T3 bootstrap parser (xbnf.xbnf → IR)
         → T4 GLL engine (unordered CFG, left recursion)
         → T5 regular-fragment DFA layer
              → T6 compile-time disambiguation
                   → T7 self-host (engine parses xbnf.xbnf)
                        → T8 CLI parse + --explain
```

Acceptance lives on the bullseye targets. T1 is the repo existing, building,
and carrying the spec. The rest is engine work.

## Bootstrap strategy

T3 is a recursive-descent parser written against `docs/xbnf.xbnf`, not a
second grammar language. It must parse `docs/xbnf.xbnf` itself and the
files under `docs/examples/`. Tests compare IR shape, not pretty-printed
round-trips, until unparse exists.

T4 is tested with IR values constructed in Go, so GLL does not wait on a
perfect bootstrap parser. The self-host gate (T7) is the join: bootstrap
IR of `xbnf.xbnf` plus GLL plus DFA plus disambiguation.

## Oracle notes

- **Spec oracle:** `docs/xbnf.xbnf` and the example grammars. The engine is
  wrong if it cannot parse these, or if it accepts an ambiguous grammar
  without a disambiguator.
- **Semantic oracle:** unordered CFG. A test that depends on alternative
  order is a bug in the test.
- **Performance oracle comes later.** First slice is correctness. `--explain`
  (T8) makes DFA vs GLL promotion visible so performance work has a knob.
- **Residue:** example grammars that use `@col` (python-subset) and macros
  (arrai) will not pass until those features exist. T7's gate is
  `xbnf.xbnf` plus `json.xbnf` and `calc.xbnf`.
