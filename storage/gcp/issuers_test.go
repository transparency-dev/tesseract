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
	"bytes"
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
			srv := newTestStorage(t, tt.bucket)
			defer srv.Stop()

			_, err := NewIssuerStorage(t.Context(), tt.bucket, srv.Client())
			if err != nil {
				t.Errorf("NewIssuerStorage() error = %v", err)
				return
			}
		})
	}
}

func TestNewRootsStorage(t *testing.T) {
	tests := []struct {
		name   string
		bucket string
	}{
		{
			name:   "valid path",
			bucket: testBucket,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			srv := newTestStorage(t, testBucket)
			defer srv.Stop()

			_, err := NewRootsStorage(t.Context(), testBucket, srv.Client())
			if err != nil {
				t.Errorf("NewRemoteRootsStorage() error = %v", err)
				return
			}
		})
	}
}

func TestLoadAll(t *testing.T) {
	tests := []struct {
		name    string
		isRoots bool // false = Issuers, true = RemoteRoots
		data    []storage.KV
		wantErr bool
	}{
		{
			name:    "Load issuers (single)",
			isRoots: false,
			data: []storage.KV{
				{K: []byte("issuer1"), V: []byte("data1")},
			},
			wantErr: false,
		},
		{
			name:    "Load roots (multiple)",
			isRoots: true,
			data: []storage.KV{
				{K: []byte("root1"), V: []byte("root_data1")},
				{K: []byte("root2"), V: []byte("root_data2")},
			},
			wantErr: false,
		},
		{
			name:    "Load empty bucket",
			isRoots: false,
			data:    []storage.KV{},
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			srv := newTestStorage(t, testBucket)
			defer srv.Stop()

			var s *IssuersStorage
			var err error

			// Explicitely pass nil clients to force re-creation with the correct settings.
			if tt.isRoots {
				s, err = NewRootsStorage(t.Context(), testBucket, nil)
			} else {
				s, err = NewIssuerStorage(t.Context(), testBucket, nil)
			}
			if err != nil {
				t.Fatalf("Storage creation failed: %v", err)
			}

			if len(tt.data) > 0 {
				if err := s.AddIfNotExist(t.Context(), tt.data); err != nil {
					t.Fatalf("Failed to setup test data: %v", err)
				}
			}

			gotKVs, err := s.LoadAll(t.Context())
			if (err != nil) != tt.wantErr {
				t.Errorf("LoadAll() error = %v, wantErr %v", err, tt.wantErr)
				return
			}

			if len(gotKVs) != len(tt.data) {
				t.Errorf("LoadAll() returned %d items, want %d", len(gotKVs), len(tt.data))
			}

			wKV := map[string][]byte{}
			for _, kv := range tt.data {
				wKV[string(kv.K)] = kv.V
			}
			for _, gKV := range gotKVs {
				wV, ok := wKV[string(gKV.K)]
				if !ok {
					t.Errorf("LoadAll() returned unexpected key %q", gKV.K)
				}
				if !bytes.Equal(gKV.V, wV) {
					t.Errorf("LoadAll() key %q = %s, want %s", gKV.K, gKV.V, wV)
				}
			}
		})
	}
}

func TestAddIfNotExist(t *testing.T) {
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

			// Explicitely pass a nil client to force re-creation with the correct settings.
			s, err := NewIssuerStorage(t.Context(), testBucket, nil)
			if err != nil {
				t.Fatalf("NewIssuerStorage() failed: %v", err)
			}

			// Apply KV updates.
			err = s.AddIfNotExist(context.Background(), tt.kv)
			if (err != nil) != tt.wantErr {
				t.Errorf("AddIfNotExist() error = %v, wantErr %v", err, tt.wantErr)
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

			// Explicitely pass nil clients to force re-creation with the correct settings.
			s, err := NewIssuerStorage(t.Context(), testBucket, nil)
			if err != nil {
				t.Fatalf("NewIssuerStorage() failed: %v", err)
			}
			// Create test env with pre-existing entries.
			err = s.AddIfNotExist(context.Background(), tt.setup)
			if err != nil {
				t.Errorf("Setup: AddIfNotExist() error = %v", err)
				return
			}

			// Apply KV updates.
			err = s.AddIfNotExist(context.Background(), tt.kv)
			if gotErr := err != nil; gotErr != tt.wantErr {
				t.Fatalf("AddIfNotExist = %v, want err %t", err, tt.wantErr)
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
