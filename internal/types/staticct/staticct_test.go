// Copyright 2025 Google LLC. All Rights Reserved.
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

package staticct

import (
	"fmt"
	"math/rand/v2"
	"testing"

	"github.com/transparency-dev/tesseract/testdata"
)

// oldExtractTimestampFromBundle extracts the timestamp from the Nth entry in the provided serialised entry bundle.
//
// This is the original naive implementation which works by parsing and extrating everything from the bundle,
// and pulling out only the 64bit timestamp we're actually interested in.
//
// This is only being kept around for comparison with the faster implementation in ExtractTimestampFromBundle,
// this func & the benchmark which references it can be removed shortly.
func oldExtractTimestampFromBundle(ebRaw []byte, eIdx uint64) (uint64, error) {
	eb := EntryBundle{}
	if err := eb.UnmarshalText(ebRaw); err != nil {
		return 0, fmt.Errorf("failed to unmarshal entry bundle: %v", err)
	}

	if l := uint64(len(eb.Entries)); l <= eIdx {
		return 0, fmt.Errorf("entry bundle has only %d entries, but wanted at least %d", l, eIdx)
	}
	e := Entry{}
	t, err := UnmarshalTimestamp([]byte(eb.Entries[eIdx]))
	if err != nil {
		return 0, fmt.Errorf("failed to extract timestamp from entry %d in entry bundle: %v", eIdx, e)
	}
	return t, nil
}

func BenchmarkOldExtractTimestampFromBundle(b *testing.B) {
	for b.Loop() {
		i := rand.Uint64N(256)
		_, err := oldExtractTimestampFromBundle(testdata.ExampleFullTile, i)
		if err != nil {
			b.Fatalf("timestampOld: %v", err)
		}
	}
}

func BenchmarkExtractTimestampFromBundle(b *testing.B) {
	for b.Loop() {
		i := rand.Uint64N(256)
		_, err := ExtractTimestampFromBundle(testdata.ExampleFullTile, i)
		if err != nil {
			b.Fatalf("timestamp: %v", err)
		}
	}
}

func TestExtractTimestampFromBundle(t *testing.T) {
	for i := uint64(0); i < 256; i++ {
		ts1, err := oldExtractTimestampFromBundle(testdata.ExampleFullTile, i)
		if err != nil {
			t.Fatalf("timestampOld: %v", err)
		}
		ts2, err := ExtractTimestampFromBundle(testdata.ExampleFullTile, i)
		if err != nil {
			t.Fatalf("timestampOld: %v", err)
		}
		if ts1 != ts2 {
			t.Errorf("%d: timestampOld %d != timestamp %d", i, ts1, ts2)
		}
	}
}
