# GCP TesseraCT CI Environment

## Overview

This config uses the [gcp/cloudrun](/deployment/modules/gcp/tesseract/cloudrun)
module to define a CI environment to run TesseraCT on Cloud Run, backed by
Tessera.

At a high level, this environment consists of:

- One Spanner instance with two databases:
  - one for Tessera
  - one for antispam
- A GCS Bucket
- Secret Manager
- Cloud Run

### Automatic Deployment

This GCP TesseraCT conformance CI environment is designed to be deployed by the
Cloud Build ([OpenTofu module](/deployment/modules/gcp/cloudbuild/conformance/),
[Terragrunt configuration](/deployment/live/gcp/static-ct-ci/cloudbuild/ci/)).

### Manual Deployment

First authenticate via `gcloud` as a principal with sufficient ACLs for
the project:

```sh
gcloud auth application-default login
```

Set the required environment variables:

```sh
export GOOGLE_PROJECT={VALUE}
export GOOGLE_REGION={VALUE} # e.g: us-central1
unset TESSERA_BASE_NAME
```

You need an Artifact Registry repository to store container images; adapt the
configs and commands below to use your registry of choice. The rest of these
instructions assume that the repository is hosted on GCP, and called
`${GOOGLE_REGION}-docker.pkg.dev/${GOOGLE_PROJECT}/docker-ci`. For reference,
here's a [OpenTofu module](/deployment/modules/gcp/artifactregistry/) you can
use to set up such a registry.

Build and push the Docker image to Artifact Registry repository:

```sh
gcloud auth configure-docker ${GOOGLE_REGION}-docker.pkg.dev
docker build -f ./cmd/tesseract/gcp/Dockerfile -t tesseract-gcp:latest .
docker build -f ./cmd/tesseract/gcp/ci/Dockerfile -t conformance-gcp:latest .
docker tag conformance-gcp:latest ${GOOGLE_REGION}-docker.pkg.dev/${GOOGLE_PROJECT}/docker-ci/conformance-gcp:latest
docker push ${GOOGLE_REGION}-docker.pkg.dev/${GOOGLE_PROJECT}/docker-ci/conformance-gcp
```

Deploy the Terraform config with OpenTofu:

  1. `cd` to
  [/deployment/live/gcp/static-ct-ci/logs/ci/](/deployment/live/gcp/static-ct-ci/logs/ci/).
  2. Run `terragrunt apply`.

