# TesseraCT on GCP

This binary can be deployed on GCP, and primarily meant to be deployed on
[Managed Instance Groups (MIG)](./live/gcp/static-ct-staging/logs).
It also runs on [Cloud Run](./live/gcp/static-ct/logs/ci) and directly on [GCE VMs](./live/gcp/test).

In this document, you will find information specific to this GCP implementation.
You can find more information about TesseraCT in general in the
[architecture design doc](/docs/architecture.md), and in TesseraCT's
[configuration guide](../).

## GCE VMs

Customs monitoring settings need to be applied when running on GCE VMs.

By default, TesseraCT exports OpenTelemetry metrics and traces to GCP
infrastructure. It is not currently possible to opt-out of this.

When running TesseraCT on a GCE VM, OpenTelemetry exporters
[need to be configured manually with a project ID](https://github.com/GoogleCloudPlatform/opentelemetry-operations-go/blob/main/exporter/metric/README.md#authentication).
Set this project ID via the `otel_project_id` flag. This is not required when
TesseraCT does not run on a VM.

When running multiple instances of TesseraCT from the terminal, run
`export OTEL_RESOURCE_ATTRIBUTES="service.instance.id=$VALUE"` with a different
`$VALUE` for each instance.
