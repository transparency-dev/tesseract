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
	"context"
	"crypto/tls"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/johannesboyne/gofakes3"
	"github.com/johannesboyne/gofakes3/backend/s3mem"

	"github.com/transparency-dev/tesseract/internal/types/staticct"
	"github.com/transparency-dev/tesseract/storage"
)

const testBucket = "bucket"

func newTestStorage(t *testing.T, bucket string) (*aws.Config, func(*s3.Options), func()) {
	t.Helper()

	// Set up gofakes3 server
	backend := s3mem.New()
	faker := gofakes3.New(backend)
	ts := httptest.NewServer(faker.Server())

	opts := func(o *s3.Options) {
		o.UsePathStyle = true
		o.BaseEndpoint = aws.String(ts.URL)
	}

	// Setup AWS SDK v2 config
	cfg, err := config.LoadDefaultConfig(
		context.TODO(),
		config.WithCredentialsProvider(credentials.NewStaticCredentialsProvider("ACCESS_KEY", "SECRET_KEY", "")),
		config.WithRegion("us-east"),
		config.WithHTTPClient(&http.Client{
			Transport: &http.Transport{
				TLSClientConfig: &tls.Config{InsecureSkipVerify: true},
			},
		}),
	)
	if err != nil {
		t.Fatalf("Failed to set up SDK config: %v", err)
	}

	client := s3.NewFromConfig(cfg, opts)
	_, err = client.CreateBucket(t.Context(), &s3.CreateBucketInput{
		Bucket: aws.String(bucket),
	})
	if err != nil {
		t.Fatalf("Failed to create bucket: %v", err)
	}
	return &cfg, opts, ts.Close
}

func TestNewIssuerStorage(t *testing.T) {
	tests := []struct {
		name   string
		bucket string
	}{
		{
			name:   "valid bucket",
			bucket: testBucket,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg, opts, done := newTestStorage(t, testBucket)
			defer done()

			_, err := NewIssuerStorage(t.Context(), Options{Bucket: tt.bucket, SDKConfig: cfg, S3Options: opts})
			if err != nil {
				t.Errorf("NewIssuerStorage() error = %v", err)
				return
			}
		})
	}
}

func TestAddIssuersIfNotExist(t *testing.T) {
	tests := []struct {
		name    string
		kv      []storage.KV
		want    map[string][]byte
		wantErr bool
	}{
		{
			name: "add single issuer",
			kv: []storage.KV{
				{K: []byte("issuer1"), V: []byte("issuer1 data")},
			},
			want: map[string][]byte{
				"issuer1": []byte("issuer1 data"),
			},
			wantErr: false,
		},
		{
			name: "add multiple issuers",
			kv: []storage.KV{
				{K: []byte("issuer2"), V: []byte("issuer2 data")},
				{K: []byte("issuer3"), V: []byte("issuer3 data")},
			},
			want: map[string][]byte{
				"issuer2": []byte("issuer2 data"),
				"issuer3": []byte("issuer3 data"),
			},
			wantErr: false,
		},
		{
			name: "add existing issuer",
			kv: []storage.KV{
				{K: []byte("issuer1"), V: []byte("issuer1 data")},
				{K: []byte("issuer1"), V: []byte("issuer1 data")},
			},
			want: map[string][]byte{
				"issuer1": []byte("issuer1 data"),
			},
			wantErr: false,
		},
		{
			name: "add new issuer and existing issuer",
			kv: []storage.KV{
				{K: []byte("issuer1"), V: []byte("issuer1 data")},
				{K: []byte("issuer4"), V: []byte("issuer4 data")},
				{K: []byte("issuer1"), V: []byte("issuer1 data")},
			},
			want: map[string][]byte{
				"issuer1": []byte("issuer1 data"),
				"issuer4": []byte("issuer4 data"),
			},
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg, opts, done := newTestStorage(t, testBucket)
			defer done()

			s, err := NewIssuerStorage(t.Context(), Options{Bucket: testBucket, SDKConfig: cfg, S3Options: opts})
			if err != nil {
				t.Fatalf("NewIssuerStorage() failed: %v", err)
			}

			// Apply KV updates.
			err = s.AddIssuersIfNotExist(context.Background(), tt.kv)
			if (err != nil) != tt.wantErr {
				t.Errorf("AddIssuersIfNotExist() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if tt.wantErr {
				return
			}

			// Now look for expected final state.
			for k, v := range tt.want {
				objName := filepath.Join(staticct.IssuersPrefix, k)
				gotObj, err := s.s3Client.GetObject(t.Context(), &s3.GetObjectInput{
					Bucket: aws.String(testBucket),
					Key:    aws.String(objName),
				})
				if err != nil {
					t.Fatalf("Failed to fetch object %q: %v", objName, err)
				}
				got, err := io.ReadAll(gotObj.Body)
				if err != nil {
					t.Fatalf("Failed to read object %q: %v", objName, err)
				}
				if !reflect.DeepEqual(got, v) {
					t.Errorf("Object %q content mismatch: got %v, want %v", objName, got, v)
				}
			}
		})
	}
}

