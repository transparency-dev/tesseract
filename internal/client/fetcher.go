// Copyright 2024 Google LLC. All Rights Reserved.
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

package client

import (
	"context"
	"encoding/hex"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path"
	"strconv"
	"strings"

	"github.com/cenkalti/backoff/v5"
	"github.com/transparency-dev/tessera/api/layout"
	"k8s.io/klog/v2"
)

// NewHTTPFetcher creates a new HTTPFetcher for the log rooted at the given URL, using
// the provided HTTP client.
//
// rootURL should end in a trailing slash.
// c may be nil, in which case http.DefaultClient will be used.
func NewHTTPFetcher(rootURL *url.URL, c *http.Client) (*HTTPFetcher, error) {
	if !strings.HasSuffix(rootURL.String(), "/") {
		rootURL.Path += "/"
	}
	if c == nil {
		c = http.DefaultClient
	}
	return &HTTPFetcher{
		c:         c,
		rootURL:   rootURL,
		backOff:   []backoff.RetryOption{backoff.WithMaxTries(1)},
		userAgent: "TesseraCT client",
	}, nil
}

// HTTPFetcher knows how to fetch log artifacts from a log being served via HTTP.
type HTTPFetcher struct {
	c          *http.Client
	rootURL    *url.URL
	authHeader string
	backOff    []backoff.RetryOption
	userAgent  string
}

// SetAuthorizationHeader sets the value to be used with an Authorization: header
// for every request made by this fetcher.
func (h *HTTPFetcher) SetAuthorizationHeader(v string) {
	h.authHeader = v
}

// SetUserAgent sets the user agent to use when sending requests.
func (h *HTTPFetcher) SetUserAgent(ua string) {
	h.userAgent = ua
}

// EnableRetries causes requests which result in a non-permanent error to be retried with up to maxRetries attempts.
func (h *HTTPFetcher) EnableRetries(maxRetries uint) {
	h.backOff = []backoff.RetryOption{backoff.WithBackOff(backoff.NewExponentialBackOff()), backoff.WithMaxTries(10)}
}

func (h HTTPFetcher) fetch(ctx context.Context, p string) ([]byte, error) {
	return backoff.Retry(ctx, func() ([]byte, error) {
		u, err := h.rootURL.Parse(p)
		if err != nil {
			return nil, fmt.Errorf("invalid URL: %v", err)
		}
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
		if err != nil {
			return nil, fmt.Errorf("NewRequestWithContext(%q): %v", u.String(), err)
		}
		if h.authHeader != "" {
			req.Header.Add("Authorization", h.authHeader)
		}
		if h.userAgent != "" {
			req.Header.Add("User-Agent", h.userAgent)
		}
		r, err := h.c.Do(req)
		if err != nil {
			return nil, fmt.Errorf("get(%q): %v", u.String(), err)
		}
		switch r.StatusCode {
		case http.StatusOK:
			// All good, continue below
			break
		case http.StatusTooManyRequests:
			seconds, err := strconv.ParseInt(r.Header.Get("Retry-After"), 10, 32)
			if err != nil {
				// The server didn't say how long to wait, so we'll wait an arbitrary amount of time.
				seconds = 10
			}
			return nil, backoff.RetryAfter(int(seconds))
		case http.StatusNotFound:
			// Need to return ErrNotExist here, by contract, and also let the backoff know not to retry).
			return nil, backoff.Permanent(fmt.Errorf("get(%q): %w", u.String(), os.ErrNotExist))
		case http.StatusBadRequest, http.StatusUnauthorized, http.StatusForbidden, http.StatusMethodNotAllowed, http.StatusConflict, http.StatusUnprocessableEntity:
			// Should not retry for any of these status codes.
			return nil, backoff.Permanent(fmt.Errorf("get(%q): %v", u.String(), r.StatusCode))
		default:
			// Everything else will be retried
			return nil, fmt.Errorf("get(%q): %v", u.String(), r.StatusCode)
		}

		defer func() {
			if err := r.Body.Close(); err != nil {
				klog.Errorf("resp.Body.Close(): %v", err)
			}
		}()
		return io.ReadAll(r.Body)
	}, h.backOff...)
}

func (h HTTPFetcher) ReadCheckpoint(ctx context.Context) ([]byte, error) {
	return h.fetch(ctx, layout.CheckpointPath)
}

func (h HTTPFetcher) ReadTile(ctx context.Context, l, i uint64, p uint8) ([]byte, error) {
	return PartialOrFullResource(ctx, p, func(ctx context.Context, p uint8) ([]byte, error) {
		return h.fetch(ctx, layout.TilePath(l, i, p))
	})
}

func (h HTTPFetcher) ReadEntryBundle(ctx context.Context, i uint64, p uint8) ([]byte, error) {
	return PartialOrFullResource(ctx, p, func(ctx context.Context, p uint8) ([]byte, error) {
		return h.fetch(ctx, ctEntriesPath(i, p))
	})
}

func (h HTTPFetcher) ReadIssuer(ctx context.Context, hash []byte) ([]byte, error) {
	return h.fetch(ctx, ctIssuerPath(hash))
}

// FileFetcher knows how to fetch log artifacts from a filesystem rooted at Root.
type FileFetcher struct {
	Root string
}

func (f FileFetcher) ReadCheckpoint(_ context.Context) ([]byte, error) {
	return os.ReadFile(path.Join(f.Root, layout.CheckpointPath))
}

func (f FileFetcher) ReadTile(ctx context.Context, l, i uint64, p uint8) ([]byte, error) {
	return PartialOrFullResource(ctx, p, func(ctx context.Context, p uint8) ([]byte, error) {
		return os.ReadFile(path.Join(f.Root, layout.TilePath(l, i, p)))
	})
}

func (f FileFetcher) ReadEntryBundle(ctx context.Context, i uint64, p uint8) ([]byte, error) {
	return PartialOrFullResource(ctx, p, func(ctx context.Context, p uint8) ([]byte, error) {
		return os.ReadFile(path.Join(f.Root, ctEntriesPath(i, p)))
	})
}

func (f FileFetcher) ReadIssuer(ctx context.Context, hash []byte) ([]byte, error) {
	return os.ReadFile(path.Join(f.Root, ctIssuerPath(hash)))
}

func ctEntriesPath(n uint64, p uint8) string {
	return fmt.Sprintf("tile/data/%s", layout.NWithSuffix(0, n, p))
}

func ctIssuerPath(hash []byte) string {
	return fmt.Sprintf("issuer/%s", hex.EncodeToString(hash))
}
