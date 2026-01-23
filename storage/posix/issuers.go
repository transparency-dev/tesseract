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
	"k8s.io/klog/v2"
)

// IssuersStorage is a key value store backed by files to store issuer chains.
type IssuersStorage struct {
	dir string
}

// NewIssuerStorage creates a new POSIX based issuer storage.
//
// If the directory doesn't exists, NewIssuerStorage creates it and its parents.
// The issuers will be stored in a directory called "issuer" within the provided root directory.
func NewIssuerStorage(ctx context.Context, root string) (*IssuersStorage, error) {
	dir := filepath.Join(root, staticct.IssuersPrefix)
	if err := mkdirAll(dir, dirPerm); err != nil {
		return nil, fmt.Errorf("failed to make directory structure: %w", err)
	}

	return &IssuersStorage{dir}, nil
}

// NewRootsStorage creates a new POSIX based root storage.
//
// If the directory doesn't exist, NewRootsStorage creates it and its parents.
// Root certs will be stored in a directory called "roots" within the provided root directory.
func NewRootsStorage(ctx context.Context, parent string) (*IssuersStorage, error) {
	dir := filepath.Join(parent, storage.RootsPrefix)
	if err := mkdirAll(dir, dirPerm); err != nil {
		return nil, fmt.Errorf("failed to make directory structure: %w", err)
	}

	return &IssuersStorage{dir}, nil
}

func (s *IssuersStorage) LoadAll(ctx context.Context) ([]storage.KV, error) {
	files, err := os.ReadDir(s.dir)
	if err != nil {
		return nil, fmt.Errorf("os.ReadDir(%q): %v", s.dir, err)
	}
	kvs := []storage.KV{}
	for _, f := range files {
		if f.IsDir() {
			continue
		}
		p := filepath.Join(s.dir, f.Name())
		b, err := os.ReadFile(p)
		if err != nil {
			return nil, fmt.Errorf("failed to read existing file %q: %v", p, err)
		}
		kvs = append(kvs, storage.KV{
			K: []byte(f.Name()),
			V: b,
		})
	}
	return kvs, nil
}

// AddIfNotExist stores values under their Key if there isn't an object under Key already.
func (s *IssuersStorage) AddIfNotExist(ctx context.Context, kv []storage.KV) error {
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
		klog.Infof("AddIfNotExist: added %q in %q", string(kv.K), s.dir)
	}
	return errors.Join(errs...)
}
