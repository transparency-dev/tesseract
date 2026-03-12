// Copyright 2016 Google LLC. All Rights Reserved.
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

// The ct_server binary runs the CT personality.
package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	_ "net/http/pprof"
	"os"

	"os/signal"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	"cloud.google.com/go/logging"
	"k8s.io/klog/v2"

	"cloud.google.com/go/spanner"
	gcs "cloud.google.com/go/storage"
	"github.com/dustin/go-humanize"
	"github.com/transparency-dev/tessera"
	tgcp "github.com/transparency-dev/tessera/storage/gcp"
	gcp_as "github.com/transparency-dev/tessera/storage/gcp/antispam"
	"github.com/transparency-dev/tesseract"
	"github.com/transparency-dev/tesseract/internal/logger"
	"github.com/transparency-dev/tesseract/storage"
	"github.com/transparency-dev/tesseract/storage/gcp"
	"go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp"
	"golang.org/x/mod/sumdb/note"
	"google.golang.org/api/option"
)

func init() {
	flag.Var(&notAfterStart, "not_after_start", "Start of the range of acceptable NotAfter values, inclusive. Leaving this unset or empty implies no lower bound to the range. RFC3339 UTC format, e.g: 2024-01-02T15:04:05Z.")
	flag.Var(&notAfterLimit, "not_after_limit", "Cut off point of notAfter dates - only notAfter dates strictly *before* notAfterLimit will be accepted. Leaving this unset or empty means no upper bound on the accepted range. RFC3339 UTC format, e.g: 2024-01-02T15:04:05Z.")
	flag.Var(&additionalSigners, "additional_signer_private_key_secret_name", "Private key secret name for additional Ed25519 checkpoint signatures, may be supplied multiple times. Format: projects/{projectId}/secrets/{secretName}/versions/{secretVersion}.")
	flag.Var(&rootsRejectFingerprints, "roots_reject_fingerprints", "Hex-encoded SHA-256 fingerprint of a root certificate to reject. May be specified multiple times.")
	flag.Float64Var(&dedupRL, "rate_limit_dedup", 100, "Rate limit for resolving duplicate submissions, in requests per second - i.e. duplicate requests for already integrated entries, which need to be fetched from the log storage by TesseraCT to extract their timestamp. When 0, all duplicate submissions are rejected. When negative, no rate limit is applied.")
	// DEPRECATED: will be removed shortly
	flag.Float64Var(&dedupRL, "pushback_max_dedupe_in_flight", 100, "DEPRECATED: use rate_limit_dedup. Maximum number of in-flight duplicate add requests - i.e. the number of requests matching entries that have already been integrated, but need to be fetched by the client to retrieve their timestamp. When 0, duplicate entries are always pushed back.")
}

