#!/usr/bin/env python3
"""Check the built CLI's effective option names for collisions."""

from pathlib import Path
import re
import subprocess


CLI = str(Path(__file__).resolve().parent / "libaws")


def check():
    root = subprocess.run([CLI, "--help"], capture_output=True, text=True, timeout=30)
    assert root.returncode == 1 and not root.stderr, root
    commands = [line.split()[0] for line in root.stdout.splitlines() if line.strip()]
    assert commands and len(commands) == len(set(commands)), commands
    assert all(re.fullmatch(r"[a-z0-9-]+", command) for command in commands), commands
    conflicts = []
    for command in commands:
        result = subprocess.run([CLI, command, "--help"], capture_output=True, text=True, timeout=30)
        assert result.returncode == 0 and not result.stderr, result
        _, heading, options = result.stdout.partition("\nOptions:\n")
        assert heading, result
        seen = set()
        for line in options.splitlines():
            if not line.startswith("  --"):
                continue
            declaration = re.split(r"\s{2,}|\s+\[", line.strip(), maxsplit=1)[0]
            flags = set()
            for spelling in declaration.split(", "):
                flag = spelling.split()[0]
                assert flag.startswith("-"), line
                if flag in seen:
                    conflicts.append(f"{command}: duplicate option {flag}")
                seen.add(flag)
                flags.add(flag)
            if "-p" in flags and "--preview" not in flags:
                conflicts.append(f"{command}: -p is reserved for --preview: {declaration}")
            if "--preview" in flags and "-p" not in flags:
                conflicts.append(f"{command}: --preview must keep the -p alias: {declaration}")
        assert {"--help", "-h"} <= seen, result
    if conflicts:
        raise SystemExit("\n".join(conflicts))
    print(f"checked option aliases for {len(commands)} commands")


if __name__ == "__main__":
    check()
