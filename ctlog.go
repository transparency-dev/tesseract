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

package tesseract

import (
	"context"
	"crypto"
	"crypto/x509"
	"encoding/asn1"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/transparency-dev/tesseract/internal/ccadb"
	"github.com/transparency-dev/tesseract/internal/ct"
	"github.com/transparency-dev/tesseract/internal/x509util"
	"github.com/transparency-dev/tesseract/storage"
	"k8s.io/klog/v2"
)

// ChainValidationConfig contains parameters to configure chain validation.
type ChainValidationConfig struct {
	// RootsPEMFile is the path to the file containing root certificates that
	// are acceptable to the log. The certs are served through get-roots
	// endpoint.
	RootsPEMFile string
	// RootsRemoteFetchURL configures an endpoint to fetch additional roots from.
	RootsRemoteFetchURL string
	// RootsRemoteFetchInterval configures the frequency at which to fetch
	// roots from RootsRemoteEndpoint.
	RootsRemoteFetchInterval time.Duration
	// RejectExpired controls if true then the certificate validity period will be
	// checked against the current time during the validation of submissions.
	// This will cause expired certificates to be rejected.
	RejectExpired bool
	// RejectUnexpired controls if TesseraCT rejects certificates that are
	// either currently valid or not yet valid.
	// TODO(phboneff): evaluate whether we need to keep this one.
	RejectUnexpired bool
	// ExtKeyUsages lists Extended Key Usage values that newly submitted
	// certificates MUST contain. By default all are accepted. The
	// values specified must be ones known to the x509 package, comma separated.
	ExtKeyUsages string
	// RejectExtensions lists X.509 extension OIDs that newly submitted
	// certificates MUST NOT contain. Empty by default. Values must be
	// specificed in dotted string form (e.g. "2.3.4.5").
	RejectExtensions string
	// NotAfterStart defines the start of the range of acceptable NotAfter
	// values, inclusive.
	// Leaving this unset implies no lower bound to the range.
	NotAfterStart *time.Time
	// NotAfterLimit defines the end of the range of acceptable NotAfter values,
	// exclusive.
	// Leaving this unset implies no upper bound to the range.
	NotAfterLimit *time.Time
	// AcceptSHA1 specifies whether cert chains using SHA-1 based signing algorithms
	// are allowed.
	// CAUTION: This is a temporary solution and it will eventually be removed.
	// DO NOT depend on it.
	AcceptSHA1 bool
}

// systemTimeSource implements ct.TimeSource.
type systemTimeSource struct{}

// Now returns the true current local time.
func (s systemTimeSource) Now() time.Time {
	return time.Now()
}

var sysTimeSource = systemTimeSource{}

