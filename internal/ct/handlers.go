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

package ct

import (
	"context"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"math/rand/v2"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/transparency-dev/tessera"
	"github.com/transparency-dev/tesseract/internal/otel"
	"github.com/transparency-dev/tesseract/internal/types/rfc6962"
	"github.com/transparency-dev/tesseract/internal/types/tls"
	"github.com/transparency-dev/tesseract/internal/x509util"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/metric"
	"golang.org/x/time/rate"
	"k8s.io/klog/v2"
)

const (
	// HTTP content type header
	contentTypeHeader string = "Content-Type"
	// MIME content type for JSON
	contentTypeJSON string = "application/json"
	// The name of the JSON response map key in get-roots responses
	jsonMapKeyCertificates string = "certificates"
)

// entrypointName identifies a CT entrypoint as defined in section 4 of RFC 6962.
type entrypointName = string

// Constants for entrypoint names, as exposed in statistics/logging.
const (
	addChainName    = entrypointName("AddChain")
	addPreChainName = entrypointName("AddPreChain")
	getRootsName    = entrypointName("GetRoots")
)

var (
	// Metrics are all per-log (label "origin"), but may also be
	// per-entrypoint (label "ep") or per-return-code (label "rc").
	once                   sync.Once
	knownLogs              metric.Int64Gauge       // origin => value (always 1.0)
	lastSCTIndex           metric.Int64Gauge       // origin => value
	lastSCTTimestamp       metric.Int64Gauge       // origin => value
	reqCounter             metric.Int64Counter     // origin, op => value
	rspCounter             metric.Int64Counter     // origin, op, code => value
	reqDuration            metric.Float64Histogram // origin, op, code => value
	rateLimitedRequests    metric.Int64Counter     // origin, reason
	notBeforeAgeUnverified metric.Float64Histogram // origin ==> value
)

// setupMetrics initializes all the exported metrics.
func setupMetrics() {
	// TODO(phboneff): add metrics for chain storage.
	knownLogs = mustCreate(meter.Int64Gauge("tesseract.known_logs",
		metric.WithDescription("Set to 1 for known logs")))

	lastSCTTimestamp = mustCreate(meter.Int64Gauge("tesseract.last_sct.timestamp",
		metric.WithDescription("Time of last SCT since epoch"),
		metric.WithUnit("ms")))

	lastSCTIndex = mustCreate(meter.Int64Gauge("tesseract.last_sct.index",
		metric.WithDescription("Index of last SCT"),
		metric.WithUnit("{entry}")))

	reqCounter = mustCreate(meter.Int64Counter("tesseract.http.request.count",
		metric.WithDescription("CT HTTP requests"),
		metric.WithUnit("{request}")))

	rspCounter = mustCreate(meter.Int64Counter("tesseract.http.response.count",
		metric.WithDescription("CT HTTP responses"),
		metric.WithUnit("{response}")))

	// TODO(phboneff): switch back to s, in Tessera as well.
	reqDuration = mustCreate(meter.Float64Histogram("tesseract.http.request.duration",
		metric.WithDescription("CT HTTP response duration"),
		metric.WithUnit("ms"),
		metric.WithExplicitBucketBoundaries(otel.SubSecondLatencyHistogramBuckets...)))

	notBeforeAgeUnverified = mustCreate(meter.Float64Histogram("tesseract.notbefore.age.unverified",
		metric.WithDescription("Submission notBefore age"),
		metric.WithUnit("s"),
		metric.WithExplicitBucketBoundaries(otel.SubmissionAgeHistogramBuckets...)))

	rateLimitedRequests = mustCreate(meter.Int64Counter("tesseract.http.request.ratelimited.count",
		metric.WithDescription("CT HTTP rate-limited requests"),
		metric.WithUnit("{request}")))
}

// entrypoints is a list of entrypoint names as exposed in statistics/logging.
var entrypoints = []entrypointName{addChainName, addPreChainName, getRootsName}

// pathHandlers maps from a path to the relevant AppHandler instance.
type pathHandlers map[string]appHandler

