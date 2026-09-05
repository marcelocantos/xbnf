#!/usr/bin/env bash
# Copyright 2026 Marcelo Cantos
# SPDX-License-Identifier: Apache-2.0
#
# Keep/discard gate for BenchmarkJSON64K. Interleaves old vs new so thermal
# drift and background load are common-mode. Refuses a verdict when the run
# is too noisy. See docs/parse-speed.md.
set -euo pipefail

ROOT="$(CDPATH= cd "$(dirname "$0")/.." && pwd)"
cd "$ROOT"

export GOWORK=off
export GOMAXPROCS=1
export LC_ALL=C

SELF=0
BASE=""
PAIRS="${PAIRS:-10}"
BENCHTIME="${BENCHTIME:-2s}"
SKIP_IDLE=0
IDLE_SECS="${IDLE_SECS:-90}"
HOG_CPU="${HOG_CPU:-40}"
LOAD_MAX="${LOAD_MAX:-2.5}"

usage() {
	cat <<'EOF'
Usage: scripts/bench-json-stable.sh [--base REF] [--self] [--pairs N] [--benchtime T] [--skip-idle]

Interleave BenchmarkJSON64K of the working tree against REF (default HEAD).
--self runs the same binary as both sides (harness sanity). Waits for load1
≤ 2.5 and no transient >40% CPU before starting.

Prints TIME / MEM / VERDICT. Exit 0 on KEEP, DISCARD, or SELF-OK; 2 on NOISY
or SELF-FAIL. Do not keep or discard a parse-speed change on a NOISY run.

  make bench-stable              # dirty tree vs HEAD
  make bench-stable BASE=a9c96f5
  make bench-stable SELF=1
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

# One BenchmarkJSON64K line → "ns B allocs" on stdout. cwd must be an engine/ dir
# so compileDoc can open ../docs/examples/json.xbnf.
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
		"${runner[@]}" -test.run '^$' -test.bench '^BenchmarkJSON64K$' -test.benchmem \
			-test.benchtime="$BENCHTIME" -test.count=1
	) >"$log" || {
		cat "$log" >&2
		return 1
	}
	awk '
		$1 ~ /^BenchmarkJSON64K/ {
			ns=""; bytes=""; allocs=""
			for (i = 1; i <= NF; i++) {
				if ($(i+1) == "ns/op") ns=$i
				if ($(i+1) == "B/op") bytes=$i
				if ($(i+1) == "allocs/op") allocs=$i
			}
			if (ns == "") exit 1
			printf "%s %s %s\n", ns, bytes, allocs
			exit 0
		}
		END { if (ns == "") exit 1 }
	' "$log"
}

