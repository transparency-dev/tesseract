// Copyright 2024 Google LLC. All Rights Reserved.
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//	http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.
package x509util

import (
	"bytes"
	"crypto/sha256"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/asn1"
	"errors"
	"fmt"
	"math/big"
	"sync"
	"time"

	"github.com/transparency-dev/tessera/ctonly"
	"github.com/transparency-dev/tesseract/internal/types/rfc6962"

	"golang.org/x/crypto/cryptobyte"
	cryptobyte_asn1 "golang.org/x/crypto/cryptobyte/asn1"
)

type tbsCertificate struct {
	Raw                asn1.RawContent
	Version            int `asn1:"optional,explicit,default:0,tag:0"`
	SerialNumber       *big.Int
	SignatureAlgorithm pkix.AlgorithmIdentifier
	Issuer             asn1.RawValue
	Validity           validity
	Subject            asn1.RawValue
	PublicKey          publicKeyInfo
	UniqueId           asn1.BitString   `asn1:"optional,tag:1"`
	SubjectUniqueId    asn1.BitString   `asn1:"optional,tag:2"`
	Extensions         []pkix.Extension `asn1:"omitempty,optional,explicit,tag:3"`
}

type validity struct {
	NotBefore, NotAfter time.Time
}

type publicKeyInfo struct {
	Raw       asn1.RawContent
	Algorithm pkix.AlgorithmIdentifier
	PublicKey asn1.BitString
}

// removeExtension takes a DER-encoded TBSCertificate, removes the extension
// specified by oid (preserving the order of other extensions), and returns the
// result still as a DER-encoded TBSCertificate.  This function will fail if
// there is not exactly 1 extension of the type specified by the oid present.
func removeExtension(tbsData []byte, oid asn1.ObjectIdentifier) ([]byte, error) {
	var tbs tbsCertificate
	rest, err := asn1.Unmarshal(tbsData, &tbs)
	if err != nil {
		return nil, fmt.Errorf("failed to parse TBSCertificate: %v", err)
	} else if rLen := len(rest); rLen > 0 {
		return nil, fmt.Errorf("trailing data (%d bytes) after TBSCertificate", rLen)
	}
	extAt := -1
	for i, ext := range tbs.Extensions {
		if ext.Id.Equal(oid) {
			if extAt != -1 {
				return nil, errors.New("multiple extensions of specified type present")
			}
			extAt = i
		}
	}
	if extAt == -1 {
		return nil, errors.New("no extension of specified type present")
	}
	tbs.Extensions = append(tbs.Extensions[:extAt], tbs.Extensions[extAt+1:]...)
	// Clear out the asn1.RawContent so the re-marshal operation sees the
	// updated structure (rather than just copying the out-of-date DER data).
	tbs.Raw = nil

	data, err := asn1.Marshal(tbs)
	if err != nil {
		return nil, fmt.Errorf("failed to re-marshal TBSCertificate: %v", err)
	}
	return data, nil
}

// RemoveCTPoison takes a DER-encoded TBSCertificate and removes the CT poison
// extension (preserving the order of other extensions), and returns the result
// still as a DER-encoded TBSCertificate.  This function will fail if there is
// not exactly 1 CT poison extension present.
func RemoveCTPoison(tbsData []byte) ([]byte, error) {
	return removeExtension(tbsData, rfc6962.OIDExtensionCTPoison)
}

// entryPool holds ctonly.Entry instances which can be re-used across requests.
var entryPool = sync.Pool{
	New: func() any {
		return &ctonly.Entry{}
	},
}

// ReturnEntry returns an Entry back to the pool for reuse.
func ReturnEntry(e *ctonly.Entry) {
	if e != nil {
		entryPool.Put(e)
	}
}

// newEntryFromPool returns an Entry instance from entryPool.
// The fields of the instance will be reset to default values, with the exception of FingerprintsChain
// which is guaranteed to have at least enough capacity to have fpLen entries appended to it.
//
// Instances returned by this function should be returned to the pool via ReturnEntry func above.
func newEntryFromPool(fpLen int) *ctonly.Entry {
	r := entryPool.Get().(*ctonly.Entry)
	r.IssuerKeyHash = nil
	r.Precertificate = nil
	r.Certificate = nil
	if cap(r.FingerprintsChain) < fpLen {
		r.FingerprintsChain = make([][32]byte, 0, fpLen)
	} else {
		r.FingerprintsChain = r.FingerprintsChain[:0]
	}
	return r
}

