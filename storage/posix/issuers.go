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

package posix

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"sync"

	"github.com/transparency-dev/tesseract/storage"
)

// IssuersStorage is a key value store backed by files to store issuer chains.
type IssuersStorage struct {
	dir          string
	prefix       string
	contentType  string
	knownIssuers sync.Map
}

// NewIssuerStorage creates a new GCSStorage.
//
// The specified bucket must exist or an error will be returned.
func NewIssuerStorage(ctx context.Context, dir string, prefix string, contentType string) (*IssuersStorage, error) {
	r := &IssuersStorage{
		dir:         dir,
		prefix:      prefix,
		contentType: contentType,
	}

	return r, nil
}

// AddIssuers stores Issuers values under their Key if there isn't an object under Key already.
func (s *IssuersStorage) AddIssuersIfNotExist(ctx context.Context, kv []storage.KV) error {
	errs := make([]error, 0)
	for _, kv := range kv {
		k := string(kv.K)
		_, exists := s.knownIssuers.Load(k)
		if exists {
			continue
		}
		p := filepath.Join(s.dir, s.prefix, k)
		if err := createEx(p, kv.V); err != nil {
			if !errors.Is(err, os.ErrExist) {
				errs = append(errs, err)
				continue
			}
		}
		s.knownIssuers.Store(k, struct{}{})
	}
	return errors.Join(errs...)
}
