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

// package posix implements a test issuer storage system on a local filesystem.
// It is not fit for production use.
package posix

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"path"
	"path/filepath"
	"strings"

	"github.com/transparency-dev/tesseract/internal/types/staticct"
	"github.com/transparency-dev/tesseract/storage"
	"k8s.io/klog/v2"
)

// IssuersStorage is a key value store backed by the local filesystem to store issuer chains.
type IssuersStorage string

// NewIssuerStorage creates a new IssuerStorage.
//
// It creates the underying directory if it does not exist already.
func NewIssuerStorage(path string) (IssuersStorage, error) {
	// Does nothing if the dictory already exists.
	p := filepath.Join(path, staticct.IssuersPrefix)
	if err := os.MkdirAll(p, 0755); err != nil {
		return "", fmt.Errorf("failed to create path %q: %v", path, err)
	}
	return IssuersStorage(p), nil
}

// keyToObjName converts bytes to filesystem path.
//
// empty keys, and keys including a '/' character are not allowed to avoid
// confusion with directory names. This list of exclusions is not exhaustive,
// and does not guarantee that it will fit all filesystems.
func (s IssuersStorage) keyToObjName(key []byte) (string, error) {
	if string(key) == "" {
		return "", fmt.Errorf("key cannot be empty")
	}
	if strings.Contains(string(key), string(os.PathSeparator)) {
		return "", fmt.Errorf("key %q cannot contain '/'", string(key))
	}
	return path.Join(string(s), string(key)), nil
}

// AddIssuers stores Issuers values under their Key if there isn't an object under Key already.
func (s IssuersStorage) AddIssuersIfNotExist(_ context.Context, kv []storage.KV) error {
	for _, kv := range kv {
		objName, err := s.keyToObjName(kv.K)
		if err != nil {
			return fmt.Errorf("failed to convert key to object name: %v", err)
		}
		// We first try and see if this issuer cert has already been stored.
		if f, err := os.ReadFile(objName); err != nil {
			if errors.Is(err, os.ErrNotExist) {
				if err := os.WriteFile(objName, kv.V, 0644); err != nil {
					return fmt.Errorf("failed to write object %q: %v", objName, err)
				}
				klog.V(2).Infof("AddIssuersIfNotExist: added %q", objName)
				continue
			}
			return fmt.Errorf("failed to read object %q: %v", objName, err)
		} else if bytes.Equal(f, kv.V) {
			klog.V(2).Infof("AddIssuersIfNotExist: object %q already exists with identical contents, continuing", objName)
			continue
		}
		return fmt.Errorf("object %q already exists with different content", objName)
	}
	return nil
}
