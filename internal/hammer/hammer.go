// Copyright 2024 The Tessera authors. All Rights Reserved.
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

// hammer is a tool to load test a Static CT API log.
package main

import (
	"bytes"
	"context"
	"crypto/ecdh"
	"crypto/ecdsa"
	"crypto/ed25519"
	"crypto/rsa"
	"crypto/tls"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"errors"
	"flag"
	"fmt"
	"io"
	"math/rand/v2"
	"net"
	"net/http"
	"net/http/httptrace"
	"net/url"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"

	tdnote "github.com/transparency-dev/formats/note"
	"github.com/transparency-dev/tesseract/internal/client"
	"github.com/transparency-dev/tesseract/internal/client/gcp"
	"github.com/transparency-dev/tesseract/internal/hammer/loadtest"
	"github.com/transparency-dev/tesseract/internal/types/rfc6962"
	"github.com/transparency-dev/tesseract/internal/types/staticct"
	"golang.org/x/mod/sumdb/note"
	"golang.org/x/net/http2"

	"k8s.io/klog/v2"
)

func init() {
	flag.Var(&logURL, "log_url", "Log storage root URL (can be specified multiple times), e.g. https://log.server/and/path/")
	flag.Var(&writeLogURL, "write_log_url", "Root URL for writing to a log (can be specified multiple times), e.g. https://log.server/and/path/ (optional, defaults to log_url)")
}

var (
	logURL      multiStringFlag
	writeLogURL multiStringFlag

	origin                    = flag.String("origin", os.Getenv("CT_LOG_ORIGIN"), "Origin of the log, for checkpoints and the monitoring prefix. This is defaulted to the environment variable CT_LOG_ORIGIN")
	logPubKey                 = flag.String("log_public_key", os.Getenv("CT_LOG_PUBLIC_KEY"), "Public key for the log. This is defaulted to the environment variable CT_LOG_PUBLIC_KEY")
	intermediateCACertPath    = flag.String("intermediate_ca_cert_path", "./internal/hammer/testdata/test_intermediate_ca_cert.pem", "Intermediate CA certificate path for certificate generator")
	intermediateCAKeyPath     = flag.String("intermediate_ca_key_path", "./internal/hammer/testdata/test_intermediate_ca_private_key.pem", "Intermediate CA key path for certificate generator (Only RSA is accepted)")
	certSigningPrivateKeyPath = flag.String("cert_sign_private_key_path", "./internal/hammer/testdata/test_leaf_cert_signing_private_key.pem", "Certificate signing private key path for certificate generator (Only RSA is accepted)")

	maxReadOpsPerSecond = flag.Int("max_read_ops", 20, "The maximum number of read operations per second")
	numReadersRandom    = flag.Int("num_readers_random", 4, "The number of readers looking for random leaves")
	numReadersFull      = flag.Int("num_readers_full", 4, "The number of readers downloading the whole log")

	maxWriteOpsPerSecond = flag.Int("max_write_ops", 0, "The maximum number of write operations per second")
	numWriters           = flag.Int("num_writers", 0, "The number of independent write tasks to run")
	numMMDVerifiers      = flag.Int("num_mmd_verifiers", 0, "The number of MMD verifiers performing inclusion proof for the added leaves")
	mmdDuration          = flag.Duration("mmd_duration", 10*time.Second, "The Maximum Merge Delay (MMD) duration of the log")

	dupChance    = flag.Float64("dup_chance", 0.1, "The probability of a generated leaf being a duplicate of a previous value")
	serialOffset = flag.Int64("serial_offset", 0, "The certificate serial number offset")

	leafWriteGoal = flag.Int64("leaf_write_goal", 0, "Exit after writing this number of leaves, or 0 to keep going indefinitely")
	maxRunTime    = flag.Duration("max_runtime", 0, "Fail after this amount of time has passed, or 0 to keep going indefinitely")

	showUI = flag.Bool("show_ui", true, "Set to false to disable the text-based UI")

	bearerToken      = flag.String("bearer_token", "", "The bearer token for auth. For GCP this is the result of `gcloud auth print-access-token`")
	bearerTokenWrite = flag.String("bearer_token_write", "", "The bearer token for auth to write. For GCP this is the result of `gcloud auth print-identity-token`. If unset will default to --bearer_token.")

	httpTimeout = flag.Duration("http_timeout", 30*time.Second, "Timeout for HTTP requests")
	forceHTTP2  = flag.Bool("force_http2", false, "Use HTTP/2 connections *only*")

	hc *http.Client
)