func TestAllKVAreWritten(t *testing.T) {
	tests := []struct {
		name    string
		setup   []storage.KV
		kv      []storage.KV
		want    map[string][]byte
		wantErr bool
	}{
		{
			name: "Don't bail batch if idempotent write happens",
			setup: []storage.KV{
				{K: []byte("issuer1"), V: []byte("issuer1 data")},
			},
			kv: []storage.KV{
				{K: []byte("issuer1"), V: []byte("issuer1 data")},
				{K: []byte("issuer2"), V: []byte("issuer2 data")},
				{K: []byte("issuer3"), V: []byte("issuer3 data")},
			},
			want: map[string][]byte{
				"issuer1": []byte("issuer1 data"),
				"issuer2": []byte("issuer2 data"),
				"issuer3": []byte("issuer3 data"),
			},
		},
		{
			name: "add existing issuer with different data",
			setup: []storage.KV{
				{K: []byte("issuer1"), V: []byte("issuer1 data")},
			},
			kv: []storage.KV{
				{K: []byte("issuer1"), V: []byte("different data")},
			},
			want: map[string][]byte{
				"issuer1": []byte("issuer1 data"),
			},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg, opts, done := newTestStorage(t, testBucket)
			defer done()

			s, err := NewIssuerStorage(t.Context(), Options{Bucket: testBucket, SDKConfig: cfg, S3Options: opts})
			if err != nil {
				t.Fatalf("NewIssuerStorage() failed: %v", err)
			}
			// Create test env with pre-existing entries.
			err = s.AddIssuersIfNotExist(context.Background(), tt.setup)
			if err != nil {
				t.Errorf("Setup: AddIssuersIfNotExist() error = %v", err)
				return
			}

			// Apply KV updates.
			err = s.AddIssuersIfNotExist(context.Background(), tt.kv)
			if gotErr := err != nil; gotErr != tt.wantErr {
				t.Fatalf("AddIssuersIfNotExist = %v, want err %t", err, tt.wantErr)
			}
			if tt.wantErr {
				return
			}

			// Now look for expected final state.
			for k, v := range tt.want {
				objName := filepath.Join(staticct.IssuersPrefix, k)
				gotObj, err := s.s3Client.GetObject(t.Context(), &s3.GetObjectInput{
					Bucket: aws.String(testBucket),
					Key:    aws.String(objName),
				})
				if err != nil {
					t.Fatalf("Failed to fetch object %q: %v", objName, err)
				}
				got, err := io.ReadAll(gotObj.Body)
				if err != nil {
					t.Fatalf("Failed to read object %q: %v", objName, err)
				}
				if !reflect.DeepEqual(got, v) {
					t.Errorf("Object %q content mismatch: got %v, want %v", objName, got, v)
				}
			}
		})
	}
}