// Global flags that affect all log instances.
var (
	notAfterStart     timestampFlag
	notAfterLimit     timestampFlag
	additionalSigners multiStringFlag
	dedupRL           float64

	// Functionality flags
	httpEndpoint             = flag.String("http_endpoint", "localhost:6962", "Endpoint for HTTP (host:port).")
	maskInternalErrors       = flag.Bool("mask_internal_errors", false, "Don't return error strings with Internal Server Error HTTP responses.")
	origin                   = flag.String("origin", "", "Origin of the log, for checkpoints. This MUST match the log's submission prefix as per https://c2sp.org/static-ct-api.")
	pathPrefix               = flag.String("path_prefix", "", "Prefix to use on endpoints URL paths: HOST:PATH_PREFIX/ct/v1/ENDPOINT.")
	rootsPemFile             = flag.String("roots_pem_file", "", "Path to the file containing root certificates that are acceptable to the log.")
	rootsRemoteFetchURL      = flag.String("roots_remote_fetch_url", "https://ccadb.my.salesforce-sites.com/ccadb/RootCACertificatesIncludedByRSReportCSV", "URL to fetch additional trusted roots from.")
	rootsRemoteFetchInterval = flag.Duration("roots_remote_fetch_interval", time.Duration(0), "Interval between two fetches from roots_fetch_url, e.g. \"1h\".")
	rootsRejectFingerprints  multiStringFlag
	rejectExpired            = flag.Bool("reject_expired", false, "If true then the certificate validity period will be checked against the current time during the validation of submissions. This will cause expired certificates to be rejected.")
	rejectUnexpired          = flag.Bool("reject_unexpired", false, "If true then TesseraCT rejects certificates that are either currently valid or not yet valid.")
	extKeyUsages             = flag.String("ext_key_usages", "", "If set, will restrict the set of such usages that the server will accept. By default all are accepted. The values specified must be ones known to the x509 package.")
	rejectExtensions         = flag.String("reject_extension", "", "A list of X.509 extension OIDs, in dotted string form (e.g. '2.3.4.5') which, if present, should cause submissions to be rejected.")
	acceptSHA1               = flag.Bool("accept_sha1_signing_algorithms", true, "If true, accept chains that use SHA-1 based signing algorithms. This flag will eventually be removed, and such algorithms will be rejected.")
	enablePublicationAwaiter = flag.Bool("enable_publication_awaiter", true, "If true then the certificate is integrated into log before returning the response.")
	witnessPolicyFile        = flag.String("witness_policy_file", "", "(Optional) Path to the file containing the witness policy in the format described at https://git.glasklar.is/sigsum/core/sigsum-go/-/blob/main/doc/policy.md")
	witnessTimeout           = flag.Duration("witness_timeout", tessera.DefaultWitnessTimeout, "Maximum time to wait for witness responses.")
	notBeforeRL              = flag.String("rate_limit_old_not_before", "", "Optionally rate limits submissions with old notBefore dates. Expects a value of with the format: \"<go duration>:<rate limit>\", e.g. \"30d:50\" would impose a limit of 50 certs/s on submissions whose notBefore date is >= 30days old.")

	// Performance flags
	httpDeadline                = flag.Duration("http_deadline", time.Second*10, "Deadline for HTTP requests.")
	inMemoryAntispamCacheSize   = flag.String("inmemory_antispam_cache_size", "256k", "Maximum number of entries to keep in the in-memory antispam cache. Unitless with SI metric prefixes, such as '256k'.")
	checkpointInterval          = flag.Duration("checkpoint_interval", 1500*time.Millisecond, "Interval between publishing checkpoints when the log has grown")
	checkpointRepublishInterval = flag.Duration("checkpoint_republish_interval", 30*time.Second, "Interval between republishing a checkpoint for a log which hasn't grown since the previous checkpoint was published")
	batchMaxSize                = flag.Uint("batch_max_size", tessera.DefaultBatchMaxSize, "Maximum number of entries to process in a single sequencing batch.")
	batchMaxAge                 = flag.Duration("batch_max_age", tessera.DefaultBatchMaxAge, "Maximum age of entries in a single sequencing batch.")
	pushbackMaxOutstanding      = flag.Uint("pushback_max_outstanding", tessera.DefaultPushbackMaxOutstanding, "Maximum number of in-flight add requests - i.e. the number of entries with sequence numbers assigned, but which are not yet integrated into the log.")
	pushbackMaxAntispamLag      = flag.Uint("pushback_max_antispam_lag", gcp_as.DefaultPushbackThreshold, "Maximum permitted lag for antispam follower, before log starts returning pushback.")
	clientHTTPTimeout           = flag.Duration("client_http_timeout", 5*time.Second, "Timeout for outgoing HTTP requests")
	clientHTTPMaxIdle           = flag.Int("client_http_max_idle", 200, "Maximum number of idle HTTP connections for outgoing requests.")
	clientHTTPMaxIdlePerHost    = flag.Int("client_http_max_idle_per_host", 200, "Maximum number of idle HTTP connections per host for outgoing requests.")
	garbageCollectionInterval   = flag.Duration("garbage_collection_interval", 10*time.Second, "Interval between scans to remove obsolete partial tiles and entry bundles. Set to 0 to disable.")

	// Infrastructure setup flags
	bucket                     = flag.String("bucket", "", "Name of the GCS bucket to store the log in.")
	gcsUseGRPC                 = flag.Bool("gcs_use_grpc", false, "Use gRPC-based GCS client.")
	gcsConnections             = flag.Int("gcs_connections", 4, "Size of connection pool for GCS gRPC client.")
	spannerDB                  = flag.String("spanner_db_path", "", "Spanner database path: projects/{projectId}/instances/{instanceId}/databases/{databaseId}.")
	spannerAntispamDB          = flag.String("spanner_antispam_db_path", "", "Spanner antispam deduplication database path projects/{projectId}/instances/{instanceId}/databases/{databaseId}.")
	spannerConnections         = flag.Int("spanner_connections", 4, "Number of Spanner connections to configure.")
	signerPublicKeySecretName  = flag.String("signer_public_key_secret_name", "", "Public key secret name for checkpoints and SCTs signer. Format: projects/{projectId}/secrets/{secretName}/versions/{secretVersion}.")
	signerPrivateKeySecretName = flag.String("signer_private_key_secret_name", "", "Private key secret name for checkpoints and SCTs signer. Format: projects/{projectId}/secrets/{secretName}/versions/{secretVersion}.")
	traceFraction              = flag.Float64("trace_fraction", 0, "Fraction of open-telemetry span traces to sample")
	otelProjectID              = flag.String("otel_project_id", "", "GCP project ID for OpenTelemetry exporter.")
	slogLevel                  = flag.Int("slog_level", 0, "The cut-off threshold for structured logging. Default is INFO. See https://pkg.go.dev/log/slog#Level.")
	logToCloudAPI              = flag.Bool("log_to_cloud_api", false, "Export logs directly to Cloud Logging API instead of stderr.")
	slogGCPHandler             = flag.Bool("slog_gcp_handler", false, "Whether to use a custom GCP slog handler.")
	slogStdOut                 = flag.Bool("slog_std_out", false, "Set to true for slog to output to stdout. Defaults to stderr.")
	klogEnable                 = flag.Bool("klog_enable", true, "Set to true to enable klog logging.")
	klogCopyTo                 = flag.String("klog_copy_to", "WARNING", "Set to to redirect klog logging to default logs (INFO, WARNING, ERROR, FATAL). Leave empty to disable.")
)

