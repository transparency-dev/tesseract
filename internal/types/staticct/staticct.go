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

package staticct

import (
	"bytes"
	"encoding/base64"
	"fmt"
	"math"

	"github.com/transparency-dev/tessera/api/layout"
	"golang.org/x/crypto/cryptobyte"
)

const (
	IssuersPrefix      = "issuer/"
	IssuersContentType = "application/pkix-cert"
)

///////////////////////////////////////////////////////////////////////////////
// The following structures represent those outlined in Static CT API.
///////////////////////////////////////////////////////////////////////////////

// EntryBundle represents a sequence of entries in the log.
// These entries correspond to a leaf tile in the hash tree.
type EntryBundle struct {
	// Entries stores the leaf entries of the log, in order.
	Entries [][]byte
}

// UnmarshalText implements encoding/TextUnmarshaler and reads EntryBundles
// which are encoded using the Static CT API spec.
// TODO(phbnf): we can probably parse every individual leaf directly, since most callers
// of this method tend to do so.
func (t *EntryBundle) UnmarshalText(raw []byte) error {
	entries := make([][]byte, 0, layout.EntryBundleWidth)
	s := cryptobyte.String(raw)

	for len(s) > 0 {
		entry := []byte{}
		var timestamp uint64
		var entryType uint16
		var extensions, fingerprints cryptobyte.String
		if !s.ReadUint64(&timestamp) || !s.ReadUint16(&entryType) || timestamp > math.MaxInt64 {
			return fmt.Errorf("invalid data tile")
		}

		bb := []byte{}
		b := cryptobyte.NewBuilder(bb)
		b.AddUint64(timestamp)
		b.AddUint16(entryType)

		switch entryType {
		case 0: // x509_entry
			if !s.ReadUint24LengthPrefixed((*cryptobyte.String)(&entry)) ||
				!s.ReadUint16LengthPrefixed(&extensions) ||
				!s.ReadUint16LengthPrefixed(&fingerprints) {
				return fmt.Errorf("invalid data tile x509_entry")
			}
			b.AddUint24LengthPrefixed(func(b *cryptobyte.Builder) {
				b.AddBytes(entry)
			})
			b.AddUint16LengthPrefixed(func(b *cryptobyte.Builder) {
				b.AddBytes(extensions)
			})
			b.AddUint16LengthPrefixed(func(b *cryptobyte.Builder) {
				b.AddBytes(fingerprints)
			})

		case 1: // precert_entry
			IssuerKeyHash := [32]byte{}
			var defangedCrt, extensions cryptobyte.String
			if !s.CopyBytes(IssuerKeyHash[:]) ||
				!s.ReadUint24LengthPrefixed(&defangedCrt) ||
				!s.ReadUint16LengthPrefixed(&extensions) ||
				!s.ReadUint24LengthPrefixed((*cryptobyte.String)(&entry)) ||
				!s.ReadUint16LengthPrefixed(&fingerprints) {
				return fmt.Errorf("invalid data tile precert_entry")
			}
			b.AddBytes(IssuerKeyHash[:])
			b.AddUint24LengthPrefixed(func(b *cryptobyte.Builder) {
				b.AddBytes(defangedCrt)
			})
			b.AddUint16LengthPrefixed(func(b *cryptobyte.Builder) {
				b.AddBytes(extensions)
			})
			b.AddUint24LengthPrefixed(func(b *cryptobyte.Builder) {
				b.AddBytes(entry)
			})
			b.AddUint16LengthPrefixed(func(b *cryptobyte.Builder) {
				b.AddBytes(fingerprints)
			})
		default:
			return fmt.Errorf("invalid data tile: unknown type %d", entryType)
		}
		entries = append(entries, b.BytesOrPanic())
	}

	t.Entries = entries
	return nil
}

