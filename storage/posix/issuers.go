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
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/transparency-dev/tesseract/internal/types/staticct"
	"github.com/transparency-dev/tesseract/storage"
)

// IssuersStorage is a key value store backed by files to store issuer chains.
type IssuersStorage struct {
	dir string
}

// NewIssuerStorage creates a new POSIX based issuer storage.
//
// The issuers will be stored in a directory called "issuer" within the provided root directory.
func NewIssuerStorage(ctx context.Context, dir string) (*IssuersStorage, error) {
	r := &IssuersStorage{
		dir: filepath.Join(dir, staticct.IssuersPrefix),
	}

	return r, nil
}

// AddIssuers stores Issuers values under their Key if there isn't an object under Key already.
func (s *IssuersStorage) AddIssuersIfNotExist(ctx context.Context, kv []storage.KV) error {
	errs := make([]error, 0)
	for _, kv := range kv {
		k := string(kv.K)
		if strings.ContainsRune(k, filepath.Separator) {
			errs = append(errs, fmt.Errorf("%q is an invalid key", k))
			continue
		}
		p := filepath.Join(s.dir, k)
		if err := createEx(p, kv.V); err != nil {
			if errors.Is(err, os.ErrExist) {
				existing, err := os.ReadFile(p)
				if err != nil {
					errs = append(errs, fmt.Errorf("failed to read existing file %q: %v", p, err))
					continue
				}
				if !bytes.Equal(kv.V, existing) {
					errs = append(errs, fmt.Errorf("non-idempotent write for preexisting file %q", p))
					continue
				}
				// It already existed, but it also already contains the same data, so we're good.
				continue
			}
			errs = append(errs, err)
			continue
		}
	}
	return errors.Join(errs...)
}
