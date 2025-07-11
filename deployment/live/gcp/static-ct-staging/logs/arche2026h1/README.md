# Arche2026h1 staging log

## Overview

This config uses the
[gcp/tesseract/cloudrun](/deployment/modules/gcp/tesseract/cloudrun) module to
run TesseraCT on Cloud Run, backed by Trillian Tessera and preloaded with
argon2026h1 entries.

### Update the roots

Run the following command from the root of the repository.

It fetches roots from argon2026h1, and filters out roots that don't parse with
`crypto/x509` and `internal/lax509`.  As of 2025-03-21, these roots are:

- https://crt.sh/?id=298, with a negative serial number
- https://crt.sh/?id=9027356, affected by https://github.com/golang/go/issues/69463.

```bash
go run github.com/google/certificate-transparency-go/client/ctclient@master get-roots --log_uri=https://ct.googleapis.com/logs/us1/argon2026h1/ --text=false | \
awk \
  '/-----BEGIN CERTIFICATE-----/{c=1; pem=$0; show=1; next}
   c{pem=pem ORS $0}
   /-----END CERTIFICATE-----/{c=0; if(show) print pem}
   ($0=="MIIFxDCCBKygAwIBAgIBAzANBgkqhkiG9w0BAQUFADCCAUsxGDAWBgNVBC0DDwBT"||$0=="MIIFVjCCBD6gAwIBAgIQ7is969Qh3hSoYqwE893EATANBgkqhkiG9w0BAQUFADCB"){show=0}' \
   > deployment/live/gcp/static-ct-staging/logs/arche2026h1/roots.pem
```

### Automatic Deployment

This GCP TesseraCT preloaded staging environment is designed to be deployed by
the Cloud Build ([Terraform module](/deployment/modules/gcp/cloudbuild/tesseract/),
[Terragrunt configuration](/deployment/live/gcp/static-ct-staging/cloudbuild/arche2026h1/)).

### Manual Deployment

TODO(phboneff): come back to this, Cloud Run doesn't trigger a deployment if the
tag does not change value.

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

You need an Artifact Registry repository to store container images; adapt the
configs and commands below to use your registry of choice. The rest of these
instructions assume that the repository is hosted on GCP, and called
`${GOOGLE_REGION}-docker.pkg.dev/${GOOGLE_PROJECT}/docker-staging`. For
reference, here's a [terraform
module](/deployment/modules/gcp/artifactregistry/) you can use to set up such a
registry.

Build and push the Docker image to Artifact Registry repository:

```sh
gcloud auth configure-docker ${GOOGLE_REGION}-docker.pkg.dev
docker build -f ./cmd/gcp/Dockerfile -t tesseract-binary-gcp:latest .
docker build -f ./cmd/gcp/staging/Dockerfile -t tesseract-gcp:latest .
docker tag tesseract-gcp:latest ${GOOGLE_REGION}-docker.pkg.dev/${GOOGLE_PROJECT}/docker-staging/tesseract-gcp:latest
docker push ${GOOGLE_REGION}-docker.pkg.dev/${GOOGLE_PROJECT}/docker-staging/tesseract-gcp
```

Terraforming the project can be done by:

1. `cd` to
[/deployment/live/gcp/static-ct-staging/logs/arche2026h1/](/deployment/live/gcp/static-ct-staging/logs/arche2026h1/).
2. Run `terragrunt apply`.
