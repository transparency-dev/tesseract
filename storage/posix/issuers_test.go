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
		path    string
		wantErr bool
	}{
		{
			name:    "valid path",
			path:    "",
			wantErr: false,
		},
		{
			name:    "non-existent path",
			path:    "nonexistent",
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := NewIssuerStorage(t.Context(), filepath.Join(tmpDir, tt.path))
			if (err != nil) != tt.wantErr {
				t.Errorf("NewIssuerStorage() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
		})
	}
}

func TestAddIssuersIfNotExist(t *testing.T) {
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
			err = s.AddIssuersIfNotExist(context.Background(), tt.kv)
			if (err != nil) != tt.wantErr {
				t.Errorf("AddIssuersIfNotExist() error = %v, wantErr %v", err, tt.wantErr)
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
