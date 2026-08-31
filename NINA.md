# NINA.md

## Architecture and testing

- Keep `cmd/` packages as thin CLI wrappers: parse arguments, translate them into `lib/` calls, and handle process I/O and fatal errors.
- Put behavior that should be unit- or integration-tested in `lib/`, with its tests under `lib/`. Do not add command-package tests to compensate for testable logic in `cmd/`; move that logic into `lib/` instead.
- The CLI wrappers are exercised through `examples/`, which invoke the built `libaws` command directly.
- `test.sh` must invoke every repository test and directly runs Python examples through `uv run --locked`, so `bash test.sh` is supported. All Go `_test.go` files live under `lib/`, where `test.sh` compiles and runs each file in isolation with `lib/lib_test.go` and all non-test library sources. Shared test helpers needed by more than one test file belong in `lib/lib_test.go`.
