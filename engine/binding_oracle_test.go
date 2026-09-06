// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package engine_test

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"runtime/debug"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/marcelocantos/xbnf/engine"
	"github.com/marcelocantos/xbnf/syntax"
)

type bindingOracleWitness struct {
	n, pad, m string
	refs      []string
}

// TestBindingOracleReconvergence compares the shipped parser with finite language
// enumeration, not a second chart parser. Enumerating every choice of n, padding,
// and m gives the complete language of copy below; no engine internals contribute
// to the accepted strings or their witnesses. Candidate strings independently
// vary the prefix and each copied suffix, including impossible combinations.
//
// Both callers reach the same copy invocation. The recursive root moves that
// invocation to other positions. Within copy, different n/padding splits meet at
// the same join invocation before %n. The delayed alternative and permutations
// exercise completion orders while keeping #prefer attached to the short branch.
// A selected tree may use any valid witness; ambiguous trees need not be identical.
func TestBindingOracleReconvergence(t *testing.T) {
	const maxPrefix = 5 // n and padding each have length 1 or 2; include both exterior bounds.
	const maxCopy = 3   // A/C have maximum length 2; include an overlong copy and the empty copy.
	for _, mode := range []string{"single", "repeated", "multiple"} {
		for order := 0; order < 4; order++ {
			for _, delayed := range []bool{false, true} {
				t.Run(fmt.Sprintf("%s/order%d/delayed%t", mode, order, delayed), func(t *testing.T) {
					long := `(?="a") "aa"`
					extra := ""
					if delayed {
						long = "Later"
						extra = "Later -> Last ;\nLast -> (?=\"a\") \"aa\" ;\n"
					}
					a := []string{`(?="a") "a" #prefer`, long}
					b := []string{`(?="a") "a" #prefer`, `(?="a") "aa"`}
					if order&1 != 0 {
						slices.Reverse(a)
					}
					if order&2 != 0 {
						slices.Reverse(b)
					}
					body := `n=A B join %n`
					switch mode {
					case "repeated":
						body += ` ":" %n`
					case "multiple":
						body = `n=A B m=C join %n ":" %m`
					}
					src := `root -> "(" root ")" | left #prefer | right ;
left -> copy ;
right -> copy ;
copy -> ` + body + ` ;
A -> ` + strings.Join(a, " | ") + ` ;
B -> ` + strings.Join(b, " | ") + ` ;
C -> (?="b") "b" #prefer | (?="b") "bb" ;
join -> (?="!") "!" ;
#wrap -> () ;
` + extra
					c := compileBindingOracle(t, src)

					// These loops are the oracle: choose each complete derivation and
					// concatenate its literals, independently of parser scheduling.
					valid := map[string][]bindingOracleWitness{}
					ms := []string{""}
					if mode == "multiple" {
						ms = []string{"b", "bb"}
					}
					for _, n := range []string{"a", "aa"} {
						for _, pad := range []string{"a", "aa"} {
							for _, m := range ms {
								text := n + pad + m + "!" + n
								refs := []string{n}
								switch mode {
								case "repeated":
									text += ":" + n
									refs = append(refs, n)
								case "multiple":
									text += ":" + m
									refs = append(refs, m)
								}
								valid[text] = append(valid[text], bindingOracleWitness{n: n, pad: pad, m: m, refs: refs})
							}
						}
					}

					var candidates []string
					maxM, maxSecond := 0, 0
					if mode == "multiple" {
						maxM = maxCopy
					}
					if mode != "single" {
						maxSecond = maxCopy
					}
					for prefix := 0; prefix <= maxPrefix; prefix++ {
						for m := 0; m <= maxM; m++ {
							for first := 0; first <= maxCopy; first++ {
								for second := 0; second <= maxSecond; second++ {
									text := strings.Repeat("a", prefix) + strings.Repeat("b", m) + "!" +
										strings.Repeat("a", first)
									switch mode {
									case "repeated":
										text += ":" + strings.Repeat("a", second)
									case "multiple":
										text += ":" + strings.Repeat("b", second)
									}
									candidates = append(candidates, text)
								}
							}
						}
					}

					failures, checked := 0, 0
					var samples []string
					for _, reverse := range []bool{false, true} {
						if reverse {
							// The same compiled grammar and pool must survive different input orders.
							slices.Reverse(candidates)
						}
						for _, text := range candidates {
							for _, depth := range []int{0, 2} {
								in := strings.Repeat("(", depth) + text + strings.Repeat(")", depth)
								res := c.Parse("root", in)
								want := valid[text]
								checked++
								problem := ""
								if res.OK != (len(want) != 0) {
									problem = fmt.Sprintf("OK=%t, want %t; error=%q", res.OK, len(want) != 0, res.Error)
								} else if res.OK {
									got, copies := selectedBindingWitness(res.Tree)
									matched := false
									for _, w := range want {
										matched = matched || (got.n == w.n && got.pad == w.pad &&
											got.m == w.m && slices.Equal(got.refs, w.refs))
									}
									if copies != 1 || !matched {
										problem = fmt.Sprintf("selected %d copies with %+v; valid derivations %+v", copies, got, want)
									} else if res.End != len(in) || res.Tree.Text != in {
										problem = fmt.Sprintf("incomplete public result: End=%d, tree text=%q", res.End, res.Tree.Text)
									}
								}
								if problem != "" {
									failures++
									if len(samples) < 4 {
										samples = append(samples, fmt.Sprintf("%q: %s", in, problem))
									}
								}
							}
						}
					}
					if failures != 0 {
						t.Errorf("%d/%d oracle checks failed:\n%s", failures, checked, strings.Join(samples, "\n"))
					}
				})
			}
		}
	}
}

