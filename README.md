# :deciduous_tree: TesseraCT

[![Go Report Card](https://goreportcard.com/badge/github.com/transparency-dev/tesseract)](https://goreportcard.com/report/github.com/transparency-dev/tesseract)
[![OpenSSF Scorecard](https://api.scorecard.dev/projects/github.com/transparency-dev/tesseract/badge)](https://scorecard.dev/viewer/?uri=github.com/transparency-dev/tesseract)
[![Slack Status](https://img.shields.io/badge/Slack-Chat-blue.svg)](https://transparency-dev.slack.com/)

TesseraCT is a [Certificate Transparency (CT)](https://certificate.transparency.dev/)
log implementation. It implements [static-ct-api](https://c2sp.org/static-ct-api)
using the [Tessera](https://github.com/transparency-dev/tessera)
library to store data, and is aimed at running production-grade CT logs.

At the moment, TesseraCT can run on GCP, AWS, a POSIX filesystems, or on some
S3-compatible systems alongside a SQL database with [different levels of maturity](#mega-status).

## Table of contents

[:mega: Status](#mega-status) \
[:motorway: Roadmap](#motorway-roadmap) \
[:joystick: Usage](#joystick-usage) \
[:test_tube: Public test instances](#test_tube-public-test-instances) \
[:card_index_dividers: Repository structure](#card_index_dividers-repository-structure) \
[:raising_hand: FAQ](#raising_hand-faq) \
[:troll: History](#troll-history) \
[:wrench: Contributing](#wrench-contributing) \
[:page_facing_up: License](#page_facing_up-license) \
[:wave: Contact](#wave-contact)

## :mega: Status

TesseraCT is under active development, and will reach alpha in 2025Q3 🚀.

|Platform         |Architecture            |Our use-case          |Performance|Binary                                  |Deployment                                                      |
|-----------------|------------------------|----------------------|-----------|----------------------------------------|----------------------------------------------------------------|
|GCP              |Spanner + GCS + MIG     |public staging logs   |TODO       |[gcp](/cmd/tesseract/gcp/main.go)       |[doc](/deployment/live/gcp/static-ct-staging/logs/arche2025h1/) |
|GCP              |Spanner + GCS + CloudRun|continuous integration|TODO       |[gcp](/cmd/tesseract/gcp/main.go)       |[example](/deployment/live/gcp/static-ct/logs/ci)               |
|GCP              |Spanner + GCS + VM      |codelab, load tests   |TODO       |[gcp](/cmd/tesseract/gcp/main.go)       |[doc](/deployment/live/gcp/test/)                               |
|AWS              |RDS + S3 + ECS          |continuous integration|TODO       |[aws](/cmd/tesseract/aws/main.go)       |[example](/deployment/live/aws/conformance/ci/)                 |
|AWS              |RDS + S3 + VM           |codelab, load tests   |TODO       |[aws](/cmd/tesseract/aws/main.go)       |[doc](/deployment/live/aws/test/)                               |
|POSIX            |ZFS + VM                |one-off test          |NA         |[posix](/cmd/experimental/posix/main.go)|[doc](/cmd/experimental/posix/)                                 |
|Custom S3 + MySQL|MinIO + MySQL + VM      |one-off test          |NA         |[aws](/cmd/tesseract/aws/main.go)       |[doc](/docs/architecture/NONCLOUD.md)                           |

These deployments come with different levels of maturity depending on
our use-case.
We dedicated most of our time to the GCP with Spanner + GCS + MIG implementation
since we use it for our [public staging logs](#test_tube-public-test-instances).

We'd love to [hear your feedback](#wave-contact) on any of these implementations,
no matter wihch status it is at.

Read the FAQ to understand [why we chose these platforms](#why-these-platforms).

## :motorway: Roadmap

Our objective is to allow log operators to run production static-ct-api CT logs
starting with [temporal shards](https://googlechrome.github.io/CertificateTransparency/log_policy.html#temporal-sharding)
covering 2026 onwards.

At the moment, we are aiming for Beta in 2025Q3, and GA by the end of 2025.

|  #  | Step                                           | Status             | Target release |
| --- | ---------------------------------------------- | ------------------ | ---------------|
|  1  | Storage for GCP, AWS, and POSIX                | :white_check_mark: | alpha          |
|  2  | Lightweight CT compatible x509 fork            | :white_check_mark: | alpha          |
|  3  | static-ct-api APIs                             | :white_check_mark: | alpha          |
|  4  | Basic Antispam                                 | :white_check_mark: | alpha          |
|  5  | Monitoring and metrics                         | :white_check_mark: | alpha          |
|  6  | Secure key management [#219](issues/219)       | :hammer:           | beta           |
|  7  | Witnessing [#443](issues/443)                  | :hammer:           | beta           |
|  8  | Structured logging [#346](issues/346)          | :hammer:           | beta           |
|  9  | CCADB based root update [#212](issues/212)     | :hammer:           | beta           |
|  10 | Client                                         | :hammer:           | 1.0            |
|  11 | Stable APIs                                    | :hammer:           | 1.0            |

Current public library APIs are unlikely to change in any significant way,
however the API is subject to minor breaking changes until we tag 1.0.
Any feedback is welcome.

If you're interested in additional features, [get in touch](#wave-contact).

## :joystick: Usage

### Getting Started

The most hands-on place to start is with one of the
[GCP](./deployment/live/gcp/test),[AWS](./deployment/live/aws/test), or
[POSIX](/cmd/tesseract/posix#codelab) codelabs. These codelabs will guide you
through bringing up your own test TesseraCT deployment.

We also run [public test instances](#test_tube-public-test-instances) that you
can interact with using the [static-ct-api](https://c2sp.org/static-ct-api) API.

You can also have a look at the `main.go` files under [`/cmd/tesseract/`](./cmd/tesseract/)
to understand how to build a TesseraCT server.

Last, you can explore our [documentation](#card_index_dividers-repository-structure).

### Running on a different platform

TesseraCT can theoretically run on any platform
[Tessera](https://github.com/transparency-dev/tessera) supports.

If you'd still like to run TesseraCT on a different platform that Tessera
supports, have a look at Tessera's [Getting Started guide](https://github.com/transparency-dev/tessera/tree/main?tab=readme-ov-file#getting-started),
TesseraCT's `main.go` files under [`/cmd/tesseract/`](./cmd/tesseract/) and their
respective [architecture docs](https://github.com/transparency-dev/tesseract/tree/main/docs/architecture).

We'd love to know what platform you're interested in using,
[come and talk to us](#wave-contact)!

## :test_tube: Public test instances

TODO

## :card_index_dividers: Repository structure

This repository contains:

1. **[Binaries](./cmd/)**: TesseraCT and auxiliary tools
1. **[Deployment configs](./deployment/)**: purely informative, DO NOT
depend on them
1. **Libraries**: enabling the building of [static-ct-api](https://c2sp.org/static-ct-api)
   logs with [Tessera](https://github.com/transparency-dev/tessera):
   [ctlog](./ctlog.go), [storage](./storage/), ([internal](./internal/))
1. Documentation
     <!--Please, keep this in sync with ./docs/README.md -->
     - [Configuration](./docs/configuration.md)
     - [Performance](./docs/performance.md)
     - Architecture
       - GCP: TODO
       - AWS: TODO
       - POSIX: TODO
       - [S3+MySQL](./docs/architecture/NONCLOUD.md)
     - [Deployment](./deployment/)
     - Codelabs
       - [GCP](./deployment/live/gcp/test/)
       - [AWS](./deployment/live/aws/test/)
     - [Chain parsing with lax509](./internal/lax509/)

## :raising_hand: FAQ

### TesseraWhat?

TesseraCT is the concatenation of Tessera and CT (Certificate Transparency),
which also happens to be a [4-dimensional hypercube](https://en.wikipedia.org/wiki/Tesseract).

### What's the difference between Tessera and TesseraCT?

[Tessera](https://github.com/transparency-dev/tessera) is a Go library for
building [tile-based transparency logs (tlogs)](https://c2sp.org/tlog-tiles) on
various deployment backends. TesseraCT is a service using the Tessera library
with CT specific settings to implement Certificate Transparency logs complying
with [static-ct-api](https://c2sp.org/static-ct-api). TesseraCT supports a
subset of Tessera's backends. A TesseraCT serving stack is composed of:

- one or multiple instances of a TesseraCT binary using the Tessera library
- Tessera's backend infrastructure
- a minor additional storage system for [chain issuers](https://github.com/C2SP/C2SP/blob/main/static-ct-api.md#issuers)

### Why these platforms?

After chatting with various CT log operators, we decided to focus on GCP, AWS,
and to explore non-cloud-native deloyments. We welcome feedbacks on these and
requests for additional backend implementations. If you have any,
[come and talk to us](#wave-contact)!

## :troll: History

TesseraCT is the successor to [Trillian's CTFE](https://github.com/google/certificate-transparency-go/tree/master/trillian/ctfe).
It was built upon its codebase, and introduces these main changes:

- **API**: TesseraCT implements [static-ct-api](https://c2sp.org/static-ct-api)
rather than [RFC6962](https://www.rfc-editor.org/rfc/rfc6962).
- **Backend implementation**: TesseraCT uses [Tessera](https://github.com/transparency-dev/tessera)
rather than [Trillian](https://github.com/google/trillian). This means that
TesseraCT integrates entries faster, is cheaper to maintain, requires running a
single binary rather than 3, and does not need additional services for leader election.
- **Single tenancy**: One TesseraCT instance serves a single CT log, as opposed
to the CTFE which could serve multiple logs per instance. To run multiple logs,
simply bring up multiple independent TesseraCT stacks. For reliability, each log
can still be served by multiple TesseraCT _instances_.
- **Configuration**: TesseraCT is fully configured using flags, and does not
need a proto config anymore.
- **Chain parsing**: TesseraCT uses [internal/lax509](./internal/lax509/) to
validate certificate chains. It is built on top of Go's standard
[crypto/x509](https://pkg.go.dev/crypto/x509) library, with a minimal set of CT
specific enhancements. It **does not** use the full [crypto/x509 fork](https://github.com/google/certificate-transparency-go/tree/master/x509)
that the CTFE was using. This means that TesseraCT can benefit from the good care
and attention given to [crypto/x509](https://pkg.go.dev/crypto/x509). As a
result, a very small number of chains do not validate anymore, head over to
`internal/lax509`'s [README](./internal/lax509/README.md) for additional details.

## :wrench: Contributing

See [CONTRIBUTING.md](./CONTRIBUTING.md) for details.

## :page_facing_up: License

This repo is licensed under the Apache 2.0 license, see [LICENSE](./LICENSE) for
details.

## :wave: Contact

Are you interested in running a TesseraCT instance? Do you have a feature
request? you can find us here:

- [GitHub issues](https://github.com/transparency-dev/tesseract/issues)
- [Slack](https://transparency-dev.slack.com/) ([invitation](https://transparency.dev/slack/))
- [Mailing list](https://groups.google.com/forum/#!forum/trillian-transparency)
