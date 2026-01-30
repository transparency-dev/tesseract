# TesseraCT binaries

This directory contains TesseraCT binaries for [AWS or Vanilla S3+MySQL](./aws/),
[GCP](./gcp/), and [POSIX](./posix/).

It also contains Dockerfiles to build these binaries, and to
bundle them with the CA roots they accept.

## TesseraCT configuration

This file explains how to configure TesseraCT binaries.

It contains two main parts:

 1. [Chain lifecycle](#life-of-a-chain): with settings impacting how submission
 are processed
 2. [Setup](#setup): with instructions on how to setup TesseraCT resources

You can find information about implementation-specific settings in each
corresponding subdirectory.

### Life of a chain

When TesseraCT receives a new chain submission, it is first
[filtered](#chain-filtering), and is then sent to the Tessera library where it goes
through the [process of being added to the log](#adding-to-the-log).
The chain may be [deduplicated using TesseraCT's and Tessera's antispam features](#antispam).

#### Chain filtering

Which chains will be accepted by TesseraCT can be controlled by setting the
following flags to filter submissions.

##### Roots

TesseraCT MUST be configured with a set of roots that it trusts, and will only
accept certificates that chain up to one of these roots. There are two
mechanisms to set these roots:

1. Manually, via a PEM file. Use the `root_pem_file` flag to configure its path.
Roots from this file are read once at startup, and remain trusted thereafter.
2. Automatically, from a remote endpoint like [CCADB's](https://ccadb.my.salesforce-sites.com/ccadb/RootCACertificatesIncludedByRSReportCSV).
The URL of that endpoint is set via `roots_remote_fetch_url`. Roots are first
fetched at startup, and then every `roots_remote_fetch_interval`. Each time
roots are fetched from this remote endpoint, newly found roots become trusted.
Newly found roots are backed up in the log's storage, under `roots/`. Roots are
never removed from this directory. Roots in the `roots/` directory are loaded
once at startup, and remain trusted thereafter. This backup mechanism ensures
that the log can start with all its roots, even if the remote endpoint is down.

##### Other filtering

- `reject_expired`: If true, TesseraCT rejects expired certificates.
- `reject_unexpired`: If true, TesseraCT rejects certificates that are either
currently valid or not yet valid.
- `ext_key_usage`: A list of comma separated [Extended Key Usages (EKU) from x509](https://pkg.go.dev/crypto/x509#ExtKeyUsage).
Certificates which DO NOT include one or more of these EKUs to be accepted. If
empty, no filtering is applied.
- `reject_extension`: A comma separated list of X.509 extension OIDs, in dotted
string form (e.g. `2.3.4.5`). Certificates that include one or more of these extensions
will be rejected.
- `not_after_start`: Start of the range of acceptable NotAfter values,
inclusive. Leaving this unset or empty implies no lower bound to the range.
RFC3339 UTC format, e.g: `2024-01-02T15:04:05Z`.
- `not_after_limit`: Cut off point of NotAfter dates - only notAfter dates
strictly _before_ `not_after_limit` will be accepted. Leaving this unset or empty
means no upper bound on the accepted range. RFC3339 UTC format, e.g:
`2024-01-02T15:04:05Z`.
- `accept_sha1_signing_algorithms`: If true, allow chains that use SHA-1 based
signing algorithms. This flag is a temporary solution to allow chains submitted
by Chrome's Merge Delay Monitor Root. It will eventually be removed and chains
using such algorithms will be rejected.
- `rate_limit_old_not_before`: This optional flag can be set define a limit on how
many "old" certificates and precertificates will be accepted per second.
The flag value should be of the form `<age>:<limit>`, where `<limit>` is a
per-second rate limit, and `<age>` defines how old a given submission's
`notBefore` date must be for that submission to be subject to the rate limit.
`<age>` must be formatted per Go's [time.ParseDuration](https://pkg.go.dev/time#ParseDuration),
and `<limit>` is a positive real number.
E.g. `28h=500` means that a rate-limit of 500 submissions/s will be applied to any
certificate, or precertificate, whose `notBefore` date is at least 28 hours old
at the time of submission.

#### Adding to the log

Tessera stages entries submitted via `Add`, then [sequences them in a batch](#sequencing-and-batching),
assigning them with a durable sequence number. Asynchronously from this, entries
are integrated into the log (i.e durably written into the log together with
hashes committing to them), and then [published every checkpoint interval](#checkpoint-publication)
(i.e a checkpoint which commits to these new entries is published). TesseraCT
can optionally [wait for the full process to be done](#publication-awaiter)
before sending responses to clients.

##### Sequencing and Batching

The `batch_max_age` and `batch_max_size` flags control the maximum age and number
of entries in a single [sequencing batch](https://github.com/transparency-dev/tessera?tab=readme-ov-file#sequencing).
These flags affect the overall performance of TesseraCT. The correct values for
these flags depend on multiple factors, such as the driver used, the number
of TesseraCT servers or their steady QPS rate. Default values have been chosen sensibly
and should be fine to get started with, but we invite you to experiment with values
tailored to your setup.

##### Checkpoint publication

The interval between publishing new checkpoints is controlled with the `checkpoint_interval` and
`checkpoint_republish_interval` flags.

The `checkpoint_interval` flag controls the interval between checkpoints when the log is actively growing,
and the `checkpoint_republish_interval` controls the interval between checkpoints of the _same_ size
(i.e. where the log is not currently growing).

If, for some reason, it's necessary to entirely _disable_ republication of checkpoints, this can be
achieved by setting the `checkpoint_republish_flag` to zero.

Due to constraints imposed by underlying storage systems, these intervals have a lower limit. 
Tessera ensures that the configured interval is larger than any such lower limit during the initialization
process.

| Backend | Minimum Permitted Checkpoint Interval (ms) |
| ------- | ------------------------------------------ |
| AWS     | 1000                                       |
| GCP     | 1200                                       |

Note that when used in conjunction with `enable_publication_awaiter`, `checkpoint_interval`
directly impacts `add-*` request response time.

##### Publication awaiter

The `enable_publication_awaiter` flag enables [Tessera's publication awaiter](https://github.com/transparency-dev/tessera?tab=readme-ov-file#synchronous-publication):
when this flag is on, TesseraCT responds to `add-*` requests only after a
`checkpoint` committing to that entry has been published. When this flag is off,
TesseraCT sends responses to `add-*` requests as soon as Tessera has durably
assigned a sequence number to the corresponding entry. Such entries will
then get integrated and published asynchronously, which may or may not happen
before TesseraCT responds to the `add-*` request are sent, or after.

While this flag provides stronger guarantees to clients, it will likely lead to a
lower log throughput, an increased number of open sockets, and RAM usage.
Make sure that the `http_deadline` flag leaves enough time for requests
to be fully processed.

This flag is on by default.

##### Witnessing

Witnessing is a mechanism which provides security against split-view attacks.

With witnessing enabled, TesseraCT will attempt to gather counter-signatures from
the configured witnesses for each checkpoint it publishes.

> [!NOTE]
> The public witness network is currently experimental.

To enable witnessing:
1. Generate an Ed25519 note signer key, you can use `go run github.com/transparency-dev/serverless-log/cmd/generate_keys@HEAD` to do this.
   Note that the `log_origin` you provide SHOULD match the `origin` for your log.
2. Register your newly generated key with the public witness network by adding it to the config here: 
   https://github.com/transparency-dev/witness-network/blob/main/site/static/testing/log-list.1 .
3. Set an `additional_signer` flag on your TesseraCT binary to pass your new key (the exact flag name and
   value depend on the platform).
4. Create a `witness.policy` file to configure the set of witnesses to request signatures from (see below for
   more information), and provide the path to this file to TesseraCT using the `witness_policy_file` flag.

###### Policy file

TesseraCT's witness policy file uses the Sigsum
[Trust policy format](https://git.glasklar.is/sigsum/core/sigsum-go/-/blob/main/doc/policy.md) to describe
the set of witness signatures to fetch.

A very simple example policy which requests a cosignature from just one witness is shown below:

```
witness o1 transparency.dev/DEV:witness-little-garden+4b7fca75+AStusOxINQNUTN5Oj8HObRkh2yHf/MwYaGX4CPdiVEPM https://api.transparency.dev/dev/witness/little-garden 

quorum o1
```

#### Antispam

The `pushback_max_antispam_lag`, `rate_limit_dedup` and
`inmemory_antispam_cache_size` flags control how [TesseraCT's Antispam feature](/docs/architecture.md#antispam)
works, which itself is built on top of [Tessera's Antispam](https://github.com/transparency-dev/tessera?tab=readme-ov-file#antispam)
capabilities. It is composed of three main steps:

1. A process **asynchronous** with `add-*`, populates the antispam database
with entry indices as the log grows. This is handled by Tessera.
2. A call **synchronous** with **all** `add-*` to the antispam database to
identifies whether the submission is a duplicate of a previously accepted entry.
This is handled by Tessera.
3. If this entry is a duplicate, a last call **synchronous** with this duplicate
`add-*` call fetches the previous entry from the log to extract the information required
to recreate the previously issued SCT. This is handled by TesseraCT.

The `pushback_max_antispam_lag` flag controls the limit of how far behind the
current size of the tree the asynchronous process in `(1)` can fall.
When this value is exceeded, TesseraCT returns `429 - Too Many Requests` to
**all** `add-*` requests. This protects TesseraCT from accepting new entries
without being able to tell whether they are duplicates of recently integrated
entries.

The `inmemory_antispam_cache_size` flag controls the number of recently-added
entries which indexes are kept in memory.  This both makes subsequent some `(2)`
calls faster, and provides optimistic coverage for entries submitted _very_
recently and which have not yet been processed by the asynchronous process in
`(1)`.

The `rate_limit_dedup` flag rate limits how many concurrent `add-*` requests
identified as duplicates will be processed by the **synchronous** process in
`(3)` which fetches entries and extracts information required to build SCTs.
When this value is exceeded, TesseraCT returns `429 -Too Many Requests` to
subsequent **duplicate** `add-*` requests only. Non-duplicate `add-*` requests
are not impacted, and can still be processed. This limits the amount of
resources TesseraCT spends on servicing duplicate requests.

#### Garbage Collection

The `garbage_collection_interval` flag controls Tessera's Garbage Collection.
It controls how often Tessera scans for partial tiles and entry bundles, and
deletes them if a corresponding full tile or entry bundle has since been
published. Tessera automatically keeps track of the garbage collection progress
through the tree, keeping up with new entries but avoiding re-examining earlier
parts which have already been processed. To achieve this, it examines the log
sequentially in chunks, one chunk per `garbage_collection_interval`. It removes
any obsolete partial resources found within the current chunk. Currently, each
chunk is defined as up-to [100](https://github.com/search?q=repo%3Atransparency-dev%2Ftessera+maxBundlesPerRun&type=code)
entry bundles along with the vertical "slice" of the merkle tree which covers
these bundles.

Every `garbage_collection_interval`, Tessera pre-computes
the potential paths of [100](https://github.com/search?q=repo%3Atransparency-dev%2Ftessera+maxBundlesPerRun&type=code)
partial entry bundle and partial tile directories. It then attempts to delete them
one after another. If a path resolves to an existing partial entry bundle or tile
directory, it is deleted. Otherwise, nothing happens and it moves to the next one.

To ensure that garbage collection is cleaning the log faster than it grows, 
`100*256/garbage_collection_interval` must be higher than the log's throughput.

If garbage collection was disabled, and is later re-enabled, it will take
`(current_log_size - log_size_when_GC_was_disabled)/256/100*garbage_collection_interval` for garbage collection to catch
up.

### Setup

#### Origin and submission prefix

The origin used in Checkpoints is specified with the `origin` flag, and should
be derived from the submission prefix for the log as explained below.

The submission prefix of a log has two parts, the host and the serving path:
`https://$HOST/$PATH_PREFIX/ct/v1/...`.

As per [static-ct-api specs](https://c2sp.org/static-ct-api):
> the origin line
MUST be the submission prefix of the log as a schema-less URL with no trailing
slashes.

Use your upstream serving infrastructure to make sure that requests to these
URLs are correctly routed to a TesseraCT server. TesseraCT will serve the
requests it receives regardless of their `$HOST`. However, it will expect
requests to be received on `$PATH_PREFIX`, as specified by the `path_prefix` flag.

#### Memory considerations

TesseraCT's memory footprint is directly impacted by:

- `inmemory_antispam_cache_size`: the number of recently-added entries which indexes
are kept in memory
- `batch_max_size`: the number of entries that are kept in memory before
sequenced in a batch
- [The number of cached issuers keys](https://github.com/transparency-dev/tesseract/blob/main/storage/storage.go)
- `enable_publication_awaiter` and `http_deadline`: they impact the number of
concurrent requests, hence the amount of RAM being used

#### Frontend redundancy

For added availability multiple TesseraCT instances can run concurrently with the
same Tessera resources. Adding more instances will not necessarily increase
performance, the primary goal of concurrency is to allow for better
availability.

Multiple TesseraCT server can run concurrently on AWS or GCP. It is also
possible to run concurrent servers with the POSIX and Vanilla S3 + MySQL
implementations, **but** this will depend on the underlying storage systems
being used.

#### Running multiple logs

To run multiple logs, run multiple TesseraCT instances configured with different
Tessera resources. For simplicity, it is not possible to serve multiple logs
from a single TesseraCT instance.