// appHandler connects an HTTP static-ct-api endpoint with log storage.
// It is an implementation of the http.Handler interface.
type appHandler struct {
	log     *log
	opts    *HandlerOptions
	handler func(context.Context, *HandlerOptions, *log, http.ResponseWriter, *http.Request) (int, []attribute.KeyValue, error)
	name    entrypointName
	method  string // http.MethodGet or http.MethodPost
}

// ServeHTTP for an AppHandler invokes the underlying handler function but
// does additional common error and stats processing.
func (a appHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	logCtx := a.opts.RequestLog.start(r.Context())
	logCtx, span := tracer.Start(logCtx, fmt.Sprintf("tesseract.ServeHTTP.%s", a.name))
	defer span.End()

	originAttr := originKey.String(a.log.origin)
	operationAttr := operationKey.String(a.name)
	attrs := []attribute.KeyValue{originAttr, operationAttr}

	reqCounter.Add(logCtx, 1, metric.WithAttributes(attrs...))
	startTime := time.Now()
	a.opts.RequestLog.origin(logCtx, a.log.origin)
	defer func() {
		latency := time.Since(startTime).Seconds()
		reqDuration.Record(r.Context(), latency, metric.WithAttributes(attrs...))
	}()

	klog.V(2).Infof("%s: request %v %q => %s", a.log.origin, r.Method, r.URL, a.name)
	// TODO(phboneff): add a.Method directly on the handler path and remove this test.
	if r.Method != a.method {
		klog.Warningf("%s: %s wrong HTTP method: %v", a.log.origin, a.name, r.Method)
		a.opts.sendHTTPError(w, http.StatusMethodNotAllowed, fmt.Errorf("method not allowed: %s", r.Method))
		a.opts.RequestLog.status(logCtx, http.StatusMethodNotAllowed)
		return
	}

	// For GET requests all params come as form encoded so we might as well parse them now.
	// POSTs will decode the raw request body as JSON later.
	if r.Method == http.MethodGet {
		if err := r.ParseForm(); err != nil {
			a.opts.sendHTTPError(w, http.StatusBadRequest, fmt.Errorf("failed to parse form data: %s", err))
			a.opts.RequestLog.status(logCtx, http.StatusBadRequest)
			return
		}
	}

	// impose a deadline on this onward request.
	// TODO(phbnf): fine tune together with deduplication
	ctx, cancel := context.WithTimeout(logCtx, a.opts.Deadline)
	defer cancel()

	statusCode, hattrs, err := a.handler(ctx, a.opts, a.log, w, r)
	attrs = append(attrs, hattrs...)
	attrs = append(attrs, codeKey.Int(statusCode))
	a.opts.RequestLog.status(ctx, statusCode)
	klog.V(2).Infof("%s: %s <= st=%d", a.log.origin, a.name, statusCode)
	rspCounter.Add(logCtx, 1, metric.WithAttributes(attrs...))
	if err != nil {
		klog.Warningf("%s: %s handler error: %v", a.log.origin, a.name, err)
		a.opts.sendHTTPError(w, statusCode, err)
		span.SetStatus(codes.Error, err.Error())
		return
	}

	// Additional check, for consistency the handler must return an error for non-200 st
	if statusCode != http.StatusOK {
		klog.Warningf("%s: %s handler non 200 without error: %d %v", a.log.origin, a.name, statusCode, err)
		a.opts.sendHTTPError(w, http.StatusInternalServerError, fmt.Errorf("http handler misbehaved, st: %d", statusCode))
		if statusCode >= 500 {
			span.SetStatus(codes.Error, "handler non-200 without error")
		}
		return
	}
}

// RateLimits knows how to apply configurable rate limits to submissions.
type RateLimits struct {
	notBeforeLimit time.Duration
	notBefore      *rate.Limiter
	dedup          *rate.Limiter
	issuerLimit    float64
	issuer         *sync.Map
}