// TestBindingOracleNestedScopes exhausts opening/closing-name combinations for
// one to three nested elements. Its oracle is string equality at each depth;
// names deliberately include a prefix pair and a multibyte character. A binding
// belongs to its own element even when several active invocations use the same
// capture name, and every accepted public subtree must exhibit that pairing.
func TestBindingOracleNestedScopes(t *testing.T) {
	const maxDepth = 3
	names := []string{"a", "aa", "β"}
	for _, reverse := range []bool{false, true} {
		t.Run(fmt.Sprintf("reversed%t", reverse), func(t *testing.T) {
			alts := []string{`(?=.) "a" #prefer`, `(?=.) "aa"`, `(?=.) "β"`}
			if reverse {
				slices.Reverse(alts)
			}
			c := compileBindingOracle(t, `element -> "<" n=Name ">" element? "</" %n ">" ;
Name -> `+strings.Join(alts, " | ")+` ;
#wrap -> () ;`)
			failures, checked := 0, 0
			var samples []string
			for depth := 1; depth <= maxDepth; depth++ {
				combinations := 1
				for i := 0; i < 2*depth; i++ {
					combinations *= len(names)
				}
				for choice := 0; choice < combinations; choice++ {
					open, close := make([]string, depth), make([]string, depth)
					n := choice
					for i := range open {
						open[i] = names[n%len(names)]
						n /= len(names)
						close[i] = names[n%len(names)]
						n /= len(names)
					}
					var text strings.Builder
					for _, name := range open {
						fmt.Fprintf(&text, "<%s>", name)
					}
					for i := depth - 1; i >= 0; i-- {
						fmt.Fprintf(&text, "</%s>", close[i])
					}
					in := text.String()
					want := slices.Equal(open, close)
					res := c.Parse("element", in)
					checked++
					problem := ""
					if res.OK != want {
						problem = fmt.Sprintf("OK=%t, want %t; error=%q", res.OK, want, res.Error)
					} else if res.OK {
						var found []string
						var walk func(engine.Node)
						walk = func(node engine.Node) {
							if node.Name == "element" {
								binding, ref := "", ""
								refs := 0
								for _, child := range node.Children {
									if child.Name == "n" {
										binding = child.Text
									}
									if child.Kind == "ref" {
										ref = child.Text
										refs++
									}
								}
								if binding == "" || refs != 1 || ref != binding {
									problem = fmt.Sprintf("element binds %q but has %d refs containing %q", binding, refs, ref)
								}
								found = append(found, binding)
							}
							for _, child := range node.Children {
								walk(child)
							}
						}
						walk(res.Tree)
						if !slices.Equal(found, open) {
							problem = fmt.Sprintf("selected binding scopes %q, want %q", found, open)
						}
					}
					if problem != "" {
						failures++
						if len(samples) < 4 {
							samples = append(samples, fmt.Sprintf("%q: %s", in, problem))
						}
					}
				}
			}
			if failures != 0 {
				t.Errorf("%d/%d oracle checks failed:\n%s", failures, checked, strings.Join(samples, "\n"))
			}
		})
	}
}

