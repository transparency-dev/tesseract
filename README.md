# :deciduous_tree: TesseraCT

[![Go Report Card](https://goreportcard.com/badge/github.com/transparency-dev/tesseract)](https://goreportcard.com/report/github.com/transparency-dev/tesseract)
[![Slack Status](https://img.shields.io/badge/Slack-Chat-blue.svg)](https://transparency-dev.slack.com/)

TesseraCT is a [Certificate Transparency (CT)](https://certificate.transparency.dev/)
log implementation. It implements [static-ct-api](https://c2sp.org/static-ct-api)
using the [Tessera](https://github.com/transparency-dev/tessera)
library to store data, and is aimed at running production-grade CT logs.

At the moment, TesseraCT can be [deployed](./deployment/) on GCP and AWS.
There is also an experimental binary which uses Tessera's POSIX storage backend.

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

TesseraCT is under active development, and will soon reach alpha 🚀.

At the moment, TesseraCT supports Amazon Web Service and Google Cloud Platform.
Read the FAQ to understand [why we chose these platforms](#why-these-platforms),
or if [you're interested in others](#can-i-run-tesseract-outside-gcp-or-aws).

[Public test instances](#test_tube-public-test-instances) run on GCP.

## :motorway: Roadmap

TODO

## :joystick: Usage

### Getting Started

The most hands-on place to start is with the [GCP](./deployment/live/gcp/test) or
[AWS](./deployment/live/aws/test) codelab. These codelabs will guide you
through bringing up your own test TesseraCT deployment.

We also run [public test instances](#test_tube-public-test-instances) that you
can interact with using the [static-ct-api](https://c2sp.org/static-ct-api) API.

You can also have a look at the `main.go` files under [`/cmd/tesseract/`](./cmd/tesseract/)
to understand how to build a TesseraCT server.

Last, you can explore our [documentation](#card_index_dividers-repository-structure).

### Running on a different platform

TesseraCT can theoretically run on any platform
[Tessera](https://github.com/transparency-dev/tessera) supports. We've already experimented
with platforms other than GCP and AWS, [have a look at the FAQ for more information](#can-i-run-tesseract-outside-gcp-or-aws).

If you'd still like to run TesseraCT on a different platform that Tessera
supports, have a look at Tessera's [Getting Started guide](https://github.com/transparency-dev/tessera/tree/main?tab=readme-ov-file#getting-started),
TesseraCT's `main.go` files under [`/cmd/tesseract/`](./cmd/tesseract/) and their
respective [architecture docs](https://github.com/transparency-dev/tesseract/tree/main/docs/architecture).

For any other request, please [come and talk to us](#wave-contact)!

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
       - [Non-cloud](./docs/architecture/NONCLOUD.md)
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

After chatting with various CT log operators, we decided to focus on GCP and AWS
to begin with in an effort address current needs of log operators. We're
welcoming contributions and requests for additional backend implementations.
If you're interested, [come and talk to us](#wave-contact)!

### Can I run TesseraCT outside GCP or AWS?

At the moment, this is not officially supported. If you're interested in
running outside GCP or AWS, [read this](./docs/architecture/NONCLOUD.md) and
[get in touch](#wave-contact)!

There is an experimental [POSIX binary](/cmd/experimental/posix) which uses
Tessera's POSIX backend for storing the log on local filesystems. At the moment
this is not ready for production use, but questions and bug reports are
very welcome!

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