// OldSubmission configures a rate limit on old certs.
//
// Submissions whose notBefore date is at least as old as age will be subject to the specified number of entries per second.
func (r *RateLimits) NotBefore(age time.Duration, limit float64) {
	r.notBeforeLimit = age
	r.notBefore = rate.NewLimiter(rate.Limit(limit), int(math.Ceil(limit)))
	klog.Infof("Configured OldSubmission limiter with %0.2f qps for certs aged >= %s", limit, age)
}

// Issuer configures a rate limit on the first issuer of each chain.
//
// Submissions will be subject to the specified number of entries per second.
func (r *RateLimits) Issuer(limit float64) {
	r.issuerLimit = limit
	r.issuer = &sync.Map{}
	klog.Infof("Configured Issuer limiter with %0.2f qps per issuer", limit)
}

// Dedup configures a rate limit on entries being deduplicated.
//
// Submissions will be subject to the specified number of entries per second.
func (r *RateLimits) Dedup(limit float64) {
	r.dedup = rate.NewLimiter(rate.Limit(limit), int(math.Ceil(limit)))
	klog.Infof("Configured DedupInFlight limiter with %0.2f qps", limit)
}

// AcceptNotBefore returns true if the provided chain should be accepted, and false otherwise.
func (r *RateLimits) AcceptNotBefore(ctx context.Context, chain []*x509.Certificate) bool {
	if len(chain) == 0 {
		return false
	}
	if r.notBefore != nil {
		if age := time.Since(chain[0].NotBefore); age >= r.notBeforeLimit {
			if r.notBefore.Allow() {
				return true
			}
			rateLimitedRequests.Add(ctx, 1, metric.WithAttributes(rateLimitReasonKey.String("old_cert")))
			return false
		}
	}
	return true
}

// AcceptIssuer returns true if a chain should be accepted based on its first issuer, and false othwerwise.
func (r *RateLimits) AcceptIssuer(ctx context.Context, chain []*x509.Certificate) bool {
	if len(chain) == 0 {
		return false
	}
	if r.issuer != nil && r.issuerLimit >= 0 {
		issuer := sha256.Sum256(chain[0].RawIssuer)
		v, _ := r.issuer.LoadOrStore(issuer, rate.NewLimiter(rate.Limit(r.issuerLimit), int(math.Ceil(r.issuerLimit))))
		rl := v.(*rate.Limiter)
		if rl.Allow() {
			return true
		}
		rateLimitedRequests.Add(ctx, 1, metric.WithAttributes(rateLimitReasonKey.String("issuer")))
		return false
	}
	return true
}

// AcceptDedup returns true if a duplicate entry is permitted to be resolved.
func (r *RateLimits) AcceptDedup(ctx context.Context) bool {
	if r.dedup != nil {
		if r.dedup.Allow() {
			return true
		}
		rateLimitedRequests.Add(ctx, 1, metric.WithAttributes(rateLimitReasonKey.String("dedup")))
		return false
	}
	return true
}

// HandlerOptions describes log handlers options.
type HandlerOptions struct {
	// Deadline is a timeout for HTTP requests.
	Deadline time.Duration
	// RequestLog provides structured logging of TesseraCT requests.
	RequestLog requestLog
	// MaskInternalErrors indicates if internal server errors should be masked
	// or returned to the user containing the full error message.
	MaskInternalErrors bool
	// TimeSource indicated the system time and can be injfected for testing.
	// TODO(phbnf): hide inside the log
	TimeSource TimeSource
	// PathPrefix prefixes static-ct-api endpoint paths.
	PathPrefix string
	// RateLimits describes optional rate limits to enforce.
	RateLimits RateLimits
}

