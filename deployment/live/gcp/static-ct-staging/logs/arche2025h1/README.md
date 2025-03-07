# GCP SCTFE CI Environment

## Overview

This config uses the [gcp/conformance](/deployment/modules/gcp/tesseract/conformance) module to
define a Tesseract log Cloud Run, backed by Trillian Tessera and preloaded with argon2025h1 entries.

### Update the roots
Run `run github.com/google/certificate-transparency-go/client/ctclient@master get-roots --log_uri=https://ct.googleapis.com/logs/us1/argon2025h1/ --text=false > roots.pem`

### Automatic Deployment
TODO(phboneff): edit

This GCP SCTFE conformance CI environment is designed to be deployed by the Cloud Build ([Terraform module](/deployment/modules/gcp/cloudbuild/conformance/), [Terragrunt configuration](/deployment/live/gcp/static-ct/cloudbuild/prod/)).

### Manual Deployment
TODO(phboneff): edit

First authenticate via `gcloud` as a principle with sufficient ACLs for
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

You need an Artifact Registry repository to store container images; adapt the configs and commands below to use your registry of choice. The rest of these instructions assume that the repository is hosted on GCP, and called `${GOOGLE_REGION}-docker.pkg.dev/${GOOGLE_PROJECT}/docker-ci`. For reference, here's a [terraform module](/deployment/modules/gcp/artifactregistry/) you can use to set up such a registry.

Build and push the Docker image to Artifact Registry repository:

```sh
gcloud auth configure-docker ${GOOGLE_REGION}-docker.pkg.dev
docker build -f ./cmd/gcp/Dockerfile -t sctfe-gcp:latest .
docker build -f ./cmd/gcp/ci/Dockerfile -t conformance-gcp:latest .
docker tag conformance-gcp:latest ${GOOGLE_REGION}-docker.pkg.dev/${GOOGLE_PROJECT}/docker-ci/conformance-gcp:latest
docker push ${GOOGLE_REGION}-docker.pkg.dev/${GOOGLE_PROJECT}/docker-ci/conformance-gcp
```

Terraforming the project can be done by:
  1. `cd` to the relevant directory (deployment/live/gcp/static-ct/ci/) for the environment to deploy/change.
  2. Run `terragrunt apply`.