// nolint:staticcheck
func main() {
	if *klogEnable {
		klog.InitFlags(nil)
	}
	flag.Parse()
	ctx := context.Background()

	var logWriter io.Writer = os.Stderr
	if *slogStdOut {
		logWriter = os.Stdout
	}
	var logClient *logging.Client

	if *logToCloudAPI {
		if *otelProjectID == "" {
			klog.Exitf("--otel_project_id is required when --log_to_cloud_api is true")
		}
		var err error
		logClient, err = logging.NewClient(ctx, "projects/"+*otelProjectID)
		if err != nil {
			klog.Exitf("Failed to create Cloud Logging client: %v", err)
		}
		defer func() {
			if err := logClient.Close(); err != nil {
				klog.Errorf("Failed to close Cloud Logging client: %v", err)
			}
		}()

		logWriter = &cloudLoggingWriter{
			logger: logClient.Logger("tesseract"),
		}
	}

	handler := slog.NewJSONHandler(logWriter, &slog.HandlerOptions{
		Level:       slog.Level(*slogLevel),
		ReplaceAttr: logger.GCPReplaceAttr,
	})
	if *slogGCPHandler {
		slog.SetDefault(slog.New(logger.NewGCPContextHandler(handler, *otelProjectID)))
	} else {
		slog.SetDefault(slog.New(handler))
	}

	// Example Logs for debugging
	slog.Debug("TESSERACT_LOG_TEST: slog.Debug")
	slog.Info("TESSERACT_LOG_TEST: slog.Info")
	slog.Warn("TESSERACT_LOG_TEST: slog.Warn")
	slog.Error("TESSERACT_LOG_TEST: slog.Error")
	slog.DebugContext(ctx, "TESSERACT_LOG_TEST: slog.DebugContext")
	slog.InfoContext(ctx, "TESSERACT_LOG_TEST: slog.InfoContext")
	slog.WarnContext(ctx, "TESSERACT_LOG_TEST: slog.WarnContext")
	slog.ErrorContext(ctx, "TESSERACT_LOG_TEST: slog.ErrorContext")
	fmt.Fprintln(os.Stderr, `{"severity":"INFO","TESSERACT_LOG_TEST: Stderr pipe is open"}`)

	shutdownOTel := initOTel(ctx, *traceFraction, *origin, *otelProjectID)
	defer shutdownOTel(ctx)

	signer, err := NewSecretManagerSigner(ctx, *signerPublicKeySecretName, *signerPrivateKeySecretName)
	if err != nil {
		klog.Exitf("Can't create secret manager signer: %v", err)
	}

	hc := &http.Client{
		Transport: &http.Transport{
			MaxIdleConns:        *clientHTTPMaxIdle,
			MaxIdleConnsPerHost: *clientHTTPMaxIdlePerHost,
			DisableKeepAlives:   false,
		},
		Timeout: *clientHTTPTimeout,
	}

	gcsClient := gcsClientFromFlags(ctx, hc)
	fetchedRootsBackupStorage, err := gcp.NewRootsStorage(ctx, *bucket, gcsClient)
	if err != nil {
		klog.Exitf("failed to initialize GCS backup storage for remotely fetched roots: %v", err)
	}

	chainValidationConfig := tesseract.ChainValidationConfig{
		RootsPEMFile:             *rootsPemFile,
		RootsRemoteFetchURL:      *rootsRemoteFetchURL,
		RootsRemoteFetchInterval: *rootsRemoteFetchInterval,
		RootsRemoteFetchBackup:   fetchedRootsBackupStorage,
		RejectExpired:            *rejectExpired,
		RejectUnexpired:          *rejectUnexpired,
		ExtKeyUsages:             *extKeyUsages,
		RejectExtensions:         *rejectExtensions,
		NotAfterStart:            notAfterStart.t,
		NotAfterLimit:            notAfterLimit.t,
		AcceptSHA1:               *acceptSHA1,
		RejectRoots:              rootsRejectFingerprints,
	}
	if *acceptSHA1 {
		klog.Info(`**** WARNING **** This server will accept chains signed
using SHA-1 based algorithms. This feature is available to allow chains
submitted by Chrome's Merge Delay Monitor Root for the time being, but will
eventually go away. See /internal/lax509/README.md for more information.`)
	}

	hOpts := tesseract.LogHandlerOpts{
		NotBeforeRL: notBeforeRLFromFlags(),
		DedupRL:     dedupRL,
	}
	logHandler, err := tesseract.NewLogHandler(ctx, *origin, signer, chainValidationConfig, newGCPStorage(gcsClient, hc), *httpDeadline, *maskInternalErrors, *pathPrefix, hOpts)
	if err != nil {
		klog.Exitf("Can't initialize CT HTTP Server: %v", err)
	}

	if *klogCopyTo != "" {
		klog.CopyStandardLogTo(*klogCopyTo)
	}
	klog.Info("**** CT HTTP Server Starting ****")
	http.Handle("/", otelhttp.NewHandler(logHandler, "/"))

	// Bring up the HTTP server and serve until we get a signal not to.
	srv := http.Server{
		Addr: *httpEndpoint,
		// Set timeout for reading headers to avoid a slowloris attack.
		ReadHeaderTimeout: 5 * time.Second,
	}
	shutdownWG := new(sync.WaitGroup)
	shutdownWG.Add(1)
	go awaitSignal(func() {
		defer shutdownWG.Done()
		// Allow 60s for any pending requests to finish then terminate any stragglers
		// TODO(phboneff): maybe wait for the sequencer queue to be empty?
		ctx, cancel := context.WithTimeout(context.Background(), time.Second*60)
		defer cancel()
		klog.Info("Shutting down HTTP server...")
		if err := srv.Shutdown(ctx); err != nil {
			klog.Errorf("srv.Shutdown(): %v", err)
		}
		klog.Info("HTTP server shutdown")
	})

	if err := srv.ListenAndServe(); err != http.ErrServerClosed {
		klog.Warningf("Server exited: %v", err)
	}
	// Wait will only block if the function passed to awaitSignal was called,
	// in which case it'll block until the HTTP server has gracefully shutdown
	shutdownWG.Wait()
	klog.Flush()
}