func NewPathHandlers(ctx context.Context, opts *HandlerOptions, log *log) pathHandlers {
	once.Do(func() { setupMetrics() })
	knownLogs.Record(ctx, 1, metric.WithAttributes(originKey.String(log.origin)))

	prefix := strings.TrimRight(opts.PathPrefix, "/")
	if prefix != "" && !strings.HasPrefix(prefix, "/") {
		prefix = "/" + prefix
	}

	// Bind each endpoint to an appHandler instance.
	// TODO(phboneff): try and get rid of PathHandlers and appHandler
	ph := pathHandlers{
		prefix + rfc6962.AddChainPath:    appHandler{opts: opts, log: log, handler: addChain, name: addChainName, method: http.MethodPost},
		prefix + rfc6962.AddPreChainPath: appHandler{opts: opts, log: log, handler: addPreChain, name: addPreChainName, method: http.MethodPost},
		prefix + rfc6962.GetRootsPath:    appHandler{opts: opts, log: log, handler: getRoots, name: getRootsName, method: http.MethodGet},
	}

	return ph
}

// sendHTTPError generates a custom error page to give more information on why something didn't work
func (opts *HandlerOptions) sendHTTPError(w http.ResponseWriter, statusCode int, err error) {
	errorBody := http.StatusText(statusCode)
	if !opts.MaskInternalErrors || statusCode != http.StatusInternalServerError {
		errorBody += fmt.Sprintf("\n%v", err)
	}
	http.Error(w, errorBody, statusCode)
}

// parseBodyAsJSONChain tries to extract cert-chain out of request.
func parseBodyAsJSONChain(r *http.Request) (rfc6962.AddChainRequest, error) {
	body, err := io.ReadAll(r.Body)
	if err != nil {
		klog.V(1).Infof("Failed to read request body: %v", err)
		return rfc6962.AddChainRequest{}, err
	}

	var req rfc6962.AddChainRequest
	if err := json.Unmarshal(body, &req); err != nil {
		klog.V(1).Infof("Failed to parse request body: %v", err)
		return rfc6962.AddChainRequest{}, err
	}

	// The cert chain is not allowed to be empty. We'll defer other validation for later
	if len(req.Chain) == 0 {
		klog.V(1).Infof("Request chain is empty: %q", body)
		return rfc6962.AddChainRequest{}, errors.New("cert chain was empty")
	}

	return req, nil
}

