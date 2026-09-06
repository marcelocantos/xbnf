#!/usr/bin/env bash
# Copyright 2026 Marcelo Cantos
# SPDX-License-Identifier: Apache-2.0
#
# Keep/discard gate for BenchmarkJSON64K (--bench json, default) or
# BenchmarkCorpus across all live language corpora (--bench corpus).
# Interleaves old vs new so thermal drift and background load are common-mode.
# Refuses a verdict when the run is too noisy. See docs/parse-speed.md.
set -euo pipefail

ROOT="$(CDPATH= cd "$(dirname "$0")/.." && pwd)"
cd "$ROOT"

export GOWORK=off
export GOMAXPROCS=1
export LC_ALL=C

SELF=0
BASE=""
BENCH="${BENCH:-json}"
PAIRS="${PAIRS:-10}"
BENCHTIME="${BENCHTIME:-2s}"
SKIP_IDLE=0
IDLE_SECS="${IDLE_SECS:-90}"
HOG_CPU="${HOG_CPU:-40}"
LOAD_MAX="${LOAD_MAX:-2.5}"

usage() {
	cat <<'EOF'
Usage: scripts/bench-json-stable.sh [--base REF] [--self] [--bench json|corpus]
                                     [--pairs N] [--benchtime T] [--skip-idle]

Interleave a benchmark of the working tree against REF (default HEAD).
--bench json (default) runs BenchmarkJSON64K in ./engine. --bench corpus runs
BenchmarkCorpus in ./eval — one sub-benchmark per live language manifest,
plus an aggregate row summing per-language ns/op. --self runs the same
binary as both sides (harness sanity). Waits for load1 ≤ 2.5 and no
transient >40% CPU before starting.

Prints TIME / MEM / VERDICT (plus one row per language and an aggregate row
for --bench corpus). Exit 0 on KEEP, DISCARD, MIXED, or SELF-OK; 2 on NOISY
or SELF-FAIL. Do not keep or discard a parse-speed change on a NOISY run.

  make bench-stable                    # dirty tree vs HEAD, JSON gate
  make bench-stable BASE=a9c96f5
  make bench-stable SELF=1
  make bench-corpus                    # dirty tree vs HEAD, corpus gate
  make bench-stable BENCH=corpus SELF=1
EOF
}

while [ $# -gt 0 ]; do
	case "$1" in
	--base)
		BASE="${2:?}"
		shift 2
		;;
	--self)
		SELF=1
		shift
		;;
	--bench)
		BENCH="${2:?}"
		shift 2
		;;
	--pairs)
		PAIRS="${2:?}"
		shift 2
		;;
	--benchtime)
		BENCHTIME="${2:?}"
		shift 2
		;;
	--skip-idle)
		SKIP_IDLE=1
		shift
		;;
	-h | --help)
		usage
		exit 0
		;;
	*)
		echo "unknown flag: $1" >&2
		usage >&2
		exit 2
		;;
	esac
done

if [ -z "$BASE" ]; then
	BASE=HEAD
fi

case "$BENCH" in
json)
	PKG="./engine"
	SUBDIR="engine"
	BENCH_NAME="BenchmarkJSON64K"
	;;
corpus)
	PKG="./eval"
	SUBDIR="eval"
	BENCH_NAME="BenchmarkCorpus"
	;;
*)
	echo "invalid --bench: $BENCH (want json or corpus)" >&2
	usage >&2
	exit 2
	;;
esac

load1() {
	# Darwin prints "{ 1.2 1.3 1.4 }" so $1 is just "{".
	sysctl -n vm.loadavg | tr -d '{}' | awk '{ print $1 }'
}

# mode=transient skips always-on AV (it will not drop if we wait).
# mode=resident prints only those. Empty mode prints every hog.
hogs() {
	python3 - "$HOG_CPU" "$$" "$PPID" "${1:-}" <<'PY'
import subprocess, sys

lim = float(sys.argv[1])
ignore = {sys.argv[2], sys.argv[3]}
mode = sys.argv[4] if len(sys.argv) > 4 else ""
av = ("BDLDaemon", "Bitdefender", "bdagent", "CarbonBlack", "falcond")
skip = ("kernel_task", "WindowServer", "loginwindow", "sysmond")
out = subprocess.check_output(["ps", "-axo", "pid=,%cpu=,comm="], text=True)
for line in out.splitlines():
    parts = line.split(None, 2)
    if len(parts) < 3:
        continue
    pid, cpu, comm = parts[0], float(parts[1]), parts[2]
    if pid in ignore or cpu < lim:
        continue
    if any(s in comm for s in skip):
        continue
    isav = any(s in comm for s in av)
    if mode == "transient" and isav:
        continue
    if mode == "resident" and not isav:
        continue
    print(f"  {pid:>5} {cpu:6.1f}  {comm}")
PY
}

