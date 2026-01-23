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
	"strings"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/aws/smithy-go"
	"github.com/google/go-cmp/cmp"
	"github.com/transparency-dev/tesseract/internal/types/staticct"
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

// Options holds various settings for NewIssuerStorage.
type Options struct {
	// Bucket is the bucket to use for storing issuers.
	Bucket string
	// SDKConfig is an optional configuration for the AWS SDK, if nil the default config will be used.
	SDKConfig *aws.Config
	// S3Options are used when creating a new AWS S3 client. This MUST be provided if SDKConfig is not nil.
	S3Options func(*s3.Options)
}

// newPrefixedStorage creates a new S3 based issuer storage.
// Objects will be stored under prefix/.
func newPrefixedStorage(ctx context.Context, opts Options, prefix string) (*IssuersStorage, error) {
	var sdkConfig aws.Config
	if opts.SDKConfig != nil {
		sdkConfig = *opts.SDKConfig
	} else {
		var err error
		sdkConfig, err = config.LoadDefaultConfig(ctx)
		if err != nil {
			return nil, fmt.Errorf("failed to load default AWS configuration: %v", err)
		}
		// We need a non-nil options func to pass in to s3.NewFromConfig below or it'll panic, so
		// we'll use a "do nothing" placeholder.
		opts.S3Options = func(_ *s3.Options) {}
	}

	r := &IssuersStorage{
		s3Client:    s3.NewFromConfig(sdkConfig, opts.S3Options),
		bucket:      opts.Bucket,
		prefix:      prefix,
		contentType: staticct.IssuersContentType,
	}

	return r, nil
}

// NewIssuerStorage creates a new S3 based issuer storage.
func NewIssuerStorage(ctx context.Context, opts Options) (*IssuersStorage, error) {
	return newPrefixedStorage(ctx, opts, staticct.IssuersPrefix)
}

// NewRootsStorage creates a new S3 based roots storage.
func NewRootsStorage(ctx context.Context, opts Options) (*IssuersStorage, error) {
	return newPrefixedStorage(ctx, opts, storage.RootsPrefix)
}

// keyToObjName converts bytes to an S3 object name.
func (s *IssuersStorage) keyToObjName(key []byte) string {
	return path.Join(s.prefix, string(key))
}

// keyToObjName converts an S3 object name to a key.
func (s *IssuersStorage) objNameToKey(objName string) []byte {
	return []byte(strings.TrimPrefix(objName, s.prefix))
}

// LoadAll loads all the values in the bucket under the prefix.
func (s *IssuersStorage) LoadAll(ctx context.Context) ([]storage.KV, error) {
	errs := []error(nil)
	kvs := []storage.KV{}

	paginator := s3.NewListObjectsV2Paginator(s.s3Client, &s3.ListObjectsV2Input{
		Bucket: aws.String(s.bucket),
		Prefix: aws.String(s.prefix),
	})

	for paginator.HasMorePages() {
		page, err := paginator.NextPage(ctx)
		if err != nil {
			// If listing fails, stop iterating
			errs = append(errs, fmt.Errorf("failed to list objects in bucket %q prefix %q: %w", s.bucket, s.prefix, err))
			break
		}

		for _, obj := range page.Contents {
			resp, err := s.s3Client.GetObject(ctx, &s3.GetObjectInput{
				Bucket: aws.String(s.bucket),
				Key:    obj.Key,
			})
			if err != nil {
				errs = append(errs, fmt.Errorf("failed to get object %q: %w", *obj.Key, err))
				continue
			}

			data, err := io.ReadAll(resp.Body)
			if errC := resp.Body.Close(); errC != nil {
				klog.Errorf("resp.Body.Close(): %v", errC)
			}
			if err != nil {
				errs = append(errs, fmt.Errorf("failed to read object body %q: %w", *obj.Key, err))
				continue
			}

			kvs = append(kvs, storage.KV{K: s.objNameToKey(*obj.Key), V: data})
		}
	}

	return kvs, errors.Join(errs...)
}

// AddIfNotExist stores values under their Key if there isn't an object under Key already.
func (s *IssuersStorage) AddIfNotExist(ctx context.Context, kv []storage.KV) error {
	eg := errgroup.Group{}
	for _, kv := range kv {
		objName := s.keyToObjName(kv.K)
		put := &s3.PutObjectInput{
			Bucket:      aws.String(s.bucket),
			Key:         aws.String(objName),
			Body:        bytes.NewReader(kv.V),
			ContentType: aws.String(s.contentType),
			IfNoneMatch: aws.String("*"),
		}

		eg.Go(func() error {
			// If we run into a precondition failure error, check that the object
			// which exists contains the same content that we want to write.
			// If so, we can consider this write to be idempotently successful.
			if _, err := s.s3Client.PutObject(ctx, put); err != nil {
				var apiErr smithy.APIError
				if errors.As(err, &apiErr) && apiErr.ErrorCode() == "PreconditionFailed" {
					existingObj, err := s.s3Client.GetObject(ctx, &s3.GetObjectInput{
						Bucket: aws.String(s.bucket),
						Key:    aws.String(objName),
					})
					if err != nil {
						return fmt.Errorf("failed to fetch existing object %q: %v", objName, err)
					}
					existing, err := io.ReadAll(existingObj.Body)
					if err != nil {
						return fmt.Errorf("failed to read object %q: %v", objName, err)
					}
					if !bytes.Equal(existing, kv.V) {
						klog.Errorf("Resource %q non-idempotent write:\n%s", objName, cmp.Diff(existing, kv.V))
						return fmt.Errorf("precondition failed: resource content for %q differs from data to-be-written", objName)
					}
					return nil
				}
				return fmt.Errorf("failed to write object %q to bucket %q: %w", objName, s.bucket, err)
			}
			klog.Infof("AddIfNotExist: added %q in bucket %q", objName, s.bucket)
			return nil
		})
	}
	return eg.Wait()
}