// addChainInternal is called by add-chain and add-pre-chain as the logic involved in
// processing these requests is almost identical
func addChainInternal(ctx context.Context, opts *HandlerOptions, log *log, w http.ResponseWriter, r *http.Request, isPrecert bool) (int, []attribute.KeyValue, error) {
	ctx, span := tracer.Start(ctx, "tesseract.addChainInternal")
	defer span.End()

	var method entrypointName
	if isPrecert {
		method = addPreChainName
	} else {
		method = addChainName
	}

	// Check the contents of the request and convert to slice of certificates.
	addChainReq, err := parseBodyAsJSONChain(r)
	if err != nil {
		return http.StatusBadRequest, nil, fmt.Errorf("%s: failed to parse add-chain body: %s", log.origin, err)
	}
	// Log the DERs now because they might not parse as valid X.509.
	for _, der := range addChainReq.Chain {
		opts.RequestLog.addDERToChain(ctx, der)
	}
	chain, err := parseChain(addChainReq.Chain)
	if err != nil {
		return http.StatusBadRequest, nil, fmt.Errorf("failed to parse add-chain contents: %s", err)
	}

	notBeforeAgeUnverified.Record(ctx, time.Since(chain[0].NotBefore).Seconds())
	if ok := opts.RateLimits.AcceptNotBefore(ctx, chain); !ok {
		opts.RequestLog.addCertToChain(ctx, chain[0])
		w.Header().Add("Retry-After", strconv.Itoa(rand.IntN(5)+1)) // random retry within [1,6) seconds
		return http.StatusTooManyRequests,
			[]attribute.KeyValue{tooManyRequestsReasonKey.String("rate_limit_old_cert")},
			errors.New(http.StatusText(http.StatusTooManyRequests))
	}

	chain, err = log.chainValidator.Validate(chain, isPrecert)
	if err != nil {
		return http.StatusBadRequest, nil, fmt.Errorf("failed to verify add-chain contents: %s", err)
	}
	for _, cert := range chain {
		opts.RequestLog.addCertToChain(ctx, cert)
	}

	if ok := opts.RateLimits.AcceptIssuer(ctx, chain); !ok {
		w.Header().Add("Retry-After", strconv.Itoa(rand.IntN(5)+1)) // random retry within [1,6) seconds
		return http.StatusTooManyRequests, []attribute.KeyValue{tooManyRequestsReasonKey.String("rate_limit_issuer")}, errors.New(http.StatusText(http.StatusTooManyRequests))
	}

	// Get the current time in the form used throughout RFC6962, namely milliseconds since Unix
	// epoch, and use this throughout.
	nanosPerMilli := int64(time.Millisecond / time.Nanosecond)
	timeMillis := uint64(opts.TimeSource.Now().UnixNano() / nanosPerMilli)

	entry, err := x509util.EntryFromChain(chain, isPrecert, timeMillis)
	if err != nil {
		return http.StatusBadRequest, nil, fmt.Errorf("failed to build MerkleTreeLeaf: %s", err)
	}

	if err := log.storage.AddIssuerChain(ctx, chain[1:]); err != nil {
		return http.StatusInternalServerError, nil, fmt.Errorf("failed to store issuer chain: %s", err)
	}

	klog.V(2).Infof("%s: %s => storage.Add", log.origin, method)
	future, err := log.storage.Add(ctx, entry)
	// helper function to return a 429
	tooManyRequests := func(reason string) (int, []attribute.KeyValue, error) {
		w.Header().Add("Retry-After", strconv.Itoa(rand.IntN(5)+1)) // random retry within [1,6) seconds
		return http.StatusTooManyRequests, []attribute.KeyValue{tooManyRequestsReasonKey.String(reason)}, errors.New(http.StatusText(http.StatusTooManyRequests))
	}
	if err != nil {
		switch {
		// Record the fact there was pushback, if any.
		case errors.Is(err, tessera.ErrPushbackAntispam):
			return tooManyRequests("tessera_pushback_antispam")
		case errors.Is(err, tessera.ErrPushbackIntegration):
			return tooManyRequests("tessera_pushback_integration")
		case errors.Is(err, tessera.ErrPushback):
			return tooManyRequests("tessera_pushback_other")
		}
		// If it's not a pushback, just flag that it's an errored request to avoid high cardinality of attribute values.
		return http.StatusInternalServerError, nil, fmt.Errorf("couldn't store the leaf: %v", err)
	}

	index, err := future()
	if err != nil {
		return http.StatusInternalServerError, nil, fmt.Errorf("couldn't resolve tessera future: %v", err)
	}

	if index.IsDup {
		if ok := opts.RateLimits.AcceptDedup(ctx); !ok {
			w.Header().Add("Retry-After", strconv.Itoa(rand.IntN(5)+1)) // random retry within [1,6) seconds
			return http.StatusTooManyRequests, []attribute.KeyValue{duplicateKey.Bool(index.IsDup), tooManyRequestsReasonKey.String("rate_limit_dedup")}, errors.New(http.StatusText(http.StatusTooManyRequests))
		}
		entry.Timestamp, err = log.storage.DedupFuture(ctx, future)
		if err != nil {
			return http.StatusInternalServerError, []attribute.KeyValue{duplicateKey.Bool(index.IsDup)}, fmt.Errorf("could not resolve duplicate: %v", err)
		}
	}

	// Always use the returned leaf as the basis for an SCT.
	var loggedLeaf rfc6962.MerkleTreeLeaf
	leafValue := entry.MerkleTreeLeaf(index.Index)
	if rest, err := tls.Unmarshal(leafValue, &loggedLeaf); err != nil {
		return http.StatusInternalServerError, nil, fmt.Errorf("failed to reconstruct MerkleTreeLeaf: %s", err)
	} else if len(rest) > 0 {
		return http.StatusInternalServerError, nil, fmt.Errorf("extra data (%d bytes) on reconstructing MerkleTreeLeaf", len(rest))
	}

	// As the Log server has definitely got the Merkle tree leaf, we can
	// generate an SCT and respond with it.
	sct, err := log.signSCT(&loggedLeaf)
	if err != nil {
		return http.StatusInternalServerError, nil, fmt.Errorf("failed to generate SCT: %s", err)
	}
	sctBytes, err := tls.Marshal(*sct)
	if err != nil {
		return http.StatusInternalServerError, nil, fmt.Errorf("failed to marshall SCT: %s", err)
	}
	// We could possibly fail to issue the SCT after this but it's v. unlikely.
	opts.RequestLog.issueSCT(ctx, sctBytes)
	err = marshalAndWriteAddChainResponse(sct, w)
	if err != nil {
		// reason is logged and http status is already set
		return http.StatusInternalServerError, nil, fmt.Errorf("failed to write response: %s", err)
	}
	klog.V(3).Infof("%s: %s <= SCT", log.origin, method)
	if !index.IsDup {
		lastSCTTimestamp.Record(ctx, otel.Clamp64(sct.Timestamp), metric.WithAttributes(originKey.String(log.origin)))
		lastSCTIndex.Record(ctx, otel.Clamp64(index.Index), metric.WithAttributes(originKey.String(log.origin)))
	}

	return http.StatusOK, []attribute.KeyValue{duplicateKey.Bool(index.IsDup)}, nil
}

