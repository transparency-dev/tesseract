# TesseraCT on GCP

This binary can be deployed on GCP, works well with
[Managed Instance Groups (MIG)](./live/gcp/static-ct-staging/logs) and directly
on [GCE VMs](./live/gcp/test).

It can also be run on [Cloud Run](./live/gcp/static-ct/logs/ci), although we
have observed slightly reduced performance in this environment.

In this document, you will find information specific to this GCP implementation.
You can find more information about TesseraCT in general in the
[architecture design doc](/docs/architecture.md), and in TesseraCT's
[configuration guide](../).

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
