# The MetaCensus API contract. Run from `contract/`.
#
# buf runs from `contract/go`, because buf and protoc-gen-go are Go `tool`
# dependencies of that module and `go tool` only works inside its own module.
# Every relative path in buf.gen.yaml is therefore relative to `contract/go`.

.PHONY: all gen lint format format-check breaking golden test check clean deps

GO_DIR := go
TS_DIR := ts
PROTO  := ../proto
BUF    := go tool buf

BREAKING_AGAINST ?= origin/main

all: check

## deps — install the TypeScript toolchain
deps:
	cd $(TS_DIR) && npm ci

## gen — regenerate Go and TypeScript from the .proto sources
gen:
	cd $(GO_DIR) && $(BUF) generate --template $(PROTO)/buf.gen.yaml

## golden — regenerate the golden JSON and the TypeScript cross-check
golden: gen
	cd $(GO_DIR) && go test ./... -update

## lint — buf's STANDARD rules
lint:
	cd $(GO_DIR) && $(BUF) lint $(PROTO)

## format — rewrite the .proto sources canonically
format:
	cd $(GO_DIR) && $(BUF) format -w $(PROTO)

## format-check — fail if the sources are not canonically formatted
format-check:
	cd $(GO_DIR) && $(BUF) format --diff --exit-code $(PROTO)

## breaking — compare against BREAKING_AGAINST, skipping when it predates the contract
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

## check — everything CI runs, minus the freshness diff
check: lint format-check test
	cd $(GO_DIR) && go build ./... && go vet ./...
	cd $(TS_DIR) && npm run check

## clean — remove generated output; `make gen golden` puts it back
clean:
	rm -rf $(GO_DIR)/metacensus $(TS_DIR)/src $(TS_DIR)/test $(GO_DIR)/testdata
