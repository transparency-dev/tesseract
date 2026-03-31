# TesseraCT on GCP

This binary can be deployed on GCP, works well with
[Managed Instance Groups (MIG)](./live/gcp/static-ct-staging/logs) and directly
on [GCE VMs](./live/gcp/test).

It can also be run on [Cloud Run](./live/gcp/static-ct-ci/logs/ci), although we
have observed slightly reduced performance in this environment.

In this document, you will find information specific to this GCP implementation.
You can find more information about TesseraCT in general in the
[architecture design doc](/docs/architecture.md), and in TesseraCT's
[configuration guide](../).

## Keys and Secret Manager

Log private and public keys are stored as secrets in Secret Manager, and the full secret version resource
names passed to 
`--signer_private_key_secret_name` and `--signer_public_key_secret_name` respectively.

> [!WARNING]
> While the `latest` version alias is supported, unless you are sure you know what you are doing, we 
> strongly recommend the use of specific version IDs instead.
>
> Using `latest` will cause the log's key to be updated without warning if a new secret version is
> created. Since CT for the WebPKI currently does not support log key rotation, other than through
> retiring log shards and bringing up new ones, automatic rotation of the log key, inadvertant or 
> otherwise, will therefore almost certainly result in an unplanned outage.

## Witnessing

> [!WARNING]
> Witnessing support is experimental at the moment - there is work actively underway
> in this space by the transparency community.
> Correspondingly, the interaction with the configured witnesses is currently hard-coded
> to fail-open in the event that insufficient witness cosignatures were acquired - i.e. 
> checkpoints will continue to be published with or without witness cosignatures.

This binary has support for interacting with [tlog-witness](https://c2sp.org/tlog-witness)
compliant witnesses. To enable this, pass the path of a file containing a witness policy
to the `--witness_policy_file` flag, and ensure that at least one Secret Manager resource
containing a note-compatible `Ed25519` signer known to the configured witness(es) is
provided via the `--additional_signer` flag.

The witness policy file is expected to contain a text-based description of the policy in
the format described by https://git.glasklar.is/sigsum/core/sigsum-go/-/blob/main/doc/policy.md

## Logging

TesseraCT on GCP uses two logging systems as it transitions to structured logging:

### Standard `klog` (Legacy)
Many internal libraries and older code use `klog`. 
- **Routing**: `klog` is configured to write to `stderr` (`-logtostderr`). When running in GCE/COS, the Docker daemon intercepts `stderr` because it is configured with `--log-driver=gcplogs`. 
- **Expected Fields**: Because Docker's `gcplogs` driver handles the transmission, it automatically decorates logs with:
  - `container`: name, id, image name, etc.
  - `instance`: VM name, id, zone.

### Structured `slog` (Recommended)
Newer code and the main server logs use `log/slog`.
- **Routing**: By default, `slog` bakes OpenTelemetry trace context and exports logs **directly to the Cloud Logging API** (bypassing `stderr`). 
- **Expected Fields**: Because it bypasses `stderr` and the Docker `gcplogs` driver, it does not get automatic container/instance decoration by Docker. Instead, at startup, TesseraCT queries the GCE Metadata Server and reads flags to manually bake these fields into the default `slog` logger. You can expect:
  - `message`
  - `severity`
  - `timestamp`
  - `logging.googleapis.com/trace`, `logging.googleapis.com/spanId` (if OpenTelemetry span present)
  - `container`: `name`, `imageName` (passed via Terraform flags)
  - `instance`: `name`, `id`, `zone` (queried from GCE metadata server)

## GCE VMs

Custom monitoring settings need to be applied when running on GCE VMs, these are
outlined below.

By default, TesseraCT exports OpenTelemetry metrics and traces to GCP
infrastructure. It is not currently possible to opt-out of this.

When running TesseraCT on a GCE VM, OpenTelemetry exporters
[need to be configured manually with a project ID](https://github.com/GoogleCloudPlatform/opentelemetry-operations-go/blob/main/exporter/metric/README.md#authentication).
Set this project ID via the `otel_project_id` flag. This is not required when
TesseraCT does not run on a VM.

When running multiple instances of TesseraCT from the terminal, run
`export OTEL_RESOURCE_ATTRIBUTES="service.instance.id=$VALUE"` with a different
`$VALUE` for each instance.
