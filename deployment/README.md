# :gear: Deployment

This directory contains Terraform [modules](./modules/) to deploy TesseraCT on
GCP and AWS with various setups, and the [instantiations](./live/) we use for
our deployments.

Our deployments consist of:

- **Test logs**: meant to be brought up and turned down quickly for ad-hoc
testing from a Virtual Machine.
- **Continuous Integration (CI) logs**: brought up every merge on the main
branch, undergo automated testing, and are then brought down.
- **Staging logs**: not-yet-ready-for-production, but production-like logs.

Each cloud platform requires its own TesseraCT binary and Tessera
infrastructure. This table summarizes which cloud providers are supported, and
on which platform each instantiation runs:

| Cloud| Binary               | Test log                         | CI logs                                            | Staging logs                                            |
|------|----------------------|----------------------------------|----------------------------------------------------|---------------------------------------------------------|
| GCP  | [cmd/tesseract/gcp](../cmd/tesseract/gcp/)| [VM](./live/gcp/test/)| [Cloud Run](./live/gcp/static-ct/logs/ci/)| [Managed Instance Group](./live/gcp/static-ct-staging/logs/)|
| AWS  | [cmd/tesseract/aws](../cmd/tesseract/aws/)| [VM](./live/aws/test/)| [Fargate](./live/aws/test/)               |                                                         |

## Codelab

The [GCP](./live/gcp/test) and [AWS](./live/aws/test) codelabs will guide you
through bringing up a test TesseraCT log, adding a few entries to it, and check
that the log is sound.
