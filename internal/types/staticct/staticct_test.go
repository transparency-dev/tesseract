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
	"testing"

	"github.com/transparency-dev/tesseract/internal/types/rfc6962"
	"github.com/transparency-dev/tesseract/testdata"
)

func TestExtractSCTInputFromBundle(t *testing.T) {
	eb := EntryBundle{}
	if err := eb.UnmarshalText(testdata.ExampleFullTile); err != nil {
		t.Fatalf("failed to unmarshal full tile: %v", err)
	}
	for i := uint64(0); i < uint64(len(eb.Entries)); i++ {
		entry, err := ExtractSCTInputFromBundle(testdata.ExampleFullTile, i)
		if err != nil {
			t.Fatalf("ExtractSCTInputFromBundle(%d): %v", i, err)
		}
		expected := Entry{}
		if err := expected.UnmarshalText(eb.Entries[i]); err != nil {
			t.Fatalf("UnmarshalText(%d): %v", i, err)
		}
		extractedIdx, err := ParseCTExtensionsBytes(entry.Extensions)
		if err != nil || extractedIdx != expected.LeafIndex {
			t.Errorf("%d: extracted index %d != expected %d", i, extractedIdx, expected.LeafIndex)
		}
		if entry.Timestamp != expected.Timestamp {
			t.Errorf("%d: entry.Timestamp %d != expected %d", i, entry.Timestamp, expected.Timestamp)
		}
		isPrecert := entry.EntryType == rfc6962.PrecertLogEntryType
		if isPrecert != expected.IsPrecert {
			t.Errorf("%d: isPrecert %v != expected %v", i, isPrecert, expected.IsPrecert)
		}
		var cert []byte
		if isPrecert {
			cert = entry.PrecertEntry.TBSCertificate
			if !bytes.Equal(entry.PrecertEntry.IssuerKeyHash[:], expected.IssuerKeyHash) {
				t.Errorf("%d: entry.IssuerKeyHash mismatch", i)
			}
		} else {
			cert = entry.X509Entry.Data
		}
		if !bytes.Equal(cert, expected.Certificate) {
			t.Errorf("%d: entry.Certificate mismatch", i)
		}
	}
}
