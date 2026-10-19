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
	"crypto/sha256"
	"crypto/x509"
	"encoding/asn1"
	"encoding/hex"
	"encoding/pem"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"slices"
	"strings"
	"time"

	"github.com/transparency-dev/tesseract/internal/ccadb"
	"github.com/transparency-dev/tesseract/internal/ct"
	"github.com/transparency-dev/tesseract/internal/x509util"
	"github.com/transparency-dev/tesseract/storage"
)

// ChainValidationConfig contains parameters to configure chain validation.
type ChainValidationConfig struct {
	// RootsPEMFile is the path to the file containing root certificates that
	// are acceptable to the log. The certs are served through get-roots
	// endpoint.
	RootsPEMFile string
	// RootsRemoteFetchURLs configures an endpoint to fetch additional roots from.
	RootsRemoteFetchURLs []string
	// RootsRemoteFetchInterval configures the frequency at which to fetch
	// roots from RootsRemoteEndpoint.
	RootsRemoteFetchInterval time.Duration
	// RootsRemoteFetchBackup is a persistent backup storage for roots fetched
	// remotely.
	RootsRemoteFetchBackup storage.RootsStorage
	// RejectRoots is a list of hex-encoded SHA-256 fingerprints of ASN.1 DER
	// encoded root certificates or their SubjectPublicKeyInfo that should never be trusted.
	RejectRoots []string
	// RejectIntermediates is a list of hex-encoded SHA-256 fingerprints of ASN.1 DER
	// encoded intermediate certificates or their SubjectPublicKeyInfo that should never be trusted.
	RejectIntermediates []string
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

var defaultBlockedSPKIs = []string{
	"ebed6475c2c2d80fcb199f3fe8dd6e6b3ce60b77eb15d4e42c3dec0c6f5b5105",
	"49dd353dc2b919a3770645117d0e60c16115e7d1bc259eefe734b00ea434fe5f",
	"e0254c7fcdc265e2f0f8a428dabb75ab4ae039b4872d62f5ee57f0877d5a8a41",
	"5c0660a2b164910d8ff589c18e14552e9e0ec237dcefe2cb736d41a4811f81a0",
	"43bffa19d6ef651451f6841e019931e96f6f2eaeff28ab4b6f1ce4576de2717b",
	"63ecefee431d49523bc1136c4e72d0206a477086caf606cd5cf025686c109cf8",
	"ec4ffa1ad0ac88081314b7aaffef6a91f97cd57291bb5ba686c5415f5eb4f6b6",
	"92c063d67e8f14db71d288e11266ffdaf69cbf115f7bab14f3bc2f68d25e0e30",
}

// newChainValidator checks that a chain validation config is valid,
// parses it, and loads resources to validate chains.
func newChainValidator(ctx context.Context, cfg ChainValidationConfig) (ct.ChainValidator, error) {
	// Load the trusted roots.
	if cfg.RootsPEMFile == "" {
		return nil, errors.New("empty rootsPemFile")
	}

	roots, err := x509util.NewPEMCertPool(cfg.RejectRoots)
	if err != nil {
		return nil, fmt.Errorf("failed to create roots pool: %v", err)
	}
	if err := roots.AppendCertsFromPEMFile(cfg.RootsPEMFile); err != nil {
		return nil, fmt.Errorf("failed to read trusted roots from %q: %v", cfg.RootsPEMFile, err)
	}

	rejectIntermediates := slices.Concat(defaultBlockedSPKIs, cfg.RejectIntermediates)
	if _, err := x509util.NewPEMCertPool(rejectIntermediates); err != nil {
		return nil, fmt.Errorf("failed to validate rejected intermediates: %v", err)
	}

	if cfg.RejectExpired && cfg.RejectUnexpired {
		return nil, errors.New("configuration would reject all certificates")
	}

	// Validate the time interval.
	if cfg.NotAfterStart != nil && cfg.NotAfterLimit != nil && (cfg.NotAfterLimit).Before(*cfg.NotAfterStart) {
		return nil, fmt.Errorf("'Not After' limit %q before start %q", cfg.NotAfterLimit.Format(time.RFC3339), cfg.NotAfterStart.Format(time.RFC3339))
	}

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

	if cfg.RootsRemoteFetchBackup != nil {
		kvs, err := cfg.RootsRemoteFetchBackup.LoadAll(ctx)
		if err != nil {
			return nil, fmt.Errorf("failed to load previously remotely fetched root from remote root backup storage: %v", err)
		}
		certs := make([][]byte, 0, len(kvs))
		for _, kv := range kvs {
			certs = append(certs, kv.V)
		}
		parsed, added := roots.AppendCertsFromPEMs(certs...)
		slog.InfoContext(ctx, "Fetched roots from remote root backup storage", slog.Int("fetched", len(certs)), slog.Int("parsed", parsed), slog.Int("added", added))
	}

	if cfg.RootsRemoteFetchInterval > 0 && len(cfg.RootsRemoteFetchURLs) > 0 {
		fetchAndAppendRemoteRoots := func(url string) {
			rr, err := ccadb.Fetch(ctx, url, []string{ccadb.ColPEM})
			if err != nil {
				slog.ErrorContext(ctx, "Couldn't fetch roots", slog.String("url", url), slog.Any("error", err))
				return
			}
			pems := make([][]byte, 0, len(rr))
			for _, r := range rr {
				if len(r) < 1 {
					slog.ErrorContext(ctx, "Couldn't parse root: empty row", slog.String("url", url))
					continue
				}
				pems = append(pems, r[0])
				if cfg.RootsRemoteFetchBackup != nil {
					block, _ := pem.Decode(r[0])
					if block == nil {
						slog.ErrorContext(ctx, "Failed to decode PEM block in fetched data", slog.String("url", url))
						continue
					}
					sha := sha256.Sum256(block.Bytes)
					key := []byte(hex.EncodeToString(sha[:]))
					if err := cfg.RootsRemoteFetchBackup.AddIfNotExist(ctx, []storage.KV{{K: key, V: r[0]}}); err != nil {
						slog.ErrorContext(ctx, "Couldn't store roots", slog.String("key", string(key)), slog.Any("error", err))
						continue
					}
				}
			}
			parsed, added := roots.AppendCertsFromPEMs(pems...)
			slog.InfoContext(ctx, "Fetched roots", slog.Int("fetched", len(pems)), slog.Int("parsed", parsed), slog.Int("added", added), slog.String("url", url))
		}

		for _, url := range cfg.RootsRemoteFetchURLs {
			fetchAndAppendRemoteRoots(url)
		}

		go func() {
			ticker := time.NewTicker(cfg.RootsRemoteFetchInterval)
			defer ticker.Stop()
			for {
				select {
				case <-ctx.Done():
					return
				case <-ticker.C:
					for _, url := range cfg.RootsRemoteFetchURLs {
						fetchAndAppendRemoteRoots(url)
					}
				}
			}
		}()
	}

	cv := ct.NewChainValidator(roots, rejectIntermediates, cfg.RejectExpired, cfg.RejectUnexpired, cfg.NotAfterStart, cfg.NotAfterLimit, extKeyUsages, rejectExtIds, cfg.AcceptSHA1)

	return cv, nil
}

// NotBeforeRL configures rate limits based on certificate not_before's age.
type NotBeforeRL struct {
	AgeThreshold time.Duration
	RateLimit    float64
}

type LogHandlerOpts struct {
	NotBeforeRL       *NotBeforeRL
	DedupRL           float64
	MaxCertChainBytes int64
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
		mux.Handle(path, http.MaxBytesHandler(handler, opts.MaxCertChainBytes))
	}

	// Health checking endpoint.
	mux.HandleFunc("/healthz", func(resp http.ResponseWriter, req *http.Request) {
		_, _ = fmt.Fprint(resp, "ok")
	})

	return mux, nil
}
