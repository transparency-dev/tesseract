// Copyright 2025 the Tessera authors. All Rights Reserved.
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
	"crypto"
	"crypto/x509"
	"encoding/pem"
	"errors"
	"flag"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/transparency-dev/tessera"
	tposix "github.com/transparency-dev/tessera/storage/posix"
	tposix_as "github.com/transparency-dev/tessera/storage/posix/antispam"
	"github.com/transparency-dev/tesseract"
	"github.com/transparency-dev/tesseract/storage"
	"github.com/transparency-dev/tesseract/storage/posix"
	"golang.org/x/mod/sumdb/note"
	"k8s.io/klog/v2"
)

func init() {
	flag.Var(&notAfterStart, "not_after_start", "Start of the range of acceptable NotAfter values, inclusive. Leaving this unset implies no lower bound to the range. RFC3339 UTC format, e.g: 2024-01-02T15:04:05Z.")
	flag.Var(&notAfterLimit, "not_after_limit", "Cut off point of notAfter dates - only notAfter dates strictly *before* notAfterLimit will be accepted. Leaving this unset means no upper bound on the accepted range. RFC3339 UTC format, e.g: 2024-01-02T15:04:05Z.")
}

// Global flags that affect all log instances.
var (
	notAfterStart timestampFlag
	notAfterLimit timestampFlag

	enablePublicationAwaiter = flag.Bool("enable_publication_awaiter", true, "If true then the certificate is integrated into log before returning the response.")
	httpEndpoint             = flag.String("http_endpoint", "localhost:6962", "Endpoint for HTTP (host:port).")
	httpDeadline             = flag.Duration("http_deadline", time.Second*10, "Deadline for HTTP requests.")
	maskInternalErrors       = flag.Bool("mask_internal_errors", false, "Don't return error strings with Internal Server Error HTTP responses.")
	origin                   = flag.String("origin", "", "Origin of the log, for checkpoints and the monitoring prefix.")
	storageDir               = flag.String("storage_dir", "", "Path to root of log storage.")
	pushbackMaxOutstanding   = flag.Uint("pushback_max_outstanding", tessera.DefaultPushbackMaxOutstanding, "Maximum number of number of in-flight add requests - i.e. the number of entries with sequence numbers assigned, but which are not yet integrated into the log.")
	pushbackMaxDupesInFlight = flag.Uint("pushback_max_dupes_in_flight", 100, "Maximum number of number of in-flight duplicate add requests. When 0, duplicate entries are always pushed back.")
	rootsPemFile             = flag.String("roots_pem_file", "", "Path to the file containing root certificates that are acceptable to the log. The certs are served through get-roots endpoint.")
	rejectExpired            = flag.Bool("reject_expired", false, "If true then the certificate validity period will be checked against the current time during the validation of submissions. This will cause expired certificates to be rejected.")
	rejectUnexpired          = flag.Bool("reject_unexpired", false, "If true then CTFE rejects certificates that are either currently valid or not yet valid.")
	extKeyUsages             = flag.String("ext_key_usages", "", "If set, will restrict the set of such usages that the server will accept. By default all are accepted. The values specified must be ones known to the x509 package.")
	rejectExtensions         = flag.String("reject_extension", "", "A list of X.509 extension OIDs, in dotted string form (e.g. '2.3.4.5') which, if present, should cause submissions to be rejected.")
	privKeyFile              = flag.String("private_key", "", "Location of private key file. If unset, uses the contents of the LOG_PRIVATE_KEY environment variable.")

	clientHTTPTimeout        = flag.Duration("client_http_timeout", 5*time.Second, "Timeout for outgoing HTTP requests")
	clientHTTPMaxIdle        = flag.Int("client_http_max_idle", 2000, "Maximum number of idle HTTP connections for outgoing requests.")
	clientHTTPMaxIdlePerHost = flag.Int("client_http_max_idle_per_host", 1000, "Maximum number of idle HTTP connections per host for outgoing requests.")
)

