# Infrastructure inventory

Read this before changing inventory discovery, trigger reconstruction, or error handling.

## Selection and errors

- `InfraListSet` selects an exact nonempty `libaws.infraset` tag, independent of resource names. A service may require account-wide names or membership metadata to discover that tag; configuration reads must follow selection.
- Membership tags are a per-resource snapshot, not an account-wide transaction. Paginate them fully and reuse the verified result when assigning ownership. IAM may truncate tag results below `MaxItems`; follow `IsTruncated` and reject missing or nonadvancing cursors.
- A resource deleted before membership can be read is omitted. Access errors remain errors. After membership establishes ownership, disappearance and other configuration errors remain errors rather than becoming successful empty inventory.
- The top-level inventory owns the trigger channel. Join every worker and drain producers even if Lambda discovery or configuration fails. Worker recovery must report an error, not call `logRecover`, which re-panics. Unsupported AWS configuration should be rejected explicitly before dereferencing optional fields.
- S3 inventory represents no lifecycle or one enabled, bucket-wide expiration in positive days. Rule IDs and an empty `Filter` prefix do not change behavior. Creation and convergence use `Filter`, not the deprecated lifecycle `Prefix` field. Re-ensure declarations to converge obsolete single-rule configurations; inventory rejects obsolete or unrepresentable lifecycles rather than inventing a TTL.

## Relationships

- API triggers require the expected HTTP/WebSocket protocol, an unqualified Lambda identity in the caller's account/partition and current region, and exactly one `AWS_PROXY` integration to that identity. Read every integration page. APIs without a verified relationship remain standalone resources; never hide them merely because their names match a Lambda.
- Scoped EventBridge, alarm, S3, and API trigger roots must belong to the selected set. Arbitrary untagged or cross-set inbound triggers are outside this owned-set view.
- SES rules lack ownership tags. Derive candidates from exact receipt-rule source ARNs in the selected function's invocation grants, then verify each rule's actual Lambda action and recipient. Wildcard or otherwise unscopable SES permissions are errors.
- Shared DNS zones and ACM certificates are references, not owned resources. Only same-set custom domains supply mapping candidates, and scoped DNS reads use their recorded zone and exact record name.
- Tag-scoped absence is not deletion proof. Cleanup tests must also check unique resource names/IDs through service APIs, including roots that could lose their ownership tag.

## Validation

- Focused checks: `bash test_one.sh infra_list` and `bash test_one.sh infra_s3_race`.
- `examples/misc/infrasets` provisions differently named, overlapping-name sets and invokes `lib/infra_list_live_test.go`. Its live cases cover malformed lifecycle configuration, API target drift, paginated IAM profile membership, queue-deletion identity errors, queue deletion after selection, and retagging between membership and configuration reads. SDK middleware controls page sizes and mutation timing; responses still come from AWS.
- SQS acknowledges deletion before it completes (up to 60 seconds). Cleanup acceptance waits for independent queue-name absence with a bounded deadline; ownership-tag disappearance is insufficient.
- S3 lifecycle and encryption reads can alternate between new and old configuration after a write. The lifecycle acceptance test observes the exact SDK response consumed by inventory and asserts its representation/error on every observation until the intended state appears; no failed assertion is retried. It uses a separate empty bucket, removed rather than restored. The encryption fixture uses `waitLiveAWSFixtureStable` for direct-read convergence before its account-wide assertions.
- Run live acceptance through `bash test.sh`, with `LIBAWS_TEST_ACCOUNT` and `LIBAWS_TEST_DOMAIN` armed for the authorized scratch account. Do not overlap live suites. The example owns and removes the fixtures; the Go test skips before provider access unless given its unique fixture ID.
