# Repository context

## Purpose
TesseraCT is a Certificate Transparency (CT) log server implementing the
[static-ct-api](https://c2sp.org/static-ct-api) submission API on top of the
[Tessera](https://github.com/transparency-dev/tessera) tile-based transparency
log library (`go.mod:35`). It is consumed by CT log operators who run a single
TesseraCT instance per CT log on GCP, AWS, vanilla S3+MySQL, or POSIX
filesystems (`README.md:34-43`, `docs/architecture.md:9-18`). Successor to
Trillian's CTFE (`README.md:261-283`).

## Tech stack
- Go (`go 1.25.0` in `go.mod:3`); CI matrix tests `1.25.x` and `1.26.x`
  (`.github/workflows/go_test.yml:11-12`).
- Tessera log library `github.com/transparency-dev/tessera` and its storage
  drivers `tessera/storage/{aws,gcp,posix}` (`go.mod:35`, imports in
  `cmd/tesseract/*/main.go`).
- Cloud SDKs: `cloud.google.com/go/{spanner,storage,secretmanager,logging,...}`,
  `github.com/aws/aws-sdk-go-v2/{config,credentials,service/s3,service/secretsmanager}`
  (`go.mod:6-19`).
- Local persistence/utility libs: `github.com/dgraph-io/badger/v4`,
  `github.com/go-sql-driver/mysql`, `golang.org/x/mod/sumdb/note`,
  `github.com/transparency-dev/{merkle,formats}` (`go.mod:23,27,33-34,45`).
- Observability: OpenTelemetry (`go.opentelemetry.io/otel*`, `contrib/...`)
  with Prometheus, GCP, and OTLP exporters (`go.mod:36-44`).
- TUIs for the load-tester and fsck use
  `github.com/charmbracelet/bubbletea`, `rivo/tview`, and `gdamore/tcell`
  (`go.mod:21-26`).
- Container images built with `ko` using
  `gcr.io/distroless/static-debian13:nonroot` as base (`.ko.yaml:1`); per-binary
  `Dockerfile`s also exist (e.g. `cmd/tesseract/{aws,gcp,posix}/Dockerfile`,
  `cmd/fsck/Dockerfile`).

## Entry points
- `cmd/tesseract/gcp/main.go` — TesseraCT server for GCP (Spanner + GCS +
  Secret Manager) (`cmd/tesseract/gcp/main.go:15-52`).
- `cmd/tesseract/aws/main.go` — TesseraCT server for AWS / vanilla S3+MySQL
  (RDS or MySQL-compatible + S3-compatible + Secrets Manager)
  (`cmd/tesseract/aws/main.go:15-45`).
- `cmd/tesseract/posix/main.go` — TesseraCT server backed by a POSIX filesystem
  with BadgerDB for antispam (`cmd/tesseract/posix/main.go:15-49`).
- `cmd/tesseract/gcp/generate_key/main.go` — helper to generate a checkpoint
  signer key.
- `cmd/fsck/main.go` — integrity-checker CLI for a static-ct based log
  (`cmd/fsck/main.go:15-16`).
- `cmd/experimental/fetch_roots/main.go` — fetch PEM roots from CCADB
  (`cmd/experimental/fetch_roots/main.go:15-17`).
- `cmd/experimental/migrate/gcp/main.go`, `cmd/experimental/migrate/posix/main.go`
  — migrate data from an existing static-ct log into a TesseraCT instance.
- `internal/hammer/hammer.go` — `hammer` load-testing tool for static-ct-api
  logs (`internal/hammer/hammer.go:15-16`, `internal/hammer/README.md`).
- `ctlog.go` — library entry point: `NewLogHandler` wires a chain validator,
  Tessera-backed `ct.Log`, and HTTP handlers including `/healthz`
  (`ctlog.go:223-269`).

## Layout
- `cmd/tesseract/` — production server binaries plus per-platform Dockerfiles
  and `README.md` (`cmd/tesseract/README.md`).
- `cmd/fsck/` — log integrity checker binary.
- `cmd/experimental/` — `fetch_roots` and `migrate/{gcp,posix}` tools; treat
  as experimental.
- `internal/ct/` — core CT log logic: chain validation, handlers, request log,
  signatures, time source (`internal/ct/`).
- `internal/lax509/` — minimal fork of `crypto/x509` that allows precertificate
  chains; explicitly not safe for use outside this repo
  (`internal/lax509/README.md:1-11`).
- `internal/x509util/` — PEM cert pool helpers used by the chain validator
  (`internal/x509util/pem_cert_pool.go`).
- `internal/ccadb/` — CCADB CSV fetcher used to refresh trusted roots
  (`ctlog.go:155-188`).
- `internal/client/` — read client over static-ct-api (also a GCP subdir).
- `internal/types/` — wire types: `rfc6962/`, `staticct/`, `tls/`.
- `internal/hammer/` — load-testing TUI + `loadtest/` workers.
- `internal/logger/` — slog helpers and GCP log integration
  (`internal/logger/`, `internal/logger/levels.go`).
- `internal/otel/` — OTel sampler and helper code.
- `internal/testdata/` — fixtures for tests.
- `storage/` — `CTStorage` shim over Tessera plus per-platform `issuers.go`
  (`storage/storage.go`, `storage/{aws,gcp,posix}/issuers.go`); POSIX adds
  `file_ops.go`.
- `deployment/` — `live/{aws,gcp}/` and `modules/{aws,gcp}/` Terraform/Tofu
  configs, plus `terragrunt-opentofu/` shim. README states deployments are
  informative; do not depend on them (`deployment/README.md`, `README.md:193-194`).
- `docs/` — `architecture.md`, `performance.md`, `assets/`.
- `testdata/` — repo-level test fixtures (e.g. `example_tile`).
- `ctlog.go` / `ctlog_test.go` — public library API at the repo root.

## Build, test, run
- Build everything: `go build ./...`.
- Run all tests with the race detector (matches CI):
  `go test -v -race ./...` (`.github/workflows/go_test.yml:21`).
- Lint: `golangci-lint run` (`CONTRIBUTING.md:74-79`); config in
  `.golangci.yml` enables `depguard` (denies `k8s.io/klog`*) and `sloglint`
  (`attr-only`, `context: all`).
- Vulnerability scan: `govulncheck` (`.github/workflows/govulncheck.yml`).
- Run a server locally (POSIX, simplest): `go run ./cmd/tesseract/posix
  -http_endpoint=localhost:6962 -origin=<log-origin> -roots_pem_file=<path>
  ...` — see `cmd/tesseract/README.md` for the full flag set and
  `cmd/tesseract/posix/README.md` for the codelab.
- Container images: per-platform `Dockerfile`s in each `cmd/tesseract/*`
  directory; `ko` builds via `.github/workflows/ko_build.yml` using
  `.ko.yaml`.
- No `Makefile` is present; everything goes through `go` / `golangci-lint`
  directly.

## Conventions
- Use `log/slog` for logging — `klog` is denied by `depguard`
  (`.golangci.yml:11-18`). Helpers and custom verbosity levels live in
  `internal/logger/` (`internal/logger/levels.go`,
  `cmd/tesseract/README.md:299-310`).
- `sloglint` enforces attribute-style calls (`slog.String(...)`, etc.) and
  requires a `context.Context` on slog calls (`.golangci.yml:19-21`); use
  `slog.InfoContext` / `slog.ErrorContext` and pass `ctx` (see e.g.
  `ctlog.go:151,158,164,177,183`).
- All storage backend access flows through `storage.CTStorage` and the
  `storage.IssuerStorage` / `storage.RootsStorage` interfaces
  (`storage/storage.go:58-101`); per-platform code lives under
  `storage/{aws,gcp,posix}/`.
- Chain parsing for submissions must use `internal/lax509`, not
  `crypto/x509` directly — it is the only path that accepts precert chains
  (`README.md:276-283`, `internal/lax509/README.md`).
- One TesseraCT process serves one CT log; to run multiple logs, run multiple
  processes (`README.md:270-273`, `docs/architecture.md:36-39`).
- The `origin` flag MUST match the log's submission prefix as required by
  static-ct-api (`cmd/tesseract/README.md:251-267`).
- `internal/lax509` is excluded from linting and formatting on purpose
  (`.golangci.yml:7-9,22-25`); keep it minimal and only sync with upstream
  for security or critical fixes (`internal/lax509/README.md:8-10`).
- Contributors must be listed in `AUTHORS` / `CONTRIBUTORS` and sign the CLA
  (`CONTRIBUTING.md:21-49`).
- License headers (Apache 2.0) appear at the top of Go source files; new
  files follow the same pattern.

## Gotchas
- The `pushback_max_dedupe_in_flight` flag is deprecated in favour of
  `rate_limit_dedup` and will be removed; do not add new uses
  (`cmd/tesseract/posix/main.go:57-58`; same comment in the AWS and GCP
  mains).
- The `accept_sha1_signing_algorithms` flag is a temporary workaround for
  Chrome's Merge Delay Monitor Root and is slated for removal; do not depend
  on it (`ctlog.go:79-83`, `cmd/tesseract/README.md:75-78`).
- Tessera's MySQL-only driver is not supported by TesseraCT
  (`docs/architecture.md:16-18`).
- S3-compatible backends (MinIO, etc.) do not all provide S3's consistency
  guarantees and may not be safe to use (`docs/architecture.md:139-141`).
- Garbage collection cadence has a throughput floor:
  `100*256/garbage_collection_interval` must exceed the log's write rate,
  otherwise the log accumulates partial tiles
  (`cmd/tesseract/README.md:236-247`).
- Issuer-cert dedupe cache in `storage.cachedStoreIssuers` is capped at
  `maxCachedIssuerKeys = 1<<20` keys (~64MB); past that it stops caching new
  issuers (`storage/storage.go:47-50,182-211`).
- Anything under `deployment/` is "purely informative, DO NOT depend on
  [it]" (`README.md:193-194`, `deployment/README.md`).

## External dependencies
Runtime dependencies vary by binary/platform:
- GCP server (`cmd/tesseract/gcp`): Google Cloud Spanner, Google Cloud
  Storage, Secret Manager, optionally Cloud Logging
  (`cmd/tesseract/gcp/main.go:35-50`, `docs/architecture.md:82-104`).
- AWS server (`cmd/tesseract/aws`): MySQL (RDS or compatible),
  S3 (or S3-compatible such as MinIO), AWS Secrets Manager
  (`cmd/tesseract/aws/main.go:32-44`, `docs/architecture.md:106-141`).
- POSIX server (`cmd/tesseract/posix`): a POSIX/ZFS-style filesystem plus
  BadgerDB for antispam; any HTTP server is expected to serve the read-side
  monitoring files (`cmd/tesseract/posix/main.go:38-49`,
  `docs/architecture.md:143-156`).
- Chain-source services: CCADB CSV endpoint(s) configured via
  `roots_remote_fetch_url` (`ctlog.go:154-204`,
  `internal/ccadb/ccadb.go`).
- Witnessing: optional Sigsum-style witness network; configured via a policy
  file passed with `witness_policy_file`
  (`cmd/tesseract/README.md:151-183`).
