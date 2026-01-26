# GCP TesseraCT Test Environment

This directory contains configs to deploy TesseraCT's log infrastructure on GCP,
which a TesseraCT server running on a VM can then use.

> [!CAUTION]
> This test environment creates real Google Cloud Platform resources running in
> your project. They will cost you real money. For the purposes of this demo,
> it is strongly recommended that you create a new project so that you can
> easily clean up at the end.

## Prerequisites

You'll need to have a VM running in the same GCP project that you can SSH to,
with [Go](https://go.dev/doc/install) and
[terragrunt](https://terragrunt.gruntwork.io/docs/getting-started/install/)
with [OpenTofu](https://opentofu.org/) installed, and your favourite terminal multiplexer.

## Overview

This config uses the
[gcp/tesseract/test](/deployment/modules/gcp/tesseract/test) module to deploy
resources necessary to run a test TesseraCT log. TesseraCT itself will run on a
VM.

At a high level, these resources consists of:

- One Spanner instance with two databases:
  + one for Tessera
  + one for antispam
- A GCS Bucket
- Secret Manager

The TesseraCT binary will provide [static-ct-api submission APIs](https://github.com/C2SP/C2SP/blob/main/static-ct-api.md#submission-apis),
while [static-ct-api monitoring APIs](https://github.com/C2SP/C2SP/blob/main/static-ct-api.md#monitoring-apis)
are provided directly by Tessera's GCS bucket.

## Codelab

This codelab will guide you to bring up a test TesseraCT log infrastructure, add
a few entries to it, and check that the log is sound.

To start with, authenticate with a role that has sufficient access to create
resources.

First, set the required environment variables:

```bash
export GOOGLE_PROJECT={VALUE}
export GOOGLE_REGION={VALUE} # e.g: us-central1
export TESSERA_BASE_NAME={VALUE} # e.g: test-static-ct
```

> [!TIP]
> `TESSERA_BASE_NAME` will be used to prefix the name of various resources, and
> must be less than 21 characters to avoid hitting naming limits.

Then authenticate via `gcloud` as a principal with sufficient ACLs for
the project:

```bash
gcloud auth application-default login --project=$GOOGLE_PROJECT
```

Next, you need to create an ECDSA P-256 keypair specifically for exclusive use with your new log.
This keypair will be used to sign/verify the `SCT`s and `Checkpoint`s from your log, and should be stored
in Secret Manager using the names "${TESSERA_BASE_NAME}-log-secret" and "${TESSERA_BASE_NAME}-log-public"
for the private and public keys, respectively. 

You can use the steps below to create this key:

1. Ensure you have enabled the
   [GCP Secret Manager API](https://cloud.google.com/security/products/secret-manager?hl=en)
2. Use the `cmd/tesseract/gcp/generate_key` tool to create and store a new key:
   ```sh
   go run github.com/transparency-dev/tesseract/cmd/tesseract/gcp/generate_key@main \
      --project_id="${GOOGLE_PROJECT}" \
      --log_origin="${TESSERA_BASE_NAME}"
   ```


Apply the Terragrunt config to deploy resources:

```sh
terragrunt apply -working-dir=deployment/live/gcp/test
```

> [!NOTE]
> The first time you run this command, Terragrunt will ask whether you want to
> create a Terragrunt remote state bucket. Answer `y`.

Store the Secret Manager resource ID of signer key pair into environment variables:

```sh
export TESSERACT_SIGNER_ECDSA_P256_PUBLIC_KEY_ID="${TESSERA_BASE_NAME}-log-public"
export TESSERACT_SIGNER_ECDSA_P256_PRIVATE_KEY_ID="${TESSERA_BASE_NAME}-log-secret"
```

## Run TesseraCT

<!-- Try and keep in sync as much as possible with ../../aws/test/README.md 
There are enough differences for now to justify to keep them distinct -->

This section will help you bring a TesseraCT server up using the resources
created above. [static-ct-api submission APIs](https://github.com/C2SP/C2SP/blob/main/static-ct-api.md#submission-apis)
will be served by the TesseraCT server at `http://localhost:6962/${TESSERA_BASE_NAME}`.
[static-ct-api monitoring APIs](https://github.com/C2SP/C2SP/blob/main/static-ct-api.md#monitoring-apis)
will be directly accessible via the GCS bucket, at `https://storage.googleapis.com/${GOOGLE_PROJECT}-${TESSERA_BASE_NAME}-bucket`.

Decide whether to run TesseraCT such that it accepts:

- [fake test chains](#with-fake-chains)
- [real TLS chains](#with-real-tls-chains)

### With fake chains

On the VM, run the following command to prepare the roots pem file and bring
TesseraCT up:

```bash
cat internal/testdata/fake-ca.cert internal/hammer/testdata/test_root_ca_cert.pem > /tmp/fake_log_roots.pem
```

```bash
go run ./cmd/tesseract/gcp/ \
  --bucket=${GOOGLE_PROJECT}-${TESSERA_BASE_NAME}-bucket \
  --spanner_db_path=projects/${GOOGLE_PROJECT}/instances/${TESSERA_BASE_NAME}/databases/${TESSERA_BASE_NAME}-db \
  --spanner_antispam_db_path=projects/${GOOGLE_PROJECT}/instances/${TESSERA_BASE_NAME}/databases/${TESSERA_BASE_NAME}-antispam-db \
  --roots_pem_file=/tmp/fake_log_roots.pem \
  --origin=${TESSERA_BASE_NAME} \
  --path_prefix=${TESSERA_BASE_NAME} \
  --signer_public_key_secret_name=${TESSERACT_SIGNER_ECDSA_P256_PUBLIC_KEY_ID} \
  --signer_private_key_secret_name=${TESSERACT_SIGNER_ECDSA_P256_PRIVATE_KEY_ID} \
  --otel_project_id=${GOOGLE_PROJECT} \
  --v=1
```

Confirm that TesseraCT is running properly by fetching a checkpoint from
a different terminal:

```bash
 curl -X GET -H "Authorization: Bearer $(gcloud auth print-access-token)" "https://storage.googleapis.com/${GOOGLE_PROJECT}-${TESSERA_BASE_NAME}-bucket/checkpoint"
```

Decide whether to run generate test chains:

- [manually](#generate-chains-manually)
- [automatically](#generate-chains-automatically)

#### Generate chains manually

In a different terminal, generate a chain manually. The password for the private
key is `gently`:

```bash
mkdir -p /tmp/httpschain
openssl genrsa -out /tmp/httpschain/cert.key 2048
openssl req -new -key /tmp/httpschain/cert.key -out /tmp/httpschain/cert.csr -config=internal/testdata/fake-ca.cfg
openssl x509 -req -days 3650 -in /tmp/httpschain/cert.csr -CAkey internal/testdata/fake-ca.privkey.pem -CA internal/testdata/fake-ca.cert -outform pem -out /tmp/httpschain/chain.pem -provider legacy -provider default
cat internal/testdata/fake-ca.cert >> /tmp/httpschain/chain.pem
```

Finally, submit the chain to TesseraCT:

```bash
go run github.com/google/certificate-transparency-go/client/ctclient@master upload --cert_chain=/tmp/httpschain/chain.pem --skip_https_verify --log_uri=http://localhost:6962/${TESSERA_BASE_NAME}
```

#### Generate chains automatically

In a different terminal, retrieve the log public key in PEM format.

```bash
gcloud secrets versions access $TESSERACT_SIGNER_ECDSA_P256_PUBLIC_KEY_ID > /tmp/log_public_key.pem
```

Generate the certificate chains and submit them to TesseraCT using the [hammer tool](/internal/hammer/README.md):

```bash
go run ./internal/hammer \
  --log_url=https://storage.googleapis.com/${GOOGLE_PROJECT}-${TESSERA_BASE_NAME}-bucket \
  --write_log_url=http://localhost:6962/${TESSERA_BASE_NAME} \
  --origin=$TESSERA_BASE_NAME \
  --log_public_key=$(openssl ec -pubin -inform PEM -in /tmp/log_public_key.pem -outform der | base64 -w 0) \
  --max_read_ops=0 \
  --num_readers_random=0 \
  --num_readers_full=0 \
  --num_writers=8 \
  --max_write_ops=4 \
  --num_mmd_verifiers=0 \
  --bearer_token=$(gcloud auth print-access-token)
```

### With real TLS chains

We'll run a TesseraCT instance and copy certificates from an existing RFC6962
log to it.  It uses the [preloader tool from certificate-transparency-go](https://github.com/google/certificate-transparency-go/blob/master/preload/preloader/preloader.go).

First, save the source log URI:

```bash
export SOURCE_LOG_URI=https://ct.googleapis.com/logs/xenon2022
```

Then, get fetch the roots the source logs accepts, and edit configs accordingly.
Two roots that TesseraCT cannot load with the [internal/lax509](/internal/lax509/)
library need to be removed.

```bash
go run github.com/google/certificate-transparency-go/client/ctclient@master get-roots --log_uri=${SOURCE_LOG_URI} --text=false | \
awk \
  '/-----BEGIN CERTIFICATE-----/{c=1; pem=$0; show=1; next}
   c{pem=pem ORS $0}
   /-----END CERTIFICATE-----/{c=0; if(show) print pem}
   ($0=="MIIFxDCCBKygAwIBAgIBAzANBgkqhkiG9w0BAQUFADCCAUsxGDAWBgNVBC0DDwBT"||$0=="MIIFVjCCBD6gAwIBAgIQ7is969Qh3hSoYqwE893EATANBgkqhkiG9w0BAQUFADCB"){show=0}' \
   > /tmp/log_roots.pem
```

Run TesseraCT with the same roots:

```bash
go run ./cmd/tesseract/gcp/ \
  --bucket=${GOOGLE_PROJECT}-${TESSERA_BASE_NAME}-bucket \
  --spanner_db_path=projects/${GOOGLE_PROJECT}/instances/${TESSERA_BASE_NAME}/databases/${TESSERA_BASE_NAME}-db \
  --roots_pem_file=/tmp/log_roots.pem \
  --origin=${TESSERA_BASE_NAME} \
  --path_prefix=${TESSERA_BASE_NAME} \
  --spanner_antispam_db_path=projects/${GOOGLE_PROJECT}/instances/${TESSERA_BASE_NAME}/databases/${TESSERA_BASE_NAME}-antispam-db \
  --signer_public_key_secret_name=${TESSERACT_SIGNER_ECDSA_P256_PUBLIC_KEY_ID} \
  --signer_private_key_secret_name=${TESSERACT_SIGNER_ECDSA_P256_PRIVATE_KEY_ID} \
  --otel_project_id=${GOOGLE_PROJECT} \
  --v=1
```

Confirm that TesseraCT is running properly by fetching a checkpoint from
a different terminal:

```bash
 curl -X GET -H "Authorization: Bearer $(gcloud auth print-access-token)" "https://storage.googleapis.com/${GOOGLE_PROJECT}-${TESSERA_BASE_NAME}-bucket/checkpoint"
```

In a different terminal, run `preloader` to submit certificates from another log
to TesseraCT.

```bash
go run github.com/google/certificate-transparency-go/preload/preloader@master \
  --target_log_uri=http://localhost:6962/${TESSERA_BASE_NAME} \
  --source_log_uri=${SOURCE_LOG_URI} \
  --num_workers=8 \
  --parallel_fetch=4 \
  --parallel_submit=4 \
  --v=1
```

Since the source and destination log
[might not be configured the exact same set of roots](/internal/lax509/README.md#Chains),
it is expected to see errors when submitting a certificate chaining to a missing
root. This is what the error would look like:

```bash
W0623 11:57:05.122711    6819 handlers.go:168] test-static-ct: AddPreChain handler error: failed to verify add-chain contents: chain failed to validate: x509: certificate signed by unknown authority (possibly because of "x509: cannot verify signature: insecure algorithm SHA1-RSA" while trying to verify candidate authority certificate "Merge Delay Monitor Root")
```

<!-- TODO: add fsck instructions -->

## Clean up

> [!IMPORTANT]
> You need to run this step on your project if you want to ensure you don't get
> charged into perpetuity for the resources we've setup.

**This will delete your project!**
Do not do this on a project that you didn't create expressly and exclusively to
run this demo.

```bash
gcloud projects delete ${GOOGLE_PROJECT}
```
