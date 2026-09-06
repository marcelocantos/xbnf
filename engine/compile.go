// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package engine

import (
	"fmt"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/marcelocantos/xbnf/grammar"
)

const (
	ekNT = iota
	ekTerm
	ekDFA
	ekLook
	ekNegLook
)

type elem struct {
	kind  int
	nt    string
	label string
	name  string // `name=` label from grammar.Named
	term  grammar.Term
	df    *dfa   // set for ekDFA
	nid   int    // dense id of nt, for ekNT / lookahead
	desc  string // interned describeElem text, for parse-failure messages
	// treeKind and treeName are the Node.Kind and Node.Name of the leaf node
	// the tree builder makes for this element, resolved at compile time so
	// appendElem does no type switch and no rule-table lookup. treeKind is ""
	// when the element makes no node of its own: an Empty/PosProp terminal
	// contributes nothing, and a structured regular body is walked instead.
	treeKind string
	treeName string
}

type prod struct {
	nt   string
	nid  int // dense id of nt, for famKey
	rhs  []elem
	dirs prodDirs
	// fallback marks the synthetic `level ::= tighter` alternative of a
	// precedence stack. It loses to any other derivation of the same span.
	fallback bool
	// firstBase is where this production's slots start in Compiled.slotFirst:
	// slot (pid, ip) is slotFirst[prods[pid].firstBase+ip]. It fits in the
	// padding after fallback, so prod is no larger for it.
	firstBase int32
}

// #assoc values, resolved at compile time so the tree builder compares an int
// per production instead of a string.
const (
	assocUnset = iota // no #assoc directive, or one with an unknown value
	assocLeft
	assocRight
	assocNone // #assoc=none: chaining the operator is an error
)

// prodDirs is the disambiguation vocabulary attached to one production.
type prodDirs struct {
	prefer   bool
	avoid    bool
	priority int
	assoc    int
}

func parseDirs(ds []grammar.Directive) prodDirs {
	var d prodDirs
	for _, x := range ds {
		switch x.Name {
		case "prefer":
			d.prefer = true
		case "avoid":
			d.avoid = true
		case "priority":
			n, _ := strconv.Atoi(x.Value)
			d.priority = n
		case "assoc":
			switch x.Value {
			case "left":
				d.assoc = assocLeft
			case "right":
				d.assoc = assocRight
			case "none":
				d.assoc = assocNone
			}
		}
	}
	return d
}

// Compiled is a grammar compiled to GLL slots and DFAs.
type Compiled struct {
	first   string
	regular map[string]bool
	dfa     map[string]*dfa
	prods   []prod
	ntProds map[string][]int
	wrap    grammar.Term
	wrapDFA *dfa
	rules   map[string]grammar.Rule
	// slotFirst is every slot's FIRST set in one flat array: the set of
	// prods[pid].rhs[ip:] lives at slotFirst[prods[pid].firstBase+ip]. One
	// array rather than a slice per production, because admits reads it on
	// the hottest path in the parser and a slice of slices costs a second
	// bounds check and a pointer chase into a separately allocated row.
	slotFirst []firstInfo
	// unitProd[pid] marks a production whose whole right-hand side is one
	// nonterminal. Neither of its two slots matches anything, so fork and
	// pop run them in place instead of scheduling a descriptor for each.
	unitProd []bool
	// captureSlots marks named elements read by a later %ref in the same
	// production. Only these elements extend a descriptor's binding context.
	// It is nil for grammars without such references, preserving the CFG path.
	captureSlots [][]bool
	// pred[nt] maps the first input byte to a unique production of nt when
	// the alternatives are non-nullable and FIRST-disjoint on ASCII.
	pred     map[string]*bytePred
	predN    []*bytePred
	ntProdsN [][]int
	ntNID    map[string]int
	// ntInfo[nid] is everything the tree builder needs to know about a
	// nonterminal, so that building a tree touches no nonterminal name.
	ntInfo []ntInfo
	// spineProd[pid] marks a production the builder must walk as a
	// transparent left-recursive spine rather than nest.
	spineProd []bool
	// treeCycle marks nonterminals that may participate in a tree cycle
	// without input progress. It is nil when the zero-growth graph is acyclic.
	treeCycle []bool
	// tw is the scratch tree walker reused by dfaNode. Like the DFA
	// transition caches it makes one Compiled single-parse-at-a-time.
	tw       twalk
	Warnings []string
}