compile_engine() {
	local dest="$1"
	local dir="$2"
	(cd "$dir" && go test -c -o "$dest" ./engine)
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
NEW_DIR="$ROOT/engine"
OLD_DIR="$ROOT/engine"

echo "=== bench-json-stable ==="
echo "root: $ROOT"
echo "pairs: $PAIRS × $BENCHTIME   GOMAXPROCS=1"

wait_idle

echo "compile new (working tree)..."
compile_engine "$NEW_BIN" "$ROOT"

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
	compile_engine "$OLD_BIN" "$OLD_SRC"
	OLD_DIR="$OLD_SRC/engine"
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
		old="$(run_one "$OLD_BIN" "$OLD_DIR")"
		new="$(run_one "$NEW_BIN" "$NEW_DIR")"
	else
		new="$(run_one "$NEW_BIN" "$NEW_DIR")"
		old="$(run_one "$OLD_BIN" "$OLD_DIR")"
	fi
	printf '%s %s\n' "$old" "$new" >>"$PAIRS_FILE"
	# old_ns old_B old_alloc new_ns new_B new_alloc
	echo "  pair $((i + 1))/$PAIRS  old=$(echo "$old" | awk '{printf "%.2f ms", $1/1e6}')  new=$(echo "$new" | awk '{printf "%.2f ms", $1/1e6}')"
	i=$((i + 1))
done

echo "load1 at end: $(load1)"

SELF="$SELF" OLD_LABEL="$OLD_LABEL" NEW_LABEL="$NEW_LABEL" python3 - "$PAIRS_FILE" <<'PY'
import os, statistics, sys

path = sys.argv[1]
old_ns, new_ns = [], []
old_b, new_b, old_a, new_a = [], [], [], []
with open(path) as f:
    for line in f:
        p = line.split()
        if len(p) != 6:
            raise SystemExit(f"bad pair line: {line!r}")
        ons, ob, oa, nns, nb, na = map(float, p)
        old_ns.append(ons)
        new_ns.append(nns)
        old_b.append(ob)
        new_b.append(nb)
        old_a.append(oa)
        new_a.append(na)

n = len(old_ns)
if n < 4:
    raise SystemExit("need at least 4 pairs")


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


ratios = [o / n_ for o, n_ in zip(old_ns, new_ns)]  # >1 ⇒ new faster
wins = sum(1 for o, n_ in zip(old_ns, new_ns) if n_ < o)
lose = sum(1 for o, n_ in zip(old_ns, new_ns) if n_ > o)
med_r = med(ratios)
old_cv, new_cv, r_cv = cv(old_ns), cv(new_ns), cv(ratios)
old_m, new_m = med(old_ns), med(new_ns)
ob, nb = med(old_b), med(new_b)
oa, na = med(old_a), med(new_a)

print()
print(f"old {os.environ.get('OLD_LABEL','old')}: " +
      " ".join(f"{x/1e6:.2f}" for x in old_ns) +
      f"  median {fmt_ms(old_m)}  cv {old_cv:.1%}")
print(f"new {os.environ.get('NEW_LABEL','new')}: " +
      " ".join(f"{x/1e6:.2f}" for x in new_ns) +
      f"  median {fmt_ms(new_m)}  cv {new_cv:.1%}")
print(f"pair speedup old/new: " +
      " ".join(f"{r:.3f}" for r in ratios) +
      f"  median {med_r:.3f}  cv {r_cv:.1%}  new-faster {wins}/{n}")
print(f"B/op   old {fmt_mb(ob)}  new {fmt_mb(nb)}")
print(f"allocs old {oa:.0f}  new {na:.0f}")

# Absolute CVs can be large when the package clocks down mid-run; pair ratios
# cancel that. A high ratio CV means the two sides are not seeing the same noise.
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

def side(old, new, rel=0.01):
    if new < old * (1 - rel) and new < old - 1:
        return "WIN"
    if new > old * (1 + rel) and new > old + 1:
        return "LOSE"
    return "TIE"

mem_b = side(ob, nb)
mem_a = side(oa, na)
if mem_b == "WIN" or mem_a == "WIN":
    mem = "WIN" if mem_b != "LOSE" and mem_a != "LOSE" else "MIXED"
elif mem_b == "LOSE" or mem_a == "LOSE":
    mem = "LOSE"
else:
    mem = "TIE"

self = os.environ.get("SELF") == "1"
if self:
    # Same binary must sit on 1.00. A biased or contended harness fails here.
    if abs(med_r - 1.0) <= 0.025 and r_cv <= 0.03 and time != "NOISY":
        verdict = "SELF-OK"
        rc = 0
    else:
        verdict = "SELF-FAIL"
        rc = 2
else:
    if time == "NOISY":
        verdict = "NOISY"
        rc = 2
    elif time == "WIN" or (time == "TIE" and mem == "WIN"):
        verdict = "KEEP"
        rc = 0
    else:
        verdict = "DISCARD"
        rc = 0

print()
print(f"TIME: {time}")
print(f"MEM: {mem}")
print(f"VERDICT: {verdict}")
sys.exit(rc)
PY
