package main

import (
	"context"
	"crypto/x509"
	"encoding/base64"
	"encoding/pem"
	"errors"
	"flag"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"time"

	"github.com/transparency-dev/formats/log"
	tdnote "github.com/transparency-dev/formats/note"
	"github.com/transparency-dev/tessera/api/layout"
	"github.com/transparency-dev/tesseract/internal/client"
	"github.com/transparency-dev/tesseract/internal/types/staticct"
	"golang.org/x/mod/sumdb/note"
	"k8s.io/klog/v2"
)

var (
	monitoringURL = flag.String("monitoring_url", "", "Base tlog-tiles URL")
	leafIndex     = flag.String("leaf_index", "", "The index of the leaf to fetch")
	origin        = flag.String("origin", os.Getenv("CT_LOG_ORIGIN"), "Origin of the log, for checkpoints and the monitoring prefix. This is defaulted to the environment variable CT_LOG_ORIGIN")
	logPubKey     = flag.String("log_public_key", os.Getenv("CT_LOG_PUBLIC_KEY"), "Public key for the log. This is defaulted to the environment variable CT_LOG_PUBLIC_KEY")
)

func main() {
	klog.InitFlags(nil)
	flag.Parse()

	if *monitoringURL == "" {
		klog.Exitf("--monitoring_url must be set")
	}
	if *leafIndex == "" {
		klog.Exitf("--leaf_index must be set")
	}
	li, err := strconv.ParseUint(*leafIndex, 10, 64)
	if err != nil {
		klog.Exitf("Invalid --leaf_index: %v", err)
	}

	logURL, err := url.Parse(*monitoringURL)
	if err != nil {
		klog.Exitf("Invalid --monitoring_url %q: %v", *monitoringURL, err)
	}
	hc := &http.Client{
		Timeout: 30 * time.Second,
	}
	fetcher, err := client.NewHTTPFetcher(logURL, hc)
	if err != nil {
		klog.Exitf("Failed to create HTTP fetcher: %v", err)
	}

	ctx := context.Background()

	cpRaw, err := fetcher.ReadCheckpoint(ctx)
	if err != nil {
		klog.Exitf("Failed to fetch checkpoint: %v", err)
	}
	logSigV, err := logSigVerifier(*origin, *logPubKey)
	if err != nil {
		klog.Exitf("Failed to create verifier: %v", err)
	}
	cp, _, _, err := log.ParseCheckpoint(cpRaw, *origin, logSigV)
	if err != nil {
		klog.Exitf("Failed to parse checkpoint: %v", err)
	}

	if li >= cp.Size {
		klog.Exitf("Leaf index %d is out of range for log size %d", li, cp.Size)
	}

	bundleIndex := li / uint64(layout.EntryBundleWidth)
	indexInBundle := li % uint64(layout.EntryBundleWidth)

	bundle, err := client.GetEntryBundle(ctx, fetcher.ReadEntryBundle, bundleIndex, cp.Size)
	if err != nil {
		klog.Exitf("Failed to get entry bundle: %v", err)
	}

	if int(indexInBundle) >= len(bundle.Entries) {
		klog.Exitf("Index %d is out of range for bundle of size %d", indexInBundle, len(bundle.Entries))
	}
	entryData := bundle.Entries[indexInBundle]

	var entry staticct.Entry
	if err := entry.UnmarshalText(entryData); err != nil {
		klog.Exitf("Failed to unmarshal entry: %v", err)
	}

	certBytes := entry.Certificate
	if entry.IsPrecert {
		// For precertificates, the `Certificate` field holds the TBSCertificate.
		// We need to wrap this in a `Certificate` structure to be able to parse it.
		// This is a bit of a hack, but it's what the `x509` package expects.
		cert, err := x509.ParseCertificate(entry.Precertificate)
		if err != nil {
			klog.Exitf("Failed to parse precertificate: %v", err)
		}
		certBytes = cert.Raw
	}

	pemBlock := &pem.Block{
		Type:  "CERTIFICATE",
		Bytes: certBytes,
	}

	if err := pem.Encode(os.Stdout, pemBlock); err != nil {
		klog.Exitf("Failed to encode PEM: %v", err)
	}
}

// logSigVerifier creates a note.Verifier for the Static CT API log by taking
// an origin string and a base64-encoded public key.
func logSigVerifier(origin, b64PubKey string) (note.Verifier, error) {
	if origin == "" {
		return nil, errors.New("origin cannot be empty")
	}
	if b64PubKey == "" {
		return nil, errors.New("log public key cannot be empty")
	}

	derBytes, err := base64.StdEncoding.DecodeString(b64PubKey)
	if err != nil {
		return nil, fmt.Errorf("error decoding public key: %s", err)
	}
	pub, err := x509.ParsePKIXPublicKey(derBytes)
	if err != nil {
		return nil, fmt.Errorf("error parsing public key: %v", err)
	}

	verifierKey, err := tdnote.RFC6962VerifierString(origin, pub)
	if err != nil {
		return nil, fmt.Errorf("error creating RFC6962 verifier string: %v", err)
	}
	logSigV, err := tdnote.NewVerifier(verifierKey)
	if err != nil {
		return nil, fmt.Errorf("error creating verifier: %v", err)
	}

	return logSigV, nil
}
