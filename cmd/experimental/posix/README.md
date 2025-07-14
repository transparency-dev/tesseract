# POSIX static-ct server

This directory contains an experimental `static-ct` server which uses Tessera's
POSIX backend for storing the log.  It's not yet ready to be used as a
production log!

## Running

Generate an ECDSA key like so:

```bash
$ openssl ecparam -name prime256v1 -genkey -noout -out test-ecdsa-priv.pem 
```

And then start a log with the following command:

```bash
$ go run ./cmd/experimental/posix/ \
  --private_key=./test-ecdsa-priv.pem \
  --origin=example.com/test-ecdsa \
  --storage_dir=/tmp/ecdsa_log \
  --roots_pem_file=deployment/live/gcp/static-ct-staging/logs/arche2025h1/roots.pem
badger 2025/07/09 19:28:48 INFO: All 2 tables opened in 0s
badger 2025/07/09 19:28:48 INFO: Discard stats nextEmptySlot: 0
badger 2025/07/09 19:28:48 INFO: Set nextTxnTs to 79
badger 2025/07/09 19:28:48 INFO: Deleting empty file: /tmp/ecdsa_log/.state/antispam/000003.vlog
I0709 19:28:48.980391 1398578 main.go:95] **** CT HTTP Server Starting ****
```

The server should now be listening on port `:6962`.

You can try "preloading" the log with the contents of another CT log, e.g.:

```bash
go run github.com/google/certificate-transparency-go/preload/preloader@master \
  --target_log_uri=http://localhost:6962/example.com/test-ecdsa \
  --source_log_uri=https://ct.googleapis.com/logs/eu1/xenon2025h2/ \
  --num_workers=2 \
  --start_index=130000 \
  --parallel_fetch=2 \
  --parallel_submit=512

```