func main() {
	klog.InitFlags(nil)
	flag.Parse()

	hc = &http.Client{
		Transport: &http.Transport{
			MaxIdleConns:        *numWriters + *numReadersFull + *numReadersRandom,
			MaxIdleConnsPerHost: *numWriters + *numReadersFull + *numReadersRandom,
			DisableKeepAlives:   false,
		},
		Timeout: *httpTimeout,
	}
	if *forceHTTP2 {
		hc.Transport = &http2.Transport{
			// So http2.Transport doesn't complain the URL scheme isn't 'https'
			AllowHTTP: true,
			// Pretend we are dialing a TLS endpoint. (Note, we ignore the passed tls.Config)
			DialTLSContext: func(ctx context.Context, network, addr string, cfg *tls.Config) (net.Conn, error) {
				var d net.Dialer
				return d.DialContext(ctx, network, addr)
			},
		}
	}

	// If bearerTokenWrite is unset, default it to whatever bearerToken has (which may too be unset).
	if *bearerTokenWrite == "" {
		*bearerTokenWrite = *bearerToken
	}

	ctx, cancel := context.WithCancel(context.Background())

	logSigV, err := logSigVerifier(*origin, *logPubKey)
	if err != nil {
		klog.Exitf("Failed to create verifier: %v", err)
	}

	r := mustCreateReaders(ctx, logURL)
	if len(writeLogURL) == 0 {
		writeLogURL = logURL
	}
	w := mustCreateWriters(writeLogURL)

	var cpRaw []byte
	cons := client.UnilateralConsensus(r.ReadCheckpoint)
	tracker, err := client.NewLogStateTracker(ctx, r.ReadCheckpoint, r.ReadTile, cpRaw, logSigV, logSigV.Name(), cons)
	if err != nil {
		klog.Exitf("Failed to create LogStateTracker: %v", err)
	}
	// Fetch initial state of log
	_, _, _, err = tracker.Update(ctx)
	if err != nil {
		klog.Exitf("Failed to get initial state of the log: %v", err)
	}

	ha := loadtest.NewHammerAnalyser(func() uint64 { return tracker.LatestConsistent.Size })
	ha.Run(ctx)

	intermediateCACert, err := loadIntermediateCACert(*intermediateCACertPath)
	if err != nil {
		klog.Exitf("Failed to load intermediate CA certificate from %s: %v", *intermediateCACertPath, err)
	}
	intermediateCAKey, err := loadPrivateKey(*intermediateCAKeyPath)
	if err != nil {
		klog.Exitf("Failed to load intermediate CA private key from %s: %v", *intermediateCAKeyPath, err)
	}
	if err := verifySupportedKeyAlgorithm(intermediateCAKey); err != nil {
		klog.Exitf("Failed to support intermediate CA key algorithm for generating deterministic certificate: %v", err)
	}
	privateKey, err := loadPrivateKey(*certSigningPrivateKeyPath)
	if err != nil {
		klog.Exitf("Failed to load certificate signing private key from %s: %v", *certSigningPrivateKeyPath, err)
	}
	if err := verifySupportedKeyAlgorithm(privateKey); err != nil {
		klog.Exitf("Failed to support certificate signing private key algorithm for generating deterministic certificate: %v", err)
	}

	gen := newLeafGenerator(tracker.LatestConsistent.Size, *dupChance, intermediateCACert, intermediateCAKey, privateKey)
	opts := loadtest.HammerOpts{
		MaxReadOpsPerSecond:  *maxReadOpsPerSecond,
		MaxWriteOpsPerSecond: *maxWriteOpsPerSecond,
		NumReadersRandom:     *numReadersRandom,
		NumReadersFull:       *numReadersFull,
		NumWriters:           *numWriters,
		NumMMDVerifiers:      *numMMDVerifiers,
		MMDDuration:          *mmdDuration,
	}
	hammer := loadtest.NewHammer(&tracker, r.ReadEntryBundle, w, gen, ha.SeqLeafChan, ha.ErrChan, opts)

	exitCode := 0
	if *leafWriteGoal > 0 {
		go func() {
			startTime := time.Now()
			goal := tracker.LatestConsistent.Size + uint64(*leafWriteGoal)
			klog.Infof("Will exit once tree size is at least %d", goal)
			tick := time.NewTicker(1 * time.Second)
			for {
				select {
				case <-ctx.Done():
					return
				case <-tick.C:
					if tracker.LatestConsistent.Size >= goal {
						elapsed := time.Since(startTime)
						klog.Infof("Reached tree size goal of %d after %s; exiting", goal, elapsed)
						cancel()
						return
					}
				}
			}
		}()
	}
	if *maxRunTime > 0 {
		go func() {
			klog.Infof("Will fail after %s", *maxRunTime)
			for {
				select {
				case <-ctx.Done():
					return
				case <-time.After(*maxRunTime):
					klog.Infof("Max runtime reached; exiting")
					exitCode = 1
					cancel()
					return
				}
			}
		}()
	}
	hammer.Run(ctx)

	if *showUI {
		c := loadtest.NewController(hammer, ha)
		c.Run(ctx)
	} else {
		<-ctx.Done()
	}
	os.Exit(exitCode)
}

