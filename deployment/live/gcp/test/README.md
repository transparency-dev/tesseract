# GCP TesseraCT Test Environment

## Prerequisites

You'll need to have a VM running in the same GCP project that you can SSH to,
with [Go](https://go.dev/doc/install) and
[terragrunt](https://terragrunt.gruntwork.io/docs/getting-started/install/)
installed, and your favourite terminal multiplexer.

## Overview

This config uses the [gcp/test](/deployment/modules/gcp/test) module to
deploy resources necessary to run a test TesseraCT log. TesseraCT itself
will run on a VM.

At a high level, these resources consists of:

- One Spanner instance with two databases:
  - one for Tessera
  - one for antispam
- A GCS Bucket
- Secret Manager

## Manual deployment

First, set the required environment variables:

```bash
export GOOGLE_PROJECT={VALUE}
export GOOGLE_REGION={VALUE} # e.g: us-central1
export TESSERA_BASE_NAME={VALUE} # e.g: test-static-ct
```

> [!TIP]
> `TESSERA_BASE_NAME` will be used to prefix the name of various resources, and
> must be less than 21 characters to avoid hitting naming limits.

Then authenticate via `gcloud` as a principle with sufficient ACLs for
the project:

```bash
gcloud auth application-default login --project=$GOOGLE_PROJECT
```

Apply the Terragrunt config to deploy resources:

```sh
terragrunt apply --terragrunt_working_dir=deployment/live/gcp/test
```

> [!NOTE]
> The first time you run this command, Terragrunt will ask whether you want to
> create a Terragrunt remote state bucket. Answer `y`.

Store the Secret Manager resource ID of signer key pair into environment variables:

```sh
export TESSERACT_SIGNER_ECDSA_P256_PUBLIC_KEY_ID=$(terragrunt output -raw ecdsa_p256_public_key_id -terragrunt-working-dir=deployment/live/gcp/test)
export TESSERACT_SIGNER_ECDSA_P256_PRIVATE_KEY_ID=$(terragrunt output -raw ecdsa_p256_private_key_id -terragrunt-working-dir=deployment/live/gcp/test)
```

## Run TesseraCT

### With fake chains

On the VM, run the following command to bring TesseraCT up:

```bash
go run ./cmd/gcp/ \
  --bucket=${GOOGLE_PROJECT}-${TESSERA_BASE_NAME}-bucket \
  --spanner_db_path=projects/${GOOGLE_PROJECT}/instances/${TESSERA_BASE_NAME}/databases/${TESSERA_BASE_NAME}-db \
  --spanner_antispam_db_path=projects/${GOOGLE_PROJECT}/instances/${TESSERA_BASE_NAME}/databases/${TESSERA_BASE_NAME}-antispam-db \
  --roots_pem_file=./internal/testdata/fake-ca.cert \
  --origin=${TESSERA_BASE_NAME} \
  --signer_public_key_secret_name=${TESSERACT_SIGNER_ECDSA_P256_PUBLIC_KEY_ID} \
  --signer_private_key_secret_name=${TESSERACT_SIGNER_ECDSA_P256_PRIVATE_KEY_ID} \
  --otel_project_id=${GOOGLE_PROJECT}
```

In a different terminal you can either mint and submit certificates manually, or
use the [ct_hammer tool](https://github.com/google/certificate-transparency-go/blob/master/trillian/integration/ct_hammer/main.go)
to do this.

#### Generate chains manually

Generate a chain manually. The password for the private key is `gently`:

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

#### Automatically generate chains

Retrieve the log public key in PEM format, convert it to DER format, and generate the hammer configuration file. (Install the `xxd` command if you haven't already)

```bash
gcloud secrets versions access $TESSERACT_SIGNER_ECDSA_P256_PUBLIC_KEY_ID > /tmp/log_public_key.pem
LOG_PUBLIC_KEY_DER=$(openssl pkey -pubin -in /tmp/log_public_key.pem -outform DER | xxd -i -c1000 | sed s/\,\ 0/\\\\/g | sed s/^..0x/\\\\x/g)
mkdir -p /tmp/hammercfg
cat > /tmp/hammercfg/hammer.cfg << EOF
config {
  roots_pem_file: ""
  public_key: {
    der: "$LOG_PUBLIC_KEY_DER"
  }
}
EOF
```

Clone the [certificate-transparency-go](https://github.com/google/certificate-transparency-go) repo, and from there run:

```bash
go run ./trillian/integration/ct_hammer \
  --ct_http_servers=localhost:6962/${TESSERA_BASE_NAME} \
  --max_retry=2m \
  --invalid_chance=0 \
  --get_sth=0 \
  --get_sth_consistency=0 \
  --get_proof_by_hash=0 \
  --get_entries=0 \
  --get_roots=0 \
  --get_entry_and_proof=0 \
  --max_parallel_chains=4 \
  --skip_https_verify=true \
  --operations=10000 \
  --rate_limit=150 \
  --log_config=/tmp/hammercfg/hammer.cfg \
  --testdata_dir=./trillian/testdata/
```

### With real HTTPS certificates

We'll run a TesseraCT instance and copy certificates from an existing RFC6962
log to it.  It uses the [ct_hammer tool from certificate-transparency-go](https://github.com/google/certificate-transparency-go/blob/master/trillian/integration/ct_hammer/main.go).

First, save the source log URI:

```bash
export SRC_LOG_URI=https://ct.googleapis.com/logs/xenon2022
```

Then, get fetch the roots the source logs accepts, and edit configs accordingly.
Two roots that TesseraCT cannot load with the [internal/lax509](/internal/lax509/)
library need to be removed.

```bash
gcloud secrets versions access $TESSERACT_SIGNER_ECDSA_P256_PUBLIC_KEY_ID > /tmp/log_public_key.pem
LOG_PUBLIC_KEY_DER=$(openssl pkey -pubin -in /tmp/log_public_key.pem -outform DER | xxd -i -c1000 | sed s/\,\ 0/\\\\/g | sed s/^..0x/\\\\x/g)
mkdir -p /tmp/hammercfg
go run github.com/google/certificate-transparency-go/client/ctclient@master get-roots --log_uri=${SRC_LOG_URI} --text=false | \
awk \
  '/-----BEGIN CERTIFICATE-----/{c=1; pem=$0; show=1; next}
   c{pem=pem ORS $0}
   /-----END CERTIFICATE-----/{c=0; if(show) print pem}
   ($0=="MIIFxDCCBKygAwIBAgIBAzANBgkqhkiG9w0BAQUFADCCAUsxGDAWBgNVBC0DDwBT"||$0=="MIIFVjCCBD6gAwIBAgIQ7is969Qh3hSoYqwE893EATANBgkqhkiG9w0BAQUFADCB"){show=0}' \
   > /tmp/hammercfg/roots.pem
cat > /tmp/hammercfg/hammer.cfg << EOF
config {
  roots_pem_file: "/tmp/hammercfg/roots.pem"
  public_key: {
    der: "$LOG_PUBLIC_KEY_DER"
  }
}
EOF
```

Run TesseraCT with the same roots:

```bash
go run ./cmd/gcp/ \
  --bucket=${GOOGLE_PROJECT}-${TESSERA_BASE_NAME}-bucket \
  --spanner_db_path=projects/${GOOGLE_PROJECT}/instances/${TESSERA_BASE_NAME}/databases/${TESSERA_BASE_NAME}-db \
  --roots_pem_file=/tmp/hammercfg/roots.pem \
  --origin=${TESSERA_BASE_NAME} \
  --spanner_antispam_db_path=projects/${GOOGLE_PROJECT}/instances/${TESSERA_BASE_NAME}/databases/${TESSERA_BASE_NAME}-antispam-db \
  --signer_public_key_secret_name=${TESSERACT_SIGNER_ECDSA_P256_PUBLIC_KEY_ID} \
  --signer_private_key_secret_name=${TESSERACT_SIGNER_ECDSA_P256_PRIVATE_KEY_ID} \
  --otel_project_id=${GOOGLE_PROJECT} \
  -v=3
```

Clone the [certificate-transparency-go](https://github.com/google/certificate-transparency-go) repo, and from there run `ct_hammer` in a different terminal:

```bash
go run ./trillian/integration/ct_hammer \
  --ct_http_servers=localhost:6962/${TESSERA_BASE_NAME} \
  --max_retry=2m \
  --invalid_chance=0 \
  --get_sth=0 \
  --get_sth_consistency=0 \
  --get_proof_by_hash=0 \
  --get_entries=0 \
  --get_roots=0 \
  --get_entry_and_proof=0 \
  --max_parallel_chains=4 \
  --skip_https_verify=true \
  --operations=10000 \
  --rate_limit=150 \
  --log_config=/tmp/hammercfg/hammer.cfg \
  --src_log_uri=${SRC_LOG_URI}
```
