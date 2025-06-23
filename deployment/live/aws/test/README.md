# AWS TesseraCT Test Environment

> [!CAUTION]
> 
> This test environment creates real Amazon Web Services resources running in your account. They will cost you real money. For the purposes of this demo, it is strongly recommended that you create a new account so that you can easily clean up at the end.

## Prerequisites

You'll need to have a EC2 Amazon Linux VM running in the same AWS account that you can SSH to,
with [Go](https://go.dev/doc/install) and 
[terragrunt](https://terragrunt.gruntwork.io/docs/getting-started/install/) 
installed, and your favourite terminal multiplexer.

## Overview

This config uses the [aws/tesseract/test](/deployment/modules/aws/tesseract/test) module to
define a test environment to run TesseraCT, backed by Trillian Tessera.

At a high level, this environment consists of:
- One RDS Aurora MySQL database
- One S3 Bucket
- Two secrets (log public key and private key for signing digests) in AWS Secrets Manager

## Manual deployment 

Authenticate with a role that has sufficient access to create resources.
For the purpose of this test environment, and for ease of demonstration, we'll use the
`AdministratorAccess` role, and authenticate with `aws configure sso`.

**DO NOT** use this role to run any production infrastructure, or if there are
*other services running on your AWS account.

```sh
[ec2-user@ip static-ct]$ aws configure sso
SSO session name (Recommended): greenfield-session
SSO start URL [None]: https://console.aws.amazon.com/ // unless you use a custom signin console
SSO region [None]: us-east-1
SSO registration scopes [sso:account:access]:
Attempting to automatically open the SSO authorization page in your default browser.
If the browser does not open or you wish to use a different device to authorize this request, open the following URL:

https://device.sso.us-east-1.amazonaws.com/

Then enter the code:

<REDACTED>
There are 4 AWS accounts available to you.
Using the account ID <REDACTED>
The only role available to you is: AdministratorAccess
Using the role name "AdministratorAccess"
CLI default client Region [None]: us-east-1
CLI default output format [None]:
CLI profile name [AdministratorAccess-<REDACTED>]:

To use this profile, specify the profile name using --profile, as shown:

aws s3 ls --profile AdministratorAccess-<REDACTED>
```

Set the required environment variables:

```bash
export AWS_REGION={VALUE} # e.g: us-east-1
export AWS_PROFILE=AdministratorAccess-<REDACTED>
```

Terraforming the account can be done by:
  1. `cd` to [/deployment/live/aws/test/](/deployment/live/aws/test/) to deploy/change.
  2. Run `terragrunt apply`. If this fails to create the antispam database,
  connect the RDS instance to your VM using the instructions below, and run
  `terragrunt apply` again.
  
Store the Aurora RDS database and S3 bucket information into the environment variables:

```sh
export TESSERACT_DB_HOST=$(terragrunt output -raw rds_aurora_cluster_endpoint)
export TESSERACT_DB_PASSWORD=$(aws secretsmanager get-secret-value --secret-id $(terragrunt output -json rds_aurora_cluster_master_user_secret | jq --raw-output .[0].secret_arn) --query SecretString --output text | jq --raw-output .password)
export TESSERACT_BUCKET_NAME=$(terragrunt output -raw s3_bucket_name)
export TESSERACT_SIGNER_ECDSA_P256_PUBLIC_KEY_ID=$(terragrunt output -raw ecdsa_p256_public_key_id)
export TESSERACT_SIGNER_ECDSA_P256_PRIVATE_KEY_ID=$(terragrunt output -raw ecdsa_p256_private_key_id)
```

Connect the VM and Aurora database following [these instructions](https://docs.aws.amazon.com/AWSEC2/latest/UserGuide/tutorial-ec2-rds-option1.html#option1-task3-connect-ec2-instance-to-rds-database), it takes a few clicks in the UI.

## Run TesseraCT

### With fake chains

On the VM, run the following command to bring up TesseraCT:

```bash
go run ./cmd/aws \
  --http_endpoint=localhost:6962 \
  --roots_pem_file=./internal/testdata/fake-ca.cert \
  --origin=test-static-ct \
  --bucket=${TESSERACT_BUCKET_NAME} \
  --db_name=tesseract \
  --db_host=${TESSERACT_DB_HOST} \
  --db_port=3306 \
  --db_user=tesseract \
  --db_password=${TESSERACT_DB_PASSWORD} \
  --antispam_db_name=antispam_db \
  --signer_public_key_secret_name=${TESSERACT_SIGNER_ECDSA_P256_PUBLIC_KEY_ID} \
  --signer_private_key_secret_name=${TESSERACT_SIGNER_ECDSA_P256_PRIVATE_KEY_ID}
```

In a different terminal you can either mint and submit certificates manually, or
use the [ct_hammer
tool](https://github.com/google/certificate-transparency-go/blob/master/trillian/integration/ct_hammer/main.go)
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
go run github.com/google/certificate-transparency-go/client/ctclient@master upload --cert_chain=/tmp/httpschain/chain.pem --skip_https_verify --log_uri=http://localhost:6962/test-static-ct
```

#### Automatically generate chains

Retrieve the log public key in PEM format, convert it to DER format, and generate the hammer configuration file.

```bash
aws secretsmanager get-secret-value --secret-id test-static-ct-ecdsa-p256-public-key --query SecretString --output text > /tmp/log_public_key.pem
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

Finally, submit the chain to TesseraCT:

```bash
go run github.com/google/certificate-transparency-go/trillian/integration/ct_hammer@master \
  --ct_http_servers=localhost:6962/test-static-ct \
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
log to it.  It uses the [preloader tool from certificate-transparency-go](https://github.com/google/certificate-transparency-go/blob/master/preload/preloader/preloader.go).

First, save the source log URI:

```bash
export SOURCE_LOG_URI=https://ct.googleapis.com/logs/xenon2022
```

Then, get fetch the roots the source logs accepts, and edit configs accordingly.
Two roots that TesseraCT cannot load with the [internal/lax509](/internal/lax509/)
library need to be removed.

```bash
go run github.com/google/certificate-transparency-go/client/ctclient@master get-roots --log_uri=${SRC_LOG_URI} --text=false | \
awk \
  '/-----BEGIN CERTIFICATE-----/{c=1; pem=$0; show=1; next}
   c{pem=pem ORS $0}
   /-----END CERTIFICATE-----/{c=0; if(show) print pem}
   ($0=="MIIFxDCCBKygAwIBAgIBAzANBgkqhkiG9w0BAQUFADCCAUsxGDAWBgNVBC0DDwBT"||$0=="MIIFVjCCBD6gAwIBAgIQ7is969Qh3hSoYqwE893EATANBgkqhkiG9w0BAQUFADCB"){show=0}' \
   > /tmp/log_roots.pem
```

Run TesseraCT with the same roots:

```bash
go run ./cmd/aws \
  --http_endpoint=localhost:6962 \
  --roots_pem_file=/tmp/log_roots.pem \
  --origin=test-static-ct \
  --bucket=${TESSERACT_BUCKET_NAME} \
  --db_name=tesseract \
  --db_host=${TESSERACT_DB_HOST} \
  --db_port=3306 \
  --db_user=tesseract \
  --db_password=${TESSERACT_DB_PASSWORD} \
  --antispam_db_name=antispam_db \
  --signer_public_key_secret_name=${TESSERACT_SIGNER_ECDSA_P256_PUBLIC_KEY_ID} \
  --signer_private_key_secret_name=${TESSERACT_SIGNER_ECDSA_P256_PRIVATE_KEY_ID}
```

In a different terminal, run `preloader` to submit certificates from another log to TesseraCT.

```bash
go run github.com/google/certificate-transparency-go/preload/preloader@master \
  --target_log_uri=http://localhost:6962/${TESSERA_BASE_NAME} \
  --source_log_uri=${SOURCE_LOG_URI} \
  --num_workers=8 \
  --parallel_fetch=4 \
  --parallel_submit=4
```

Since the source and destination log [might not be configured the exact same set of roots](/internal/lax509/README.md#Chains), it is expected to see errors when submitting a certificate chaining to a missing root. This is what the error would look like:

```
W0623 11:57:05.122711    6819 handlers.go:168] test-static-ct: AddPreChain handler error: failed to verify add-chain contents: chain failed to validate: x509: certificate signed by unknown authority (possibly because of "x509: cannot verify signature: insecure algorithm SHA1-RSA" while trying to verify candidate authority certificate "Merge Delay Monitor Root")
```

## Cleanup

> [!IMPORTANT]  
> Do not forget to delete all the resources to avoid incuring any further cost
> when you're done using the log. The easiest way to do this, is to [close the account](https://docs.aws.amazon.com/accounts/latest/reference/manage-acct-closing.html).
> If you prefer to delete the resources with `terragrunt destroy`, bear in mind
> that this command might not destroy all the resources that were created (like
> the S3 bucket or DynamoDB instance Terraform created to store its state for
> instance). If `terragrunt destroy` shows no output, run
> `terragrunt destroy --terragrunt-log-level debug --terragrunt-debug`.
