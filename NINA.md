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
- Docker is always rootless. Use the user's Docker daemon directly, never `sudo docker` or the system Docker service. Tests that replace `DOCKER_CONFIG` must preserve the active daemon endpoint first.
- Keep example dependencies exact and reproducible with the applicable committed `go.mod`/`go.sum`, `uv.lock`/exported requirements, or `pnpm-lock.yaml`; pin container images by digest. Safe-check each dependency root independently and use only dependencies and images that are at least 14 days old. The temporary exception is `golang.org/x/crypto v0.56.0`, accepted before 2026-09-16 because it fixes reachable SSH vulnerabilities GO-2026-6354 and GO-2026-6355. Safe's module-wide GO-2026-5932 finding is accepted because libaws imports `x/crypto/ssh`, not the deprecated `openpgp` package; `govulncheck` must remain clear.
- Every behavior change must have a permanent integration test or example that exercises the actual behavior against real AWS. Unit tests supplement rather than replace this acceptance coverage.
- Every `infra.yaml` schema or semantic change must be documented in `readme.md` and exercised by a checked-in `examples/` infrastructure declaration and its live AWS test.
- Live DNS/domain examples use `LIBAWS_TEST_DOMAIN`, which names a permanent, publicly delegated Route53 zone. Tests automatically ensure and preserve its regional wildcard ACM certificate, DNS validation record, and untagged, unmapped `acm-fixture.${LIBAWS_TEST_DOMAIN}` API Gateway custom domain. Tests may create and remove only unique child resources.
- `bash test.sh` runs checks/build, then `test_runner.py` discovers every example and eligible Go test file. Python examples use `uv run --locked`; `test_one.sh` compiles each Go file in isolation with `lib/lib_test.go` and the build-eligible non-test sources, using a unique binary path. All Go `_test.go` files live under `lib/`; shared helpers belong in `lib/lib_test.go`.
- Live examples use unique names and `infra-ls --infraset NAME` for exact owned-set inventory, not the positional substring filter. Scoped inventory may enumerate membership tags but must not hydrate unrelated resources; global inventory retains its existing semantics. Tag-based absence is not proof that an untagged resource was deleted.
- `LIBAWS_TEST_JOBS` controls the bounded pool (default 4). `lib/s3_test.go` deliberately validates account-wide S3 drift and the live SES example changes the single active receipt-rule set; the runner keeps them exclusive. Do not overlap live suites in one scratch account. Per-test logs and `timings.tsv` are retained in the printed private temporary directory.
- Read [test-performance.md](test-performance.md) when evaluating runner concurrency or execution location; it records the measurement conditions and retained timing evidence.
- EC2 `--init` runs as the image's login user, not root; use explicit `sudo` for privileged guest setup. `examples/complex/s3-ec2` demonstrates small Debian Spot guests with S3 input/output and automatic poweroff. `examples/misc/ec2` covers CLI VPC/explicit-subnet selection against AWS's stored Spot Fleet request, using guests without ingress or instance credentials.
- `restore_python_deps.sh` materializes the virtualenv interpreter and its aliases. Keep `pyvenv.cfg` pointing to the real base installation; uv-managed Python aliases and a `python3 -> python` symlink into a copied interpreter can otherwise lose the standard library.
