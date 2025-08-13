# POSIX static-ct server

This directory contains a `static-ct` server which uses
[Tessera's POSIX backend](https://pkg.go.dev/github.com/transparency-dev/tessera/storage/posix#section-readme)
for storing the log.

## Filesystems

This binary, and the Tessera library it uses, relies on POSIX filesystem semantics,
including atomic operations, in order to function correctly. As such, it expects to find
a POSIX-compliant filesystem at the location provided via the `--storage_dir` flag.

`ZFS` has been tested and found to work well, other POSIX-compliant filesystems _should_
work too, `CephFS` may work, but `NFS` will almost certainly not.

> [!WARNING]
> Attempting to use a filesystem which does not provide POSIX filesystem
> semantics is overwhelmingly likely to result in a broken log!


## Codelab

Generate an ECDSA key like so:

```bash
openssl ecparam -name prime256v1 -genkey -noout -out test-ecdsa-priv.pem 
```

And then start a log with the following command:

```bash
go run ./cmd/tesseract/posix/ \
  --private_key=./test-ecdsa-priv.pem \
  --origin=example.com/test-ecdsa \
  --storage_dir=/tmp/ecdsa_log \
  --roots_pem_file=deployment/live/gcp/static-ct-staging/logs/arche2025h1/roots.pem
```

The server should now be listening on port `:6962`.

You can try "preloading" the log with the contents of another CT log, e.g.:

```bash
go run github.com/google/certificate-transparency-go/preload/preloader@master \
  --target_log_uri=http://localhost:6962/example.com/test-ecdsa \
  --source_log_uri=https://ct.googleapis.com/logs/eu1/xenon2025h1/ \
  --num_workers=2 \
  --start_index=130000 \
  --parallel_fetch=2 \
  --parallel_submit=512

```
