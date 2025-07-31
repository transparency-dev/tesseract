# Architecture

> [!NOTE]
> This doc is meant to be purely informative, and makes no commitment
> to any support level nor future plan.

## Platforms

At the moment, TesseraCT can run on the following platforms:

- [Google Cloud Platform](https://cloud.google.com) (GCP)
- [Amazon Web Services](https://aws.amazon.com) (AWS)
- S3-compliant systems alongside a MySQL database
- POSIX-compliant filesystems

TesseraCT is built on top of [Tessera](https://github.com/transparency-dev/tessera/).
At the moment, the Tessera [MySQL-only driver](https://github.com/transparency-dev/tessera?tab=readme-ov-file#storage-drivers)
is not supported by TesseraCT.

## Common infrastructure

Regardless of the platform it's running on, a TesseraCT instance is composed of
the following elements:

1. At least one instance of a server built using the [Tessera](https://github.com/transparency-dev/tessera/)
library configured with the appropriate [driver](https://github.com/transparency-dev/tessera/?tab=readme-ov-file#storage-drivers)
and [antispam](https://github.com/transparency-dev/tessera/?tab=readme-ov-file#antispam)
implementations for that platform.
1. An additional storage system to store [issuer certificates](https://github.com/C2SP/C2SP/blob/main/static-ct-api.md#issuers),
usually co-hosted with Tessera's log storage system.

TesseraCT uses the CT [specific](https://github.com/transparency-dev/tessera/blob/main/ct_only.go)
[features](https://github.com/transparency-dev/tessera/blob/main/ctonly/ct.go)
in Tessera to be compliant with the [static-ct-api specs](https://c2sp.org/static-ct-api).

By design, a TesseraCT server manages a single log.
To increase reliability, multiple identical TesseraCT instances can run
concurrently for a single CT log.
To serve multiple distinct CT logs, bring up at least one TesseraCT server per log.

For additional details, read [Tessera's design document](https://github.com/transparency-dev/tessera/tree/main/docs/design),
and the platform-specific details below.

### Antispam

[Tessera's antispam](https://github.com/transparency-dev/tessera/blob/main/docs/design/antispam.md)
is used to minimise the number of duplicate entries accepted into a log.

When Tessera detects that a submission is a duplicate of previous one, it
returns the index of that previous entry, without adding an additional entry.
This index is required, but not sufficient to build the
[`SignedCertificateTimestamp` (`SCT`)](https://github.com/C2SP/C2SP/blob/main/static-ct-api.md#sct-extension)
returned to clients. To build this `SCT`, TesseraCT also needs the timestamp of
that previous entry. TesseraCT fetches the previous entry at the given
index, and extract its timestamp to rebuild the SCT.

TesseraCT can rate limit these duplicate submission independently from
non-duplicate ones, thus allowing logs to prioritize non-duplicate submissions.

### Chain filtering

TesseraCT needs to parse incoming certificate chains to process them. This allows:

- TesseraCT to filter and reject chains based on various criteria such as the
root certificates they chain up to, or their validity date
- TesseraCT to construct the log entries which will be added to the log

By design, Go's [`crypto/x509`](https://pkg.go.dev/crypto/x509) library blocks
non-standard certificate chains, such as [precertificate chains](https://www.rfc-editor.org/rfc/rfc6962#section-3.1),
or chains that are not safe to use.
TesseraCT uses [`internal/lax509`](/internal/lax509/), a lightweight fork of
Go's [`crypto/x509`](https://pkg.go.dev/crypto/x509) library to parse chains.

## Detailed architecture

### Google Cloud Platform (GCP)

This implementation is composed of:

 1. One or multiple TesseraCT servers. For reliability, multiple servers can run
 concurrently.
 1. Tessera's backend infrastructure:
    1. A [GCS](https://cloud.google.com/storage) bucket. TesseraCT re-uses this
    bucket to store [issuer certificates](https://github.com/C2SP/C2SP/blob/main/static-ct-api.md#issuers).
    1. A [Spanner](https://cloud.google.com/spanner) database used for
    sequencing entries, and antispam.
 1. Key materials, stored in [Secret Manager](https://cloud.google.com/security/products/secret-manager)

APIs:

 1. Submission requests (`add-chain`, `add-pre-chain`, `get-roots`) are processed
 directly by TesseraCT servers.
 2. Monitoring requests (fetching the checkpoint, tiles, log entries and
 issuers) are handled directly by GCS, without going through TesseraCT
 servers.

See the Tessera [GCP design doc](https://github.com/transparency-dev/tessera/tree/main/storage/gcp)
for additional details.

### Amazon Web Services (AWS)

This implementation is composed of:

 1. One or multiple TesseraCT servers. For reliability, multiple servers can run
 concurrently.
 1. Tessera's backend infrastructure:
    1. An [S3](https://aws.amazon.com/s3/) bucket. TesseraCT re-uses this bucket
    to store [issuer certificates](https://github.com/C2SP/C2SP/blob/main/static-ct-api.md#issuers).
    1. A MySQL [RDS](https://aws.amazon.com/rds/) database used for sequencing
    entries, and antispam.
 1. Key materials, stored in [Secrets Manager](https://aws.amazon.com/secrets-manager/)

APIs:

 1. Submission requests (`add-chain`, `add-pre-chain`, `get-roots`) are processed
directly by TesseraCT servers.
 2. Monitoring requests (fetching the checkpoint, tiles, log entries and
issuers) are handled directly by S3, without going through TesseraCT
servers.

See the Tessera [AWS design doc](https://github.com/transparency-dev/tessera/tree/main/storage/aws)
for additional details.

### S3-compliant systems with a MySQL database

While TesseraCT's [AWS implementation](#amazon-web-services-aws)
uses [RDS](https://aws.amazon.com/rds/) and [S3](https://aws.amazon.com/s3/),
its configuration APIs are intended to be compatible with any MySQL database,
and S3 compatible backend, such as [MinIO](https://min.io/). If you are already
running these services, this might be a good option to consider depending on
your needs.

> [!WARNING]
> S3-compatible backends do not all provide the same guarantees
> that S3 does, and might therefore not be suitable to run TesseraCT.

### POSIX-compliant filesystems

This implementation needs only:

1. A POSIX-compliant filesystem (e.g. ZFS) to store the log, and
1. Any HTTP server capable of directly serving the files from the log stored on
that filesystem.

See the Tessera [POSIX design doc](https://pkg.go.dev/github.com/transparency-dev/tessera/storage/posix)
for additional details.

If you are comfortable running a web-server and unix-style binaries in a VM
(or on bare metal) and do not want the complexity of running additional storage
and database services, this might be a good option.
