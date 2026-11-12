// Copyright 2026 The Tessera authors. All Rights Reserved.
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

package storage

import (
	"context"
	"errors"
	"os"
	"testing"

	"github.com/transparency-dev/tessera"
)

// fakeLogReader is a tessera.LogReader for a log without a checkpoint.
type fakeLogReader struct {
	tessera.LogReader
}

func (fakeLogReader) ReadCheckpoint(context.Context) ([]byte, error) {
	return nil, os.ErrNotExist
}

func TestCTStorageShutdown(t *testing.T) {
	errShutdown := errors.New("shutdown failed")
	type ctxKey struct{}

	for _, tc := range []struct {
		name       string
		noShutdown bool
		wantErr    error
		wantCalls  int
	}{
		{
			name:       "not set",
			noShutdown: true,
		},
		{
			name:      "ok",
			wantCalls: 1,
		},
		{
			name:      "error",
			wantErr:   errShutdown,
			wantCalls: 1,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			calls := 0
			opts := &CTStorageOptions{Reader: fakeLogReader{}}
			if !tc.noShutdown {
				opts.Shutdown = func(ctx context.Context) error {
					calls++
					if ctx.Value(ctxKey{}) == nil {
						t.Error("shutdown wasn't passed the caller's context")
					}
					return tc.wantErr
				}
			}
			cts, err := NewCTStorage(t.Context(), opts)
			if err != nil {
				t.Fatalf("NewCTStorage(): %v", err)
			}

			ctx := context.WithValue(t.Context(), ctxKey{}, true)
			if err := cts.Shutdown(ctx); !errors.Is(err, tc.wantErr) {
				t.Errorf("Shutdown() = %v, want %v", err, tc.wantErr)
			}
			if calls != tc.wantCalls {
				t.Errorf("shutdown called %d times, want %d", calls, tc.wantCalls)
			}
		})
	}
}
