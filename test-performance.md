# Test-suite measurements

Read this when evaluating test-runner concurrency or comparing execution locations. These are exploratory observations, not a frozen benchmark or a performance guarantee.

## Local parallelization, 2026-09-05

| Run | Test files/examples | Workers | Full `test.sh` wall time | Result |
| --- | ---: | ---: | ---: | --- |
| Retained serial baseline, 2026-09-04 | 72 | 1 | 100m06s | passed |
| Parallel acceptance, 2026-09-05 | 75 | 4 | 34m20s | passed |

Observed speedup: **2.92×**, saving **65m46s** per full run. Checks, build, account-exclusive tests, test startup and cleanup are included. No baseline tests were removed; exact-set inventory tests, runner tests and the coexistence example were added.

Both runs used the same local WSL2 machine (8 logical CPUs, approximately 32 GiB RAM), rootless Docker, and AWS resources in `ap-southeast-1`. The baseline predates a dependency refresh, so the result does not isolate parallelization perfectly. Per-test times can increase under concurrency; aggregate task time rose from 5,992s to 7,200s while wall time fell. The exclusive S3/account-drift and SES tasks took 276s in the accepted parallel run.

The first exploratory parallel run took 36m57s but failed four Docker setup tests. Those failures were fixed before the accepted full rerun; the failed run is not used as acceptance or the speedup result.

Retained evidence:

- Serial baseline: Nina run `home_nathants_repos_libaws_dfc3c2835feed1dd_20260829T025310Z_rdVuV`, Shell `ccfaf41dac07eb734e159257250b5db6`, exit 0, 2026-09-04 17:37:26–19:17:32 UTC. Per-test times were recovered from timestamped test markers.
- Parallel acceptance: Nina run `home_nathants_repos_libaws_dfc3c2835feed1dd_20260905T134515Z_rLMVs`, Shell `3474a6439e584540436099b0bdc4031f`, exit 0. Logs and `timings.tsv`: `/tmp/libaws-tests-_5cy8ixn`. The run's private scratch directory retains `final-suite-source.json`, `baseline-source.json`, `baseline-timings.tsv`, and `test-speedup-per-file.tsv`.
- The Python interpreter restore portability fix was made afterward. Its focused regression, isolated test-file run, `make check`, and fresh Debian live-example run passed; the full-suite timing above was not rerun for that setup-only fix.

## EC2 location experiment

A clean official Debian 13/Trixie image (`ami-0392bbbc524789518`) ran on a `t3.small` Spot instance in `ap-southeast-1`: 2 vCPUs, 2 GiB RAM, encrypted delete-on-termination gp3 storage. Provisioning used `libaws ec2-*`; the working S3/EC2 example supplied the pattern for a dedicated VPC, unprivileged init with explicit `sudo`, S3 artifacts and automatic poweroff. No inbound access was needed for the successful probes.

Read-only probe medians, one warmup plus five timed repetitions per command, identical binary/script, one worker and `GOMAXPROCS=2`:

| Command | Local | EC2 |
| --- | ---: | ---: |
| `aws-account` | 0.536s | 0.047s |
| `lambda-ls` | 0.557s | 0.081s |
| `s3-ls` | 0.551s | 0.061s |
| `infra-ls --infraset` for an absent set | 7.602s | 3.925s |

The checked-in `examples/misc/infrasets` live example then passed in **163.3s locally versus 94.8s on EC2**: **1.72×** observed speedup. These isolated trials did not overlap any other live suite. Binary, example, dependency locks, Python 3.14.6, uv 0.11.32, virtualenv 21.7.4, outer concurrency 1, inner concurrency 2 and `GOMAXPROCS=2` matched. Dependencies and virtualenv seed caches were prepared before timing. Hardware, OS libraries, static credentials versus an instance role, and normal AWS propagation variability were not identical.

Successful guest software setup took **17.1s**; requesting the guest through reaching the timed trial took about **60s**, including boot/setup. Full experiment debugging, retries, artifact transfer and cleanup are separate from workload time. Initial SSH attempts failed, Python-copy preflights exposed a restore-script portability bug, and the first mutating attempt exposed a missing API-tagging permission in the temporary role. Failed attempts are not counted as successful timing samples.

The guest used read-only discovery plus narrowly scoped temporary write permissions for the live example; no long-lived credentials were copied. Permanent DNS/ACM fixtures were outside its write scope. All owned guests, disks, VPC/network resources, IAM role/profile, keypair and artifact bucket were removed. Provisioned resources stayed below the experiment's $1 budget by runtime/rate estimates; this is not an invoice measurement.

This is evidence that regional execution helps an API-heavy workload, **not a measured full-suite EC2 speedup**. A full suite on a 2 GiB guest may also encounter compilation/container CPU or memory bottlenecks. Do not multiply the local suite and example speedups to predict a full-suite result.

The same retained run's `scratch/ec2-location/` contains the probe samples, payload hashes, local/remote live logs, `live-comparison.json`, and cleanup verification. Keep its private keys and short-lived URL artifacts private; they are not benchmark inputs to publish.