func Compile(g *grammar.Grammar) (*Compiled, error) {
	if g == nil || len(g.Stmts) == 0 {
		return nil, fmt.Errorf("empty grammar")
	}
	if err := rejectLater(g.Stmts); err != nil {
		return nil, err
	}
	c := &compiler{
		rules:   map[string]grammar.Rule{},
		regular: map[string]bool{},
	}
	for _, st := range g.Stmts {
		switch s := st.(type) {
		case grammar.Rule:
			if c.first == "" {
				c.first = s.Name
			}
			c.rules[s.Name] = s
			c.order = append(c.order, s.Name)
		case grammar.Wrap:
			c.wrap = s.Body
		}
	}
	c.analyzeRegular()
	if err := c.checkDefined(); err != nil {
		return nil, err
	}
	for _, name := range c.order {
		r := c.rules[name]
		if hasMod(r.Mods, "lex") && !c.regular[name] {
			return nil, fmt.Errorf("#lex on non-regular rule %s", name)
		}
	}
	warns, err := c.checkDisambiguation()
	if err != nil {
		return nil, err
	}
	out := &Compiled{
		first:    c.first,
		regular:  c.regular,
		dfa:      map[string]*dfa{},
		ntProds:  map[string][]int{},
		wrap:     c.wrap,
		rules:    c.rules,
		Warnings: warns,
	}
	for _, name := range c.order {
		if c.regular[name] {
			d, err := buildDFA(c.rules[name].Body, c)
			if err != nil {
				return nil, err
			}
			out.dfa[name] = d
		}
	}
	if c.wrap != nil {
		if _, ok := c.wrap.(grammar.Empty); !ok {
			if d, err := buildDFA(c.wrap, c); err == nil {
				out.wrapDFA = d
			}
		}
	}
	for _, name := range c.order {
		if c.regular[name] {
			continue
		}
		c.emitRule(name, c.rules[name].Body)
	}
	out.ntNID = map[string]int{}
	for i := range c.prods {
		id, ok := out.ntNID[c.prods[i].nt]
		if !ok {
			id = len(out.ntNID)
			out.ntNID[c.prods[i].nt] = id
		}
		c.prods[i].nid = id
	}
	out.prods = c.prods
	for i, p := range c.prods {
		out.ntProds[p.nt] = append(out.ntProds[p.nt], i)
	}
	for name, d := range c.extraDFA {
		out.dfa[name] = d
	}
	bindDFAOwner(out)
	out.computeFirst()
	out.buildNTInfo()
	out.resolveElems()
	out.markCaptureSlots()
	out.markUnitProds()
	out.markSpineProds()
	return out, nil
}

// ntClass says which public node a derivation of a nonterminal becomes. It is
// the shape of the name — a user rule, one of the $q/$d/$st wrappers, or a
// transparent synthetic — decided once per nonterminal instead of by prefix
// tests on every node the builder makes.
const (
	ntClassRule   = iota // a named rule: wrap the kids in a "rule" node
	ntClassQuant         // $q…: a "quant" node, or nothing when it matched nothing
	ntClassDelim         // $d…: a "delim" node, or the kid itself when there is one
	ntClassSeq           // $st…: a "seq" node, or the kid itself when there is one
	ntClassSplice        // transparent: the kids stand in for the nonterminal
)

// ntInfo is the per-nonterminal tree-building data, indexed by dense
// nonterminal id.
type ntInfo struct {
	nt    string // the nonterminal's name, for diagnostics
	kind  string // Node.Kind of the wrapper this nonterminal builds
	name  string // Node.Name of that wrapper
	class int
}

// classifyNT decides a nonterminal's ntClass from its name. Named rules and
// the $q/$d/$st wrappers build a node; every other synthetic nonterminal is
// transparent, so its children are returned as-is.
func classifyNT(nt string) int {
	if !strings.HasPrefix(nt, "$") {
		return ntClassRule
	}
	switch {
	case strings.HasPrefix(nt, "$q") && !strings.HasPrefix(nt, "$qs") && !strings.HasPrefix(nt, "$qo"):
		return ntClassQuant
	case strings.HasPrefix(nt, "$d") && !strings.HasPrefix(nt, "$dl") && !strings.HasPrefix(nt, "$dtrail"):
		return ntClassDelim
	case strings.HasPrefix(nt, "$st"):
		return ntClassSeq
	default:
		return ntClassSplice
	}
}

func (c *Compiled) buildNTInfo() {
	c.ntInfo = make([]ntInfo, len(c.ntNID))
	for nt, id := range c.ntNID {
		info := ntInfo{nt: nt, class: classifyNT(nt)}
		switch info.class {
		case ntClassRule:
			info.kind, info.name = nodeKindRule, nt
		case ntClassQuant:
			info.kind = nodeKindQuant
		case ntClassDelim:
			info.kind = nodeKindDelim
		case ntClassSeq:
			info.kind = nodeKindSeq
		}
		c.ntInfo[id] = info
	}
}

