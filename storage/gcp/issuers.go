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
	"github.com/transparency-dev/tesseract/internal/types/staticct"
	"github.com/transparency-dev/tesseract/storage"
	"golang.org/x/sync/errgroup"
	"google.golang.org/api/googleapi"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
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
func NewIssuerStorage(ctx context.Context, bucket string, gcsClient *gcs.Client) (*IssuersStorage, error) {
	if gcsClient == nil {
		c, err := gcs.NewClient(ctx, gcs.WithJSONReads())
		if err != nil {
			return nil, fmt.Errorf("failed to create GCS client: %v", err)
		}
		gcsClient = c
	}

	r := &IssuersStorage{
		bucket:      gcsClient.Bucket(bucket),
		prefix:      staticct.IssuersPrefix,
		contentType: staticct.IssuersContentType,
	}

	return r, nil
}

// keyToObjName converts bytes to a GCS object name.
func (s *IssuersStorage) keyToObjName(key []byte) string {
	return path.Join(s.prefix, string(key))
}

// AddIssuers stores Issuers values under their Key if there isn't an object under Key already.
func (s *IssuersStorage) AddIssuersIfNotExist(ctx context.Context, kv []storage.KV) error {
	// We first try and see if this issuer cert has already been stored since reads
	// are cheaper than writes.
	eg := errgroup.Group{}
	for _, kv := range kv {
		eg.Go(func() error {
			objName := s.keyToObjName(kv.K)
			obj := s.bucket.Object(objName)

			w := obj.If(gcs.Conditions{DoesNotExist: true}).NewWriter(ctx)
			w.ContentType = s.contentType

			if _, err := w.Write(kv.V); err != nil {
				return fmt.Errorf("failed to write object %q to bucket %q: %w", objName, s.bucket.BucketName(), err)
			}

			if err := w.Close(); err != nil {
				// Need to check whether the issuer was already present so that we can hide the error if this is an idempotent write.
				// Unfortunately, the way in which this is communicated by the gcs client differs depending on whether the underlying
				// transport used is HTTP or gRPC, so we need to check both:
				failedPrecondition := false
				if ee, ok := err.(*googleapi.Error); ok && ee.Code == http.StatusPreconditionFailed {
					failedPrecondition = true
				} else if st, ok := status.FromError(err); ok && st.Code() == codes.FailedPrecondition {
					failedPrecondition = true
				}
				if failedPrecondition {
					r, err := obj.NewReader(ctx)
					if err != nil {
						return fmt.Errorf("failed to create reader for object %q in bucket %q: %w", objName, s.bucket.BucketName(), err)
					}

					existing, err := io.ReadAll(r)
					if err != nil {
						return fmt.Errorf("failed to read %q: %v", objName, err)
					}

					if !bytes.Equal(existing, kv.V) {
						klog.Errorf("Resource %q non-idempotent write:\n%s", objName, cmp.Diff(existing, kv.V))
						return fmt.Errorf("precondition failed: resource content for %q differs from data to-be-written", objName)
					}
					klog.V(2).Infof("AddIssuersIfNotExist: object %q with same data already exists in bucket %q, continuing", objName, s.bucket.BucketName())
					return nil
				}

				return fmt.Errorf("failed to close write on %q: %v", objName, err)
			}

			klog.V(2).Infof("AddIssuersIfNotExist: added %q in bucket %q", objName, s.bucket.BucketName())
			return nil
		})
	}
	return eg.Wait()
}
