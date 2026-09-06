#!/bin/bash
set -eou pipefail
cd "$(dirname "$0")"
exec 9>"$(git rev-parse --git-path libaws-tests.lock)"
flock -n 9 || { echo "another suite is running in this checkout" >&2; exit 1; }
started=$SECONDS
trap 'status=$?; printf "TOTAL suite: %ss status=%s\n" "$((SECONDS - started))" "$status"' EXIT

check_uv_export() {
    local requirements=$1
    shift
    local dir generated status
    dir=$(dirname "$requirements")
    generated=$(mktemp)
    status=0
    (cd "$dir" && uv lock --check && uv export --locked "$@" --no-emit-project --no-header --no-annotate --no-hashes) > "$generated" || status=$?
    if ((status == 0)); then
        diff -u "$requirements" "$generated" || status=$?
    fi
    rm -f "$generated"
    return "$status"
}

uv lock --check
check_uv_export build-requirements.txt --only-group build

while IFS= read -r requirements; do
    printf '\n=== %s ===\n' "$requirements"
    check_uv_export "$requirements" --no-dev
    check_uv_export "$(dirname "$requirements")/build-requirements.txt" --only-group build
done < <(find examples -type d \( -name .venv -o -name node_modules \) -prune -o -type f -name requirements.txt -print | sort)

make check
make

: "${LIBAWS_TEST_ACCOUNT:?set LIBAWS_TEST_ACCOUNT to the authorized scratch account}"
: "${LIBAWS_TEST_DOMAIN:?set LIBAWS_TEST_DOMAIN to the delegated test zone}"
[[ $(./libaws aws-account) == "$LIBAWS_TEST_ACCOUNT" ]] || { echo "AWS account guard failed" >&2; exit 1; }
uv run --locked python -u test_runner.py
