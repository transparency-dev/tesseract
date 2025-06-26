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

package aws

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"path"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/aws/smithy-go"
	"github.com/google/go-cmp/cmp"
	"github.com/transparency-dev/tesseract/storage"
	"golang.org/x/sync/errgroup"
	"k8s.io/klog/v2"
)

// IssuersStorage is a key value store backed by S3 on AWS to store issuer chains.
type IssuersStorage struct {
	s3Client    *s3.Client
	bucket      string
	prefix      string
	contentType string
}

// NewIssuerStorage creates a new IssuerStorage.
//
// The specified bucket must exist or an error will be returned.
func NewIssuerStorage(ctx context.Context, bucket string, prefix string, contentType string) (*IssuersStorage, error) {
	sdkConfig, err := config.LoadDefaultConfig(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to load default AWS configuration: %v", err)
	}

	r := &IssuersStorage{
		s3Client:    s3.NewFromConfig(sdkConfig),
		bucket:      bucket,
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
			put := &s3.PutObjectInput{
				Bucket:      aws.String(s.bucket),
				Key:         aws.String(objName),
				Body:        bytes.NewReader(kv.V),
				ContentType: aws.String(s.contentType),
				IfNoneMatch: aws.String("*"),
			}

			// If we run into a precondition failure error, check that the object
			// which exists contains the same content that we want to write.
			// If so, we can consider this write to be idempotently successful.
			if _, err := s.s3Client.PutObject(ctx, put); err != nil {
				var apiErr smithy.APIError
				if errors.As(err, &apiErr); apiErr.ErrorCode() == "PreconditionFailed" {
					existing, err := s.getIssuer(ctx, objName)
					if err != nil {
						return fmt.Errorf("failed to fetch existing content for %q: %v", objName, err)
					}
					if !bytes.Equal(existing, kv.V) {
						klog.Errorf("Resource %q non-idempotent write:\n%s", objName, cmp.Diff(existing, kv.V))
						return fmt.Errorf("precondition failed: resource content for %q differs from data to-be-written", objName)
					}

					klog.V(2).Infof("AddIssuersIfNotExist: object %q already exists in bucket %q, continuing", objName, s.bucket)
					return nil
				}
				return fmt.Errorf("failed to write object %q to bucket %q: %w", objName, s.bucket, err)
			}
			klog.V(2).Infof("AddIssuersIfNotExist: added %q in bucket %q", objName, s.bucket)
			return nil
		})
	}

	if err := errG.Wait(); err != nil {
		return err
	}

	return nil
}

// getIssuer returns the issuer at the specified key, or an error.
func (s *IssuersStorage) getIssuer(ctx context.Context, obj string) ([]byte, error) {
	r, err := s.s3Client.GetObject(ctx, &s3.GetObjectInput{
		Bucket: aws.String(s.bucket),
		Key:    aws.String(obj),
	})
	if err != nil {
		return nil, fmt.Errorf("getIssuer: failed to create reader for issuer %q in bucket %q: %w", obj, s.bucket, err)
	}

	d, err := io.ReadAll(r.Body)
	if err != nil {
		return nil, fmt.Errorf("getIssuer: failed to read %q: %v", obj, err)
	}
	return d, r.Body.Close()
}