wait_idle() {
	local resident
	resident="$(hogs resident || true)"
	if [ -n "$resident" ]; then
		echo "warning: resident CPU hog (running anyway; pair ratios must carry this):" >&2
		echo "$resident" >&2
	fi
	if [ "$SKIP_IDLE" -eq 1 ]; then
		printf 'idle check skipped; load1=%s\n' "$(load1)"
		return 0
	fi
	local deadline=$((SECONDS + IDLE_SECS))
	while :; do
		local busy
		busy="$(hogs transient || true)"
		l1="$(load1)"
		quiet=0
		awk -v l="$l1" -v m="$LOAD_MAX" 'BEGIN { exit !(l+0 <= m+0) }' && [ -z "$busy" ] && quiet=1
		if [ "$quiet" -eq 1 ]; then
			printf 'idle check: load1=%s (max %s)\n' "$l1" "$LOAD_MAX"
			return 0
		fi
		if [ "$SECONDS" -ge "$deadline" ]; then
			echo "NOISY: load1=$l1 (max $LOAD_MAX) or CPU still above ${HOG_CPU}% after ${IDLE_SECS}s:" >&2
			[ -n "$busy" ] && echo "$busy" >&2
			echo "TIME: NOISY" >&2
			echo "MEM: TIE"
			echo "VERDICT: NOISY"
			exit 2
		fi
		echo "waiting for idle (load1=$l1 max $LOAD_MAX); CPU hogs:" >&2
		[ -n "$busy" ] && echo "$busy" >&2
		sleep 3
	done
}

