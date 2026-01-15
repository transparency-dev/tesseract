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

package gcp

import (
	"context"
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/fsouza/fake-gcs-server/fakestorage"
	"github.com/transparency-dev/tesseract/internal/types/staticct"
	"github.com/transparency-dev/tesseract/storage"
)

const testBucket = "bucket"

func newTestStorage(t *testing.T, bucket string) *fakestorage.Server {
	t.Helper()

	srv, err := fakestorage.NewServerWithOptions(fakestorage.Options{
		Scheme: "http",
	})
	if err != nil {
		t.Fatalf("NewServer: %v", err)
	}
	if err := os.Setenv("STORAGE_EMULATOR_HOST", srv.URL()); err != nil {
		t.Fatalf("Setenv: %v", err)
	}
	srv.CreateBucketWithOpts(fakestorage.CreateBucketOpts{
		Name: bucket,
	})
	srv.CreateObject(fakestorage.Object{
		Content: []byte(""),
		ObjectAttrs: fakestorage.ObjectAttrs{
			Name: staticct.IssuersPrefix,
		},
	})
	return srv
}

func TestNewIssuerStorage(t *testing.T) {
	tests := []struct {
		name       string
		wantBucket string
		wantErr    bool
	}{
		{
			name:       "valid bucket",
			wantBucket: testBucket,
			wantErr:    false,
		},
		{
			name:       "non-existing bucket",
			wantBucket: "shovel",
			wantErr:    true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			srv := newTestStorage(t, testBucket)
			defer srv.Stop()

			_, err := NewIssuerStorage(t.Context(), tt.wantBucket, srv.Client())
			if (err != nil) != tt.wantErr {
				t.Errorf("NewIssuerStorage() error = %v, wantErr %v", err, tt.wantErr)
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
			srv := newTestStorage(t, testBucket)
			defer srv.Stop()

			s, err := NewIssuerStorage(t.Context(), testBucket, nil) //srv.Client())
			if err != nil {
				t.Fatalf("NewIssuerStorage() failed: %v", err)
			}

			// Apply KV updates.
			err = s.AddIssuersIfNotExist(context.Background(), tt.kv)
			if (err != nil) != tt.wantErr {
				t.Errorf("AddIssuersIfNotExist() error = %v, wantErr %v", err, tt.wantErr)
				return
			}

			// Now look for expected final state.
			for k, v := range tt.want {
				objName := filepath.Join(staticct.IssuersPrefix, k)
				gotObj, err := srv.GetObject(testBucket, objName)
				if err != nil {
					t.Fatalf("Failed to read object %q: %v", objName, err)
				}
				got := gotObj.Content
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
			srv := newTestStorage(t, testBucket)
			defer srv.Stop()

			s, err := NewIssuerStorage(t.Context(), testBucket, nil) //srv.Client())
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
				gotObj, err := srv.GetObject(testBucket, objName)
				if err != nil {
					t.Fatalf("Failed to read object %q: %v", objName, err)
				}
				got := gotObj.Content
				if !reflect.DeepEqual(got, v) {
					t.Errorf("Object %q content mismatch: got %v, want %v", objName, got, v)
				}
			}
		})
	}
}
