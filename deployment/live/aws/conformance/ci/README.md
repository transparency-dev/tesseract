# AWS TesseraCT CI Environment

## Overview

This config uses the [aws/conformance](/deployment/modules/aws/tesseract/conformance) module to
define a CI environment to run TesseraCT, backed by Tessera.

At a high level, this environment consists of:

- Aurora MySQL database, used by Tessera both for sequencing and antispam
- S3 Bucket
- Secrets Manager
- ECS

## Manual deployment

Configure an AWS profile on your workstation using your prefered method, (e.g
[sso](https://docs.aws.amazon.com/cli/latest/userguide/cli-configure-sso.html)
or [credential
files](https://docs.aws.amazon.com/cli/v1/userguide/cli-configure-files.html))

Set the required environment variables:

```shell
export AWS_PROFILE={VALUE}
```

Optionally, customize the AWS region (defaults to "us-east-1"), prefix, and base
name for resources (defaults to "static-ct-ci" and "conformance"):

```shell
export TESSERACT_BASE_NAME={VALUE}
export TESSERACT_PREFIX_NAME={VALUE}
```

Resources will be named using a `${TESSERACT_PREFIX_NAME}-${TESSERACT_BASE_NAME}`
convention.

Deploy the Terraform config with OpenTofu:
  1. `cd` to the relevant directory for the environment to deploy/change (e.g. `ci`)
  2. Run `terragrunt apply`