func main() {
	klog.InitFlags(nil)
	flag.Parse()
	ctx := context.Background()

	signer := signerFromFlags()

	chainValidationConfig := tesseract.ChainValidationConfig{
		RootsPEMFile:     *rootsPemFile,
		RejectExpired:    *rejectExpired,
		RejectUnexpired:  *rejectUnexpired,
		ExtKeyUsages:     *extKeyUsages,
		RejectExtensions: *rejectExtensions,
		NotAfterStart:    notAfterStart.t,
		NotAfterLimit:    notAfterLimit.t,
	}

	logHandler, err := tesseract.NewLogHandler(ctx, *origin, signer, chainValidationConfig, newStorage, *httpDeadline, *maskInternalErrors)
	if err != nil {
		klog.Exitf("Can't initialize CT HTTP Server: %v", err)
	}

	klog.CopyStandardLogTo("WARNING")
	klog.Info("**** CT HTTP Server Starting ****")
	http.Handle("/", logHandler)

	// Bring up the HTTP server and serve until we get a signal not to.
	srv := http.Server{Addr: *httpEndpoint}
	shutdownWG := new(sync.WaitGroup)
	go awaitSignal(func() {
		shutdownWG.Add(1)
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

func newStorage(ctx context.Context, signer note.Signer) (*storage.CTStorage, error) {
	if *storageDir == "" {
		return nil, errors.New("missing storage_dir")
	}

	cfg := tposix.Config{
		Path: *storageDir,
		HTTPClient: &http.Client{
			Transport: &http.Transport{
				MaxIdleConns:        *clientHTTPMaxIdle,
				MaxIdleConnsPerHost: *clientHTTPMaxIdlePerHost,
				DisableKeepAlives:   false,
			},
			Timeout: *clientHTTPTimeout,
		},
	}

	driver, err := tposix.New(ctx, cfg)
	if err != nil {
		return nil, fmt.Errorf("failed to initialize POSIX Tessera storage driver: %v", err)
	}
	asOpts := tposix_as.AntispamOpts{}
	antispam, err := tposix_as.NewAntispam(ctx, filepath.Join(*storageDir, ".state", "antispam"), asOpts)
	if err != nil {
		return nil, fmt.Errorf("failed to initialize POSIX antispam database: %v", err)
	}

	opts := tessera.NewAppendOptions().
		WithCheckpointSigner(signer).
		WithCTLayout().
		WithAntispam(uint(250<<10), antispam).
		WithBatching(256, 500*time.Millisecond).
		WithPushback(*pushbackMaxOutstanding)

	// TODO(phbnf): figure out the best way to thread the `shutdown` func NewAppends returns back out to main so we can cleanly close Tessera down
	// when it's time to exit.
	appender, _, reader, err := tessera.NewAppender(ctx, driver, opts)
	if err != nil {
		return nil, fmt.Errorf("failed to initialize GCP Tessera appender: %v", err)
	}

	issuerStorage, err := posix.NewIssuerStorage(ctx, *storageDir)
	if err != nil {
		return nil, fmt.Errorf("failed to initialize GCP issuer storage: %v", err)
	}

	return storage.NewCTStorage(ctx, appender, issuerStorage, reader, *enablePublicationAwaiter, *pushbackMaxDupesInFlight)
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
	if !strings.HasSuffix(w, "Z") {
		return fmt.Errorf("timestamps MUST be in UTC, got %v", w)
	}
	tt, err := time.Parse(time.RFC3339, w)
	if err != nil {
		return fmt.Errorf("can't parse %q as RFC3339 timestamp: %v", w, err)
	}
	t.t = &tt
	return nil
}

func signerFromFlags() crypto.Signer {
	kf := *privKeyFile
	if kf == "" {
		kf = os.Getenv("LOG_PRIVATE_KEY")
	}
	if kf == "" {
		klog.Exitf("Must specify --priv_key or LOG_PRIVATE_KEY environment variable.")
	}
	r, err := os.ReadFile(kf)
	if err != nil {
		klog.Exitf("Failed to read private key from %q: %v", kf, err)
	}
	block, _ := pem.Decode(r)
	if err != nil {
		klog.Exitf("Failed to parse PEM private key: %v", err)
	}
	k, err := x509.ParseECPrivateKey(block.Bytes)
	if err != nil {
		klog.Exitf("Failed to parse private key: %v", err)
	}
	return k
}
