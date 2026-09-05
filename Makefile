VERSION := $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)
LDFLAGS := -ldflags "-X github.com/marcelocantos/xbnf.Version=$(VERSION)"

# Parent directory has a go.work that does not include this module.
export GOWORK := off

MAKEFLAGS += -j$(shell nproc 2>/dev/null || sysctl -n hw.ncpu 2>/dev/null || echo 4)

.PHONY: all build test vet clean smoke sandbox bullseye bench-json bench-stable eval-corpus eval-languages golden-update

all: build

build: bin/xbnf

bin/xbnf:
	go build $(LDFLAGS) -o bin/xbnf ./cmd/xbnf

test:
	go test ./...

vet:
	go vet ./...

clean:
	rm -rf bin/

smoke: build
	bin/xbnf --version
	bin/xbnf --help-agent >/dev/null
	bin/xbnf sandbox -h >/dev/null

sandbox: build
	bin/xbnf sandbox

# JSON 64 KB parse speed vs encoding/json and wbnf. Snapshot only — not a keep/discard gate.
bench-json:
	go test ./engine/ -run '^$' -bench 'BenchmarkJSON64K' -benchmem -benchtime=2s -count=3

# Standing T22.1 corpus command (json-smoke). Language tracks add their own manifests.
eval-corpus:
	go run ./cmd/xbnf eval eval/testdata/json-smoke/manifest.json

eval-languages:
	@fail=0; \
	for m in sql xml go python yaml javascript commonmark; do \
	  go run ./cmd/xbnf eval eval/testdata/$$m/manifest.json || fail=1; \
	done; \
	exit $$fail

# 🎯T25.1 golden ratchet: rewrite tree fingerprints after a deliberate tree change. See docs/parse-speed.md.
golden-update:
	XBNF_GOLDEN=update go test ./engine ./eval -run '^TestGolden$$' -count=1

# Interleaved BenchmarkJSON64K keep/discard. BASE=HEAD (dirty tree) or a SHA. SELF=1 is A vs A.
# See docs/parse-speed.md.
bench-stable:
	./scripts/bench-json-stable.sh $(if $(SELF),--self,$(if $(BASE),--base $(BASE),)) $(if $(PAIRS),--pairs $(PAIRS),) $(if $(SKIP_IDLE),--skip-idle,) $(if $(BENCHTIME),--benchtime $(BENCHTIME),)

# Standing invariants hook read by /cv (bullseye_convergence).
bullseye:
	@set -e; \
	go vet ./... && echo "✓ vet"; \
	go test ./... >/dev/null && echo "✓ tests"; \
	go build ./... && echo "✓ build"; \
	dirty=$$(git status --porcelain | grep -vE 'bullseye\.yaml$$' || true); \
	if [ -z "$$dirty" ]; then echo "✓ working tree clean"; \
	else \
	  echo ""; \
	  echo "================================================================"; \
	  echo "⚠  DIRTY WORKING TREE"; \
	  echo ""; \
	  echo "Warning only — invariants still pass (exit 0)."; \
	  echo "Look at the files below before starting a new target."; \
	  echo "Leftover work from a different objective → park it in a commit first."; \
	  echo "This session's WIP on the recommended target → continue."; \
	  echo "================================================================"; \
	  echo "$$dirty"; \
	  echo "================================================================"; \
	  echo ""; \
	fi
