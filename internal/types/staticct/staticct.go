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
	"errors"
	"fmt"
	"math"

	"github.com/transparency-dev/tessera/api/layout"
	"github.com/transparency-dev/tesseract/internal/types/rfc6962"
	"github.com/transparency-dev/tesseract/internal/types/tls"
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

// NewCertificateTimestamp creates an rfc6962.CertificateTimestamp with the provided RFC6962 CTExtensions byte slice.
func NewCertificateTimestamp(ext []byte, timestamp uint64, isPrecert bool, cert []byte, ikh [32]byte) *rfc6962.CertificateTimestamp {
	ct := &rfc6962.CertificateTimestamp{
		SCTVersion:    rfc6962.V1,
		SignatureType: rfc6962.CertificateTimestampSignatureType,
		Timestamp:     timestamp,
		Extensions:    ext,
	}
	if isPrecert {
		ct.EntryType = rfc6962.PrecertLogEntryType
		ct.PrecertEntry = &rfc6962.PreCert{
			IssuerKeyHash:  ikh,
			TBSCertificate: cert,
		}
	} else {
		ct.EntryType = rfc6962.X509LogEntryType
		ct.X509Entry = &rfc6962.ASN1Cert{Data: cert}
	}
	return ct
}

// ExtractCertificateTimestampFromLeaf parses a TLS-encoded MerkleTreeLeaf byte slice
// and returns the corresponding CertificateTimestamp.
func ExtractCertificateTimestampFromLeaf(leafBytes []byte) (*rfc6962.CertificateTimestamp, error) {
	var leaf rfc6962.MerkleTreeLeaf
	if rest, err := tls.Unmarshal(leafBytes, &leaf); err != nil {
		return nil, fmt.Errorf("failed to unmarshal MerkleTreeLeaf: %w", err)
	} else if len(rest) > 0 {
		return nil, fmt.Errorf("extra data (%d bytes) after MerkleTreeLeaf", len(rest))
	}
	if leaf.TimestampedEntry == nil {
		return nil, errors.New("nil TimestampedEntry in MerkleTreeLeaf")
	}
	return &rfc6962.CertificateTimestamp{
		SCTVersion:    rfc6962.V1,
		SignatureType: rfc6962.CertificateTimestampSignatureType,
		Timestamp:     leaf.TimestampedEntry.Timestamp,
		EntryType:     leaf.TimestampedEntry.EntryType,
		X509Entry:     leaf.TimestampedEntry.X509Entry,
		PrecertEntry:  leaf.TimestampedEntry.PrecertEntry,
		Extensions:    leaf.TimestampedEntry.Extensions,
	}, nil
}

// ExtractSCTInputFromBundle extracts the SCT input fields of the Nth entry from the provided serialised entry bundle.
//
// This implementation avoids unnecessary parsing and allocation by skipping over
// uninteresting bytes and ignoring extensions and fingerprints.
func ExtractSCTInputFromBundle(ebRaw []byte, N uint64) (*rfc6962.CertificateTimestamp, error) {
	s := cryptobyte.String(ebRaw)

	i := uint64(0)
	for len(s) > 0 {
		var timestamp uint64
		var entryType uint16
		if !s.ReadUint64(&timestamp) || !s.ReadUint16(&entryType) || timestamp > math.MaxInt64 {
			return nil, fmt.Errorf("invalid data tile when reading entry %d", i)
		}

		var l32 uint32
		var l16 uint16
		switch entryType {
		case 0: // x509_entry
			if i == N {
				var entry []byte
				var extensions, fingerprints cryptobyte.String
				if
				// entry
				!s.ReadUint24LengthPrefixed((*cryptobyte.String)(&entry)) ||
					// extensions
					!s.ReadUint16LengthPrefixed(&extensions) ||
					// fingerprints
					!s.ReadUint16LengthPrefixed(&fingerprints) {
					return nil, fmt.Errorf("invalid data tile x509_entry at index %d", i)
				}
				return NewCertificateTimestamp([]byte(extensions), timestamp, false, bytes.Clone(entry), [32]byte{}), nil
			}
			if
			// entry
			!s.ReadUint24(&l32) || !s.Skip(int(l32)) ||
				// extensions
				!s.ReadUint16(&l16) || !s.Skip(int(l16)) ||
				// fingerprints
				!s.ReadUint16(&l16) || !s.Skip(int(l16)) {
				return nil, fmt.Errorf("invalid data tile x509_entry when reading index %d", i)
			}

		case 1: // precert_entry
			if i == N {
				issuerKeyHash := [32]byte{}
				var defangedCrt, extensions, fingerprints cryptobyte.String
				var entry []byte
				if
				// issuer key hash
				!s.CopyBytes(issuerKeyHash[:]) ||
					// defangedCrt
					!s.ReadUint24LengthPrefixed(&defangedCrt) ||
					// extensions
					!s.ReadUint16LengthPrefixed(&extensions) ||
					// entry
					!s.ReadUint24LengthPrefixed((*cryptobyte.String)(&entry)) ||
					// fingerprints
					!s.ReadUint16LengthPrefixed(&fingerprints) {
					return nil, fmt.Errorf("invalid data tile precert_entry at index %d", i)
				}
				return NewCertificateTimestamp([]byte(extensions), timestamp, true, bytes.Clone(defangedCrt), issuerKeyHash), nil
			}
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
				return nil, fmt.Errorf("invalid data tile precert_entry when reading index %d", i)
			}
		default:
			return nil, fmt.Errorf("invalid data tile: unknown type %d", entryType)
		}
		i++
	}

	return nil, fmt.Errorf("requested entry index %d, but found only %d entries", N, i)
}

// ParseCTExtensionsBytes parses binary CTExtensions into an index.
func ParseCTExtensionsBytes(ext []byte) (uint64, error) {
	extensions := cryptobyte.String(ext)
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
		return 0, fmt.Errorf("invalid SCT extension data: %x", ext)
	}
	return leafIdx, nil
}

// ParseCTExtensions parses base64-encoded CTExtensions into an index.
// Code is inspired by https://github.com/FiloSottile/sunlight/blob/main/tile.go.
func ParseCTExtensions(ext string) (uint64, error) {
	extensionBytes, err := base64.StdEncoding.DecodeString(ext)
	if err != nil {
		return 0, fmt.Errorf("can't decode extensions: %v", err)
	}
	return ParseCTExtensionsBytes(extensionBytes)
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
	t.LeafIndex, err = ParseCTExtensionsBytes([]byte(t.RawExtensions))
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