// markSpineProds records the productions of a transparent left-recursive list
// ($qs, $dl). Nesting those through prodKidsInto would append the prefix
// children again at every spine node, which is O(n²) node copies.
func (c *Compiled) markSpineProds() {
	c.spineProd = make([]bool, len(c.prods))
	for i := range c.prods {
		pr := &c.prods[i]
		c.spineProd[i] = c.ntInfo[pr.nid].class == ntClassSplice && leftRec(pr)
	}
}

// markUnitProds records which productions are `A ::= B` for a nonterminal B.
// It runs after resolveElems, so a right-hand side that turned out to be a
// regular rule is already ekDFA and is not counted: those match in place
// inside process and never reach the create/fork path this marks.
func (c *Compiled) markUnitProds() {
	c.unitProd = make([]bool, len(c.prods))
	for i := range c.prods {
		rhs := c.prods[i].rhs
		c.unitProd[i] = len(rhs) == 1 && rhs[0].kind == ekNT
	}
}

func (c *Compiled) markCaptureSlots() {
	for pid, pr := range c.prods {
		for ip, e := range pr.rhs {
			ref, ok := e.term.(grammar.Ref)
			if !ok {
				continue
			}
			for at := 0; at < ip; at++ {
				if pr.rhs[at].name != ref.Name {
					continue
				}
				if c.captureSlots == nil {
					c.captureSlots = make([][]bool, len(c.prods))
				}
				if c.captureSlots[pid] == nil {
					c.captureSlots[pid] = make([]bool, len(pr.rhs))
				}
				c.captureSlots[pid][at] = true
				break // As with %ref lookup, the first matching name binds.
			}
		}
	}
}

// resolveElems fills per-element DFA pointers and nid indexes, and turns
// leftover ekNT references to regular rules into ekDFA.
func (c *Compiled) resolveElems() {
	n := len(c.ntNID)
	c.predN = make([]*bytePred, n)
	c.ntProdsN = make([][]int, n)
	for nt, pids := range c.ntProds {
		if id, ok := c.ntNID[nt]; ok {
			c.ntProdsN[id] = pids
		}
	}
	for nt, bp := range c.pred {
		if id, ok := c.ntNID[nt]; ok {
			c.predN[id] = bp
		}
	}
	for i := range c.prods {
		rhs := c.prods[i].rhs
		for j := range rhs {
			e := &rhs[j]
			if e.kind == ekNT && c.IsDFA(e.nt) {
				e.kind = ekDFA
			}
			if e.kind == ekDFA {
				e.df = c.dfa[e.nt]
			}
			if e.kind == ekNT || e.kind == ekLook || e.kind == ekNegLook {
				if id, ok := c.ntNID[e.nt]; ok {
					e.nid = id
				} else {
					e.nid = -1
				}
			}
			// Intern the failure description: noteFail asks for it on every
			// failed element match, and quoting a string term there was one
			// allocation per failure.
			e.desc = describeElem(*e)
			c.resolveElemNode(e)
		}
	}
}

// resolveElemNode fills the element's treeKind/treeName: the node the builder
// makes for it without inspecting the term, the name or the rule table.
func (c *Compiled) resolveElemNode(e *elem) {
	switch e.kind {
	case ekTerm:
		e.treeName = e.name
		switch e.term.(type) {
		case grammar.Empty, grammar.PosProp:
			e.treeKind = ""
		case grammar.Ref:
			e.treeKind = nodeKindRef
		case grammar.String:
			e.treeKind = nodeKindString
		default:
			e.treeKind = nodeKindChar
		}
	case ekDFA:
		// dfaNode flattens a /leaf/ body — and an unknown name — to one text
		// node, so those need neither the rule table nor the tree walk. A
		// $lf element is a leaf too, but it is anonymous: it carries only the
		// `name=` label its reference gave it.
		if strings.HasPrefix(e.nt, leafDFAPrefix) {
			e.treeKind, e.treeName = nodeKindLeaf, e.name
			return
		}
		rule, ok := c.rules[e.nt]
		if _, isLeaf := rule.Body.(grammar.Leaf); !ok || isLeaf {
			e.treeKind, e.treeName = nodeKindLeaf, e.nt
			if e.name != "" {
				e.treeName = e.name
			}
		}
	}
}

func bindDFAOwner(c *Compiled) {
	if c == nil {
		return
	}
	for _, d := range c.dfa {
		bindOneDFA(d, c)
	}
	bindOneDFA(c.wrapDFA, c)
}

