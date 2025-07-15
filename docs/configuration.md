# TesseraCT Configuration

## Flags

### Checkpoint Interval

The `checkpoint_interval` flag controls the interval duration between checkpoint
publishing. Tessera enforces the minimum permitted checkpoint interval check
during the TesseraCT application initialization process.

| Backend | Minimum Permitted Checkpoint Interval (ms) |
| ------- | ------------------------------------------ |
| AWS     | 1000                                       |
| GCP     | 1200                                       |

### Publication Awaiter

The `enable_publication_awaiter` flag enables the publication awaiter, which
waits for a checkpoint larger than the index in the SCT to be published before
returning that SCT.

### In-memory Antispam Cache Size

The `inmemory_antispam_cache_size` flags controls the maximum number of entries
in the [in-memory antispam cache](https://github.com/transparency-dev/tessera?tab=readme-ov-file#antispam).
The value should be calculated against the allocated instance memory size.

### Sequencing Batch

The `batch_max_age` and `batch_max_size` flags control the maximum age and size
of entries in a single sequencing batch. Many factors affecting the optimal
values for these flags, such as the number of TesseraCT servers, and their
steady QPS rate.

### AWS

TesseraCT expects both databases from `db_name` and `antispam_db_name` flags are
located in the same Aurora DB cluster.

### GCP

By default, TesseraCT exports OpenTelemetry metrics and traces to GCP
infrastructure. It is not currently possible to opt-out of this. When running
TesseraCT locally on a VM OpenTelemetry exporters
[need to be configured manually with a project ID](https://github.com/GoogleCloudPlatform/opentelemetry-operations-go/blob/main/exporter/metric/README.md#authentication).
Set this project ID via the `otel_project_id` flag. This is not required when
TesseraCT does not run on a VM.