// newChainValidator checks that a chain validation config is valid,
// parses it, and loads resources to validate chains.
func newChainValidator(ctx context.Context, cfg ChainValidationConfig) (ct.ChainValidator, error) {
	// Load the trusted roots.
	if cfg.RootsPEMFile == "" {
		return nil, errors.New("empty rootsPemFile")
	}
	roots := x509util.NewPEMCertPool()
	if err := roots.AppendCertsFromPEMFile(cfg.RootsPEMFile); err != nil {
		return nil, fmt.Errorf("failed to read trusted roots: %v", err)
	}

	if cfg.RejectExpired && cfg.RejectUnexpired {
		return nil, errors.New("configuration would reject all certificates")
	}

	// Validate the time interval.
	if cfg.NotAfterStart != nil && cfg.NotAfterLimit != nil && (cfg.NotAfterLimit).Before(*cfg.NotAfterStart) {
		return nil, fmt.Errorf("'Not After' limit %q before start %q", cfg.NotAfterLimit.Format(time.RFC3339), cfg.NotAfterStart.Format(time.RFC3339))
	}

	var err error
	var extKeyUsages []x509.ExtKeyUsage
	// Filter which extended key usages are allowed.
	if cfg.ExtKeyUsages != "" {
		lExtKeyUsages := strings.Split(cfg.ExtKeyUsages, ",")
		extKeyUsages, err = ct.ParseExtKeyUsages(lExtKeyUsages)
		if err != nil {
			return nil, fmt.Errorf("failed to parse ExtKeyUsages: %v", err)
		}
	}

	var rejectExtIds []asn1.ObjectIdentifier
	// Filter which extensions are rejected.
	if cfg.RejectExtensions != "" {
		lRejectExtensions := strings.Split(cfg.RejectExtensions, ",")
		rejectExtIds, err = ct.ParseOIDs(lRejectExtensions)
		if err != nil {
			return nil, fmt.Errorf("failed to parse RejectExtensions: %v", err)
		}
	}

	if cfg.RootsRemoteFetchInterval > 0 && cfg.RootsRemoteFetchURL != "" {
		fetchAndAppendRemoteRoots := func() {
			rr, err := ccadb.Fetch(ctx, cfg.RootsRemoteFetchURL, []string{ccadb.ColPEM})
			if err != nil {
				klog.Errorf("Couldn't fetch roots from %q: %s", cfg.RootsRemoteFetchURL, err)
				return
			}
			pems := make([][]byte, 0, len(rr))
			for _, r := range rr {
				if len(r) < 1 {
					klog.Errorf("Couldn't parse root from %q: empty row", cfg.RootsRemoteFetchURL)
					continue
				}
				pems = append(pems, r[0])
			}
			parsed, added := roots.AppendCertsFromPEMs(pems...)
			klog.Infof("Fetched %d roots, parsed %d, and added %d from %q", len(pems), parsed, added, cfg.RootsRemoteFetchURL)
		}

		fetchAndAppendRemoteRoots()

		go func() {
			ticker := time.NewTicker(cfg.RootsRemoteFetchInterval)
			defer ticker.Stop()
			for {
				select {
				case <-ctx.Done():
					return
				case <-ticker.C:
					fetchAndAppendRemoteRoots()
				}
			}
		}()
	}

	cv := ct.NewChainValidator(roots, cfg.RejectExpired, cfg.RejectUnexpired, cfg.NotAfterStart, cfg.NotAfterLimit, extKeyUsages, rejectExtIds, cfg.AcceptSHA1)

	return cv, nil
}

// NotBeforeRL configures rate limits based on certificate not_before's age.
type NotBeforeRL struct {
	AgeThreshold time.Duration
	RateLimit    float64
}

type LogHandlerOpts struct {
	NotBeforeRL *NotBeforeRL
	DedupRL     float64
}

// NewLogHandler creates a Tessera based CT log plugged into HTTP handlers.
//
// HTTP server handlers implement static-ct-api submission APIs:
// https://c2sp.org/static-ct-api#submission-apis.
// It populates the data served via monitoring APIs (https://c2sp.org/static-ct-api#submission-apis)
// but it _does not_ implement monitoring APIs itself. Monitoring APIs should
// be served independently, either through the storage's system serving
// infrastructure directly (GCS over HTTPS for instance), or with an
// independent serving stack of your choice.
func NewLogHandler(ctx context.Context, origin string, signer crypto.Signer, cfg ChainValidationConfig, cs storage.CreateStorage, httpDeadline time.Duration, maskInternalErrors bool, pathPrefix string, opts LogHandlerOpts) (http.Handler, error) {
	cv, err := newChainValidator(ctx, cfg)
	if err != nil {
		return nil, fmt.Errorf("newCertValidationOpts(): %v", err)
	}
	log, err := ct.NewLog(ctx, origin, signer, cv, cs, sysTimeSource)
	if err != nil {
		return nil, fmt.Errorf("newLog(): %v", err)
	}

	ctOpts := &ct.HandlerOptions{
		Deadline:           httpDeadline,
		RequestLog:         &ct.DefaultRequestLog{},
		MaskInternalErrors: maskInternalErrors,
		TimeSource:         sysTimeSource,
		PathPrefix:         pathPrefix,
	}
	if opts.NotBeforeRL != nil {
		ctOpts.RateLimits.NotBefore(opts.NotBeforeRL.AgeThreshold, opts.NotBeforeRL.RateLimit)
	}
	if opts.DedupRL >= 0 {
		ctOpts.RateLimits.Dedup(opts.DedupRL)
	}

	handlers := ct.NewPathHandlers(ctx, ctOpts, log)
	mux := http.NewServeMux()
	// Register handlers for all the configured logs.
	for path, handler := range handlers {
		mux.Handle(path, handler)
	}

	// Health checking endpoint.
	mux.HandleFunc("/healthz", func(resp http.ResponseWriter, req *http.Request) {
		_, _ = fmt.Fprint(resp, "ok")
	})

	return mux, nil
}
