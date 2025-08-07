# TesseraCT Configuration

## Append lifecycle

### Checkpoint Interval

The `checkpoint_interval` flag controls the interval duration between checkpoint
publishing events. Due to constraints imposed by underlying storage systems,
this interval has a lower limit. Tessera ensures that the configured interval
is larger than that lower limit during the initialization process.

| Backend | Minimum Permitted Checkpoint Interval (ms) |
| ------- | ------------------------------------------ |
| AWS     | 1000                                       |
| GCP     | 1200                                       |

Note that when used in conjunction with `enable_publication_awaiter`, `checkpoint_interval`
directly impacts `add-*` requests response time.

### Publication Awaiter

The `enable_publication_awaiter` flag enables [Tessera's publication awaiter](https://github.com/transparency-dev/tessera?tab=readme-ov-file#synchronous-publication),
which waits for a checkpoint larger than the index in the SCT to be published
before returning that SCT. When this flag is on, TesseraCT
responds to `add-*` requests only after a checkpoint covering that entry has been
published. When this flag is off, TesseraCT responses to `add-*` requests as
soon as Tessera has assigned a sequence number to the corresponding entry. The
entry will get integrated and published afterwards, which might happen before
TesseraCT responses to the `add-*` request, or after.

This flag is on by default.

### Sequencing Batch

The `batch_max_age` and `batch_max_size` flags control the maximum age and size
of entries in a single [sequencing batch](https://github.com/transparency-dev/tessera?tab=readme-ov-file#sequencing).
These flags affect the overall performance of TesseraCT. The correct values for
these flags depend on multiple factors, such as the driver used, the number
of TesseraCT servers or their steady QPS rate.

## Antispam

The `pushback_max_antispam_lag`, `pushback_max_dedupe_in_flight` and
`inmemory_antispam_cache_size` flags control how [TesseraCT's Antispam
feature](./architecture.md#antispam) works, which itself is built on top of
[Tessera's Antispam](https://github.com/transparency-dev/tessera?tab=readme-ov-file#antispam)
capabilities.

The antispam database is populated **asynchronously** with `add-*` calls, and follows
the log growth. `inmemory_antispam_cache_size` number of entries from this database
are cached locally on each TesseraCT server. When a request lands on a
server, TesseraCT first queries the antispam database (or its cache) to identify
if the entry is a duplicate of a previous one. If yes, the previous entry is
fetched **synchronously** to extract information required to build its SCT.

The `pushback_max_antispam_lag` flag controls the maximum number of entries that
have already been integrated into the log, but that have not been
added to the antispam database yet by the **asynchronous** process. When this
value is exceeded, TesseraCT returns `429 - Too Many Requests` to **all** `add-*`
requests. This protects TesseraCT from accepting new entries without being able
to tell whether or not they are duplicates of recently integrated submissions.

The `pushback_max_dedupe_in_flight` flag rate limits how many concurrent `add-*`
requests identified as duplicates can be processed by the
**synchronous** process that fetches entries and extracts information required
to build SCTs. When this value is exceeded, TesseraCT returns `429 - Too Many Requests`
to subsequent **duplicate** `add-*` requests only.  Non-duplicate `add-*`
requests are not impacted, and can still be processed.  This limits the number of
resources TesseraCT can spend on duplicate requests.

## Chain filtering

Chains accepted by TesseraCT can be filtered by setting the following flags:

- `root_pem_file`: Path to the file containing root certificates that are
acceptable to the log. Chains MUST be signed by of one the roots included in
this file.
- `reject_expired`: If true, TesseraCT rejects expired certificates.
- `reject_unexpired`: If true, TesseraCT rejects certificates that are either
currently valid or not yet valid.
- `ext_key_usage`: A list of comma separated [Extended Key Usages (EKU) from x509](https://pkg.go.dev/crypto/x509#ExtKeyUsage).
Certificates MUST include one or more of these EKUs to be accepted. If empty, no
filtering is applied.
- `reject_extension`: A comma separated list of X.509 extension OIDs, in dotted
string form (e.g. '2.3.4.5'). Certificates that include one or more of these EKUs
will be rejected.
- `not_after_start`: Start of the range of acceptable NotAfter values,
inclusive. Leaving this unset or empty implies no lower bound to the range.
RFC3339 UTC format, e.g: 2024-01-02T15:04:05Z.
- `not_after_limit`: Cut off point of notAfter dates - only notAfter dates
strictly *before* notAfterLimit will be accepted. Leaving this unset or empty
means no upper bound on the accepted range. RFC3339 UTC format, e.g:
2024-01-02T15:04:05Z.

## Origin and submission prefix

The origin used in Checkpoints is specified with the `origin` flag.  As per
[static-ct-api specs](https://c2sp.org/static-ct-api), `the origin line MUST be
the submission prefix of the log as a schema-less URL with no trailing slashes`.
The submission prefix of a log has two parts: the host and the serving path
part: `https://$HOST/$PATH_PREFIX/ct/v1/...`. Use your frontend serving
infrastructure to make sure that requests to these URLs are correctly routed to a
TesseraCT server. TesseraCT will serve requests it receives regardless of their
$HOST.  However, it will match the $PATH_PREFIX, with the `path_prefix` flag.

## Memory considerations

TesseraCT's memory footprint is directly impacted by:

- `inmemory_antispam_cache_size`: the number of entries from the antispam
database cached locally
- `batch_max_size`: the number of entries that are kept in memory before
sequenced in a batch
- [The number of cached issuers keys](https://github.com/transparency-dev/tesseract/blob/main/storage/storage.go)

## Platform specific configuration

### AWS

TesseraCT expects the databases configured with the `db_name` and
`antispam_db_name` flags to be located in the same Aurora DB cluster.

### GCP

#### VM

By default, TesseraCT exports OpenTelemetry metrics and traces to GCP
infrastructure. It is not currently possible to opt-out of this.

When running TesseraCT locally on a VM OpenTelemetry exporters
[need to be configured manually with a project ID](https://github.com/GoogleCloudPlatform/opentelemetry-operations-go/blob/main/exporter/metric/README.md#authentication).
Set this project ID via the `otel_project_id` flag. This is not required when
TesseraCT does not run on a VM.

When running multiple instances of TesseraCT from the terminal, run
`export OTEL_RESOURCE_ATTRIBUTES="service.instance.id=$VALUE"` with a different
`$VALUE` for each instance.