// Different capture contexts can complete the same production over the same
// span. Each is a valid derivation, so #assoc must compare their boundaries as
// well as the competitors kept within one context. For aaa!aaa there are exactly
// two witnesses: (n, B, ref, C) = (a, aa, a, aa) or (aa, a, aa, a).
func TestBindingOracleCompletionContexts(t *testing.T) {
	for order := 0; order < 8; order++ {
		var rules strings.Builder
		for i, name := range []string{"A", "B", "C"} {
			alts := []string{`(?="a") "a" #prefer`, `(?="a") "aa"`}
			if order&(1<<i) != 0 {
				slices.Reverse(alts)
			}
			fmt.Fprintf(&rules, "%s -> %s ;\n", name, strings.Join(alts, " | "))
		}
		for _, assoc := range []string{"left", "right", "none"} {
			t.Run(fmt.Sprintf("order%d/%s", order, assoc), func(t *testing.T) {
				c := compileBindingOracle(t, `copy -> n=A B "!" %n C #assoc=`+assoc+` ;
`+rules.String()+`#wrap -> () ;`)
				res := c.Parse("copy", "aaa!aaa")
				if assoc == "none" {
					if res.OK || !strings.Contains(res.Error, "#assoc=none") {
						t.Errorf("two valid binding contexts must fail #assoc=none; got %+v", res)
					}
				} else {
					wantN, wantPad := "aa", "a"
					if assoc == "right" {
						wantN, wantPad = wantPad, wantN
					}
					w, count := selectedBindingWitness(res.Tree)
					post := ""
					for _, child := range res.Tree.Children {
						if child.Name == "C" {
							post = child.Text
						}
					}
					if !res.OK || count != 1 || w.n != wantN || w.pad != wantPad ||
						!slices.Equal(w.refs, []string{wantN}) || post != wantPad || res.Packed != 0 {
						t.Errorf("#assoc=%s: got %+v, C=%q, result %+v; want n/ref=%q, B/C=%q, Packed=0",
							assoc, w, post, res, wantN, wantPad)
					}
				}
				// There is only one witness for aa!aa, even under #assoc=none.
				res = c.Parse("copy", "aa!aa")
				w, count := selectedBindingWitness(res.Tree)
				if !res.OK || count != 1 || w.n != "a" || w.pad != "a" ||
					!slices.Equal(w.refs, []string{"a"}) || res.Packed != 0 {
					t.Errorf("unique binding witness rejected or marked ambiguous: %+v", res)
				}
			})
		}
	}
}

// A root invocation and a recursive call at the same position can complete the
// same production with the same binding spans. Those are duplicate evidence for
// one witness; separate GSS frames must not manufacture public ambiguity.
func TestBindingOracleRecursiveCompletionIdentity(t *testing.T) {
	c := compileBindingOracle(t, `s -> (s "x" | n=A B "!" %n) #longest ;
A -> (?="a") "a" #prefer | (?="a") "aa" ;
B -> (?="a") "a" #prefer | (?="a") "aa" ;
#wrap -> () ;`)
	for _, in := range []string{"aa!a", "aa!ax", "aa!axx"} {
		res := c.Parse("s", in)
		if !res.OK || res.Packed != 0 {
			t.Errorf("%q has one derivation, got %+v", in, res)
		}
	}
}