func addChain(ctx context.Context, opts *HandlerOptions, log *log, w http.ResponseWriter, r *http.Request) (int, []attribute.KeyValue, error) {
	ctx, span := tracer.Start(ctx, "tesseract.addChain")
	defer span.End()

	return addChainInternal(ctx, opts, log, w, r, false)
}

func addPreChain(ctx context.Context, opts *HandlerOptions, log *log, w http.ResponseWriter, r *http.Request) (int, []attribute.KeyValue, error) {
	ctx, span := tracer.Start(ctx, "tesseract.addPreChain")
	defer span.End()

	return addChainInternal(ctx, opts, log, w, r, true)
}

func getRoots(ctx context.Context, opts *HandlerOptions, log *log, w http.ResponseWriter, _ *http.Request) (int, []attribute.KeyValue, error) {
	_, span := tracer.Start(ctx, "tesseract.getRoots")
	defer span.End()

	// Pull out the raw certificates from the parsed versions
	// TODO(phbnf): precompute the answer
	rawCerts := make([][]byte, 0, len(log.chainValidator.Roots()))
	for _, cert := range log.chainValidator.Roots() {
		rawCerts = append(rawCerts, cert.Raw)
	}

	jsonMap := make(map[string]any)
	jsonMap[jsonMapKeyCertificates] = rawCerts
	enc := json.NewEncoder(w)
	err := enc.Encode(jsonMap)
	if err != nil {
		klog.Warningf("%s: get_roots failed: %v", log.origin, err)
		return http.StatusInternalServerError, nil, fmt.Errorf("get-roots failed with: %s", err)
	}

	return http.StatusOK, nil, nil
}

// marshalAndWriteAddChainResponse is used by add-chain and add-pre-chain to create and write
// the JSON response to the client
func marshalAndWriteAddChainResponse(sct *rfc6962.SignedCertificateTimestamp, w http.ResponseWriter) error {
	sig, err := tls.Marshal(sct.Signature)
	if err != nil {
		return fmt.Errorf("failed to marshal signature: %s", err)
	}

	rsp := rfc6962.AddChainResponse{
		SCTVersion: sct.SCTVersion,
		Timestamp:  sct.Timestamp,
		ID:         sct.LogID.KeyID[:],
		Extensions: base64.StdEncoding.EncodeToString(sct.Extensions),
		Signature:  sig,
	}

	w.Header().Set(contentTypeHeader, contentTypeJSON)
	jsonData, err := json.Marshal(&rsp)
	if err != nil {
		return fmt.Errorf("failed to marshal add-chain: %s", err)
	}

	_, err = w.Write(jsonData)
	if err != nil {
		return fmt.Errorf("failed to write add-chain resp: %s", err)
	}

	return nil
}
