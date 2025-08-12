# :gear: Deployment

This directory contains OpenTofu [modules](./modules/) to deploy TesseraCT on
GCP and AWS with various setups, and the [instantiations](./live/) we use for
our deployments.

To run TesseraCT on [POSIX](/cmd/tesseract/posix) or [Vanilla S3+MySQL](/cmd/tesseract/aws),
go to the respective binary documentation directly.

> [!WARNING]
> These config files are primarily meant to support our own deployments, and to
> provide a starting point for other deployments. You can fork them, but DO NOT
> take a dependency on them. We reserve the right to break backward compatibility
> or to remove them at any point. We will not add features that are not relevant
> to our deployments. With that in mind, if you believe that you've found a bug,
> feel free to send a PR or open an issue.

Our deployments consist of:

- **Test logs**: meant to be brought up and turned down quickly for ad-hoc
testing from a Virtual Machine.
- **Continuous Integration (CI) logs**: brought up every merge on the main
branch, undergo automated testing, and are then brought down.
- **Staging logs**: not-yet-ready-for-production, but production-like logs.

Each cloud platform requires its own TesseraCT binary and Tessera
infrastructure. This table summarizes which cloud providers are supported, and
on which platform each instantiation runs:

| Cloud   | Binary                                      | Test log              | CI logs                                   | Staging logs                             |
|---------|---------------------------------------------|-----------------------|-------------------------------------------|------------------------------------------|
| GCP     | [cmd/tesseract/gcp](/cmd/tesseract/gcp)     | [VM](./live/gcp/test) | [Cloud Run](./live/gcp/static-ct/logs/ci) | [MIG](./live/gcp/static-ct-staging/logs) |
| AWS     | [cmd/tesseract/aws](/cmd/tesseract/aws)     | [VM](./live/aws/test) | [Fargate](./live/aws/conformance/ci)      |                                          |

## Codelab

The [GCP](./live/gcp/test) and [AWS](./live/aws/test) codelabs will guide you
through bringing up a test TesseraCT log, adding a few entries to it, and check
that the log is sound.
