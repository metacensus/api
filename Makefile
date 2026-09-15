# The MetaCensus API contract. Run from the repository root.
#
# Two modules. The root is the contract — the generated types, the manifest,
# the wire encoder and the server — and it requires exactly two things.
# routegen/ is the generator and everything that tests what it emits; it is
# never imported and never published, so it may require whatever it needs,
# which is why chi and buf live there. buf and protoc-gen-go are its `tool`
# dependencies; `go tool` only runs inside its own module, so the binaries are
# built into ./bin and run from the repository root, which is what every
# relative path in buf.gen.yaml is relative to.
#
# GOWORK=off and GOTOOLCHAIN are load-bearing, both lessons from
# metacensus/infra#52. A go.work anywhere above this checkout would resolve tool
# versions against the union of its members and quietly lift the pins; and the
# `go` directive is a floor, not a ceiling, so without GOTOOLCHAIN a developer
# on a newer Go builds the plugins against a newer stdlib. protoc-gen-go stamps
# its own version into every .pb.go, and gofmt's doc-comment handling has
# changed between Go releases, so either one lands as generated-code drift in a
# PR that never touched a .proto.

.PHONY: help all gen generated-paths lint format format-check breaking breaking-public test check clean deps hooks tools \
        release release-major release-minor release-patch latest list delete-tag

# `make` with no target lists the targets rather than running the whole suite,
# matching metacensus/infra. The listing is generated from the `## name — what
# it does` comments below, so a target and its description cannot drift; infra
# hand-writes its help text, which is the same information twice.
.DEFAULT_GOAL := help

GO_DIR   := go
TS_DIR   := ts
PROTO    := proto
ROUTEGEN := routegen
BIN      := $(CURDIR)/bin

BUF := $(BIN)/buf

# Every path `gen` writes, named once. `clean` removes exactly these and the
# pre-commit hook asks git about exactly these, so the three cannot disagree.
# go/server/routes_gen.go is the one generated file sharing a directory with
# hand-written source, which is why the list is of paths rather than of
# directories.
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

# The public surface answers "does this break what is deployed?" rather than
# "what was released?", because its callers are browsers holding a build of the
# SPA nobody can redeploy, and none of them consume a tag. See README.md,
# "Versioning the public surface".
PUBLIC_PROTO_DIR        := $(PROTO)/metacensus/public
# The same directory as buf sees it, i.e. relative to the module root.
PUBLIC_PACKAGE_PATH     := metacensus/public
PUBLIC_BREAKING_AGAINST ?= origin/main

help:
	@echo "MetaCensus API contract. Run from the repository root."
	@echo ""
	@awk -F' — ' '/^## /{ sub(/^## /, ""); printf "  make %-16s %s\n", $$1, $$2 }' $(MAKEFILE_LIST)
	@echo ""
	@echo "  A fresh clone needs nothing first: gen and check install what they need."

## all — an alias for check
all: check

## tools — build the pinned code generators out of routegen/
tools: $(BIN)/buf $(BIN)/protoc-gen-go

$(BIN)/buf: $(ROUTEGEN)/go.mod $(ROUTEGEN)/go.sum
	@echo "building buf from source (~1 min the first time)..."
	cd $(ROUTEGEN) && $(TOOLENV) go build -o $(BIN)/buf github.com/bufbuild/buf/cmd/buf

$(BIN)/protoc-gen-go: $(ROUTEGEN)/go.mod $(ROUTEGEN)/go.sum
	cd $(ROUTEGEN) && $(TOOLENV) go build -o $(BIN)/protoc-gen-go google.golang.org/protobuf/cmd/protoc-gen-go

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
	cd $(ROUTEGEN) && $(TOOLENV) go run .

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

## breaking-public — the public surface against what is deployed, not what is tagged
#
# `--path` scopes the comparison to the public package, so one buf module carries
# two breaking checks. **This is the one buf invocation that does not run from
# the repository root**, and it has to be: `--path` resolves against the input's
# context directory, and the `--against` input is the module rooted at $(PROTO)
# inside a git archive, so a root-relative path targets no files there and buf
# answers "no .proto files were targeted" instead of failing usefully. $(BUF) is
# absolute, so running from $(PROTO) is safe.
#
# The rule set is FILE for both checks because FILE is buf's strictest. What is
# breaking here and invisible to any schema differ — a narrowed length cap, a
# newly-required field — lives in comments and in the server, and is held by
# review and by README.md rather than by a flag that would only claim to.
#
# The baseline is echoed because it is a local remote-tracking ref: `make check`
# does not fetch, so a stale one would otherwise compare against the wrong
# commit, or skip, without saying so.
#
# An empty PUBLIC_BREAKING_AGAINST means "do not ask this question", and it is
# how a release build turns the check off: on a tag the comparison would run
# backwards, because a tagged commit can sit behind origin/main and anything
# added to the public surface after the tag would read as a deletion. That case
# is decided by whoever sets the variable, not by whether a ref happens to
# resolve — a skip that depends on a ref being absent is not a skip, it is a
# coincidence.
breaking-public: $(BIN)/buf
	@if [ -z '$(PUBLIC_BREAKING_AGAINST)' ]; then \
		echo "no baseline for the public surface; nothing deployed to break"; \
	elif ! git rev-parse --verify -q '$(PUBLIC_BREAKING_AGAINST)^{commit}' >/dev/null; then \
		echo "no $(PUBLIC_BREAKING_AGAINST) to compare against; skipping"; \
	else \
		echo "public surface against $(PUBLIC_BREAKING_AGAINST) @ $$(git log -1 --format='%h %cs' '$(PUBLIC_BREAKING_AGAINST)')"; \
		if [ -z "$$(git ls-tree -r --name-only '$(PUBLIC_BREAKING_AGAINST)' -- '$(PUBLIC_PROTO_DIR)')" ]; then \
			echo "  no public package there yet; nothing deployed to break"; \
		else \
			cd $(PROTO) && $(BUF) breaking . \
				--against '$(CURDIR)/.git#ref=$(PUBLIC_BREAKING_AGAINST),subdir=$(PROTO)' \
				--path '$(PUBLIC_PACKAGE_PATH)'; \
		fi; \
	fi

## test — the contract's own invariants, then the generator's
#
# routegen is a module of its own, so ./... above cannot see it and it needs
# its own line. Its tests are the generator's unit tests and the suites that
# exercise what it emits, including the chi conformance run.
test:
	go test ./...
	cd $(ROUTEGEN) && $(TOOLENV) go test ./...

## check — everything CI runs, minus the freshness diff
check: lint format-check breaking-public test $(TS_PLUGIN)
	go build ./... && go vet ./...
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