func bindOneDFA(d *dfa, c *Compiled) {
	if d == nil {
		return
	}
	d.owner = c
	for i := range d.alts {
		bindOneDFA(d.alts[i].d, c)
	}
	for i := range d.ordered {
		bindOneDFA(d.ordered[i], c)
	}
}

func (c *Compiled) IsDFA(rule string) bool {
	_, ok := c.dfa[rule]
	return ok
}

func (c *Compiled) Parse(start, input string) *Result {
	if !utf8.ValidString(input) {
		return &Result{Error: "invalid UTF-8"}
	}
	if start == "" {
		start = c.first
	}
	return c.gll(start, input)
}

func Parse(g *grammar.Grammar, start, input string) *Result {
	c, err := Compile(g)
	if err != nil {
		return &Result{Error: err.Error()}
	}
	return c.Parse(start, input)
}

type compiler struct {
	rules    map[string]grammar.Rule
	order    []string
	first    string
	wrap     grammar.Term
	regular  map[string]bool
	prods    []prod
	hid      int
	extraDFA map[string]*dfa
	folded   map[string]string // rule name → $fi_ name under (?i:)
	// stackTighter is the next-tighter stack level while emitRule runs for
	// a level. An infix @:sep whose term rewrote to this ident must consume
	// a separator; the zero-operator case is the level's fallback.
	stackTighter string
}

func (c *compiler) analyzeRegular() {
	type info struct {
		refs []string
		bad  bool
	}
	inf := map[string]info{}
	var walk func(grammar.Term) (refs []string, bad bool)
	walk = func(t grammar.Term) (refs []string, bad bool) {
		if t == nil {
			return nil, false
		}
		switch x := t.(type) {
		case grammar.Ident:
			return []string{x.Name}, false
		case grammar.Stack, grammar.Self, grammar.Ref, grammar.ExtRef, grammar.MacroCall, grammar.PosProp:
			return nil, true
		case grammar.Alt, grammar.OrderedAlt:
			var terms []grammar.Term
			switch a := x.(type) {
			case grammar.Alt:
				terms = a.Terms
			case grammar.OrderedAlt:
				terms = a.Terms
			}
			for _, a := range terms {
				r, b := walk(a)
				refs = append(refs, r...)
				bad = bad || b
			}
		case grammar.Seq:
			for _, a := range x.Terms {
				r, b := walk(a)
				refs = append(refs, r...)
				bad = bad || b
			}
		case grammar.Named:
			return walk(x.Term)
		case grammar.Leaf:
			return walk(x.Term)
		case grammar.Quant:
			return walk(x.Term)
		case grammar.Delim:
			r1, b1 := walk(x.Term)
			r2, b2 := walk(x.Sep)
			return append(r1, r2...), b1 || b2
		case grammar.Scope:
			for _, d := range x.Decls {
				if ru, ok := d.(grammar.Rule); ok {
					r, b := walk(ru.Body)
					refs = append(refs, r...)
					bad = bad || b
				}
			}
			r, b := walk(x.Term)
			return append(refs, r...), bad || b
		case grammar.Lookahead:
			r, _ := walk(x.Term)
			return r, true
		case grammar.NegLookahead:
			r, _ := walk(x.Term)
			return r, true
		case grammar.CaseFold:
			return walk(x.Term)
		}
		return refs, bad
	}
	for name, r := range c.rules {
		refs, bad := walk(r.Body)
		inf[name] = info{refs: refs, bad: bad}
	}
	rec := map[string]bool{}
	var onstack []string
	seen := map[string]int{}
	var dfs func(string)
	dfs = func(n string) {
		if _, ok := seen[n]; ok {
			return
		}
		seen[n] = 1
		onstack = append(onstack, n)
		idx := map[string]int{}
		for i, s := range onstack {
			idx[s] = i
		}
		for _, m := range inf[n].refs {
			if j, ok := idx[m]; ok {
				for _, s := range onstack[j:] {
					rec[s] = true
				}
				continue
			}
			if _, known := inf[m]; known {
				dfs(m)
			}
		}
		onstack = onstack[:len(onstack)-1]
		seen[n] = 2
	}
	for _, n := range c.order {
		dfs(n)
	}
	changed := true
	for name := range c.rules {
		c.regular[name] = !inf[name].bad && !rec[name]
	}
	for changed {
		changed = false
		for name := range c.rules {
			if !c.regular[name] {
				continue
			}
			for _, r := range inf[name].refs {
				if _, ok := c.rules[r]; ok && !c.regular[r] {
					c.regular[name] = false
					changed = true
				}
			}
		}
	}
}

