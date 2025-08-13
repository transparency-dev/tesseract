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
	"errors"
	"flag"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"sync"
	"syscall"
	"time"

	gcs "cloud.google.com/go/storage"
	"github.com/dustin/go-humanize"
	"github.com/transparency-dev/tessera"
	tgcp "github.com/transparency-dev/tessera/storage/gcp"
	gcp_as "github.com/transparency-dev/tessera/storage/gcp/antispam"
	"github.com/transparency-dev/tesseract"
	"github.com/transparency-dev/tesseract/storage"
	"github.com/transparency-dev/tesseract/storage/gcp"
	"go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp"
	"golang.org/x/mod/sumdb/note"
	"k8s.io/klog/v2"
)

func init() {
	flag.Var(&notAfterStart, "not_after_start", "Start of the range of acceptable NotAfter values, inclusive. Leaving this unset or empty implies no lower bound to the range. RFC3339 UTC format, e.g: 2024-01-02T15:04:05Z.")
	flag.Var(&notAfterLimit, "not_after_limit", "Cut off point of notAfter dates - only notAfter dates strictly *before* notAfterLimit will be accepted. Leaving this unset or empty means no upper bound on the accepted range. RFC3339 UTC format, e.g: 2024-01-02T15:04:05Z.")
}

// Global flags that affect all log instances.
var (
	notAfterStart timestampFlag
	notAfterLimit timestampFlag

	// Functionality flags
	httpEndpoint             = flag.String("http_endpoint", "localhost:6962", "Endpoint for HTTP (host:port).")
	maskInternalErrors       = flag.Bool("mask_internal_errors", false, "Don't return error strings with Internal Server Error HTTP responses.")
	origin                   = flag.String("origin", "", "Origin of the log, for checkpoints.")
	pathPrefix               = flag.String("path_prefix", "", "Prefix to use on endpoints URL paths: HOST:PATH_PREFIX/ct/v1/ENDPOINT.")
	rootsPemFile             = flag.String("roots_pem_file", "", "Path to the file containing root certificates that are acceptable to the log. The certs are served through get-roots endpoint.")
	rejectExpired            = flag.Bool("reject_expired", false, "If true then the certificate validity period will be checked against the current time during the validation of submissions. This will cause expired certificates to be rejected.")
	rejectUnexpired          = flag.Bool("reject_unexpired", false, "If true then TesseraCT rejects certificates that are either currently valid or not yet valid.")
	extKeyUsages             = flag.String("ext_key_usages", "", "If set, will restrict the set of such usages that the server will accept. By default all are accepted. The values specified must be ones known to the x509 package.")
	rejectExtensions         = flag.String("reject_extension", "", "A list of X.509 extension OIDs, in dotted string form (e.g. '2.3.4.5') which, if present, should cause submissions to be rejected.")
	acceptSHA1               = flag.Bool("accept_sha1_signing_algorithms", true, "If true, accept chains that use SHA-1 based signing algorithms. This flag will eventually be removed, and such algorithms will be rejected.")
	enablePublicationAwaiter = flag.Bool("enable_publication_awaiter", true, "If true then the certificate is integrated into log before returning the response.")

	// Performance flags
	httpDeadline              = flag.Duration("http_deadline", time.Second*10, "Deadline for HTTP requests.")
	inMemoryAntispamCacheSize = flag.String("inmemory_antispam_cache_size", "256k", "Maximum number of entries to keep in the in-memory antispam cache. Unitless with SI metric prefixes, such as '256k'.")
	checkpointInterval        = flag.Duration("checkpoint_interval", 1500*time.Millisecond, "Interval between checkpoint publishing")
	batchMaxSize              = flag.Uint("batch_max_size", tessera.DefaultBatchMaxSize, "Maximum number of entries to process in a single sequencing batch.")
	batchMaxAge               = flag.Duration("batch_max_age", tessera.DefaultBatchMaxAge, "Maximum age of entries in a single sequencing batch.")
	pushbackMaxOutstanding    = flag.Uint("pushback_max_outstanding", tessera.DefaultPushbackMaxOutstanding, "Maximum number of number of in-flight add requests - i.e. the number of entries with sequence numbers assigned, but which are not yet integrated into the log.")
	pushbackMaxDedupeInFlight = flag.Uint("pushback_max_dedupe_in_flight", 100, "Maximum number of number of in-flight duplicate add requests - i.e. the number of requests matching entries that have already been integrated, but need to be fetched by the client to retrieve their timestamp. When 0, duplicate entries are always pushed back.")
	pushbackMaxAntispamLag    = flag.Uint("pushback_max_antispam_lag", gcp_as.DefaultPushbackThreshold, "Maximum permitted lag for antispam follower, before log starts returneing pushback.")
	clientHTTPTimeout         = flag.Duration("client_http_timeout", 5*time.Second, "Timeout for outgoing HTTP requests")
	clientHTTPMaxIdle         = flag.Int("client_http_max_idle", 20, "Maximum number of idle HTTP connections for outgoing requests.")
	clientHTTPMaxIdlePerHost  = flag.Int("client_http_max_idle_per_host", 10, "Maximum number of idle HTTP connections per host for outgoing requests.")

	// Infrastructure setup flags
	bucket                     = flag.String("bucket", "", "Name of the GCS bucket to store the log in.")
	spannerDB                  = flag.String("spanner_db_path", "", "Spanner database path: projects/{projectId}/instances/{instanceId}/databases/{databaseId}.")
	spannerAntispamDB          = flag.String("spanner_antispam_db_path", "", "Spanner antispam deduplication database path projects/{projectId}/instances/{instanceId}/databases/{databaseId}.")
	signerPublicKeySecretName  = flag.String("signer_public_key_secret_name", "", "Public key secret name for checkpoints and SCTs signer. Format: projects/{projectId}/secrets/{secretName}/versions/{secretVersion}.")
	signerPrivateKeySecretName = flag.String("signer_private_key_secret_name", "", "Private key secret name for checkpoints and SCTs signer. Format: projects/{projectId}/secrets/{secretName}/versions/{secretVersion}.")
	traceFraction              = flag.Float64("trace_fraction", 0, "Fraction of open-telemetry span traces to sample")
	otelProjectID              = flag.String("otel_project_id", "", "GCP project ID for OpenTelemetry exporter. This is only required for local runs.")
)

