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

package loadtest

import (
	"context"
	"testing"

	"github.com/transparency-dev/tesseract/internal/types/rfc6962"
)

func BenchmarkLogWriter(b *testing.B) {
	for b.Loop() {
		c := 0
		N := 1000
		throttle := make(chan bool, N)
		for range N {
			throttle <- true
		}
		w := &LogWriter{
			gen:      newAddChainReq,
			throttle: throttle,
		}
		w.writer = func(ctx context.Context, data []byte) (index uint64, timestamp uint64, err error) {
			c++
			if c >= N {
				w.cancel()
			}
			return 0, 0, nil
		}

		w.Run(b.Context())
	}
}

// newAddChainReq mimics chainGenerator.addChainRequestBody since we don't have access to it from here.
func newAddChainReq() rfc6962.AddChainRequest {
	return rfc6962.AddChainRequest{
		Chain: [][]byte{
			[]byte("one"),
			[]byte("two"),
		},
	}
}
