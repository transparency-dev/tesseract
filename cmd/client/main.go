package main

import (
	"bytes"
	"context"
	"crypto/x509"
	"encoding/base64"
	"encoding/hex"
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
	"github.com/transparency-dev/merkle/proof"
	"github.com/transparency-dev/merkle/rfc6962"
	"github.com/transparency-dev/tessera/api/layout"
	"github.com/transparency-dev/tessera/ctonly"
	"github.com/transparency-dev/tesseract/internal/client"
	"github.com/transparency-dev/tesseract/internal/types/staticct"
	"github.com/transparency-dev/tesseract/internal/x509util"
	"golang.org/x/mod/sumdb/note"
	"k8s.io/klog/v2"
)

var (
	monitoringURL = flag.String("monitoring_url", "", "Log monitoring URL.")
	leafIndex     = flag.String("leaf_index", "", "The index of the leaf to fetch.")
	origin        = flag.String("origin", "", "Origin of the log, for checkpoints and the monitoring prefix.")
	logPubKey     = flag.String("log_public_key", "", "Public key for the log, base64 encoded.")
)

var (
	hasher = rfc6962.DefaultHasher
)

func main() {
	// Verify Flags
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

	// Create client
	hc := &http.Client{
		Timeout: 30 * time.Second,
	}
	fetcher, err := client.NewHTTPFetcher(logURL, hc)
	if err != nil {
		klog.Exitf("Failed to create HTTP fetcher: %v", err)
	}
	ctx := context.Background()

	// Read Checkpoint
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

	// Fetch entry
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

	// Check that the entry has been built properly
	e := ctonly.Entry{
		Timestamp:         entry.Timestamp,
		IsPrecert:         entry.IsPrecert,
		Certificate:       entry.Certificate,
		Precertificate:    entry.Precertificate,
		IssuerKeyHash:     entry.IssuerKeyHash,
		FingerprintsChain: entry.FingerprintsChain,
	}
	var chain []*x509.Certificate
	if e.IsPrecert {
		cert, err := x509.ParseCertificate(e.Precertificate)
		if err != nil {
			klog.Exitf("Failed to parse precertificate: %v", err)
		}
		chain = append(chain, cert)
	} else {
		cert, err := x509.ParseCertificate(e.Certificate)
		if err != nil {
			klog.Exitf("Failed to parse precertificate: %v", err)
		}
		chain = append(chain, cert)
	}
	for i, hash := range entry.FingerprintsChain {
		iss, err := fetcher.ReadIssuer(ctx, hash[:])
		if err != nil {
			klog.Exitf("Failed to fetch issuer number %d: %v", i, err)
		}
		cert, err := x509.ParseCertificate(iss)
		if err != nil {
			klog.Exitf("Failed ot parse issuer number %d: %v", i, err)
		}
		chain = append(chain, cert)
	}
	ee, err := x509util.EntryFromChain(chain, entry.IsPrecert, entry.Timestamp)
	if err != nil {
		klog.Exitf("Failed to reconstruct entry from the leaf and issuers: %v", err)
	}

	var errs []error
	if e.Timestamp != ee.Timestamp {
		errs = append(errs, fmt.Errorf("timestamp don't match: %d, %d", e.Timestamp, ee.Timestamp))
	}
	if e.IsPrecert != ee.IsPrecert {
		errs = append(errs, fmt.Errorf("IsPrecert don't match: %t, %t", e.IsPrecert, ee.IsPrecert))
	}
	if !bytes.Equal(e.Certificate, ee.Certificate) {
		if e.IsPrecert {
			errs = append(errs, fmt.Errorf("TBSCertificates don't match"))
		} else {
			errs = append(errs, fmt.Errorf("certificates don't match"))
		}
	}
	if !bytes.Equal(e.Precertificate, ee.Precertificate) {
		errs = append(errs, fmt.Errorf("precertificates don't match"))
	}
	if !bytes.Equal(e.IssuerKeyHash, ee.IssuerKeyHash) {
		errs = append(errs, fmt.Errorf("IssuerKeyHashes don't match, got %q, want %q", hex.EncodeToString(e.IssuerKeyHash), hex.EncodeToString(ee.IssuerKeyHash)))
	}
	if len(e.FingerprintsChain) != len(ee.FingerprintsChain) {
		errs = append(errs, fmt.Errorf("lengths of fingerprints chains don't match: got %d, want %d", len(e.FingerprintsChain), len(ee.FingerprintsChain)))
	} else {
		for i := range e.FingerprintsChain {
			if !bytes.Equal(e.FingerprintsChain[i][:], ee.FingerprintsChain[i][:]) {
				errs = append(errs, fmt.Errorf("fingerprints %d don't match, got %q, want %q", i, hex.EncodeToString(e.FingerprintsChain[i][:]), hex.EncodeToString(ee.FingerprintsChain[i][:])))
			}
		}
	}
	if len(errs) > 0 {
		klog.Exitf("Leaf entry not built properly: %v", errors.Join(errs...))
	}

	// TODO(phboneff): check that the chain is valid
	// TODO(phboneff): if this is an end cert and it has an SCT from this very log, check that SCT

	// Build inclusion proof
	proofBuilder, err := client.NewProofBuilder(ctx, log.Checkpoint{
		Origin: *origin,
		Size:   cp.Size,
		Hash:   cp.Hash}, fetcher.ReadTile)
	if err != nil {
		klog.Exitf("Failed to create proofBuilder: %v", err)
	}
	mlh := e.MerkleLeafHash(entry.LeafIndex)
	ip, err := proofBuilder.InclusionProof(ctx, li)
	if err != nil {
		klog.Exitf("Failed to build InclusionProof %v", err)
	}
	if err := proof.VerifyInclusion(hasher, li, cp.Size, mlh, ip, cp.Hash); err != nil {
		klog.Exitf("Failed to verify inclusion of leaf %d in tree of size %d: %v", li, cp.Size, err)
	}

	pemBlock := &pem.Block{
		Type: "CERTIFICATE",
		Bytes: func() []byte {
			if entry.IsPrecert {
				return entry.Precertificate
			}
			return entry.Certificate
		}(),
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