// nolint:staticcheck
func main() {
	klog.InitFlags(nil)
	flag.Parse()
	ctx := context.Background()

	shutdownOTel := initOTel(ctx, *traceFraction, *origin, *otelProjectID)
	defer shutdownOTel(ctx)

	signer, err := NewSecretManagerSigner(ctx, *signerPublicKeySecretName, *signerPrivateKeySecretName)
	if err != nil {
		klog.Exitf("Can't create secret manager signer: %v", err)
	}

	chainValidationConfig := tesseract.ChainValidationConfig{
		RootsPEMFile:     *rootsPemFile,
		RejectExpired:    *rejectExpired,
		RejectUnexpired:  *rejectUnexpired,
		ExtKeyUsages:     *extKeyUsages,
		RejectExtensions: *rejectExtensions,
		NotAfterStart:    notAfterStart.t,
		NotAfterLimit:    notAfterLimit.t,
		AcceptSHA1:       *acceptSHA1,
	}
	if *acceptSHA1 {
		klog.Info(`**** WARNING **** This server will accept chains signed
using SHA-1 based algorithms. This feature is available to allow chains
submitted by Chrome's Merge Delay Monitor Root for the time being, but will
eventually go away. See /internal/lax509/README.md for more information.`)
	}

	logHandler, err := tesseract.NewLogHandler(ctx, *origin, signer, chainValidationConfig, newGCPStorage, *httpDeadline, *maskInternalErrors, *pathPrefix)
	if err != nil {
		klog.Exitf("Can't initialize CT HTTP Server: %v", err)
	}

	klog.CopyStandardLogTo("WARNING")
	klog.Info("**** CT HTTP Server Starting ****")
	http.Handle("/", otelhttp.NewHandler(logHandler, "/"))

	// Bring up the HTTP server and serve until we get a signal not to.
	srv := http.Server{Addr: *httpEndpoint}
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

func newGCPStorage(ctx context.Context, signer note.Signer) (*storage.CTStorage, error) {
	if *bucket == "" {
		return nil, errors.New("missing bucket")
	}

	if *spannerDB == "" {
		return nil, errors.New("missing spannerDB")
	}

	hc := &http.Client{
		Transport: &http.Transport{
			MaxIdleConns:        *clientHTTPMaxIdle,
			MaxIdleConnsPerHost: *clientHTTPMaxIdlePerHost,
			DisableKeepAlives:   false,
		},
		Timeout: *clientHTTPTimeout,
	}

	gcsClient, err := gcs.NewGRPCClient(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to create GCS client: %v", err)
	}

	gcpCfg := tgcp.Config{
		Bucket:     *bucket,
		Spanner:    *spannerDB,
		HTTPClient: hc,
		GCSClient:  gcsClient,
	}

	driver, err := tgcp.New(ctx, gcpCfg)
	if err != nil {
		return nil, fmt.Errorf("failed to initialize GCP Tessera storage driver: %v", err)
	}

	var antispam tessera.Antispam
	if *spannerAntispamDB != "" {
		antispam, err = gcp_as.NewAntispam(ctx, *spannerAntispamDB, gcp_as.AntispamOpts{PushbackThreshold: *pushbackMaxAntispamLag})
		if err != nil {
			klog.Exitf("Failed to create new GCP antispam storage: %v", err)
		}
	}

	antispamCacheSize, unit, error := humanize.ParseSI(*inMemoryAntispamCacheSize)
	if unit != "" {
		return nil, fmt.Errorf("invalid antispam cache size, used unit %q, want none", unit)
	}
	if error != nil {
		return nil, fmt.Errorf("invalid antispam cache size: %v", error)
	}

	opts := tessera.NewAppendOptions().
		WithCheckpointSigner(signer).
		WithCTLayout().
		WithAntispam(uint(antispamCacheSize), antispam).
		WithCheckpointInterval(*checkpointInterval).
		WithBatching(*batchMaxSize, *batchMaxAge).
		WithPushback(*pushbackMaxOutstanding)

	// TODO(phbnf): figure out the best way to thread the `shutdown` func NewAppends returns back out to main so we can cleanly close Tessera down
	// when it's time to exit.
	appender, _, reader, err := tessera.NewAppender(ctx, driver, opts)
	if err != nil {
		return nil, fmt.Errorf("failed to initialize GCP Tessera appender: %v", err)
	}

	issuerStorage, err := gcp.NewIssuerStorage(ctx, *bucket, gcsClient)
	if err != nil {
		return nil, fmt.Errorf("failed to initialize GCP issuer storage: %v", err)
	}

	sopts := storage.CTStorageOptions{
		Appender:          appender,
		Reader:            reader,
		IssuerStorage:     issuerStorage,
		EnableAwaiter:     *enablePublicationAwaiter,
		MaxDedupeInFlight: *pushbackMaxDedupeInFlight,
	}

	return storage.NewCTStorage(ctx, &sopts)
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
