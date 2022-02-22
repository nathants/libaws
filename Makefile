.PHONY: test cli-aws check check-static check-ineff check-err check-vet test-lib check-bodyclose check-nargs check-fmt check-hasdefault

all: cli-aws

cli-aws:
	CGO_ENABLED=0 go build -ldflags='-s -w' -tags 'netgo osusergo'

check: check-deps check-static check-ineff check-err check-vet check-lint check-bodyclose check-nargs check-fmt check-hasdefault

check-deps:
	@which staticcheck >/dev/null || (cd ~ && go get -u honnef.co/go/tools/cmd/staticcheck)
	@which golint      >/dev/null || (cd ~ && go get -u golang.org/x/lint/golint)
	@which ineffassign >/dev/null || (cd ~ && go get -u github.com/gordonklaus/ineffassign)
	@which errcheck    >/dev/null || (cd ~ && go get -u github.com/kisielk/errcheck)
	@which bodyclose   >/dev/null || (cd ~ && go get -u github.com/timakin/bodyclose)
	@which nargs       >/dev/null || (cd ~ && go get -u github.com/alexkohler/nargs/cmd/nargs)
	@which go-hasdefault >/dev/null || (cd ~ && go get -u github.com/nathants/go-hasdefault)

check-hasdefault: check-deps
	@go-hasdefault $(shell find -type f -name "*.go") || true

check-fmt: check-deps
	@go fmt ./... >/dev/null

check-nargs: check-deps
	@nargs ./...

check-bodyclose: check-deps
	@go vet -vettool=$(shell which bodyclose) ./...

check-lint: check-deps
	@golint ./... | grep -v -e unexported -e "should be" || true

check-static: check-deps
	@staticcheck ./...

check-ineff: check-deps
	@ineffassign ./*

check-err: check-deps
	@errcheck ./...

check-vet: check-deps
	@go vet ./...

test:
	go test -v lib/*.go
