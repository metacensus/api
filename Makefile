# The MetaCensus API contract. Run from the repository root; see README.md
# for the two-module layout and the GOWORK/GOTOOLCHAIN pinning.

.PHONY: help all gen generated-paths lint format format-check breaking breaking-public test check clean deps hooks tools \
        release release-major release-minor release-patch latest list delete-tag

# Lists targets (from the `## name — what it does` comments below) instead of
# running the whole suite; see `help`.
.DEFAULT_GOAL := help

GO_DIR   := go
TS_DIR   := ts
PROTO    := proto
ROUTEGEN := routegen
BIN      := $(CURDIR)/bin

BUF := $(BIN)/buf

# Every path `gen` writes, named once so `clean` and the pre-commit hook agree.
# Paths, not directories: go/server holds both generated and hand-written code.
GENERATED := $(GO_DIR)/metacensus $(GO_DIR)/routes $(GO_DIR)/server/routes_gen.go $(TS_DIR)/src

# Read out of go.mod so this can't drift from what CI's setup-go uses.
# GOTOOLCHAIN= with an empty value is silently accepted, so guard and fail
# loudly rather than build unpinned.
GOTOOLCHAIN_PIN ?= $(shell awk '/^toolchain /{t=$$2} /^go /{if (g == "") g = "go" $$2} END{print (t != "" ? t : g)}' go.mod)
ifeq ($(GOTOOLCHAIN_PIN),)
$(error could not read the Go toolchain from go.mod; refusing to build the generators unpinned)
endif
TOOLENV := GOWORK=off GOTOOLCHAIN=$(GOTOOLCHAIN_PIN)

# Latest release tag; empty (no releases yet) makes `breaking` a no-op.
BREAKING_AGAINST ?= $(shell git tag -l 'v*' --sort=v:refname | tail -1)

# The public surface; see README.md, "Versioning the public surface".
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

# A prerequisite so a fresh clone's `make gen` doesn't fail on a missing plugin.
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
# Must run from $(PROTO): `--path` resolves against the input's context dir,
# and a root-relative path against the git-archive `--against` input matches
# nothing there. $(BUF) is absolute, so `cd`ing first is safe.
#
# Empty PUBLIC_BREAKING_AGAINST means "skip" — how a release build turns this
# off, since on a tag the comparison would otherwise run backwards.
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
# routegen is its own module, so ./... above misses it; run separately.
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
# `set -e` and the empty-VERSION check matter: without them a failing
# version.sh still tags and pushes `v` — the junk tag metacensus/infra shipped.
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