// EntryFromChain generates an Entry from a chain and timestamp.
// copied from certificate-transparency-go/serialization.go
//
// Instances returned by this func should be returned to the pool via the ReturnEntry func above.
func EntryFromChain(chain []*x509.Certificate, isPrecert bool, timestamp uint64) (*ctonly.Entry, error) {
	leaf := newEntryFromPool(len(chain) - 1)
	leaf.IsPrecert = isPrecert
	leaf.Timestamp = timestamp

	if len(chain) > 1 {
		for _, c := range chain[1:] {
			leaf.FingerprintsChain = append(leaf.FingerprintsChain, sha256.Sum256(c.Raw))
		}
	}

	if !isPrecert {
		leaf.Certificate = chain[0].Raw
		return leaf, nil
	}

	// Pre-certs are more complicated. First, parse the leaf pre-cert and its
	// putative issuer.
	if len(chain) < 2 {
		ReturnEntry(leaf)
		return nil, fmt.Errorf("no issuer cert available for precert leaf building")
	}
	issuer := chain[1]
	cert := chain[0]

	// Precertificates issued by a Precertificate Signing Certificate are not
	// supported.
	//
	// SPEC: c2sp.org/static-ct-api@v1.1.0, Submission APIs.
	// "the log MAY reject precertificates issued using a Precertificate
	// Signing Certificate."
	if IsPreIssuer(issuer) {
		ReturnEntry(leaf)
		return nil, errors.New("precertificates issued by a Precertificate Signing Certificate are not supported")
	}

	// Next, post-process the DER-encoded TBSCertificate, to remove the CT poison
	// extension.
	defangedTBS, err := RemoveCTPoison(cert.RawTBSCertificate)
	if err != nil {
		ReturnEntry(leaf)
		return nil, fmt.Errorf("failed to remove poison extension: %v", err)
	}

	leaf.Precertificate = cert.Raw
	// TODO(phboneff): do we need this?
	// leaf.PrecertSigningCert = issuer.Raw
	leaf.Certificate = defangedTBS

	issuerKeyHash := sha256.Sum256(issuer.RawSubjectPublicKeyInfo)
	leaf.IssuerKeyHash = issuerKeyHash[:]
	return leaf, nil
}

// IsPreIssuer indicates if a certificate is a Precertificate Signing Certificate.
//
// From RFC6962 s3.1, these certs should contain:
// (CA:true, Extended Key Usage: Certificate Transparency, OID 1.3.6.1.4.1.11129.2.4.4)
func IsPreIssuer(cert *x509.Certificate) bool {
	if !cert.IsCA {
		return false
	}
	// Look for the extension in the Extensions field and not in ExtKeyUsage
	// since crypto/x509 does not recognize this extension as such.
	// We cannot reliably check in UnknownExtKeyUsage either, since it might
	// one day disappear from UnknownExtKeyUsage and make it to ExtKeyUsage.
	// Given that ExtKeyUsage does not contain OIDs, we would not be able to
	// detect such a move.
	for _, ext := range cert.Extensions {
		if rfc6962.OIDExtKeyUsage.Equal(ext.Id) {
			der := cryptobyte.String(ext.Value)
			if !der.ReadASN1(&der, cryptobyte_asn1.SEQUENCE) {
				continue
			}
			for !der.Empty() {
				var eku asn1.ObjectIdentifier
				if !der.ReadASN1ObjectIdentifier(&eku) {
					continue
				}
				if rfc6962.OIDExtKeyUsageCertificateTransparency.Equal(eku) {
					return true
				}
			}
		}
	}
	return false
}

// IsPrecertificate tests if a certificate is a pre-certificate as defined in CT.
// An error is returned if the CT extension is present but is not ASN.1 NULL as defined
// by the spec.
func IsPrecertificate(cert *x509.Certificate) (bool, error) {
	if cert == nil {
		return false, errors.New("nil certificate")
	}

	for _, ext := range cert.Extensions {
		if rfc6962.OIDExtensionCTPoison.Equal(ext.Id) {
			if !ext.Critical || !bytes.Equal(asn1.NullBytes, ext.Value) {
				return false, fmt.Errorf("CT poison ext is not critical or invalid: %v", ext)
			}

			return true, nil
		}
	}

	return false, nil
}
