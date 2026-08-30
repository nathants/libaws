.PHONY: test libaws check check-deps check-static check-ineff check-err check-vet test-lib check-bodyclose check-lint check-nargs check-fmt check-hasdefault check-hasdefer check-govulncheck

all: libaws

libaws:
	CGO_ENABLED=0 go build -tags 'netgo osusergo purego'



check: check-deps check-static check-ineff check-err check-vet check-bodyclose check-lint check-nargs check-fmt check-hasdefault check-hasdefer check-govulncheck

# body-close

check-deps:
	@for tool in staticcheck golint ineffassign errcheck bodyclose nargs go-hasdefault go-hasdefer govulncheck; do \
		command -v "$$tool" >/dev/null || { echo "missing required check tool: $$tool" >&2; exit 1; }; \
	done

check-govulncheck: check-deps
	@govulncheck ./...

check-hasdefault: check-deps
	@go-hasdefault $(shell find -type f -name "*.go") || true

check-hasdefer: check-deps
	@go-hasdefer $(shell find -type f -name "*.go") || true

check-fmt: check-deps
	@files="$$(find . -type f -name '*.go' -print0 | xargs -0 -r gofmt -l)"; \
		if [ -n "$$files" ]; then printf 'unformatted Go files:\n%s\n' "$$files" >&2; exit 1; fi

check-nargs: check-deps
	@nargs ./...

check-bodyclose: check-deps
	@go vet -vettool=$(shell which bodyclose) ./...

check-lint: check-deps
	@golint ./... | grep -v -e unexported -e "should be" || true

check-static: check-deps
	@staticcheck ./...

check-ineff: check-deps
	@ineffassign ./...

check-err: check-deps
	@errcheck ./...

check-vet: check-deps
	@go vet ./...

test:
	bash restore_python_deps.sh
	uv run --locked bash test.sh