func TestBindingOracleNullableAndDefault(t *testing.T) {
	// A present, empty binding wins over the default, including at EOF. The
	// same finite language is tested with and without a following literal;
	// neither recognition nor FIRST filtering may silently require a byte for
	// an empty reference. Reference defaults apply only to an absent binding.
	valid := map[string][]bindingOracleWitness{}
	for _, n := range []string{"", "a"} {
		for _, pad := range []string{"a", "aa"} {
			text := n + pad + "!" + n
			valid[text] = append(valid[text], bindingOracleWitness{n: n, pad: pad, refs: []string{n}})
		}
	}
	for _, ref := range []string{`%n`, `%n="x"`} {
		for _, terminator := range []string{"", "."} {
			t.Run(fmt.Sprintf("%s/followed%t", ref, terminator != ""), func(t *testing.T) {
				end := ""
				if terminator != "" {
					end = ` "."`
				}
				c := compileBindingOracle(t, `copy -> n=A B "!" `+ref+end+` ;
A -> (?=.) () #prefer | (?="a") "a" ;
B -> (?="a") "a" #prefer | (?="a") "aa" ;
#wrap -> () ;`)
				for prefix := 0; prefix <= 4; prefix++ {
					for _, suffix := range []string{"", "a", "aa", "x"} {
						text := strings.Repeat("a", prefix) + "!" + suffix
						in := text + terminator
						res := c.Parse("copy", in)
						want := valid[text]
						if res.OK != (len(want) != 0) {
							t.Errorf("%q: OK=%t, want %t; error=%q", in, res.OK, len(want) != 0, res.Error)
							continue
						}
						if res.OK {
							got, count := selectedBindingWitness(res.Tree)
							matched := false
							for _, w := range want {
								matched = matched || (got.n == w.n && got.pad == w.pad && slices.Equal(got.refs, w.refs))
							}
							if count != 1 || !matched {
								t.Errorf("%q: got %+v, want one of %+v", in, got, want)
							}
						}
					}
				}
			})
		}
	}

	// A default before the first occurrence of a name does not seed or replace
	// the binding read by a later reference in that same production.
	c := compileBindingOracle(t, `s -> %n="x" ":" n=A ":" %n ; A -> "a" ; #wrap -> () ;`)
	for _, in := range []string{"x:a:a", "a:a:a", "x:a:x"} {
		res := c.Parse("s", in)
		if res.OK != (in == "x:a:a") {
			t.Errorf("default before binding, %q: %+v", in, res)
		}
		if res.OK {
			var refs []string
			for _, child := range res.Tree.Children {
				if child.Kind == "ref" {
					refs = append(refs, child.Text)
				}
			}
			if !slices.Equal(refs, []string{"x", "a"}) {
				t.Errorf("default and bound references = %q, want [x a]", refs)
			}
		}
	}
	for _, tc := range []struct {
		ref, input string
		ok         bool
	}{
		{ref: `%missing=""`, input: "", ok: true},
		{ref: `%missing=""`, input: "a", ok: false},
		{ref: `%missing="a"`, input: "", ok: false},
		{ref: `%missing="a"`, input: "a", ok: true},
		{ref: `%missing`, input: "", ok: false},
		{ref: `%missing`, input: "a", ok: false},
	} {
		c := compileBindingOracle(t, `s -> `+tc.ref+` ; #wrap -> () ;`)
		res := c.Parse("s", tc.input)
		if res.OK != tc.ok {
			t.Errorf("%s on %q: OK=%t, want %t; error=%q", tc.ref, tc.input, res.OK, tc.ok, res.Error)
		}
		if res.OK {
			if len(res.Tree.Children) != 1 || res.Tree.Children[0].Kind != "ref" ||
				res.Tree.Children[0].Text != tc.input {
				t.Errorf("%s on %q: default text missing from tree %+v", tc.ref, tc.input, res.Tree)
			}
		}
	}
}

func TestBindingOracleRepeatedNames(t *testing.T) {
	c := compileBindingOracle(t, `s -> n=A ":" n=B "!" %n ;
A -> (?="a") "a" #prefer | (?="a") "aa" ;
B -> (?="b") "b" #prefer | (?="b") "bb" ;
#wrap -> () ;`)
	for _, first := range []string{"a", "aa"} {
		for _, second := range []string{"b", "bb"} {
			for _, suffix := range []string{"", "a", "aa", "b", "bb"} {
				in := first + ":" + second + "!" + suffix
				res := c.Parse("s", in)
				if res.OK != (suffix == first) {
					t.Errorf("first matching name must bind, %q: %+v", in, res)
				}
				if res.OK {
					var bindings, refs []string
					for _, child := range res.Tree.Children {
						if child.Name == "n" {
							bindings = append(bindings, child.Text)
						}
						if child.Kind == "ref" {
							refs = append(refs, child.Text)
						}
					}
					if !slices.Equal(bindings, []string{first, second}) || !slices.Equal(refs, []string{first}) {
						t.Errorf("%q: selected bindings=%q, refs=%q", in, bindings, refs)
					}
				}
			}
		}
	}
}

