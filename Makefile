# The MetaCensus API contract. Run from the repository root.
#
# buf and protoc-gen-go are `tool` dependencies of internal/tools, a module of
# its own so their ~90 transitive requirements stay out of the published
# module's go.mod. `go tool` only runs inside its own module, so instead of
# running buf from there we build the binaries into ./bin and run them from the
# repository root. Every relative path in buf.gen.yaml is therefore relative to
# the root, which is also where they read most naturally.
#
# GOWORK=off and GOTOOLCHAIN are load-bearing, both lessons from
# metacensus/infra#52. A go.work anywhere above this checkout would resolve tool
# versions against the union of its members and quietly lift the pins; and the
# `go` directive is a floor, not a ceiling, so without GOTOOLCHAIN a developer
# on a newer Go builds the plugins against a newer stdlib. protoc-gen-go stamps
# its own version into every .pb.go, and gofmt's doc-comment handling has
# changed between Go releases, so either one lands as generated-code drift in a
# PR that never touched a .proto.

.PHONY: help all gen generated-paths lint format format-check breaking test check clean deps hooks tools \
        release release-major release-minor release-patch latest list delete-tag

# `make` with no target lists the targets rather than running the whole suite,
# matching metacensus/infra. The listing is generated from the `## name — what
# it does` comments below, so a target and its description cannot drift; infra
# hand-writes its help text, which is the same information twice.
.DEFAULT_GOAL := help

GO_DIR      := go
TS_DIR      := ts
PROTO       := proto
TOOLS_DIR   := internal/tools
CHITEST_DIR := internal/chitest
BIN         := $(CURDIR)/bin

BUF := $(BIN)/buf

# Every path `gen` writes, named once. `clean` removes exactly these and the
# pre-commit hook asks git about exactly these, so the three cannot disagree.
# go/server/routes_gen.go is the one generated file sharing a directory with
# hand-written source (runtime.go, doc.go, the tests), which is why the list is
# of paths rather than of directories.
GENERATED := $(GO_DIR)/metacensus $(GO_DIR)/routes $(GO_DIR)/server/routes_gen.go $(TS_DIR)/src

# The exact Go that builds the generators, read out of go.mod rather than
# written here: go.mod is what CI's setup-go reads, and a second copy would
# have to agree with it forever with nothing making it. `toolchain` wins when
# present, since Go omits it only when it matches `go`. Lifted from
# metacensus/infra's Makefile, error guard included — GOTOOLCHAIN= with an
# empty value is silently accepted, so an unpinned build must fail loudly here
# rather than produce drifted generated code later.
GOTOOLCHAIN_PIN ?= $(shell awk '/^toolchain /{t=$$2} /^go /{if (g == "") g = "go" $$2} END{print (t != "" ? t : g)}' go.mod)
ifeq ($(GOTOOLCHAIN_PIN),)
$(error could not read the Go toolchain from go.mod; refusing to build the generators unpinned)
endif
TOOLENV := GOWORK=off GOTOOLCHAIN=$(GOTOOLCHAIN_PIN)

# The latest release tag: the published contract is what a breaking change
# breaks. Empty until the first release, which makes `breaking` a no-op.
BREAKING_AGAINST ?= $(shell git tag -l 'v*' --sort=v:refname | tail -1)

help:
	@echo "MetaCensus API contract. Run from the repository root."
	@echo ""
	@awk -F' — ' '/^## /{ sub(/^## /, ""); printf "  make %-14s %s\n", $$1, $$2 }' $(MAKEFILE_LIST)
	@echo ""
	@echo "  A fresh clone needs nothing first: gen and check install what they need."

## all — an alias for check
all: check

## tools — build the pinned code generators out of internal/tools
tools: $(BIN)/buf $(BIN)/protoc-gen-go

$(BIN)/buf: $(TOOLS_DIR)/go.mod $(TOOLS_DIR)/go.sum
	@echo "building buf from source (~1 min the first time)..."
	cd $(TOOLS_DIR) && $(TOOLENV) go build -o $(BIN)/buf github.com/bufbuild/buf/cmd/buf

$(BIN)/protoc-gen-go: $(TOOLS_DIR)/go.mod $(TOOLS_DIR)/go.sum
	cd $(TOOLS_DIR) && $(TOOLENV) go build -o $(BIN)/protoc-gen-go google.golang.org/protobuf/cmd/protoc-gen-go

## deps — reinstall the TypeScript toolchain from the lockfile
deps:
	cd $(TS_DIR) && npm ci

# The ts-proto plugin buf.gen.yaml invokes, as a prerequisite rather than as a
# step someone has to know to run first: `make gen` on a fresh clone used to
# fail inside buf with a missing-plugin path. Re-runs when the lockfile moves.
TS_PLUGIN := $(TS_DIR)/node_modules/.bin/protoc-gen-ts_proto

$(TS_PLUGIN): $(TS_DIR)/package-lock.json
	cd $(TS_DIR) && npm ci

## gen — regenerate Go and TypeScript from the .proto sources
gen: tools $(TS_PLUGIN)
	$(BUF) generate --template $(PROTO)/buf.gen.yaml
	go run ./$(GO_DIR)/cmd/routegen

## lint — buf's STANDARD rules
lint: $(BIN)/buf
	$(BUF) lint $(PROTO)

## format — rewrite the .proto sources canonically
format: $(BIN)/buf
	$(BUF) format -w $(PROTO)

## format-check — fail if the sources are not canonically formatted
format-check: $(BIN)/buf
	$(BUF) format --diff --exit-code $(PROTO)

