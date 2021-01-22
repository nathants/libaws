.PHONY: cli_aws check check-static check-ineff check-err check-vet test-lib check-bodyclose check-nargs check-fmt check-shadow

all: cli_aws

cli_aws:
	go build -o cli_aws cmd/cli_aws/main.go

check: check-deps check-static check-ineff check-err check-vet check-lint check-bodyclose check-nargs check-fmt check-shadow

check-deps:
	@which staticcheck >/dev/null || (cd ~ && go get -u github.com/dominikh/go-tools/cmd/staticcheck)
	@which golint      >/dev/null || (cd ~ && go get -u golang.org/x/lint/golint)
	@which ineffassign >/dev/null || (cd ~ && go get -u github.com/gordonklaus/ineffassign)
	@which errcheck    >/dev/null || (cd ~ && go get -u github.com/kisielk/errcheck)
	@which bodyclose   >/dev/null || (cd ~ && go get -u github.com/timakin/bodyclose)
	@which nargs       >/dev/null || (cd ~ && go get -u github.com/alexkohler/nargs/cmd/nargs)
	@which shadow      >/dev/null || (cd ~ && go get -u golang.org/x/tools/go/analysis/passes/shadow/cmd/shadow)

check-shadow: check-deps
	@go vet -vettool=$(shell which shadow) ./...

check-fmt: check-deps
	@go fmt ./...

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
