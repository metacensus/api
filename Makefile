# The MetaCensus API contract. Run from the repository root.
#
# buf and protoc-gen-go are `tool` dependencies of internal/tools, a module of
# its own so their ~90 transitive requirements stay out of the published
# module's go.mod. `go tool` only runs inside its own module, so instead of
# running buf from there we build the binaries into ./bin and run them from the
# repository root. Every relative path in buf.gen.yaml is therefore relative to
# the root, which is also where they read most naturally.

.PHONY: all gen lint format format-check breaking test check clean deps hooks tools \
        release release-major release-minor release-patch latest list delete-tag

GO_DIR    := go
TS_DIR    := ts
PROTO     := proto
TOOLS_DIR := internal/tools
BIN       := $(CURDIR)/bin

BUF := $(BIN)/buf

BREAKING_AGAINST ?= origin/main

all: check

## tools — build the pinned code generators out of internal/tools
tools: $(BIN)/buf $(BIN)/protoc-gen-go

$(BIN)/buf: $(TOOLS_DIR)/go.mod $(TOOLS_DIR)/go.sum
	cd $(TOOLS_DIR) && go build -o $(BIN)/buf github.com/bufbuild/buf/cmd/buf

$(BIN)/protoc-gen-go: $(TOOLS_DIR)/go.mod $(TOOLS_DIR)/go.sum
	cd $(TOOLS_DIR) && go build -o $(BIN)/protoc-gen-go google.golang.org/protobuf/cmd/protoc-gen-go

## deps — install the TypeScript toolchain
deps:
	cd $(TS_DIR) && npm ci

## gen — regenerate Go and TypeScript from the .proto sources
gen: tools
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
	@if git cat-file -e '$(BREAKING_AGAINST):$(PROTO)/buf.yaml' 2>/dev/null; then \
		$(BUF) breaking $(PROTO) \
			--against '.git#ref=$(BREAKING_AGAINST),subdir=$(PROTO)'; \
	else \
		echo "no $(PROTO) at $(BREAKING_AGAINST); nothing to compare against"; \
	fi

## test — the schema and route invariants
test:
	go test ./...

## check — everything CI runs, minus the freshness diff
check: lint format-check test
	# -o /dev/null: cmd/routegen is a main package, so a plain build drops a
	# binary in the working directory.
	go build -o /dev/null ./... && go vet ./...
	cd $(TS_DIR) && npm run check

## hooks — opt in to the pre-commit hook; unset core.hooksPath to opt out
hooks:
	git config core.hooksPath .githooks
	@echo "core.hooksPath set to .githooks"

## clean — remove generated output and built tools; `make gen` puts them back
clean:
	rm -rf $(GO_DIR)/metacensus $(GO_DIR)/routes $(TS_DIR)/src $(BIN)

# ---------------------------------------------------------------------------
# Release
#
# Tags are minted here, never typed: scripts/version.sh validates semver and
# the target refuses a tag that already exists. Pushing the tag is the whole
# release — the Go module needs nothing else, and the workflow publishes npm.
# ---------------------------------------------------------------------------

VERSION ?=
TYPE    ?=
MESSAGE ?=

release: scripts/version.sh
	@VERSION=$$(./scripts/version.sh "$(VERSION)" "$(TYPE)"); \
	TAG="v$$VERSION"; \
	MSG=$$([ -n "$(MESSAGE)" ] && echo "$(MESSAGE)" || echo "Release $$VERSION"); \
	if git rev-parse "$$TAG" >/dev/null 2>&1; then \
		echo "Error: Tag $$TAG already exists"; \
		exit 1; \
	fi; \
	git tag -a "$$TAG" -m "$$MSG" && \
	git push origin "$$TAG" && \
	echo "Released: $$TAG"

release-major:
	@$(MAKE) release TYPE=major

release-minor:
	@$(MAKE) release TYPE=minor

release-patch:
	@$(MAKE) release TYPE=patch

latest:
	@git tag -l "v*" | grep -E "^v[0-9]" | sort -V | tail -1

list:
	@git tag -l "v*" | grep -E "^v[0-9]" | sort -V

delete-tag:
	@if [ -z "$(TAG)" ]; then \
		echo "Usage: make delete-tag TAG=v1.2.3"; \
		exit 1; \
	fi; \
	git tag -d "$(TAG)" 2>/dev/null || true; \
	git push origin ":refs/tags/$(TAG)"