## breaking — compare against BREAKING_AGAINST, skipping when it predates the contract
breaking: $(BIN)/buf
	@if [ -z '$(BREAKING_AGAINST)' ]; then \
		echo "nothing released yet; no published contract to break"; \
	elif git cat-file -e '$(BREAKING_AGAINST):$(PROTO)/buf.yaml' 2>/dev/null; then \
		$(BUF) breaking $(PROTO) \
			--against '.git#ref=$(BREAKING_AGAINST),subdir=$(PROTO)'; \
	else \
		echo "no $(PROTO) at $(BREAKING_AGAINST); nothing to compare against"; \
	fi

## test — the schema and route invariants, then the chi conformance module
#
# internal/chitest is a module of its own so chi stays out of the published
# go.mod, which means ./... above cannot see it and it needs its own line.
test:
	go test ./...
	cd $(CHITEST_DIR) && $(TOOLENV) go test ./...

## check — everything CI runs, minus the freshness diff
check: lint format-check test $(TS_PLUGIN)
	# -o /dev/null: cmd/routegen is a main package, so a plain build drops a
	# binary in the working directory.
	go build -o /dev/null ./... && go vet ./...
	cd $(TS_DIR) && npm run check
	cd $(TS_DIR) && npm test

## hooks — opt in to the pre-commit hook; unset core.hooksPath to opt out
hooks:
	git config core.hooksPath .githooks
	@echo "core.hooksPath set to .githooks"

## clean — remove generated output and built tools; `make gen` puts them back
clean:
	rm -rf $(GENERATED) $(BIN)

## generated-paths — print what `gen` writes; the pre-commit hook reads this
generated-paths:
	@echo $(GENERATED)

# ---------------------------------------------------------------------------
# Release
#
# Tags are minted here, never typed: scripts/version.sh validates semver and
# the target refuses a tag that already exists. Pushing the tag is the whole
# release — the Go module needs nothing else, and the workflow publishes npm.
#
# `set -e` and the empty check are load-bearing. Without them, a version.sh
# that errors out still leaves VERSION empty, and the recipe cheerfully tags
# and pushes `v` — which is precisely the junk tag metacensus/infra ended up
# with. The release workflow's tag filter would not match it, so it would sit
# there forever, silently.
# ---------------------------------------------------------------------------

VERSION ?=
TYPE    ?=
MESSAGE ?=

## release — tag and push a version; prompts, and refuses a dirty tree
release: scripts/version.sh
	@set -e; \
	VERSION=$$(./scripts/version.sh "$(VERSION)" "$(TYPE)"); \
	if [ -z "$$VERSION" ]; then \
		echo "Error: version.sh produced no version; refusing to tag"; \
		exit 1; \
	fi; \
	TAG="v$$VERSION"; \
	MSG=$$([ -n "$(MESSAGE)" ] && echo "$(MESSAGE)" || echo "Release $$VERSION"); \
	if git rev-parse "$$TAG" >/dev/null 2>&1; then \
		echo "Error: Tag $$TAG already exists"; \
		exit 1; \
	fi; \
	if [ -n "$$(git status --porcelain)" ]; then \
		echo "Error: working tree is dirty; commit or clean it before releasing"; \
		exit 1; \
	fi; \
	git fetch -q origin main; \
	if ! git merge-base --is-ancestor HEAD origin/main; then \
		echo "Error: HEAD is not an ancestor of origin/main."; \
		echo "Pushing the tag would carry the commit with it and publish a"; \
		echo "version that sits on no branch, which the proxy keeps serving."; \
		exit 1; \
	fi; \
	echo "About to tag and push $$TAG."; \
	echo "That publishes @metacensus/api@$$VERSION to public npm and"; \
	echo "github.com/metacensus/api@$$TAG to proxy.golang.org."; \
	echo "Neither can be withdrawn: npm unpublish is limited to 72 hours and"; \
	echo "the Go module proxy is an immutable cache. Deleting the tag is not"; \
	echo "enough — that version stays served."; \
	if [ "$(YES)" != "1" ]; then \
		if [ -t 0 ]; then \
			printf "Proceed? [y/N] "; read -r reply; \
			case "$$reply" in y|Y|yes|YES) ;; *) echo "Aborted."; exit 1;; esac; \
		else \
			echo "Refusing to release non-interactively; pass YES=1 if you mean it."; \
			exit 1; \
		fi; \
	fi; \
	git tag -a "$$TAG" -m "$$MSG" && \
	git push origin "$$TAG" && \
	echo "Released: $$TAG"

## release-major — release, bumping the major
release-major:
	@$(MAKE) release TYPE=major

## release-minor — release, bumping the minor
release-minor:
	@$(MAKE) release TYPE=minor

## release-patch — release, bumping the patch
release-patch:
	@$(MAKE) release TYPE=patch

## latest — print the most recent version tag
latest:
	@git tag -l "v*" | grep -E "^v[0-9]" | sort -V | tail -1

## list — print every version tag
list:
	@git tag -l "v*" | grep -E "^v[0-9]" | sort -V

## delete-tag — delete TAG=vX.Y.Z locally and on the remote
delete-tag:
	@if [ -z "$(TAG)" ]; then \
		echo "Usage: make delete-tag TAG=v1.2.3"; \
		exit 1; \
	fi; \
	git tag -d "$(TAG)" 2>/dev/null || true; \
	git push origin ":refs/tags/$(TAG)"
