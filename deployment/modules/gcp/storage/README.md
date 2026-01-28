# Storage

This module creates:
 - a GCS bucket
 - a Spanner database

## GCS Bucket

By default, this bucket will be called `{project_id}-{base_name}-bucket`.

If you wish, you can name your bucket after a DNS name and use
[domain-name bucket verification](https://docs.cloud.google.com/storage/docs/domain-name-verification)
to demonstrate control of that domain.
