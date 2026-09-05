#!/usr/bin/env bash
set -euo pipefail

cd "$(dirname "$0")"

for required_command in uv python3; do
    if ! command -v "$required_command" >/dev/null 2>&1; then
        printf 'error: required command not found: %s\n' "$required_command" >&2
        exit 1
    fi
done

python_link=.venv/bin/python
if [[ -x "$python_link" && ! -L "$python_link" ]]; then
    uv sync --locked
else
    rm -rf -- .venv
    # The interpreter is copied below, so pyvenv.cfg must point to the real
    # installation, not a launcher symlink directory such as ~/.local/bin.
    uv sync --locked --python "$(readlink -f -- "$(command -v python3)")"
fi

python_copy=.venv/bin/.python-copy
if [[ -L "$python_link" ]]; then
    python_source=$(readlink -f -- "$python_link")
    # uv may write a stable managed-Python alias as home, containing only a
    # "python" symlink. Once copied, python3 needs the actual stdlib location.
    python3 - "$python_source" <<'PY'
from pathlib import Path
import sys

config = Path('.venv/pyvenv.cfg')
home = str(Path(sys.argv[1]).parent)
config.write_text(''.join(
    f'home = {home}\n' if line.startswith('home = ') else line
    for line in config.read_text().splitlines(keepends=True)
))
PY
    cp --preserve=mode,timestamps -- "$python_source" "$python_copy"
    mv -f -- "$python_copy" "$python_link"
fi

# A symlink from python3 back to the copied python can make standalone CPython
# mistake this venv for its base installation. Materialize those aliases too.
for python_alias in .venv/bin/python*; do
    if [[ -L "$python_alias" && "$python_alias" -ef "$python_link" ]]; then
        ln -- "$python_link" "$python_copy"
        mv -f -- "$python_copy" "$python_alias"
    fi
done

if [[ -L "$python_link" || ! -x "$python_link" ]]; then
    echo "failed to materialize the virtual-environment interpreter" >&2
    exit 1
fi
.venv/bin/python3 -c 'import encodings, ssl'