// A /leaf/ body is lifted into its own DFA under a synthetic name. The tree
// builder recognises one by prefix: it is the only DFA element that flattens
// to a "leaf" node with no rule name of its own.
const (
	leafDFAKind   = "lf"
	leafDFAPrefix = "$" + leafDFAKind
)

func (c *compiler) leafDFA(x grammar.Leaf) string {
	h := c.fresh(leafDFAKind)
	d := compileOneDFA(x, c)
	if c.extraDFA == nil {
		c.extraDFA = map[string]*dfa{}
	}
	c.extraDFA[h] = d
	return h
}

func (c *compiler) fresh(kind string) string {
	c.hid++
	return fmt.Sprintf("$%s%d", kind, c.hid)
}

func (c *compiler) addProd(nt string, rhs []elem) {
	c.prods = append(c.prods, prod{nt: nt, rhs: rhs})
}

func (c *compiler) addProdDirs(nt string, rhs []elem, dirs []grammar.Directive) {
	c.prods = append(c.prods, prod{nt: nt, rhs: rhs, dirs: parseDirs(dirs)})
}

// emitRule adds one production per alternative of body. Directives written
// on an alternative, or on a sequence that wraps the whole alternation,
// attach to the productions they govern.
func (c *compiler) emitRule(name string, body grammar.Term) {
	var inherited []grammar.Directive
	for {
		sq, ok := body.(grammar.Seq)
		if !ok || len(sq.Terms) != 1 {
			break
		}
		inherited = append(inherited, sq.Directives...)
		body = sq.Terms[0]
	}
	for _, a := range splitAlt(body) {
		dirs := inherited
		if sq, ok := a.(grammar.Seq); ok && len(sq.Directives) > 0 {
			dirs = append(append([]grammar.Directive{}, inherited...), sq.Directives...)
		}
		c.addProdDirs(name, c.flatten(a), dirs)
	}
}

func pegEncode(o grammar.OrderedAlt) grammar.Alt {
	alts := make([]grammar.Term, len(o.Terms))
	for i, t := range o.Terms {
		if i == 0 {
			alts[i] = t
			continue
		}
		seq := make([]grammar.Term, 0, i+1)
		for j := 0; j < i; j++ {
			seq = append(seq, grammar.NegLookahead{Term: o.Terms[j]})
		}
		seq = append(seq, t)
		if len(seq) == 1 {
			alts[i] = seq[0]
		} else {
			alts[i] = grammar.Seq{Terms: seq}
		}
	}
	return grammar.Alt{Terms: alts}
}

func splitAlt(t grammar.Term) []grammar.Term {
	if a, ok := t.(grammar.Alt); ok {
		return a.Terms
	}
	return []grammar.Term{t}
}

func (c *compiler) checkDefined() error {
	var missing []string
	seen := map[string]bool{}
	note := func(name string) {
		if name == "" || seen[name] {
			return
		}
		seen[name] = true
		missing = append(missing, name)
	}
	var walk func(t grammar.Term, locals map[string]bool)
	walk = func(t grammar.Term, locals map[string]bool) {
		if t == nil {
			return
		}
		switch x := t.(type) {
		case grammar.Ident:
			if _, ok := c.rules[x.Name]; ok {
				return
			}
			if locals[x.Name] {
				return
			}
			note(x.Name)
		case grammar.Seq:
			for _, s := range x.Terms {
				walk(s, locals)
			}
		case grammar.Alt:
			for _, s := range x.Terms {
				walk(s, locals)
			}
		case grammar.OrderedAlt:
			for _, s := range x.Terms {
				walk(s, locals)
			}
		case grammar.Named:
			walk(x.Term, locals)
		case grammar.Leaf:
			walk(x.Term, locals)
		case grammar.Quant:
			walk(x.Term, locals)
		case grammar.Delim:
			walk(x.Term, locals)
			walk(x.Sep, locals)
		case grammar.Stack:
			for _, s := range x.Levels {
				walk(s, locals)
			}
		case grammar.Scope:
			loc := map[string]bool{}
			for k, v := range locals {
				loc[k] = v
			}
			for _, d := range x.Decls {
				if r, ok := d.(grammar.Rule); ok {
					loc[r.Name] = true
				}
			}
			for _, d := range x.Decls {
				switch st := d.(type) {
				case grammar.Rule:
					walk(st.Body, loc)
				case grammar.Wrap:
					walk(st.Body, loc)
				}
			}
			walk(x.Term, loc)
		case grammar.Lookahead:
			walk(x.Term, locals)
		case grammar.NegLookahead:
			walk(x.Term, locals)
		case grammar.CaseFold:
			walk(x.Term, locals)
		case grammar.MacroCall:
			for _, a := range x.Args {
				walk(a, locals)
			}
		}
	}
	for _, name := range c.order {
		walk(c.rules[name].Body, nil)
	}
	if c.wrap != nil {
		walk(c.wrap, nil)
	}
	if len(missing) == 0 {
		return nil
	}
	return fmt.Errorf("undefined rule %s", strings.Join(missing, ", "))
}

