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

package storage

import (
	"bytes"
	"context"
	"crypto/sha256"
	"crypto/x509"
	"encoding/hex"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"strconv"
	"sync"
	"time"

	"github.com/transparency-dev/tessera"
	"github.com/transparency-dev/tessera/api/layout"
	"github.com/transparency-dev/tessera/ctonly"
	"github.com/transparency-dev/tesseract/internal/logger"
	"github.com/transparency-dev/tesseract/internal/types/rfc6962"
	"github.com/transparency-dev/tesseract/internal/types/staticct"

	"golang.org/x/mod/sumdb/note"
)

// CreateStorage instantiates a Tessera storage implementation with a signer option.
type CreateStorage func(context.Context, note.Signer) (*CTStorage, error)

const (
	// Each key is 64 bytes long, so this will take up to 64MB.
	// A CT log references ~15k unique issuer certifiates in 2024, so this gives plenty of space
	// if we ever run into this limit, we should re-think how it works.
	maxCachedIssuerKeys        = 1 << 20
	RootsPrefix                = "roots/"
	DefaultAwaiterPollInterval = 200 * time.Millisecond
)

type KV struct {
	K []byte
	V []byte
}


// IssuerStorage issuer certificates under their hex encoded sha256.
type IssuerStorage interface {
	AddIfNotExist(ctx context.Context, kv []KV) error
}

// RootsStorage stores root certificates under their hex encoded sha256.
type RootsStorage interface {
	AddIfNotExist(ctx context.Context, kv []KV) error
	LoadAll(ctx context.Context) ([]KV, error)
}

type CTStorageOptions struct {
	Appender            *tessera.Appender
	Reader              tessera.LogReader
	IssuerStorage       IssuerStorage
	AwaiterPollInterval time.Duration
	EnablePubAwaiter    bool
}

// CTStorage implements ct.Storage and tessera.LogReader.
type CTStorage struct {
	storeData        func(context.Context, *ctonly.Entry) tessera.IndexFuture
	storeIssuers     func(context.Context, []KV) error
	reader           tessera.LogReader
	awaiter          *tessera.PublicationAwaiter
	enablePubAwaiter bool
}

// NewCTStorage instantiates a CTStorage object.
func NewCTStorage(ctx context.Context, opts *CTStorageOptions) (*CTStorage, error) {
	pollInterval := opts.AwaiterPollInterval
	if pollInterval <= 0 {
		pollInterval = DefaultAwaiterPollInterval
	}
	awaiter := tessera.NewPublicationAwaiter(ctx, opts.Reader.ReadCheckpoint, pollInterval)
	ctStorage := &CTStorage{
		storeData:        tessera.NewCertificateTransparencyAppender(opts.Appender),
		storeIssuers:     cachedStoreIssuers(opts.IssuerStorage),
		reader:           opts.Reader,
		awaiter:          awaiter,
		enablePubAwaiter: opts.EnablePubAwaiter,
	}

	return ctStorage, nil
}

