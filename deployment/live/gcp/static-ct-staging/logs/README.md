# static-ct-staging log

## Overview

This directory contains the config we use to deploy our staging logs. Each log
uses
[/deployment/modules/gcp/tesseract/gce/](/deployment/modules/gcp/tesseract/gce/)
module to run TesseraCT on Managed Instance Groups backed by Tessera.

## HOWTO

### Update roots

Run the following command from the root of the repository.

It fetches roots from a Google RFC6962 log, and filters out roots that don't
parse with `crypto/x509` and `internal/lax509`. As of 2025-03-21, these roots
are:

- <https://crt.sh/?id=298>, with a negative serial number
- <https://crt.sh/?id=9027356>, affected by <https://github.com/golang/go/issues/69463>

```bash
go run github.com/google/certificate-transparency-go/client/ctclient@master get-roots --log_uri=https://ct.googleapis.com/logs/us1/argon2025h1/ --text=false | \
awk \
  '/-----BEGIN CERTIFICATE-----/{c=1; pem=$0; show=1; next}
   c{pem=pem ORS $0}
   /-----END CERTIFICATE-----/{c=0; if(show) print pem}
   ($0=="MIIFxDCCBKygAwIBAgIBAzANBgkqhkiG9w0BAQUFADCCAUsxGDAWBgNVBC0DDwBT"||$0=="MIIFVjCCBD6gAwIBAgIQ7is969Qh3hSoYqwE893EATANBgkqhkiG9w0BAQUFADCB"){show=0}' \
   > deployment/live/gcp/static-ct-staging/logs/arche2025h1/roots.pem
```

### Deploy the log

#### Automatic deployment

These GCP TesseraCT logs are designed to be deployed by
Cloud Build ([OpenTofu module](/deployment/modules/gcp/cloudbuild/tesseract/),
[Terragrunt configuration](/deployment/live/gcp/static-ct-staging/cloudbuild/tesseract/)).

#### Manual deployment

TODO(phboneff): come back to this, MIG doesn't trigger a deployment if the
tag does not change value.

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
`${GOOGLE_REGION}-docker.pkg.dev/${GOOGLE_PROJECT}/docker-staging`. For
reference, here's a [OpenTofu
module](/deployment/modules/gcp/artifactregistry/) you can use to set up such a
registry.

Build and push the Docker image to Artifact Registry repository:

```sh
gcloud auth configure-docker ${GOOGLE_REGION}-docker.pkg.dev
docker build -f ./cmd/tesseract/gcp/Dockerfile -t tesseract-gcp:latest .
docker build -f ./cmd/tesseract/gcp/staging/Dockerfile -t tesseract-staging-gcp:latest .
docker tag tesseract-staging-gcp:latest ${GOOGLE_REGION}-docker.pkg.dev/${GOOGLE_PROJECT}/docker-staging/tesseract-gcp:latest
docker push ${GOOGLE_REGION}-docker.pkg.dev/${GOOGLE_PROJECT}/docker-staging/tesseract-gcp
```

Deploy the Terraform config with OpenTofu:

```sh
terragrunt apply --working-dir=deployment/live/gcp/static-ct-staging/logs/arche2025h1/
```

### Serve data

Monitoring APIs are accessible via GCS directly.  Submission APIs served by
TesseraCT are accessible via an internal load balancer by default, and the
submission API URL is returned by Terraform as `tesseract_url`.

To make these APIs available on public endpoints, continue reading.

#### Make a log public

To make a log public, you need to both:

 1. Make its bucket public with the [`public_bucket` attribute](/deployment/modules/gcp/tesseract/gce/variables.tf)
set to `true` in the [log's Terragrunt configuration](./logs/). You may need to
disable [Public Access Prevention](https://cloud.google.com/storage/docs/public-access-prevention)
first.
 2. Configure the Global Load Balancer for this log, by adding the corresponding
 log name in [../loadbalancer/terragrunt.hcl](./loadbalancer/terragrunt.hcl), and
 apply the config. It may take up to an hour for TLS certs to be provisioned,
 and for endpoints to be available over HTTPS.

#### Make a log private

To make a log private, you need to both:

 1. Make its bucket private with the [`public_bucket` attribute](/deployment/modules/gcp/tesseract/gce/variables.tf)
set to `false` in the [log's Terragrunt configuration](./logs/).
Alternatively, switch [Public Access Prevention](https://cloud.google.com/storage/docs/public-access-prevention)
on in the Google Cloud UI.
 2. Configure the Global Load Balancer with this log removed, by deleting the corresponding
log name in [../loadbalancer/terragrunt.hcl](./loadbalancer/terragrunt.hcl),
and applying the config.  Due to a [dependency loop](https://github.com/terraform-google-modules/terraform-google-lb-http/issues/159)
between forwarding rules and the load balancer backend groups, Terragrunt might
not be able to apply the config. If you run into this go to the Load Balancer
page in the Google Cloud UI, and manually delete the corresponding forwarding
rule and backend group.

### Create additional signers

To support witnessing, additional ed25519 key material must be created and stored in Secret Manager.
These keys MUST be stored as note-formatted signing keys (see https://pkg.go.dev/golang.org/x/mod/sumdb/note#hdr-Signing_Notes).

Since such keys are not natively supported by either Terraform/OpenTofu or Secret Manager, they must be created manually.
This is easily achieved by running the command below in Cloud Shell, note that `${LOG_ORIGIN}` MUST be set to the full `origin` line of the
log which will use this key, and `${TESSERA_BASE_NAME}` SHOULD be set to whatever `locals.base_name` is in the log terragrunt config:

```bash
go run github.com/transparency-dev/serverless-log/cmd/generate_keys@HEAD \
   --key_name="${LOG_ORIGIN}" \
   --print | 
   tee >(grep -v PRIVATE | gcloud secrets create ${TESSERA_BASE_NAME}-ed25519-public-key --data-file=-) |
   grep PRIVATE | gcloud secrets create ${TESSERA_BASE_NAME}-ed25519-private-key --data-file=-
```

