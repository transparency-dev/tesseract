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
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/transparency-dev/tesseract/internal/types/staticct"
	"github.com/transparency-dev/tesseract/storage"
)

func TestNewIssuerStorage(t *testing.T) {
	tmpDir := t.TempDir()

	tests := []struct {
		name    string
		root    string
		wantErr bool
	}{
		{
			name:    "existing root",
			root:    "",
			wantErr: false,
		},
		{
			name:    "non-existing root",
			root:    "sho/vel",
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := NewIssuerStorage(t.Context(), filepath.Join(tmpDir, tt.root))
			if (err != nil) != tt.wantErr {
				t.Errorf("NewIssuerStorage() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
		})
	}
}

func TestNewRootsStorage(t *testing.T) {
	tmpDir := t.TempDir()

	tests := []struct {
		name    string
		path    string
		wantErr bool
	}{
		{
			name:    "valid path",
			path:    "",
			wantErr: false,
		},
		{
			name:    "non-existent path (creates it)",
			path:    "nonexistent",
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := NewRootsStorage(t.Context(), filepath.Join(tmpDir, tt.path))
			if (err != nil) != tt.wantErr {
				t.Errorf("NewRootsStorage() error = %v, wantErr %v", err, tt.wantErr)
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
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tmpDir := t.TempDir()
			var s *IssuersStorage
			var err error

			if tt.isRoots {
				s, err = NewRootsStorage(t.Context(), tmpDir)
			} else {
				s, err = NewIssuerStorage(t.Context(), tmpDir)
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
	tmpDir := t.TempDir()

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
			name: "add existing issuer with different data",
			kv: []storage.KV{
				{K: []byte("issuer1"), V: []byte("issuer1 data")},
				{K: []byte("issuer1"), V: []byte("different data")},
			},
			want: map[string][]byte{
				"issuer1": []byte("issuer1 data"),
			},
			wantErr: true,
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
		{
			name: "add issuer with invalid path",
			kv: []storage.KV{
				{K: []byte("dir1/dir2/issuer5"), V: []byte("issuer5 data")},
			},
			want:    map[string][]byte{},
			wantErr: true,
		},
		{
			name: "add issuer with empty path",
			kv: []storage.KV{
				{K: []byte(""), V: []byte("issuer5 data")},
			},
			want:    map[string][]byte{},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s, err := NewIssuerStorage(t.Context(), tmpDir)
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
				objName := filepath.Join(tmpDir, staticct.IssuersPrefix, k)
				got, err := os.ReadFile(objName)
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
