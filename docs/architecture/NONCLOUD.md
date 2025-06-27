# Non-cloud architecture

> [!NOTE]
> This doc is meant to be purely informative, and makes no commitment
> to any support level nor future plan.

At the moment, TesseraCT only supports [Google Cloud Platform](https://cloud.google.com)
(GCP) and [Amazon Web Services](https://aws.amazon.com) (AWS) deployments,
using platform-specific products, such as Spanner with GCS on GCP and RDS with
S3 on AWS.

TesseraCT is based on [Tessera](https://github.com/transparency-dev/tessera),
which itself offers two implementations that can be deployed outside of GCP or AWS:

- [POSIX](https://github.com/transparency-dev/tessera/tree/main/storage/posix)
- [MySQL](https://github.com/transparency-dev/tessera/tree/main/storage/mysql)

While Tessera's [AWS implementation](https://github.com/transparency-dev/tessera/tree/main/storage/aws)
uses [RDS](https://aws.amazon.com/rds/) and [S3](https://aws.amazon.com/s3/),
its configuration APIs _should_ be compatible with any MySQL database, and S3
compatible backend, such as [MinIO](https://min.io/). This might be a good option
to consider depending on your needs.

If you're interested in running outside of GCP or AWS, please [get in touch](../README.md#-contact),
we would be more than happy to discuss these various tradeoffs with you and to
help you find a good path forward.

> [!WARNING]
> S3-compatible backends do not all provide the same guarantees
> that S3 does, and might therefore not be suitable to run TesseraCT.

> [!WARNING]
> Please, note that at the moment, such a setup has not been thoroughly
> tested and **is not supported**. We do not have any immediate plan to work on
> this.