// newLeafGenerator returns a function that generates values to append to a log.
// The generator can be used by concurrent threads.
//
// dupChance provides the probability that a new leaf will be a duplicate of a previous entry.
// Leaves will be unique if dupChance is 0, and if set to 1 then all values will be duplicates.
// startSize should be set to the initial size of the log so that repeated runs of the
// hammer can start seeding leaves to avoid duplicates with previous runs.
func newLeafGenerator(startSize uint64, dupChance float64, intermediateCACert *x509.Certificate, intermediateCAKey, leafCertSigningPrivateKey any) func() []byte {
	certGen := newChainGenerator(intermediateCACert, intermediateCAKey, publicKey(leafCertSigningPrivateKey))

	sizeLocked := startSize
	var mu sync.Mutex
	return func() []byte {
		mu.Lock()
		thisSize := sizeLocked

		if thisSize > 0 && rand.Float64() <= dupChance {
			thisSize = rand.Uint64N(thisSize)
		} else {
			sizeLocked++
		}
		mu.Unlock()

		// Do this outside of the protected block so that writers don't block on leaf generation (especially for larger leaves).
		return certGen.addChainRequestBody(int64(thisSize) + *serialOffset)
	}
}

// multiStringFlag allows a flag to be specified multiple times on the command
// line, and stores all of these values.
type multiStringFlag []string

func (ms *multiStringFlag) String() string {
	return strings.Join(*ms, ",")
}

func (ms *multiStringFlag) Set(w string) error {
	*ms = append(*ms, w)
	return nil
}

