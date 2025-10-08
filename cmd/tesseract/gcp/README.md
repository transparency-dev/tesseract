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
