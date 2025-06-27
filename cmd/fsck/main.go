// Copyright 2025 The Tessera authors. All Rights Reserved.
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

// fsck is a command-line tool for checking the integrity of a static-ct based log.
package main

import (
	"context"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"errors"
	"flag"
	"fmt"
	"net/http"
	"net/url"
	"sync"
	"time"

	tdnote "github.com/transparency-dev/formats/note"
	"github.com/transparency-dev/merkle/rfc6962"
	"github.com/transparency-dev/tessera/api/layout"
	"github.com/transparency-dev/tessera/fsck"
	"github.com/transparency-dev/tesseract/internal/client"
	"golang.org/x/crypto/cryptobyte"
	"golang.org/x/mod/sumdb/note"
	"golang.org/x/sync/errgroup"
	"k8s.io/klog/v2"
)

var (
	monitoringURL = flag.String("monitoring_url", "", "Base tlog-tiles URL")
	bearerToken   = flag.String("bearer_token", "", "The bearer token for authorizing HTTP requests to the storage URL, if needed")
	N             = flag.Uint("N", 1, "The number of workers to use when fetching/comparing resources")
	origin        = flag.String("origin", "", "Origin of the log to check")
	pubKey        = flag.String("public_key", "", "The log's public key in base64 encoded DER format")
)

func main() {
	klog.InitFlags(nil)
	flag.Parse()
	ctx := context.Background()
	logURL, err := url.Parse(*monitoringURL)
	if err != nil {
		klog.Exitf("Invalid --storage_url %q: %v", *monitoringURL, err)
	}
	hc := &http.Client{
		Transport: &http.Transport{
			MaxIdleConns:        int(*N),
			MaxIdleConnsPerHost: int(*N),
			DisableKeepAlives:   false,
		},
		Timeout: 30 * time.Second,
	}
	src, err := client.NewHTTPFetcher(logURL, hc)
	if err != nil {
		klog.Exitf("Failed to create HTTP fetcher: %v", err)
	}
	src.EnableRetries(10)
	if *bearerToken != "" {
		src.SetAuthorizationHeader(fmt.Sprintf("Bearer %s", *bearerToken))
	}
	v := verifierFromFlags()
	lsc := newLogStateCollector()
	eg := errgroup.Group{}
	eg.Go(func() error {
		defer lsc.Close()
		return fsck.Check(ctx, *origin, v, src, *N, lsc.merkleLeafHasher())

	})
	eg.Go(func() error {
		return lsc.checkIssuersTask(ctx, src.ReadIssuer, *N)
	})

	if err := eg.Wait(); err != nil {
		klog.Exitf("FAILED:\n%v", err)
	}

	klog.Info("OK")
}

// logStateCollector tracks state of the target log which needs to be later checked.
//
// Currently, this is centred around the discovery and checking of issuers during entry bundle parsing.
type logStateCollector struct {
	// issuersSeen contains the set of issuer fingerprints issuersSeen in the log so far.
	issuersSeen sync.Map
	// issuersToCheck is a channel of issuer fingerprints to look up in the target log's issuer CAS.
	issuersToCheck chan []byte
}

// newLogStateCollector creates a new logStateCollector instance.
func newLogStateCollector() *logStateCollector {
	return &logStateCollector{
		issuersToCheck: make(chan []byte),
	}
}

// Close should be called once no further issuers will be added.
func (l *logStateCollector) Close() {
	close(l.issuersToCheck)
}

// checkIssuersTask reads looks up discovered issuer fingerprints in the target log's issuer CAS.
//
// This is a long-running function, it will only exit once Close has been called, and all remaining fingerprints in the
// issuersToCheck channel have been checked.
func (l *logStateCollector) checkIssuersTask(ctx context.Context, readIssuer func(context.Context, []byte) ([]byte, error), N uint) error {
	klog.Infof("Checking issuers CAS")
	errC := make(chan error)
	wg := sync.WaitGroup{}
	for range N {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for fp := range l.issuersToCheck {
				if _, err := readIssuer(ctx, fp); err != nil {
					klog.Warningf("Couldn't fetch issuer FP %x: %v", fp, err)
					errC <- fmt.Errorf("couldn't fetch issuer for %x: %v", fp, err)
					continue
				}
				klog.V(2).Infof("Issuer FP %x is present", fp)
			}
		}()
	}
	wg.Wait()
	close(errC)

	errs := []error{}
	for e := range errC {
		errs = append(errs, e)
	}
	return errors.Join(errs...)
}

// addIssuers adds the issuers in the provided byte string to the set of issuer to be checked.
func (l *logStateCollector) addIssuers(fpRaw cryptobyte.String) {
	var fp []byte
	for len(fpRaw) > 0 {
		fp, fpRaw = fpRaw[:32], fpRaw[32:]
		_, existed := l.issuersSeen.LoadOrStore(string(fp), true)
		if !existed {
			klog.V(2).Infof("Found issuer FP %x", fp)
			l.issuersToCheck <- fp
		}
	}
}

