# POSIX TesseraCT

This directory contains a `static-ct` server which uses
[Tessera's POSIX backend](https://pkg.go.dev/github.com/transparency-dev/tessera/storage/posix#section-readme)
for storing the log.

In this document, you will find information specific to this POSIX
implementation. 

[IPng Networks](https://ipng.ch/) runs [production CT logs](https://ipng.ch/s/ct/)
using the POSIX implementation. You can read more about their journey, together
with how they've configured TesseraCT and their serving infrastructure in the
great blog posts they published:
[1](https://ipng.ch/s/articles/2025/07/26/certificate-transparency-part-1-tesseract/),
[2](https://ipng.ch/s/articles/2025/08/10/certificate-transparency-part-2-sunlight/),
[3](https://ipng.ch/s/articles/2025/08/24/certificate-transparency-part-3-operations/). \
[Cheese](https://git.ipng.ch/certificate-transparency/cheese) is a tool they
wrote to ease deployment.

You can find more information about TesseraCT in general in the
[architecture design doc](/docs/architecture.md), and in TesseraCT's
[configuration guide](../).

## Filesystems

This binary, and the Tessera library it uses, relies on POSIX filesystem semantics,
including atomic operations, in order to function correctly. As such, it expects to find
a POSIX-compliant filesystem at the location provided via the `--storage_dir` flag.

`ZFS` has been tested and found to work well, other POSIX-compliant filesystems _should_
work too, `CephFS` may work, but `NFS` will almost certainly not.

> [!WARNING]
> Attempting to use a filesystem which does not provide POSIX filesystem
> semantics is overwhelmingly likely to result in a broken log!


## Witnessing

> [!WARNING]
> Witnessing support is experimental at the moment - there is work actively underway
> in this space by the transparency community.
> Correspondingly, the interaction with the configured witnesses is currently hard-coded
> to fail-open in the event that insufficient witness cosignatures were acquired - i.e. 
> checkpoints will continue to be published with or without witness cosignatures.

This binary has support for interacting with [tlog-witness](https://c2sp.org/tlog-witness)
compliant witnesses. To enable this, pass the path of a file containing a witness policy
to the `--witness_policy_file` flag, and ensure that at least one file containing a
note-compatible `Ed25519` signer known to the configured witness(es) is provided via the
`--additional_signer` flag.

The witness policy file is expected to contain a text-based description of the policy in
the format described by https://git.glasklar.is/sigsum/core/sigsum-go/-/blob/main/doc/policy.md

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
  --roots_pem_file=deployment/live/gcp/static-ct-staging/logs/arche2025h1/roots.pem \
  --v=1
```

The server should now be listening on port `:6962` to handle the _submission URLs_ from
the static-ct API. The _monitoring URLs_ are not handled via HTTP directly, and may be
served from the filesystem in `storage_dir`.

You can try "preloading" the log with the contents of another CT log, e.g.:

```bash
go run github.com/google/certificate-transparency-go/preload/preloader@master \
  --target_log_uri=http://localhost:6962/ \
  --source_log_uri=https://ct.googleapis.com/logs/eu1/xenon2025h1/ \
  --num_workers=2 \
  --start_index=130000 \
  --parallel_fetch=2 \
  --parallel_submit=512 \
  --v=1
```

Note that running this command a second time may show a lot of errors with
`HTTP status 429 Too Many Requests`; this is protection against too many duplicate
entries being sent to the log.
Use a larger `start_index` to avoid submitting duplicate entries and running into
this behaviour.
