# AWS and Vanilla S3+MySQL

This binary is primarily intended to run on AWS infrastructure, but may also be used
on-prem with local S3 and MySQL services.

For AWS-specific information on how to run this binary, see the documentation under
[/deployment](/deployment).

## Vanilla S3+MySQL support

Setting up S3 and MySQL infrastructure is out of scope for this document, but the binary
has been tested with both local MinIO and SeaweedFS instances along with a local MySQL
instance.

Configuring the binary to use these services rather than looking for AWS-specific services
is mostly achieved through the use of
[environment variables](https://docs.aws.amazon.com/sdk-for-go/v2/developer-guide/configure-gosdk.html#:~:text=profile%20you%20specify.-,Environment%20Variables,-By%20default%2C%20the)
and flags.

### S3+MySQL codelab

Below is a codelab that will guide you to start and use a TesseraCT binary. It
assumes the presence of:

- A pre-configured S3 service:
  + listening on `http://s3-server:9000`
  + with a provisioned access key `tesseract-s3` and secret `trustno1`,
  + and a configured bucket named `tesseract-test` which is publicly readable, and only writable by the `tesseract-s3` user.
- A pre-configured MySQL server:
  + running on a host called `mysql-server`
  + with a provisioned user called `tesseract-mysql` with password `tiger`.
  + and two empty database instances (named `tesseract_test_db` and `tesseract_test_antispam_db`) for which the `tesseract-mysql` user has create, read, and write privileges for all tables.

First, we need to have generated private keys for the log - this only needs doing once per log instance:

```bash
openssl ecparam -name prime256v1 -genkey -noout -out testlog-priv-key.pem
openssl ec -in testlog-priv-key.pem -pubout > testlog-pub-key.pem
```

Then set some environment variables and start the binary:

```bash
export ORIGIN=example.com/testlog
export AWS_DEFAULT_REGION="us-east-1"
export AWS_ACCESS_KEY_ID="tesseract-s3"
export AWS_SECRET_ACCESS_KEY="trustno1"
export AWS_ENDPOINT_URL_S3="http://s3-server:9000/"
export LOG_PORT=6962
go run ./cmd/tesseract/aws \
  --http_endpoint=":${LOG_PORT}" \
  --origin=${ORIGIN} \
  --bucket=tesseract-test \
  --db_host=mysql-server \
  --db_user=tesseract-mysql \
  --db_password=tiger \
  --db_name=tesseract_test_db \
  --antispam_db_name=tesseract_test_antispam_db \
  --signer_public_key_file=testlog-pub-key.pem \
  --signer_private_key_file=testlog-priv-key.pem \
  --s3_use_path_style=true \
  --roots_pem_file=internal/hammer/testdata/test_root_ca_cert.pem
```

A quick test to check that things have started ok can be made by looking for the `checkpoint` file in the
S3 bucket:

```bash
curl http://s3-server:9001/tesseract-test/checkpoint
example.com/testlog
0
47DEQpj8HBSa+/TImW+5JCeuQeRkm5NMpJWZG3hSuFU=

— example.com/testlog zqR9XAAAAZij9qPeBAMARzBFAiBL/FQimRIlQ9898LXClfQs+Lnx+iUiKemU8Vy0vZTdcQIhANfdCSKE3afv/PyRbgOj/jiDe65DSTLGh4ir67qusqMB
```

You can further test that everything is working ok using the [hammer](/internal/hammer) tool:

```bash
go run ./internal/hammer \
  --log_url=${AWS_ENDPOINT_URL}/tessera/ \
  --write_log_url=http://localhost:${LOG_PORT} \
  --log_public_key=$(openssl ec -pubin -inform PEM -in testlog-pub-key.pem -outform der | base64 -w 0) \
  --num_writers=1000 \
  --max_write_ops=500 \
  --dup_chance=0.01 \
  --leaf_write_goal=100000 \
  --origin=${ORIGIN}
```