// merkleLeafHasher returns a function which knows how to:
// - calculate RFC6962 Merkle leaf hashes for entries in a Static-CT formatted entry bundle,
// - keep track of the set of issuer cert fingerprints seen while parsing entry bundles.
func (l *logStateCollector) merkleLeafHasher() func(bundle []byte) ([][]byte, error) {
	return func(bundle []byte) ([][]byte, error) {
		r := make([][]byte, 0, layout.EntryBundleWidth)
		b := cryptobyte.String(bundle)
		for i := 0; i < layout.EntryBundleWidth && !b.Empty(); i++ {
			preimage := &cryptobyte.Builder{}
			preimage.AddUint8(0 /* version = v1 */)
			preimage.AddUint8(0 /* leaf_type = timestamped_entry */)

			// Timestamp
			if !copyBytes(&b, preimage, 8) {
				return nil, fmt.Errorf("failed to copy timestamp of entry index %d of bundle", i)
			}

			var entryType uint16
			if !b.ReadUint16(&entryType) {
				return nil, fmt.Errorf("failed to read entry type of entry index %d of bundle", i)
			}
			preimage.AddUint16(entryType)

			switch entryType {
			case 0: // X509 entry
				if !copyUint24LengthPrefixed(&b, preimage) {
					return nil, fmt.Errorf("failed to copy certificate at entry index %d of bundle", i)
				}

			case 1: // Precert entry
				// IssuerKeyHash
				if !copyBytes(&b, preimage, sha256.Size) {
					return nil, fmt.Errorf("failed to copy issuer key hash at entry index %d of bundle", i)
				}

				if !copyUint24LengthPrefixed(&b, preimage) {
					return nil, fmt.Errorf("failed to copy precert tbs at entry index %d of bundle", i)
				}

			default:
				return nil, fmt.Errorf("unknown entry type 0x%x at entry index %d of bundle", entryType, i)
			}

			if !copyUint16LengthPrefixed(&b, preimage) {
				return nil, fmt.Errorf("failed to copy SCT extensions at entry index %d of bundle", i)
			}

			ignore := cryptobyte.String{}
			if entryType == 1 {
				if !b.ReadUint24LengthPrefixed(&ignore) {
					return nil, fmt.Errorf("failed to read precert at entry index %d of bundle", i)
				}
			}
			fpRaw := cryptobyte.String{}
			if !b.ReadUint16LengthPrefixed(&fpRaw) {
				return nil, fmt.Errorf("failed to read chain fingerprints at entry index %d of bundle", i)
			}
			l.addIssuers(fpRaw)

			h := rfc6962.DefaultHasher.HashLeaf(preimage.BytesOrPanic())
			r = append(r, h)
		}
		if !b.Empty() {
			return nil, fmt.Errorf("unexpected %d bytes of trailing data in entry bundle", len(b))
		}
		return r, nil
	}
}

// copyBytes copies N bytes between from and to.
func copyBytes(from *cryptobyte.String, to *cryptobyte.Builder, N int) bool {
	b := make([]byte, N)
	if !from.ReadBytes(&b, N) {
		return false
	}
	to.AddBytes(b)
	return true
}

// copyUint16LengthPrefixed copies a uint16 length and value between from and to.
func copyUint16LengthPrefixed(from *cryptobyte.String, to *cryptobyte.Builder) bool {
	b := cryptobyte.String{}
	if !from.ReadUint16LengthPrefixed(&b) {
		return false
	}
	to.AddUint16LengthPrefixed(func(c *cryptobyte.Builder) {
		c.AddBytes(b)
	})
	return true
}

// copyUint24LengthPrefixed copies a uint24 length and value between from and to.
func copyUint24LengthPrefixed(from *cryptobyte.String, to *cryptobyte.Builder) bool {
	b := cryptobyte.String{}
	if !from.ReadUint24LengthPrefixed(&b) {
		return false
	}
	to.AddUint24LengthPrefixed(func(c *cryptobyte.Builder) {
		c.AddBytes(b)
	})
	return true
}

func verifierFromFlags() note.Verifier {
	if *origin == "" {
		klog.Exitf("Must provide the --origin flag")
	}
	if *pubKey == "" {
		klog.Exitf("Must provide the --pub_key flag")
	}
	derBytes, err := base64.StdEncoding.DecodeString(*pubKey)
	if err != nil {
		klog.Exitf("Error decoding public key: %s", err)
	}
	pub, err := x509.ParsePKIXPublicKey(derBytes)
	if err != nil {
		klog.Exitf("Error parsing public key: %v", err)
	}

	verifierKey, err := tdnote.RFC6962VerifierString(*origin, pub)
	if err != nil {
		klog.Exitf("Error creating RFC6962 verifier string: %v", err)
	}
	logSigV, err := tdnote.NewVerifier(verifierKey)
	if err != nil {
		klog.Exitf("Error creating verifier: %v", err)
	}

	klog.Infof("Using verifier string: %v", verifierKey)

	return logSigV
}