// A nullable suffix can make a recursive production complete over its child's
// exact span. A finite witness exists through the literal alternative, and tree
// selection must find one without overflowing or hanging the caller.
// Isolate each case so a regressed tree walk cannot overflow this test process
// or hang the entire suite; the reduced child stack turns runaway recursion into
// a quick failure, and the deadline also covers nonrecursive nontermination.
func TestBindingOracleNullableRecursiveCompletion(t *testing.T) {
	const helperKey = "XBNF_BINDING_ORACLE_CYCLE"
	cases := []struct {
		name, src, input string
	}{
		{
			name:  "default",
			src:   `s -> (s %missing="" | "a") #longest ; #wrap -> () ;`,
			input: "a",
		},
		{
			name:  "capture",
			src:   `s -> (s n=Empty %n | "a") #longest ; Empty -> () ; #wrap -> () ;`,
			input: "a",
		},
		{
			name:  "mutual",
			src:   `s -> (other %missing="" | "a") #longest ; other -> s ; #wrap -> () ;`,
			input: "a",
		},
		{
			name:  "unit wrapper around mutual recursion",
			src:   `s -> other ; other -> (s %missing="" | "a") #longest ; #wrap -> () ;`,
			input: "a",
		},
		{
			name:  "ordinary lookahead control",
			src:   `s -> (s (?="") | "a") #longest ; #wrap -> () ;`,
			input: "a",
		},
		{
			name:  "consuming recursion control",
			src:   `s -> (s %missing="a" | "a") #longest ; #wrap -> () ;`,
			input: "aaa",
		},
		{
			name:  "sequential nullable siblings control",
			src:   `s -> A A "a" ; A -> A %missing="" #avoid | () ; #wrap -> () ;`,
			input: "a",
		},
	}
	if name := os.Getenv(helperKey); name != "" {
		debug.SetMaxStack(1 << 20)
		for _, tc := range cases {
			if tc.name != name {
				continue
			}
			c := compileBindingOracle(t, tc.src)
			res := c.Parse("s", tc.input)
			if !res.OK || res.End != len(tc.input) || res.Tree.Text != tc.input {
				t.Fatalf("finite witness %q must parse: %+v", tc.input, res)
			}
			return
		}
		t.Fatalf("unknown subprocess case %q", name)
	}
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
			defer cancel()
			cmd := exec.CommandContext(ctx, exe, "-test.run=^TestBindingOracleNullableRecursiveCompletion$")
			cmd.Env = append(os.Environ(), helperKey+"="+tc.name)
			out, err := cmd.CombinedOutput()
			if err != nil {
				const maxDiagnostic = 2000
				if len(out) > maxDiagnostic {
					out = append(out[:maxDiagnostic], []byte("\n[diagnostic truncated]")...)
				}
				t.Fatalf("finite-tree subprocess: %v (deadline: %v)\n%s", err, ctx.Err(), out)
			}
		})
	}
}

func compileBindingOracle(t *testing.T, src string) *engine.Compiled {
	t.Helper()
	g, err := syntax.Parse([]byte(src))
	if err != nil {
		t.Fatal(err)
	}
	c, err := engine.Compile(g)
	if err != nil {
		t.Fatal(err)
	}
	return c
}

// Read the public tree only. Each generated input contains exactly one copy;
// recording its binding nodes and references identifies its selected derivation.
func selectedBindingWitness(n engine.Node) (bindingOracleWitness, int) {
	if n.Name == "copy" {
		var w bindingOracleWitness
		for _, child := range n.Children {
			switch child.Name {
			case "n":
				w.n = child.Text
			case "B":
				w.pad = child.Text
			case "m":
				w.m = child.Text
			}
			if child.Kind == "ref" {
				w.refs = append(w.refs, child.Text)
			}
		}
		return w, 1
	}
	var found bindingOracleWitness
	count := 0
	for _, child := range n.Children {
		w, n := selectedBindingWitness(child)
		if n != 0 {
			found = w
			count += n
		}
	}
	return found, count
}
