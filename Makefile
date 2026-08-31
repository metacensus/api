# The MetaCensus API contract.
#
# Everything here runs from `contract/`. The one thing worth knowing before
# reading further: buf is invoked from `contract/go`, not from `contract/proto`
# where its config lives, because buf and protoc-gen-go are Go 1.24 `tool`
# dependencies of `contract/go/go.mod` and `go tool` only works inside its own
# module. That is why every path in buf.gen.yaml is relative to `contract/go`.
#
# Nothing here installs anything globally. `go tool` builds the pinned buf and
# protoc-gen-go from the module's own dependency graph; ts-proto comes from
# contract/ts/node_modules. There is no `brew install buf`, no `go install`, and
# no version that can drift between a laptop and CI.

.PHONY: all gen lint format format-check breaking golden test check clean deps

GO_DIR   := go
TS_DIR   := ts
PROTO    := ../proto
BUF      := go tool buf

# The branch a PR is measured against for breaking-change detection. Override
# on a release branch: `make breaking BREAKING_AGAINST=origin/release-1.x`.
BREAKING_AGAINST ?= origin/main

all: check

## deps — install the TypeScript toolchain (the Go one needs no install step)
deps:
	cd $(TS_DIR) && npm ci

## gen — regenerate Go and TypeScript from the .proto sources
gen:
	cd $(GO_DIR) && $(BUF) generate --template $(PROTO)/buf.gen.yaml

## golden — regenerate the golden JSON and the TypeScript cross-check
##
## Depends on gen: the cross-check imports the generated interfaces, so
## regenerating goldens against stale types would prove nothing.
golden: gen
	cd $(GO_DIR) && go test ./... -update

## lint — buf's STANDARD rules, with the exceptions the enum convention needs
lint:
	cd $(GO_DIR) && $(BUF) lint $(PROTO)

## format — rewrite the .proto sources in buf's canonical formatting
format:
	cd $(GO_DIR) && $(BUF) format -w $(PROTO)

## format-check — fail if the sources are not canonically formatted
format-check:
	cd $(GO_DIR) && $(BUF) format --diff --exit-code $(PROTO)

## breaking — compare against BREAKING_AGAINST (default origin/main)
##
## `use: FILE` in buf.yaml is the strictest setting: it treats a field moving
## between files as breaking, not just a field changing meaning. That is right
## for a contract whose file split is explicitly a reversible code-layout
## choice — if someone moves a message between files, this makes them say so.
##
## Skips itself when the reference has no contract/proto yet, which is the case
## for the commit that introduces it.
breaking:
	@if git cat-file -e '$(BREAKING_AGAINST):contract/proto/buf.yaml' 2>/dev/null; then \
		cd $(GO_DIR) && $(BUF) breaking $(PROTO) \
			--against '../../.git#ref=$(BREAKING_AGAINST),subdir=contract/proto'; \
	else \
		echo "no contract/proto at $(BREAKING_AGAINST); nothing to compare against"; \
	fi

## test — the Go golden and wire-shape suites
test:
	cd $(GO_DIR) && go test ./...

## check — everything CI runs, minus the freshness diff (which needs git)
check: lint format-check test
	cd $(GO_DIR) && go build ./... && go vet ./...
	cd $(TS_DIR) && npm run check

## clean — remove generated output. `make gen golden` puts it all back.
clean:
	rm -rf $(GO_DIR)/metacensus $(TS_DIR)/src $(TS_DIR)/test $(GO_DIR)/testdata