// awaitSignal waits for standard termination signals, then runs the given
// function; it should be run as a separate goroutine.
func awaitSignal(doneFn func()) {
	// Arrange notification for the standard set of signals used to terminate a server
	sigs := make(chan os.Signal, 1)
	signal.Notify(sigs, syscall.SIGINT, syscall.SIGTERM)

	// Now block main and wait for a signal
	sig := <-sigs
	klog.Warningf("Signal received: %v", sig)
	klog.Flush()

	doneFn()
}

func newGCPStorage(gc *gcs.Client, hc *http.Client) func(ctx context.Context, signer note.Signer) (*storage.CTStorage, error) {
	return func(ctx context.Context, signer note.Signer) (*storage.CTStorage, error) {
		if *bucket == "" {
			return nil, errors.New("missing bucket")
		}

		if *spannerDB == "" {
			return nil, errors.New("missing spannerDB")
		}

		spannerClient, err := spanner.NewClient(ctx, *spannerDB, option.WithGRPCConnectionPool(*spannerConnections))
		if err != nil {
			return nil, fmt.Errorf("failed to create new Spanner client: %v", err)
		}

		gcpCfg := tgcp.Config{
			Bucket:        *bucket,
			Spanner:       *spannerDB,
			SpannerClient: spannerClient,
			HTTPClient:    hc,
			GCSClient:     gc,
		}

		driver, err := tgcp.New(ctx, gcpCfg)
		if err != nil {
			return nil, fmt.Errorf("failed to initialize GCP Tessera storage driver: %v", err)
		}

		var antispam tessera.Antispam
		if *spannerAntispamDB != "" {
			antispam, err = gcp_as.NewAntispam(ctx, *spannerAntispamDB, gcp_as.AntispamOpts{PushbackThreshold: *pushbackMaxAntispamLag})
			if err != nil {
				return nil, fmt.Errorf("failed to create new GCP antispam storage: %v", err)
			}
		}

		antispamCacheSize, unit, error := humanize.ParseSI(*inMemoryAntispamCacheSize)
		if unit != "" {
			return nil, fmt.Errorf("invalid antispam cache size, used unit %q, want none", unit)
		}
		if error != nil {
			return nil, fmt.Errorf("invalid antispam cache size: %v", error)
		}

		var extraSigners []note.Signer
		for _, as := range additionalSigners {
			s, err := NewSecretManagerNoteSigner(ctx, as)
			if err != nil {
				return nil, fmt.Errorf("failed to instantiate additional signer: %v", err)
			}
			extraSigners = append(extraSigners, s)
		}

		opts := tessera.NewAppendOptions().
			WithCheckpointSigner(signer, extraSigners...).
			WithCTLayout().
			WithAntispam(uint(antispamCacheSize), antispam).
			WithCheckpointInterval(*checkpointInterval).
			WithCheckpointRepublishInterval(*checkpointRepublishInterval).
			WithBatching(*batchMaxSize, *batchMaxAge).
			WithPushback(*pushbackMaxOutstanding).
			WithGarbageCollectionInterval(*garbageCollectionInterval)

		if *witnessPolicyFile != "" {
			f, err := os.ReadFile(*witnessPolicyFile)
			if err != nil {
				return nil, fmt.Errorf("failed to read witness policy file %q: %v", *witnessPolicyFile, err)
			}
			wg, err := tessera.NewWitnessGroupFromPolicy(f)
			if err != nil {
				return nil, fmt.Errorf("failed to create witness group from policy: %v", err)
			}

			// Don't block if witnesses are unavailable.
			wOpts := &tessera.WitnessOptions{
				FailOpen: true,
				Timeout:  *witnessTimeout,
			}
			opts.WithWitnesses(wg, wOpts)
		}

		// TODO(phbnf): figure out the best way to thread the `shutdown` func NewAppends returns back out to main so we can cleanly close Tessera down
		// when it's time to exit.
		appender, _, reader, err := tessera.NewAppender(ctx, driver, opts)
		if err != nil {
			return nil, fmt.Errorf("failed to initialize GCP Tessera appender: %v", err)
		}

		issuerStorage, err := gcp.NewIssuerStorage(ctx, *bucket, gc)
		if err != nil {
			return nil, fmt.Errorf("failed to initialize GCP issuer storage: %v", err)
		}

		sopts := storage.CTStorageOptions{
			Appender:      appender,
			Reader:        reader,
			IssuerStorage: issuerStorage,
			EnableAwaiter: *enablePublicationAwaiter,
		}

		return storage.NewCTStorage(ctx, &sopts)
	}
}

