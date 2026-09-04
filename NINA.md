# NINA.md

## Documentation

- Keep `readme.md` concise and consistent with its established reference style: state behavior, attributes and defaults, then show schema and an example.
- State general philosophy and tradeoffs once near the top. Feature sections should not repeat why libaws is opinionated, compare it with full-API tools, narrate implementation details, or include review history.
- Document externally relevant constraints without exhaustive internal safety proofs. Keep live-example links in the central examples list rather than repeating them in feature sections.

## Architecture and testing

- Build `infra.yaml` declarations around concrete project needs. Expose only the simple, opinionated subset of AWS behavior that we actually intend to use, add options only when a project needs them, and provide strong defaults for everything else. Do not mirror complete AWS service APIs; callers needing the full AWS configuration surface should use Terraform.
- Keep `cmd/` packages as thin CLI wrappers: parse arguments, translate them into `lib/` calls, and handle process I/O and fatal errors.
- Put behavior that should be unit- or integration-tested in `lib/`, with its tests under `lib/`. Do not add command-package tests to compensate for testable logic in `cmd/`; move that logic into `lib/` instead.
- The CLI wrappers are exercised through `examples/`, which invoke the built `libaws` command directly.
- Keep example dependencies exact and reproducible with the applicable committed `go.mod`/`go.sum`, `uv.lock`/exported requirements, or `pnpm-lock.yaml`; pin container images by digest. Safe-check each dependency root independently and use only dependencies and images that are at least 14 days old. The temporary exception is `golang.org/x/crypto v0.56.0`, accepted before 2026-09-16 because it fixes reachable SSH vulnerabilities GO-2026-6354 and GO-2026-6355. Safe's module-wide GO-2026-5932 finding is accepted because libaws imports `x/crypto/ssh`, not the deprecated `openpgp` package; `govulncheck` must remain clear.
- Every behavior change must have a permanent integration test or example that exercises the actual behavior against real AWS. Unit tests supplement rather than replace this acceptance coverage.
- Every `infra.yaml` schema or semantic change must be documented in `readme.md` and exercised by a checked-in `examples/` infrastructure declaration and its live AWS test.
- Live DNS/domain examples use `LIBAWS_TEST_DOMAIN`, which names a permanent, publicly delegated Route53 zone. Tests automatically ensure and preserve its regional wildcard ACM certificate, DNS validation record, and untagged, unmapped `acm-fixture.${LIBAWS_TEST_DOMAIN}` API Gateway custom domain. Tests may create and remove only unique child resources.
- `test.sh` must invoke every repository test and directly runs Python examples through `uv run --locked`, so `bash test.sh` is supported. All Go `_test.go` files live under `lib/`, where `test.sh` compiles and runs each file in isolation with `lib/lib_test.go` and all non-test library sources. Shared test helpers needed by more than one test file belong in `lib/lib_test.go`.