# Runs one benchmark binary from dir (so relative fixture paths resolve:
# ../docs/examples/json.xbnf for engine, testdata/... for eval). Prints
# "<name> <ns/op> <B/op> <allocs/op>" — one line per matched sub-benchmark
# (a single BenchmarkJSON64K line, or one BenchmarkCorpus/<language> line
# per live manifest).
run_one() {
	local bin="$1"
	local dir="$2"
	local log="$TMP/run.log"
	local runner=(env GOMAXPROCS=1 "$bin")
	if command -v caffeinate >/dev/null 2>&1; then
		runner=(caffeinate -i "${runner[@]}")
	fi
	(
		cd "$dir"
		"${runner[@]}" -test.run '^$' -test.bench "^${BENCH_NAME}\$" -test.benchmem \
			-test.benchtime="$BENCHTIME" -test.count=1
	) >"$log" || {
		cat "$log" >&2
		return 1
	}
	awk -v base="$BENCH_NAME" '
		$1 ~ ("^" base) {
			ns = ""; bytes = ""; allocs = ""
			for (i = 1; i <= NF; i++) {
				if ($(i+1) == "ns/op") ns = $i
				if ($(i+1) == "B/op") bytes = $i
				if ($(i+1) == "allocs/op") allocs = $i
			}
			if (ns == "") next
			name = $1
			sub(/-[0-9]+$/, "", name)   # drop the GOMAXPROCS suffix, if any
			sub("^" base, "", name)     # drop the shared Benchmark<Name> prefix
			sub(/^\//, "", name)        # drop the "/" before a sub-benchmark name
			if (name == "") name = base
			printf "%s %s %s %s\n", name, ns, bytes, allocs
			found = 1
		}
		END { if (!found) exit 1 }
	' "$log"
}

# Aborts if two runs of the same pair produced different benchmark name sets
# (e.g. BASE predates a language manifest) — paste would silently misalign
# columns otherwise.
check_names_match() {
	local old_names new_names
	old_names="$(printf '%s\n' "$1" | awk '{print $1}')"
	new_names="$(printf '%s\n' "$2" | awk '{print $1}')"
	if [ "$old_names" != "$new_names" ]; then
		echo "old/new benchmark name mismatch for this pair — cannot pair rows:" >&2
		echo "old: $old_names" >&2
		echo "new: $new_names" >&2
		exit 1
	fi
}

compile_bin() {
	local dest="$1"
	local dir="$2"
	(cd "$dir" && go test -c -o "$dest" "$PKG")
}

TMP="$(mktemp -d "${TMPDIR:-/tmp}/xbnf-bench-stable.XXXXXX")"
OLD_SRC=""
cleanup() {
	if [ -n "$OLD_SRC" ]; then
		git -C "$ROOT" worktree remove --force "$OLD_SRC" >/dev/null 2>&1 || true
	fi
	rm -rf "$TMP"
}
trap cleanup EXIT

NEW_BIN="$TMP/new.test"
OLD_BIN="$TMP/old.test"
NEW_DIR="$ROOT/$SUBDIR"
OLD_DIR="$ROOT/$SUBDIR"

echo "=== bench-json-stable ($BENCH) ==="
echo "root: $ROOT"
echo "pairs: $PAIRS × $BENCHTIME   GOMAXPROCS=1"

wait_idle

echo "compile new (working tree)..."
compile_bin "$NEW_BIN" "$ROOT"

if [ "$SELF" -eq 1 ]; then
	echo "self-check: same binary on both sides"
	cp "$NEW_BIN" "$OLD_BIN"
	OLD_DIR="$NEW_DIR"
	OLD_LABEL="self"
	NEW_LABEL="self"
else
	if git diff --quiet HEAD && git diff --cached --quiet && [ "$BASE" = HEAD ]; then
		echo "working tree matches HEAD; nothing to compare (use --self or BASE=<ref>)" >&2
		exit 2
	fi
	OLD_REV="$(git rev-parse --short "$BASE")"
	echo "compile old ($BASE → $OLD_REV)..."
	OLD_SRC="$TMP/oldsrc"
	git worktree add --detach "$OLD_SRC" "$BASE" >/dev/null
	compile_bin "$OLD_BIN" "$OLD_SRC"
	OLD_DIR="$OLD_SRC/$SUBDIR"
	OLD_LABEL="$OLD_REV"
	if git diff --quiet HEAD && git diff --cached --quiet; then
		NEW_LABEL="$(git rev-parse --short HEAD)"
	else
		NEW_LABEL="working-tree"
	fi
fi

echo "old: $OLD_LABEL"
echo "new: $NEW_LABEL"

echo "warmup..."
run_one "$OLD_BIN" "$OLD_DIR" >/dev/null
run_one "$NEW_BIN" "$NEW_DIR" >/dev/null

PAIRS_FILE="$TMP/pairs.txt"
: >"$PAIRS_FILE"

echo "interleave (alternate who runs first in each pair)..."
i=0
while [ "$i" -lt "$PAIRS" ]; do
	if [ $((i % 2)) -eq 0 ]; then
		old_multi="$(run_one "$OLD_BIN" "$OLD_DIR")"
		new_multi="$(run_one "$NEW_BIN" "$NEW_DIR")"
	else
		new_multi="$(run_one "$NEW_BIN" "$NEW_DIR")"
		old_multi="$(run_one "$OLD_BIN" "$OLD_DIR")"
	fi
	check_names_match "$old_multi" "$new_multi"
	# old_ns old_B old_alloc new_ns new_B new_alloc, one row per benchmark name.
	paste -d ' ' <(printf '%s\n' "$old_multi") <(printf '%s\n' "$new_multi") |
		while IFS=' ' read -r oname ons ob oa nname nns nb na; do
			printf '%s %s %s %s %s %s %s %s\n' "$i" "$oname" "$ons" "$ob" "$oa" "$nns" "$nb" "$na" >>"$PAIRS_FILE"
		done
	old_total="$(printf '%s\n' "$old_multi" | awk '{ s += $2 } END { printf "%.0f", s }')"
	new_total="$(printf '%s\n' "$new_multi" | awk '{ s += $2 } END { printf "%.0f", s }')"
	echo "  pair $((i + 1))/$PAIRS  old=$(awk -v v="$old_total" 'BEGIN { printf "%.2f ms", v/1e6 }')  new=$(awk -v v="$new_total" 'BEGIN { printf "%.2f ms", v/1e6 }')"
	i=$((i + 1))
done

echo "load1 at end: $(load1)"

SELF="$SELF" OLD_LABEL="$OLD_LABEL" NEW_LABEL="$NEW_LABEL" BENCH="$BENCH" python3 - "$PAIRS_FILE" <<'PY'
import os, statistics, sys

path = sys.argv[1]
bench = os.environ.get("BENCH", "json")
self_mode = os.environ.get("SELF") == "1"
old_label = os.environ.get("OLD_LABEL", "old")
new_label = os.environ.get("NEW_LABEL", "new")

# pair_idx name old_ns old_B old_alloc new_ns new_B new_alloc
rows = []
with open(path) as f:
    for line in f:
        p = line.split()
        if len(p) != 8:
            raise SystemExit(f"bad pair line: {line!r}")
        idx = int(p[0])
        name = p[1]
        ons, ob, oa, nns, nb, na = map(float, p[2:])
        rows.append((idx, name, ons, ob, oa, nns, nb, na))

n_pairs = len(set(r[0] for r in rows))
if n_pairs < 4:
    raise SystemExit("need at least 4 pairs")

names = sorted(set(r[1] for r in rows))
by_name = {}
for name in names:
    sub = sorted((r for r in rows if r[1] == name), key=lambda r: r[0])
    if len(sub) != n_pairs:
        raise SystemExit(f"{name}: expected {n_pairs} pairs, got {len(sub)}")
    by_name[name] = dict(
        old_ns=[r[2] for r in sub], old_b=[r[3] for r in sub], old_a=[r[4] for r in sub],
        new_ns=[r[5] for r in sub], new_b=[r[6] for r in sub], new_a=[r[7] for r in sub],
    )


def med(xs):
    return statistics.median(xs)


def cv(xs):
    m = statistics.mean(xs)
    if m == 0:
        return 0.0
    return statistics.pstdev(xs) / m


def fmt_ms(ns):
    return f"{ns/1e6:.2f} ms"


def fmt_mb(b):
    return f"{b/1e6:.2f} MB"


def side(old, new, rel=0.01):
    if new < old * (1 - rel) and new < old - 1:
        return "WIN"
    if new > old * (1 + rel) and new > old + 1:
        return "LOSE"
    return "TIE"


# Shared keep/discard statistics: pair ratios cancel common-mode drift; see
# docs/parse-speed.md "How to measure". Used for the single JSON series and
# for every corpus language plus its aggregate.
def compute(old_ns, new_ns, old_b, new_b, old_a, new_a, self_check):
    n = len(old_ns)
    ratios = [o / v for o, v in zip(old_ns, new_ns)]  # >1 ⇒ new faster
    wins = sum(1 for o, v in zip(old_ns, new_ns) if v < o)
    lose = sum(1 for o, v in zip(old_ns, new_ns) if v > o)
    med_r = med(ratios)
    old_cv, new_cv, r_cv = cv(old_ns), cv(new_ns), cv(ratios)
    old_m, new_m = med(old_ns), med(new_ns)
    ob, nb = med(old_b), med(new_b)
    oa, na = med(old_a), med(new_a)

    # Absolute CVs can be large when the package clocks down mid-run; pair
    # ratios cancel that. A high ratio CV means the two sides saw different noise.
    abs_noisy = old_cv > 0.12 and new_cv > 0.12
    ratio_noisy = r_cv > 0.04
    agree_win = wins >= int((n + 1) * 0.8)   # 8/10, 10/12
    agree_lose = lose >= int((n + 1) * 0.8)
    strong = agree_win or agree_lose

    if abs_noisy and ratio_noisy and not strong:
        time = "NOISY"
    elif ratio_noisy and not strong:
        time = "NOISY"
    elif med_r >= 1.03 and agree_win:
        time = "WIN"
    elif med_r <= 0.97 and agree_lose:
        time = "LOSE"
    else:
        time = "TIE"

    mem_b = side(ob, nb)
    mem_a = side(oa, na)
    if mem_b == "WIN" or mem_a == "WIN":
        mem = "WIN" if mem_b != "LOSE" and mem_a != "LOSE" else "MIXED"
    elif mem_b == "LOSE" or mem_a == "LOSE":
        mem = "LOSE"
    else:
        mem = "TIE"

    if self_check:
        # Same binary must sit on 1.00. A biased or contended harness fails here.
        if abs(med_r - 1.0) <= 0.025 and r_cv <= 0.03 and time != "NOISY":
            verdict = "SELF-OK"
        else:
            verdict = "SELF-FAIL"
    else:
        if time == "NOISY":
            verdict = "NOISY"
        elif time == "WIN" or (time == "TIE" and mem == "WIN"):
            verdict = "KEEP"
        else:
            verdict = "DISCARD"

    return dict(n=n, ratios=ratios, wins=wins, lose=lose, med_r=med_r,
                old_cv=old_cv, new_cv=new_cv, r_cv=r_cv, old_m=old_m, new_m=new_m,
                ob=ob, nb=nb, oa=oa, na=na, time=time, mem=mem, verdict=verdict)


if bench == "json":
    if len(names) != 1:
        raise SystemExit(f"expected exactly one benchmark series for --bench json, got {names}")
    s = by_name[names[0]]
    r = compute(s["old_ns"], s["new_ns"], s["old_b"], s["new_b"], s["old_a"], s["new_a"], self_mode)

    print()
    print(f"old {old_label}: " +
          " ".join(f"{x/1e6:.2f}" for x in s["old_ns"]) +
          f"  median {fmt_ms(r['old_m'])}  cv {r['old_cv']:.1%}")
    print(f"new {new_label}: " +
          " ".join(f"{x/1e6:.2f}" for x in s["new_ns"]) +
          f"  median {fmt_ms(r['new_m'])}  cv {r['new_cv']:.1%}")
    print(f"pair speedup old/new: " +
          " ".join(f"{v:.3f}" for v in r["ratios"]) +
          f"  median {r['med_r']:.3f}  cv {r['r_cv']:.1%}  new-faster {r['wins']}/{r['n']}")
    print(f"B/op   old {fmt_mb(r['ob'])}  new {fmt_mb(r['nb'])}")
    print(f"allocs old {r['oa']:.0f}  new {r['na']:.0f}")

    print()
    print(f"TIME: {r['time']}")
    print(f"MEM: {r['mem']}")
    print(f"VERDICT: {r['verdict']}")
    sys.exit(2 if r["verdict"] in ("NOISY", "SELF-FAIL") else 0)

# bench == "corpus": one row per language, then an aggregate summing
# per-language ns/B/allocs per run. See docs/parse-speed.md "Corpus gate".
lang_tag = {}
for name in names:
    s = by_name[name]
    r = compute(s["old_ns"], s["new_ns"], s["old_b"], s["new_b"], s["old_a"], s["new_a"], False)
    if r["time"] == "NOISY":
        tag = "NOISY"
    elif r["time"] == "LOSE":
        tag = "LOSE"
    elif r["time"] == "WIN" or (r["time"] == "TIE" and r["mem"] == "WIN"):
        tag = "KEEP"
    else:
        tag = "DISCARD"
    lang_tag[name] = tag
    print(f"{name:<12} speedup {r['med_r']:.3f}x  wins {r['wins']}/{r['n']}  "
          f"B/op old {fmt_mb(r['ob'])} new {fmt_mb(r['nb'])}  {tag}")

agg = {}
for key, old_key, new_key in (("ns", "old_ns", "new_ns"), ("b", "old_b", "new_b"), ("a", "old_a", "new_a")):
    agg["old_" + key] = [sum(by_name[nm][old_key][i] for nm in names) for i in range(n_pairs)]
    agg["new_" + key] = [sum(by_name[nm][new_key][i] for nm in names) for i in range(n_pairs)]

r_agg = compute(agg["old_ns"], agg["new_ns"], agg["old_b"], agg["new_b"], agg["old_a"], agg["new_a"], self_mode)
print(f"{'aggregate':<12} speedup {r_agg['med_r']:.3f}x  wins {r_agg['wins']}/{r_agg['n']}  "
      f"B/op old {fmt_mb(r_agg['ob'])} new {fmt_mb(r_agg['nb'])}  {r_agg['verdict']}")

print()
print(f"TIME: {r_agg['time']}")
print(f"MEM: {r_agg['mem']}")

if self_mode:
    verdict = r_agg["verdict"]  # SELF-OK / SELF-FAIL
elif r_agg["verdict"] == "KEEP":
    losers = sorted(nm for nm, tag in lang_tag.items() if tag == "LOSE")
    if losers:
        verdict = "MIXED"
        print(f"languages LOSE while aggregate KEEP: {', '.join(losers)}")
    else:
        verdict = "KEEP"
else:
    verdict = r_agg["verdict"]  # DISCARD or NOISY

print(f"VERDICT: {verdict}")
sys.exit(2 if verdict in ("NOISY", "SELF-FAIL") else 0)
PY
