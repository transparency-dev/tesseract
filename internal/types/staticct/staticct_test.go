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
	"bytes"
	"fmt"
	"math/rand/v2"
	"testing"

	"github.com/transparency-dev/tessera/ctonly"
	"github.com/transparency-dev/tesseract/testdata"
)

func oldExtractEntryFromBundle(ebRaw []byte, eIdx uint64) (*ctonly.Entry, error) {
	eb := EntryBundle{}
	if err := eb.UnmarshalText(ebRaw); err != nil {
		return nil, fmt.Errorf("failed to unmarshal entry bundle: %v", err)
	}

	if l := uint64(len(eb.Entries)); l <= eIdx {
		return nil, fmt.Errorf("entry bundle has only %d entries, but wanted at least %d", l, eIdx)
	}
	e := Entry{}
	if err := e.UnmarshalText([]byte(eb.Entries[eIdx])); err != nil {
		return nil, fmt.Errorf("failed to unmarshal entry %d in entry bundle: %v", eIdx, err)
	}
	return &ctonly.Entry{
		Timestamp:     e.Timestamp,
		IsPrecert:     e.IsPrecert,
		Certificate:   e.Certificate,
		IssuerKeyHash: e.IssuerKeyHash,
	}, nil
}

func BenchmarkExtractEntryFromBundle(b *testing.B) {
	for b.Loop() {
		i := rand.Uint64N(256)
		_, err := ExtractEntryFromBundle(testdata.ExampleFullTile, i)
		if err != nil {
			b.Fatalf("ExtractEntryFromBundle: %v", err)
		}
	}
}

func TestExtractEntryFromBundle(t *testing.T) {
	for i := uint64(0); i < 256; i++ {
		expected, err := oldExtractEntryFromBundle(testdata.ExampleFullTile, i)
		if err != nil {
			t.Fatalf("oldExtractEntryFromBundle: %v", err)
		}
		entry, err := ExtractEntryFromBundle(testdata.ExampleFullTile, i)
		if err != nil {
			t.Fatalf("ExtractEntryFromBundle: %v", err)
		}
		if entry.Timestamp != expected.Timestamp {
			t.Errorf("%d: timestamp mismatch", i)
		}
		if entry.IsPrecert != expected.IsPrecert {
			t.Errorf("%d: IsPrecert mismatch", i)
		}
		if !bytes.Equal(entry.Certificate, expected.Certificate) {
			t.Errorf("%d: Certificate mismatch", i)
		}
		if !bytes.Equal(entry.IssuerKeyHash, expected.IssuerKeyHash) {
			t.Errorf("%d: IssuerKeyHash mismatch", i)
		}
	}
}