type timestampFlag struct {
	t *time.Time
}

func (t *timestampFlag) String() string {
	if t.t != nil {
		return t.t.Format(time.RFC3339)
	}
	return ""
}

func (t *timestampFlag) Set(w string) error {
	if w == "" {
		return nil
	} else if !strings.HasSuffix(w, "Z") {
		return fmt.Errorf("timestamps MUST be in UTC, got %v", w)
	}
	tt, err := time.Parse(time.RFC3339, w)
	if err != nil {
		return fmt.Errorf("can't parse %q as RFC3339 timestamp: %v", w, err)
	}
	t.t = &tt
	return nil
}

func notBeforeRLFromFlags() *tesseract.NotBeforeRL {
	if *notBeforeRL == "" {
		return nil
	}
	bits := strings.Split(*notBeforeRL, ":")
	if len(bits) != 2 {
		klog.Exitf("Invalid format for --rate_limit_old_not_before flag")
	}
	a, err := time.ParseDuration(bits[0])
	if err != nil {
		klog.Exitf("Invalid age passed to --rate_limit_old_not_before flag %q: %v", bits[0], err)
	}
	l, err := strconv.ParseFloat(bits[1], 64)
	if err != nil {
		klog.Exitf("Invalid rate limit passed to --rate_limit_old_not_before %q: %v", bits[1], err)
	}
	return &tesseract.NotBeforeRL{AgeThreshold: a, RateLimit: l}
}