// DedupFuture returns the SCT input matching a future.
//
// It waits for the entry matching the future to be integrated, fetches it and
// extracts the SCT input fields from it.
//
// TODO(phbnf): cache entries (or more) to avoid reparsing the entire leaf bundle
func (cts *CTStorage) DedupFuture(ctx context.Context, f tessera.IndexFuture) (*rfc6962.CertificateTimestamp, error) {
	return trace1(ctx, "tesseract.storage.DedupFuture", func(ctx context.Context) (*rfc6962.CertificateTimestamp, error) {
		idx, cpRaw, err := cts.awaiter.Await(ctx, f)
		if err != nil {
			return nil, fmt.Errorf("error waiting for Tessera index future and its integration: %w", err)
		}

		// A https://c2sp.org/static-ct-api logsize is on the second line
		l := bytes.SplitN(cpRaw, []byte("\n"), 3)
		if len(l) < 2 {
			return nil, errors.New("invalid checkpoint - no size")
		}
		ckptSize, err := strconv.ParseUint(string(l[1]), 10, 64)
		if err != nil {
			return nil, fmt.Errorf("invalid checkpoint - can't extract size: %v", err)
		}

		eBIdx := idx.Index / layout.EntryBundleWidth
		eBRaw, err := cts.reader.ReadEntryBundle(ctx, eBIdx, layout.PartialTileSize(0, eBIdx, ckptSize))
		if err != nil {
			if errors.Is(err, os.ErrNotExist) {
				return nil, fmt.Errorf("leaf bundle at index %d not found: %v", eBIdx, err)
			}
			return nil, fmt.Errorf("failed to fetch entry bundle at index %d: %v", eBIdx, err)
		}
		eIdx := idx.Index % layout.EntryBundleWidth
		sct, err := staticct.ExtractSCTInputFromBundle(eBRaw, eIdx)
		if err != nil {
			return nil, fmt.Errorf("failed to extract SCT input for entry %d in bundle index %d: %v", eIdx, eBIdx, err)
		}
		extractedIdx, err := staticct.ParseCTExtensionsBytes(sct.Extensions)
		if err != nil {
			return nil, fmt.Errorf("failed to extract index from extensions: %v", err)
		}
		if extractedIdx != idx.Index {
			return nil, fmt.Errorf("extracted index %d does not match expected index %d", extractedIdx, idx.Index)
		}
		return sct, nil
	})
}

// Add stores CT entries.
func (cts *CTStorage) Add(ctx context.Context, entry *ctonly.Entry) (tessera.IndexFuture, error) {
	return trace1(ctx, "tesseract.storage.Add", func(ctx context.Context) (tessera.IndexFuture, error) {
		future := cts.storeData(ctx, entry)

		if cts.enablePubAwaiter {
			_, _, err := cts.awaiter.Await(ctx, future)
			if err != nil {
				return future, fmt.Errorf("error waiting for Tessera index future and its integration: %w", err)
			}
		}

		return future, nil
	})
}

// AddIssuerChain stores every chain certificate under its sha256.
//
// If an object is already stored under this hash, continues.
func (cts *CTStorage) AddIssuerChain(ctx context.Context, chain []*x509.Certificate) error {
	return traceErr(ctx, "tesseract.storage.AddIssuerChain", func(ctx context.Context) error {

		kvs := []KV{}
		for _, c := range chain {
			id := sha256.Sum256(c.Raw)
			key := []byte(hex.EncodeToString(id[:]))
			kvs = append(kvs, KV{K: key, V: c.Raw})
		}
		if err := cts.storeIssuers(ctx, kvs); err != nil {
			return fmt.Errorf("error storing intermediates: %v", err)
		}
		return nil
	})
}

// cachedStoreIssuers returns a caching wrapper for an IssuerStorage
//
// This is intended to make querying faster. It does not keep a copy of the certs, only sha256.
// Only up to maxCachedIssuerKeys keys will be stored locally.
func cachedStoreIssuers(s IssuerStorage) func(context.Context, []KV) error {
	var mu sync.RWMutex
	m := make(map[string]struct{})
	return func(ctx context.Context, kv []KV) error {
		req := []KV{}
		for _, kv := range kv {
			mu.RLock()
			_, ok := m[string(kv.K)]
			mu.RUnlock()
			if ok {
				logger.DebugExtraContext(ctx, "cachedStoreIssuers wrapper: found in local key cache", slog.String("key", string(kv.K)))
				continue
			}
			req = append(req, kv)
		}
		if err := s.AddIfNotExist(ctx, req); err != nil {
			return fmt.Errorf("issuerStorage.AddIfNotExist(): error storing issuer data in the underlying IssuerStorage: %v", err)
		}
		for _, kv := range req {
			if len(m) >= maxCachedIssuerKeys {
				logger.DebugExtraContext(ctx, "cachedStoreIssuers wrapper: local issuer cache full, will stop caching issuers.")
				return nil
			}
			mu.Lock()
			m[string(kv.K)] = struct{}{}
			mu.Unlock()
		}
		return nil
	}
}