// ExtractTimestampFromBundle extracts the timestamp from the Nth entry in the provided serialised entry bundle.
//
// This implementation attempts to avoid any unecessary allocation or parsing other than whatever is
// necessary to skip over uninteresting bytes to find the requested ExtractTimestampFromBundle.
func ExtractTimestampFromBundle(ebRaw []byte, N uint64) (uint64, error) {
	s := cryptobyte.String(ebRaw)

	i := uint64(0)
	for len(s) > 0 {
		var timestamp uint64
		var entryType uint16
		if !s.ReadUint64(&timestamp) || !s.ReadUint16(&entryType) || timestamp > math.MaxInt64 {
			return 0, fmt.Errorf("invalid data tile")
		}
		if i == N {
			return timestamp, nil
		}

		var l32 uint32
		var l16 uint16
		switch entryType {
		case 0: // x509_entry
			if
			// entry
			!s.ReadUint24(&l32) || !s.Skip(int(l32)) ||
				// extensions
				!s.ReadUint16(&l16) || !s.Skip(int(l16)) ||
				// fingerprints
				!s.ReadUint16(&l16) || !s.Skip(int(l16)) {
				return 0, fmt.Errorf("invalid data tile x509_entry")
			}

		case 1: // precert_entry
			if
			// issuer key hash
			!s.Skip(32) ||
				// defangedCrt
				!s.ReadUint24(&l32) || !s.Skip(int(l32)) ||
				// extensions
				!s.ReadUint16(&l16) || !s.Skip(int(l16)) ||
				// entry
				!s.ReadUint24(&l32) || !s.Skip(int(l32)) ||
				// fingerprints
				!s.ReadUint16(&l16) || !s.Skip(int(l16)) {
				return 0, fmt.Errorf("invalid data tile precert_entry")
			}
		default:
			return 0, fmt.Errorf("invalid data tile: unknown type %d", entryType)
		}
		i++
	}

	return 0, fmt.Errorf("requested entry index %d, but found only %d entries", N, i)
}

// parseCTExtensions parses CTExtensions into an index.
// Code is inspired by https://github.com/FiloSottile/sunlight/blob/main/tile.go.
func ParseCTExtensions(ext string) (uint64, error) {
	extensionBytes, err := base64.StdEncoding.DecodeString(ext)
	if err != nil {
		return 0, fmt.Errorf("can't decode extensions: %v", err)
	}
	extensions := cryptobyte.String(extensionBytes)
	var extensionType uint8
	var extensionData cryptobyte.String
	var leafIdx uint64
	if !extensions.ReadUint8(&extensionType) {
		return 0, fmt.Errorf("can't read extension type")
	}
	if extensionType != 0 {
		return 0, fmt.Errorf("wrong extension type %d, want 0", extensionType)
	}
	if !extensions.ReadUint16LengthPrefixed(&extensionData) {
		return 0, fmt.Errorf("can't read extension data")
	}
	if !readUint40(&extensionData, &leafIdx) {
		return 0, fmt.Errorf("can't read leaf index from extension")
	}
	if !extensionData.Empty() ||
		!extensions.Empty() {
		return 0, fmt.Errorf("invalid SCT extension data: %v", ext)
	}
	return leafIdx, nil
}

// readUint40 decodes a big-endian, 40-bit value into out and advances over it.
// It reports whether the read was successful.
// Code is copied from https://github.com/FiloSottile/sunlight/blob/main/extensions.go.
func readUint40(s *cryptobyte.String, out *uint64) bool {
	var v []byte
	if !s.ReadBytes(&v, 5) {
		return false
	}
	*out = uint64(v[0])<<32 | uint64(v[1])<<24 | uint64(v[2])<<16 | uint64(v[3])<<8 | uint64(v[4])
	return true
}

// Entry represents a CT log entry.
type Entry struct {
	Timestamp uint64
	IsPrecert bool
	// Certificate holds different things depending on whether the entry represents a Certificate or a Precertificate submission:
	//   - IsPrecert == false: the bytes here are the x509 certificate submitted for logging.
	//   - IsPrecert == true: the bytes here are the TBS certificate extracted from the submitted precert.
	Certificate []byte
	// Precertificate holds the precertificate to be logged, only used when IsPrecert is true.
	Precertificate    []byte
	IssuerKeyHash     []byte
	RawFingerprints   string
	FingerprintsChain [][32]byte
	RawExtensions     string
	LeafIndex         uint64
}