func loadPrivateKey(path string) (any, error) {
	keyBytes, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("failed to read key file: %w", err)
	}

	block, _ := pem.Decode(keyBytes)
	if block == nil {
		key, err := x509.ParsePKCS8PrivateKey(keyBytes)
		if err == nil {
			return key, nil
		}
		rsaKey, err := x509.ParsePKCS1PrivateKey(keyBytes)
		if err == nil {
			return rsaKey, nil
		}

		ecKey, err := x509.ParseECPrivateKey(keyBytes)
		if err == nil {
			return ecKey, nil
		}

		return nil, fmt.Errorf("failed to decode PEM block and failed to parse as DER: %w", err)
	}

	// Fix block type for testing keys.
	block.Type = testingKey(block.Type)

	switch block.Type {
	case "RSA PRIVATE KEY":
		return x509.ParsePKCS1PrivateKey(block.Bytes)
	case "EC PRIVATE KEY":
		return x509.ParseECPrivateKey(block.Bytes)
	case "PRIVATE KEY":
		return x509.ParsePKCS8PrivateKey(block.Bytes)
	default:
		return nil, fmt.Errorf("unsupported PEM block type: %s", block.Type)
	}
}

func loadIntermediateCACert(path string) (*x509.Certificate, error) {
	certBytes, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("failed to read certificate file: %w", err)
	}

	block, rest := pem.Decode(certBytes)
	if block == nil {
		return nil, fmt.Errorf("failed to decode PEM block")
	}
	if block.Type != "CERTIFICATE" {
		return nil, fmt.Errorf("expected PEM block type 'CERTIFICATE', got '%s'", block.Type)
	}
	if len(rest) > 0 {
		klog.Info("Warning: More than one PEM block found. Parsing only the first.")
	}

	cert, err := x509.ParseCertificate(block.Bytes)
	if err != nil {
		return nil, fmt.Errorf("failed to parse X.509 certificate: %w", err)
	}

	return cert, nil
}

// publicKey returns the public key associated with the private key.
func publicKey(privKey any) any {
	switch k := privKey.(type) {
	case *rsa.PrivateKey:
		return k.Public()
	case *ecdsa.PrivateKey:
		return k.Public()
	case *ed25519.PrivateKey:
		return k.Public()
	default:
		klog.Fatalf("Unknown private key type: %T", privKey)
		return nil // Or panic, or return an error
	}
}

func testingKey(s string) string {
	return strings.ReplaceAll(s, "TEST PRIVATE KEY", "PRIVATE KEY")
}