func (c *compiler) flatten(t grammar.Term) []elem {
	return c.flattenAt(t, false)
}

func (c *compiler) foldNT(name string) string {
	if c.folded == nil {
		c.folded = map[string]string{}
	}
	if fn, ok := c.folded[name]; ok {
		return fn
	}
	fn := "$fi_" + name
	c.folded[name] = fn
	r, ok := c.rules[name]
	if !ok {
		return name
	}
	body := grammar.CaseFold{On: true, Term: r.Body}
	c.rules[fn] = grammar.Rule{Name: fn, Body: body}
	if c.regular[name] {
		d, err := buildDFA(body, c)
		if err == nil {
			if c.extraDFA == nil {
				c.extraDFA = map[string]*dfa{}
			}
			c.extraDFA[fn] = d
			c.regular[fn] = true
		}
	} else {
		c.regular[fn] = false
		c.emitRule(fn, body)
	}
	return fn
}

func (c *compiler) flattenAt(t grammar.Term, fold bool) []elem {
	switch x := t.(type) {
	case grammar.CaseFold:
		return c.flattenAt(x.Term, x.On)
	case grammar.Seq:
		var out []elem
		for _, s := range x.Terms {
			if sq, ok := s.(grammar.Seq); ok && len(sq.Directives) > 0 {
				// A directive on a nested group governs that group only.
				h := c.fresh("grp")
				c.addProdDirs(h, c.flattenAt(sq, fold), sq.Directives)
				out = append(out, elem{kind: ekNT, nt: h})
				continue
			}
			out = append(out, c.flattenAt(s, fold)...)
		}
		return out
	case grammar.Alt:
		h := c.fresh("alt")
		if fold {
			for _, a := range x.Terms {
				c.addProd(h, c.flattenAt(a, true))
			}
		} else {
			c.emitRule(h, x)
		}
		return []elem{{kind: ekNT, nt: h}}
	case grammar.OrderedAlt:
		return c.flattenAt(pegEncode(x), fold)
	case grammar.Ident:
		name := x.Name
		if fold {
			name = c.foldNT(x.Name)
		}
		if c.regular[name] {
			return []elem{{kind: ekDFA, nt: name, label: x.Label}}
		}
		return []elem{{kind: ekNT, nt: name, label: x.Label}}
	case grammar.Named:
		out := c.flattenAt(x.Term, fold)
		if len(out) == 1 {
			out[0].name = x.Name
			return out
		}
		h := c.fresh("nm")
		c.addProd(h, out)
		return []elem{{kind: ekNT, nt: h, name: x.Name}}
	case grammar.Leaf:
		inner := x
		if fold {
			inner.Term = grammar.CaseFold{On: true, Term: x.Term}
		}
		return []elem{{kind: ekDFA, nt: c.leafDFA(inner)}}
	case grammar.Quant:
		return []elem{{kind: ekNT, nt: c.quantNT(c.flattenAt(x.Term, fold), x.Min, x.Max)}}
	case grammar.Delim:
		d := x
		if fold {
			d.Term = grammar.CaseFold{On: true, Term: x.Term}
			d.Sep = grammar.CaseFold{On: true, Term: x.Sep}
		}
		return []elem{{kind: ekNT, nt: c.delimNT(d)}}
	case grammar.Stack:
		st := x
		if fold {
			ls := make([]grammar.Term, len(x.Levels))
			for i, l := range x.Levels {
				ls[i] = grammar.CaseFold{On: true, Term: l}
			}
			st.Levels = ls
		}
		return []elem{{kind: ekNT, nt: c.stackNT(st)}}
	case grammar.Scope:
		sc := x
		if fold {
			sc.Term = grammar.CaseFold{On: true, Term: x.Term}
		}
		return []elem{{kind: ekNT, nt: c.scopeNT(sc)}}
	case grammar.Lookahead:
		inner := x.Term
		if fold {
			inner = grammar.CaseFold{On: true, Term: inner}
		}
		h := c.fresh("la")
		c.emitRule(h, inner)
		return []elem{{kind: ekLook, nt: h}}
	case grammar.NegLookahead:
		inner := x.Term
		if fold {
			inner = grammar.CaseFold{On: true, Term: inner}
		}
		h := c.fresh("nl")
		c.emitRule(h, inner)
		return []elem{{kind: ekNegLook, nt: h}}
	case grammar.Empty:
		return nil
	case grammar.PosProp, grammar.Ref:
		return []elem{{kind: ekTerm, term: x}}
	case grammar.String:
		s := x
		if fold {
			s.Fold = true
		}
		return []elem{{kind: ekTerm, term: s}}
	case grammar.CharClass:
		cc := x
		if fold {
			cc.Fold = true
		}
		return []elem{{kind: ekTerm, term: cc}}
	case grammar.Escape, grammar.AnyChar:
		return []elem{{kind: ekTerm, term: x}}
	case grammar.Self:
		return nil
	default:
		return []elem{{kind: ekTerm, term: grammar.Empty{}}}
	}
}

