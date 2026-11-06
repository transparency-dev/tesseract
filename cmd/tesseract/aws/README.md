# TesseraCT on AWS and Vanilla S3+MySQL

This binary is primarily intended to run on AWS infrastructure, but may also be used
on-prem with local S3 and MySQL services.

In this document, you will find information specific to this AWS and S3+MySQL
implementation. You can find more information about TesseraCT in general in the
[architecture design doc](/docs/architecture.md), and in TesseraCT's
[configuration guide](../).

For AWS-specific information on how to run this binary, see the documentation under
[/deployment](/deployment).

## AWS

TesseraCT expects the databases configured with the `db_name` and
`antispam_db_name` flags to be located in the same Aurora DB cluster.

## OpenTelemetry

The AWS binary configures OpenTelemetry metrics, tracing, and Go runtime metrics
using the same environment-driven exporters as the POSIX binary. The service name
is the log's `--origin`, and the service namespace is `tesseract`.
`OTEL_RESOURCE_ATTRIBUTES` can add or override resource attributes.

For OTLP export to a collector, for example:

```bash
export OTEL_METRICS_EXPORTER=otlp
export OTEL_TRACES_EXPORTER=otlp
export OTEL_EXPORTER_OTLP_PROTOCOL=grpc
export OTEL_EXPORTER_OTLP_ENDPOINT=https://collector.example.com:4317
```

Set `--trace_fraction` to a value from `0` to `1` for the fraction of root traces
to sample. The default is `0`; periodic Tessera root spans marked with
`tessera.periodic` are still sampled. Child spans follow their parent's sampling
decision. Set `OTEL_TRACES_EXPORTER=none` to disable trace export, or
`OTEL_METRICS_EXPORTER=none` to disable metric export. Exporter selection defaults
to OTLP when unset. This does not automatically configure CloudWatch or X-Ray.

### Prometheus

To expose a Prometheus scrape endpoint instead of pushing metrics over OTLP:

```bash
export OTEL_METRICS_EXPORTER=prometheus
export OTEL_EXPORTER_PROMETHEUS_HOST=127.0.0.1
export OTEL_EXPORTER_PROMETHEUS_PORT=9464
export OTEL_TRACES_EXPORTER=none
```

Metrics are available at `http://127.0.0.1:9464/metrics` on a separate listener,
not the CT HTTP endpoint. The default host is `localhost` and the default port is
`9464`. Tracing is configured independently and can remain enabled if desired.

Remote scrapers require a non-loopback bind address such as `0.0.0.0`, along with
appropriate container port mappings and security group rules. Restrict access to
trusted scrapers: the metrics listener does not provide authentication or TLS.
Use a TLS/authentication proxy where required, validate deployment inputs, and
inject exporter credentials securely rather than hardcoding secrets.

Telemetry providers shut down after HTTP requests drain on normal termination.
As in POSIX, startup failures that call `os.Exit` bypass deferred cleanup, and
telemetry shutdown does not have a separate timeout.

## Vanilla S3+MySQL support

Setting up S3 and MySQL infrastructure is out of scope for this document, but
the binary has been tested with both local MinIO and SeaweedFS instances along
with a local MySQL instance.

Configuring the binary to use these services rather than looking for
AWS-specific services is mostly achieved through the use of
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

First, we need to have generated private keys for the log - this only needs
doing once per log instance:

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
  --roots_pem_file=internal/hammer/testdata/test_root_ca_cert.pem \
  --slog_level=-4
```

A quick test to check that things have started ok can be made by looking for the
`checkpoint` file in the S3 bucket:

```bash
curl http://s3-server:9001/tesseract-test/checkpoint
```

Here's an example response:

```bash
example.com/testlog
0
47DEQpj8HBSa+/TImW+5JCeuQeRkm5NMpJWZG3hSuFU=

— example.com/testlog zqR9XAAAAZij9qPeBAMARzBFAiBL/FQimRIlQ9898LXClfQs+Lnx+iUiKemU8Vy0vZTdcQIhANfdCSKE3afv/PyRbgOj/jiDe65DSTLGh4ir67qusqMB
```

You can further test that everything is working ok using the [hammer](/internal/hammer)
tool:

```bash
go run ./internal/hammer \
  --log_url=${AWS_ENDPOINT_URL}/tessera/ \
  --write_log_url=http://localhost:${LOG_PORT} \
  --log_public_key=$(openssl ec -pubin -inform PEM -in testlog-pub-key.pem -outform der | base64 -w 0) \
  --num_writers=1000 \
  --max_write_ops=500 \
  --dup_chance=0.01 \
  --leaf_write_goal=100000 \
  --origin=${ORIGIN} \
  --slog_level=-4
```
