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

package gcp

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net/http"
	"path"

	gcs "cloud.google.com/go/storage"
	"github.com/google/go-cmp/cmp"
	"github.com/transparency-dev/tesseract/storage"
	"golang.org/x/sync/errgroup"
	"google.golang.org/api/googleapi"
	"k8s.io/klog/v2"
)

// IssuersStorage is a key value store backed by GCS on GCP to store issuer chains.
type IssuersStorage struct {
	bucket      *gcs.BucketHandle
	prefix      string
	contentType string
}

// NewIssuerStorage creates a new GCSStorage.
//
// The specified bucket must exist or an error will be returned.
func NewIssuerStorage(ctx context.Context, bucket string, prefix string, contentType string) (*IssuersStorage, error) {
	c, err := gcs.NewClient(ctx, gcs.WithJSONReads())
	if err != nil {
		return nil, fmt.Errorf("failed to create GCS client: %v", err)
	}

	r := &IssuersStorage{
		bucket:      c.Bucket(bucket),
		prefix:      prefix,
		contentType: contentType,
	}

	return r, nil
}

// keyToObjName converts bytes to a GCS object name.
func (s *IssuersStorage) keyToObjName(key []byte) string {
	return path.Join(s.prefix, string(key))
}

// AddIssuers stores Issuers values under their Key if there isn't an object under Key already.
// Wrap with a local cache to avoid unecessary read and write operations as issuers are reused
// accross chains.
func (s *IssuersStorage) AddIssuersIfNotExist(ctx context.Context, kv []storage.KV) error {
	errG := errgroup.Group{}
	for _, kv := range kv {
		errG.Go(func() error {
			objName := s.keyToObjName(kv.K)
			obj := s.bucket.Object(objName)

			w := obj.If(gcs.Conditions{DoesNotExist: true}).NewWriter(ctx)
			w.ContentType = s.contentType

			if _, err := w.Write(kv.V); err != nil {
				return fmt.Errorf("failed to write object %q to bucket %q: %w", objName, s.bucket.BucketName(), err)
			}

			if err := w.Close(); err != nil {
				// If we run into a precondition failure error, check that the object
				// which exists contains the same content that we want to write.
				// If so, we can consider this write to be idempotently successful.
				if ee, ok := err.(*googleapi.Error); ok && ee.Code == http.StatusPreconditionFailed {
					existing, existingGen, err := s.getIssuer(ctx, objName)
					if err != nil {
						return fmt.Errorf("failed to fetch existing content for %q (@%d): %v", objName, existingGen, err)
					}
					if !bytes.Equal(existing, kv.V) {
						klog.Errorf("Resource %q non-idempotent write:\n%s", objName, cmp.Diff(existing, kv.V))
						return fmt.Errorf("precondition failed: resource content for %q differs from data to-be-written", objName)
					}
					klog.V(2).Infof("AddIssuersIfNotExist: object %q already exists in bucket %q, continuing", objName, s.bucket.BucketName())
					return nil
				}
				return fmt.Errorf("failed to close write on %q: %v", objName, err)
			}
			klog.V(2).Infof("AddIssuersIfNotExist: added %q in bucket %q", objName, s.bucket.BucketName())
			return nil
		})
	}

	if err := errG.Wait(); err != nil {
		return err
	}

	return nil
}

// getIssuer returns the data and generation of the specified issuer, or an error.
func (s *IssuersStorage) getIssuer(ctx context.Context, key string) ([]byte, int64, error) {
	r, err := s.bucket.Object(key).NewReader(ctx)
	if err != nil {
		return nil, -1, fmt.Errorf("getIssuer: failed to create reader for issuer %q in bucket %q: %w", key, s.bucket.BucketName(), err)
	}

	d, err := io.ReadAll(r)
	if err != nil {
		return nil, -1, fmt.Errorf("failed to read isuer %q: %v", key, err)
	}
	return d, r.Attrs.Generation, r.Close()
}