// optMin and optMax are the bounds of `X?`, the one quantifier shape that
// lowers to a single nonterminal.
const (
	optMin = 0
	optMax = 1
)

func (c *compiler) quantNT(inner []elem, min, max int) string {
	h := c.fresh("q")
	if min == optMin && max == optMax {
		// `X?` is `$q ::= ε | inner` directly, not `$q ::= $qo` over a
		// separate `$qo ::= ε | inner`. The unit wrapper cost a GSS node,
		// a create, a descriptor, a completion and a pop per attempt
		// position; deriveInto spliced `$qo` into `$q` anyway, so the
		// kids — and the ε-first production order pick relies on — are
		// unchanged.
		c.addProd(h, nil)
		c.addProd(h, inner)
		return h
	}
	star := c.fresh("qs")
	// star ::= ε | star inner
	//
	// Loops are left-recursive on purpose. A right-recursive tail
	// (star ::= inner star) can complete at every later position, which
	// makes GLL quadratic in the number of iterations. Left recursion
	// completes each prefix once.
	c.addProd(star, nil)
	c.addProd(star, append([]elem{{kind: ekNT, nt: star}}, inner...))
	var rhs []elem
	for i := 0; i < min; i++ {
		rhs = append(rhs, inner...)
	}
	if max == grammar.Unbounded {
		rhs = append(rhs, elem{kind: ekNT, nt: star})
		c.addProd(h, rhs)
		return h
	}
	opt := c.fresh("qo")
	c.addProd(opt, nil)
	c.addProd(opt, inner)
	for i := min; i < max; i++ {
		rhs = append(rhs, elem{kind: ekNT, nt: opt})
	}
	c.addProd(h, rhs)
	return h
}

func (c *compiler) delimNT(d grammar.Delim) string {
	h := c.fresh("d")
	term := c.flatten(d.Term)
	sep := c.flatten(d.Sep)
	// list ::= term | list sep term   (left-recursive, see quantNT)
	list := c.fresh("dl")
	c.addProd(list, term)
	c.addProd(list, append(append([]elem{{kind: ekNT, nt: list}}, sep...), term...))
	body := []elem{{kind: ekNT, nt: list}}
	if d.Leading {
		lead := c.fresh("dlead")
		c.addProd(lead, nil)
		c.addProd(lead, sep)
		body = append([]elem{{kind: ekNT, nt: lead}}, body...)
	}
	if d.Trailing {
		trail := c.fresh("dtrail")
		c.addProd(trail, nil)
		c.addProd(trail, sep)
		body = append(body, elem{kind: ekNT, nt: trail})
	}
	if !d.Leading && !d.Trailing && c.stackInfix(d) {
		// term sep list — at least one separator. A single term is the
		// stack fallback; leaving it on $d lets @:op="?" @:op=":" match
		// any two juxtaposed tighter expressions.
		rhs := append(append(append([]elem{}, term...), sep...), elem{kind: ekNT, nt: list})
		c.addProd(h, rhs)
		return h
	}
	c.addProd(h, body)
	return h
}

func (c *compiler) stackInfix(d grammar.Delim) bool {
	id, ok := d.Term.(grammar.Ident)
	return ok && c.stackTighter != "" && id.Name == c.stackTighter
}

func (c *compiler) stackNT(st grammar.Stack) string {
	names := make([]string, len(st.Levels))
	for i := range st.Levels {
		names[i] = c.fresh("st")
	}
	for i, lev := range st.Levels {
		tighter := ""
		if i+1 < len(names) {
			tighter = names[i+1]
		}
		body := rewriteSelf(lev, tighter)
		prev := c.stackTighter
		c.stackTighter = tighter
		c.emitRule(names[i], body)
		c.stackTighter = prev
		if tighter != "" {
			c.addProd(names[i], []elem{{kind: ekNT, nt: tighter}})
			c.prods[len(c.prods)-1].fallback = true
		}
	}
	if len(names) == 0 {
		h := c.fresh("st")
		c.addProd(h, nil)
		return h
	}
	return names[0]
}