// UnmarshalText implements encoding/TextUnmarshaler and reads EntryBundles
// which are encoded using the Static CT API spec.
func UnmarshalTimestamp(raw []byte) (uint64, error) {
	s := cryptobyte.String(raw)
	var t uint64

	if !s.ReadUint64(&t) {
		return 0, fmt.Errorf("invalid data tile: timestamp can't be extracted")
	}
	if t > math.MaxInt64 {
		return 0, fmt.Errorf("invalid data tile: timestamp %d > math.MaxInt64", t)
	}
	return t, nil
}

// UnmarshalText implements encoding/TextUnmarshaler and reads EntryBundles
// which are encoded using the Static CT API spec.
func (t *Entry) UnmarshalText(raw []byte) error {
	s := cryptobyte.String(raw)

	entry := []byte{}
	var entryType uint16
	var extensions, fingerprints cryptobyte.String
	if !s.ReadUint64(&t.Timestamp) || !s.ReadUint16(&entryType) || t.Timestamp > math.MaxInt64 {
		return fmt.Errorf("invalid data tile")
	}

	bb := []byte{}
	b := cryptobyte.NewBuilder(bb)
	b.AddUint64(t.Timestamp)
	b.AddUint16(entryType)

	switch entryType {
	case 0: // x509_entry
		t.IsPrecert = false
		if !s.ReadUint24LengthPrefixed((*cryptobyte.String)(&entry)) ||
			!s.ReadUint16LengthPrefixed(&extensions) ||
			!s.ReadUint16LengthPrefixed(&fingerprints) {
			return fmt.Errorf("invalid data tile x509_entry")
		}
		b.AddUint24LengthPrefixed(func(b *cryptobyte.Builder) {
			b.AddBytes(entry)
			t.Certificate = bytes.Clone(entry)
		})
		b.AddUint16LengthPrefixed(func(b *cryptobyte.Builder) {
			b.AddBytes(extensions)
			t.RawExtensions = string(extensions)
		})
		b.AddUint16LengthPrefixed(func(b *cryptobyte.Builder) {
			b.AddBytes(fingerprints)
			t.RawFingerprints = string(fingerprints)
		})

	case 1: // precert_entry
		t.IsPrecert = true
		IssuerKeyHash := [32]byte{}
		var defangedCrt, extensions cryptobyte.String
		if !s.CopyBytes(IssuerKeyHash[:]) ||
			!s.ReadUint24LengthPrefixed(&defangedCrt) ||
			!s.ReadUint16LengthPrefixed(&extensions) ||
			!s.ReadUint24LengthPrefixed((*cryptobyte.String)(&entry)) ||
			!s.ReadUint16LengthPrefixed(&fingerprints) {
			return fmt.Errorf("invalid data tile precert_entry")
		}
		b.AddBytes(IssuerKeyHash[:])
		t.IssuerKeyHash = bytes.Clone(IssuerKeyHash[:])
		b.AddUint24LengthPrefixed(func(b *cryptobyte.Builder) {
			b.AddBytes(defangedCrt)
			t.Certificate = bytes.Clone(defangedCrt)
		})
		b.AddUint16LengthPrefixed(func(b *cryptobyte.Builder) {
			b.AddBytes(extensions)
			t.RawExtensions = string(extensions)
		})
		b.AddUint24LengthPrefixed(func(b *cryptobyte.Builder) {
			b.AddBytes(entry)
			t.Precertificate = bytes.Clone(entry)
		})
		b.AddUint16LengthPrefixed(func(b *cryptobyte.Builder) {
			b.AddBytes(fingerprints)
			t.RawFingerprints = string(fingerprints)
		})
	default:
		return fmt.Errorf("invalid data tile: unknown type %d", entryType)
	}

	var err error
	t.LeafIndex, err = ParseCTExtensions(base64.StdEncoding.EncodeToString([]byte(t.RawExtensions)))
	if err != nil {
		return fmt.Errorf("can't parse extensions: %v", err)
	}

	rfp := cryptobyte.String(t.RawFingerprints)
	for i := 0; len(rfp) > 0; i++ {
		fp := [32]byte{}
		if !rfp.CopyBytes(fp[:]) {
			return fmt.Errorf("can't extract fingerprint number %d", i)
		}
		t.FingerprintsChain = append(t.FingerprintsChain, fp)
	}

	if len(s) > 0 {
		return fmt.Errorf("trailing %d bytes after entry", len(s))
	}

	return nil
}
