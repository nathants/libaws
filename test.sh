#!/bin/bash
set -eou pipefail

check_uv_export() {
    local requirements=$1
    local dir generated status
    dir=$(dirname "$requirements")
    generated=$(mktemp)
    status=0
    (cd "$dir" && uv lock --check && uv export --locked --no-dev --no-emit-project --no-header --no-annotate --no-hashes) > "$generated" || status=$?
    if ((status == 0)); then
        diff -u "$requirements" "$generated" || status=$?
    fi
    rm -f "$generated"
    return "$status"
}

uv lock --check

while IFS= read -r requirements; do
    printf '\n=== %s ===\n' "$requirements"
    check_uv_export "$requirements"
done < <(find examples -type d \( -name .venv -o -name node_modules \) -prune -o -type f -name requirements.txt -print | sort)

make check
make
for dir in examples/simple/python examples/simple/go examples/simple/docker examples/complex examples/misc; do
    (
        cd "$dir"
        for name in *; do
            printf '\n=== %s/%s/test.py ===\n' "$dir" "$name"
            (cd "$name" && timeout 1800 uv run --locked python -u test.py)
        done
    )
done
(
    cd lib
    nontest=$(ls *.go | grep -v _test.go)
    for test in $(ls *_test.go | grep -v lib_test.go); do
        printf '\n=== lib/%s ===\n' "$test"
        go test lib_test.go $nontest $test -o /tmp/libaws.test -c
        timeout 600 /tmp/libaws.test -test.v -test.failfast
    done
)