func rewriteSelf(t grammar.Term, tighter string) grammar.Term {
	switch x := t.(type) {
	case grammar.Self:
		if tighter == "" {
			return grammar.Empty{}
		}
		return grammar.Ident{Name: tighter}
	case grammar.Seq:
		ts := make([]grammar.Term, len(x.Terms))
		for i, s := range x.Terms {
			ts[i] = rewriteSelf(s, tighter)
		}
		return grammar.Seq{Terms: ts, Directives: x.Directives}
	case grammar.Alt:
		ts := make([]grammar.Term, len(x.Terms))
		for i, s := range x.Terms {
			ts[i] = rewriteSelf(s, tighter)
		}
		return grammar.Alt{Terms: ts}
	case grammar.OrderedAlt:
		ts := make([]grammar.Term, len(x.Terms))
		for i, s := range x.Terms {
			ts[i] = rewriteSelf(s, tighter)
		}
		return grammar.OrderedAlt{Terms: ts}
	case grammar.Named:
		return grammar.Named{Name: x.Name, Term: rewriteSelf(x.Term, tighter)}
	case grammar.Leaf:
		return grammar.Leaf{Term: rewriteSelf(x.Term, tighter)}
	case grammar.Quant:
		return grammar.Quant{Term: rewriteSelf(x.Term, tighter), Min: x.Min, Max: x.Max}
	case grammar.Delim:
		return grammar.Delim{
			Term: rewriteSelf(x.Term, tighter), Sep: rewriteSelf(x.Sep, tighter),
			Leading: x.Leading, Trailing: x.Trailing,
		}
	case grammar.Lookahead:
		return grammar.Lookahead{Term: rewriteSelf(x.Term, tighter)}
	case grammar.NegLookahead:
		return grammar.NegLookahead{Term: rewriteSelf(x.Term, tighter)}
	case grammar.CaseFold:
		return grammar.CaseFold{On: x.On, Term: rewriteSelf(x.Term, tighter)}
	default:
		return t
	}
}

func (c *compiler) scopeNT(sc grammar.Scope) string {
	type savedRule struct {
		name string
		r    grammar.Rule
		ok   bool
		reg  bool
	}
	var saved []savedRule
	for _, d := range sc.Decls {
		r, ok := d.(grammar.Rule)
		if !ok {
			continue
		}
		old, existed := c.rules[r.Name]
		saved = append(saved, savedRule{name: r.Name, r: old, ok: existed, reg: c.regular[r.Name]})
		c.rules[r.Name] = r
		c.regular[r.Name] = localRegular(r.Body)
		if c.regular[r.Name] {
			if d, err := buildDFA(r.Body, c); err == nil {
				if c.extraDFA == nil {
					c.extraDFA = map[string]*dfa{}
				}
				c.extraDFA[r.Name] = d
			}
		} else {
			c.emitRule(r.Name, r.Body)
		}
	}
	h := c.fresh("sc")
	c.emitRule(h, sc.Term)
	for i := len(saved) - 1; i >= 0; i-- {
		sr := saved[i]
		if sr.ok {
			c.rules[sr.name] = sr.r
			c.regular[sr.name] = sr.reg
		}
	}
	return h
}

func localRegular(t grammar.Term) bool {
	switch x := t.(type) {
	case grammar.Ident, grammar.Stack, grammar.Self, grammar.Ref, grammar.ExtRef, grammar.MacroCall, grammar.PosProp:
		return false
	case grammar.Alt, grammar.OrderedAlt:
		var terms []grammar.Term
		switch a := x.(type) {
		case grammar.Alt:
			terms = a.Terms
		case grammar.OrderedAlt:
			terms = a.Terms
		}
		for _, a := range terms {
			if !localRegular(a) {
				return false
			}
		}
	case grammar.Seq:
		for _, a := range x.Terms {
			if !localRegular(a) {
				return false
			}
		}
	case grammar.Named:
		return localRegular(x.Term)
	case grammar.Leaf:
		return localRegular(x.Term)
	case grammar.Quant:
		return localRegular(x.Term)
	case grammar.Delim:
		return localRegular(x.Term) && localRegular(x.Sep)
	case grammar.Lookahead, grammar.NegLookahead:
		return false
	case grammar.CaseFold:
		return localRegular(x.Term)
	case grammar.Scope:
		return localRegular(x.Term)
	}
	return true
}