func gcsClientFromFlags(ctx context.Context, httpClient *http.Client) *gcs.Client {
	if *gcsUseGRPC {
		gcsClient, err := gcs.NewGRPCClient(ctx, option.WithGRPCConnectionPool(*gcsConnections))
		if err != nil {
			klog.Exitf("Failed to create gRPC GCS client: %v", err)
		}
		return gcsClient
	}

	gcsClient, err := gcs.NewClient(ctx, gcs.WithJSONReads(), option.WithHTTPClient(httpClient))
	if err != nil {
		klog.Exitf("Failed to create GCS client: %v", err)
	}
	return gcsClient
}

type cloudLoggingWriter struct {
	logger *logging.Logger
}

func (w *cloudLoggingWriter) Write(p []byte) (n int, err error) {
	var payload map[string]any
	if err := json.Unmarshal(p, &payload); err != nil {
		w.logger.Log(logging.Entry{Payload: string(p)})
		return len(p), nil
	}

	entry := logging.Entry{
		Payload: payload,
	}

	if trace, ok := payload["logging.googleapis.com/trace"].(string); ok {
		entry.Trace = trace
		delete(payload, "logging.googleapis.com/trace")
	}
	if span, ok := payload["logging.googleapis.com/spanId"].(string); ok {
		entry.SpanID = span
		delete(payload, "logging.googleapis.com/spanId")
	}
	if sampled, ok := payload["logging.googleapis.com/trace_sampled"].(bool); ok {
		entry.TraceSampled = sampled
		delete(payload, "logging.googleapis.com/trace_sampled")
	}
	if lvl, ok := payload["level"].(string); ok {
		entry.Severity = logging.ParseSeverity(lvl)
	}
	if msg, ok := payload["msg"].(string); ok {
		payload["message"] = msg
		delete(payload, "msg")
	}
	if ts, ok := payload["time"].(string); ok {
		if t, err := time.Parse(time.RFC3339Nano, ts); err == nil {
			entry.Timestamp = t
		}
		delete(payload, "time")
	}

	w.logger.Log(entry)
	return len(p), nil
}
