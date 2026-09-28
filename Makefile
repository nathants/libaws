.PHONY: all libaws check libcheck-installed node-deps check-lint check-security check-cli-aliases check-lambda-iam test

all: libaws

libaws:
	CGO_ENABLED=0 go build -tags 'netgo osusergo purego'

check: check-lint check-security check-cli-aliases check-lambda-iam

libcheck-installed:
	@command -v libcheck >/dev/null || { echo 'libcheck not found; install it with: go install github.com/nathants/libcheck@latest' >&2; exit 1; }

# libcheck type-checks the Docker example handlers with their locked TypeScript
# and SDK packages. The DynamoDB example pins pnpm for its image build; the
# installed pnpm reads the same lockfile here.
node-deps:
	@for dir in examples/simple/docker examples/simple/docker/dynamodb; do \
		(cd $$dir && pnpm_config_pm_on_fail=ignore pnpm install --frozen-lockfile --silent) || exit 1; \
	done

check-lint: libcheck-installed node-deps
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
