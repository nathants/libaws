.PHONY: all libaws check libcheck-installed check-lint check-security check-cli-aliases check-lambda-iam test

all: libaws

libaws:
	CGO_ENABLED=0 go build -tags 'netgo osusergo purego'

check: check-lint check-security check-cli-aliases check-lambda-iam

libcheck-installed:
	@command -v libcheck >/dev/null || { echo 'libcheck not found; install it with: go install github.com/nathants/libcheck@latest' >&2; exit 1; }

check-lint: libcheck-installed
	@libcheck check

check-security: libcheck-installed
	@libcheck security

check-cli-aliases: libaws
	@python3 check_cli_aliases.py

check-lambda-iam:
	go test ./lib -run '^TestLambdaEnsureIAM' -count=1

test:
	bash restore_python_deps.sh
	bash test.sh