// verifySupportedKeyAlgorithm returns an error if the key algorithm is not
// supported for generating deterministic certificates.
func verifySupportedKeyAlgorithm(key any) error {
	switch key.(type) {
	case *rsa.PrivateKey:
		return nil

	case *ecdsa.PrivateKey:
		return errors.New("ecdsa is not supported")

	case ed25519.PrivateKey:
		return errors.New("ed25519 is not supported")

	case *ecdh.PrivateKey:
		return errors.New("ecdh is not supported")

	default:
		return fmt.Errorf("unknown key type: %T", key)
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

func mustCreateReaders(ctx context.Context, us []string) loadtest.LogReader {
	r := []loadtest.LogReader{}
	for _, u := range us {
		if !strings.HasSuffix(u, "/") {
			u += "/"
		}
		rURL, err := url.Parse(u)
		if err != nil {
			klog.Exitf("Invalid log reader URL %q: %v", u, err)
		}

		switch rURL.Scheme {
		case "http", "https":
			c, err := client.NewHTTPFetcher(rURL, hc)
			if err != nil {
				klog.Exitf("Failed to create HTTP fetcher for %q: %v", u, err)
			}
			if *bearerToken != "" {
				c.SetAuthorizationHeader(fmt.Sprintf("Bearer %s", *bearerToken))
			}
			r = append(r, c)
		case "file":
			r = append(r, client.FileFetcher{Root: rURL.Path})
		case "gs":
			c, err := gcp.NewGSFetcher(ctx, rURL.Host, nil)
			if err != nil {
				klog.Exitf("NewGSFetcher: %v", err)
			}
			r = append(r, c)
		default:
			klog.Exitf("Unsupported scheme %s on log URL", rURL.Scheme)
		}
	}
	return loadtest.NewRoundRobinReader(r)
}

func mustCreateWriters(us []string) loadtest.LeafWriter {
	w := []loadtest.LeafWriter{}
	for _, u := range us {
		if !strings.HasSuffix(u, "/") {
			u += "/"
		}
		u += "ct/v1/add-chain"
		wURL, err := url.Parse(u)
		if err != nil {
			klog.Exitf("Invalid log writer URL %q: %v", u, err)
		}
		w = append(w, httpWriter(wURL, hc, *bearerTokenWrite))
	}
	return loadtest.NewRoundRobinWriter(w)
}

func httpWriter(u *url.URL, hc *http.Client, bearerToken string) loadtest.LeafWriter {
	cTrace := &httptrace.ClientTrace{
		GotConn: func(info httptrace.GotConnInfo) { klog.Infof("connection established %#v", info) },
	}
	return func(ctx context.Context, newLeaf []byte) (uint64, uint64, error) {
		req, err := http.NewRequest(http.MethodPost, u.String(), bytes.NewReader(newLeaf))
		if err != nil {
			return 0, 0, fmt.Errorf("failed to create request: %v", err)
		}
		if bearerToken != "" {
			req.Header.Add("Authorization", fmt.Sprintf("Bearer %s", bearerToken))
		}
		reqCtx := req.Context()
		if klog.V(2).Enabled() {
			reqCtx = httptrace.WithClientTrace(req.Context(), cTrace)
		}
		resp, err := hc.Do(req.WithContext(reqCtx))
		if err != nil {
			return 0, 0, fmt.Errorf("failed to write leaf: %v", err)
		}
		body, err := io.ReadAll(resp.Body)
		_ = resp.Body.Close()
		if err != nil {
			return 0, 0, fmt.Errorf("failed to read body: %v", err)
		}
		switch resp.StatusCode {
		case http.StatusOK:
			if resp.Request.Method != http.MethodPost {
				return 0, 0, fmt.Errorf("write leaf was redirected to %s", resp.Request.URL)
			}
			// Continue below
		case http.StatusServiceUnavailable, http.StatusBadGateway, http.StatusGatewayTimeout, http.StatusTooManyRequests:
			// These status codes may indicate a delay before retrying, so handle that here:
			time.Sleep(retryDelay(resp.Header.Get("Retry-After"), time.Second))

			return 0, 0, fmt.Errorf("log not available. Status code: %d. Body: %q %w", resp.StatusCode, body, loadtest.ErrRetry)
		default:
			return 0, 0, fmt.Errorf("write leaf was not OK. Status code: %d. Body: %q", resp.StatusCode, body)
		}
		index, timestamp, err := parseAddChainResponse(body)
		if err != nil {
			return 0, 0, fmt.Errorf("write leaf failed to parse response: %v", body)
		}
		return index, timestamp, nil
	}
}

func retryDelay(retryAfter string, defaultDur time.Duration) time.Duration {
	if retryAfter == "" {
		return defaultDur
	}
	d, err := time.Parse(http.TimeFormat, retryAfter)
	if err == nil {
		return time.Until(d)
	}
	s, err := strconv.Atoi(retryAfter)
	if err == nil {
		return time.Duration(s) * time.Second
	}
	return defaultDur
}

// parseAddChainResponse parses the add-chain response and returns the leaf
// index from the extensions and timestamp from the response.
// Code is inspired by https://github.com/FiloSottile/sunlight/blob/main/tile.go.
func parseAddChainResponse(body []byte) (uint64, uint64, error) {
	var resp rfc6962.AddChainResponse
	if err := json.Unmarshal(body, &resp); err != nil {
		return 0, 0, fmt.Errorf("can't parse add-chain response: %v", err)
	}

	leafIdx, err := staticct.ParseCTExtensions(resp.Extensions)
	if err != nil {
		return 0, 0, fmt.Errorf("can't parse extensions: %v", err)
	}
	return uint64(leafIdx), resp.Timestamp, nil
}
