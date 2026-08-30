#!/usr/bin/env bash
set -euo pipefail

cd "$(dirname "$0")"

command -v uv >/dev/null
command -v python3 >/dev/null

python_link=.venv/bin/python
if [[ -x "$python_link" && ! -L "$python_link" ]]; then
    uv sync --locked
else
    rm -rf -- .venv
    uv sync --locked --python "$(command -v python3)"
fi

python_copy=.venv/bin/.python-copy
if [[ -L "$python_link" ]]; then
    python_source=$(readlink -f -- "$python_link")
    cp --preserve=mode,timestamps -- "$python_source" "$python_copy"
    mv -f -- "$python_copy" "$python_link"
fi

if [[ -L "$python_link" || ! -x "$python_link" ]]; then
    echo "failed to materialize the virtual-environment interpreter" >&2
    exit 1
fi
