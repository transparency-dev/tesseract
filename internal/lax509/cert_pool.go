// Copyright 2011 The Go Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package lax509

import (
	"crypto/sha256"
	"crypto/x509"
	"encoding/pem"
	"sync"
)

type sum224 [sha256.Size224]byte

// CertPool is a set of certificates.
type CertPool struct {
	byName map[string][]int // cert.RawSubject => index into lazyCerts

	// lazyCerts contains funcs that return a certificate,
	// lazily parsing/decompressing it as needed.
	lazyCerts []lazyCert

	// haveSum maps from sum224(cert.Raw) to true. It's used only
	// for AddCert duplicate detection, to avoid CertPool.contains
	// calls in the AddCert path (because the contains method can
	// call getCert and otherwise negate savings from lazy getCert
	// funcs).
	haveSum map[sum224]bool

	// systemPool indicates whether this is a special pool derived from the
	// system roots. If it includes additional roots, it requires doing two
	// verifications, one using the roots provided by the caller, and one using
	// the system platform verifier.
	systemPool bool
}

// lazyCert is minimal metadata about a Cert and a func to retrieve it
// in its normal expanded *Certificate form.
type lazyCert struct {
	// rawSubject is the Certificate.RawSubject value.
	// It's the same as the CertPool.byName key, but in []byte
	// form to make CertPool.Subjects (as used by crypto/tls) do
	// fewer allocations.
	rawSubject []byte

	// constraint is a function to run against a chain when it is a candidate to
	// be added to the chain. This allows adding arbitrary constraints that are
	// not specified in the certificate itself.
	constraint func([]*x509.Certificate) error

	// getCert returns the certificate.
	//
	// It is not meant to do network operations or anything else
	// where a failure is likely; the func is meant to lazily
	// parse/decompress data that is already known to be good. The
	// error in the signature primarily is meant for use in the
	// case where a cert file existed on local disk when the program
	// started up is deleted later before it's read.
	getCert func() (*x509.Certificate, error)
}

// NewCertPool returns a new, empty CertPool.
func NewCertPool() *CertPool {
	return &CertPool{
		byName:  make(map[string][]int),
		haveSum: make(map[sum224]bool),
	}
}

// len returns the number of certs in the set.
// A nil set is a valid empty set.
func (s *CertPool) len() int {
	if s == nil {
		return 0
	}
	return len(s.lazyCerts)
}

// Clone returns a copy of s.
func (s *CertPool) Clone() *CertPool {
	p := &CertPool{
		byName:     make(map[string][]int, len(s.byName)),
		lazyCerts:  make([]lazyCert, len(s.lazyCerts)),
		haveSum:    make(map[sum224]bool, len(s.haveSum)),
		systemPool: s.systemPool,
	}
	for k, v := range s.byName {
		indexes := make([]int, len(v))
		copy(indexes, v)
		p.byName[k] = indexes
	}
	for k := range s.haveSum {
		p.haveSum[k] = true
	}
	copy(p.lazyCerts, s.lazyCerts)
	return p
}

// AddCert adds a certificate to a pool.
func (s *CertPool) AddCert(cert *x509.Certificate) {
	if cert == nil {
		panic("adding nil Certificate to CertPool")
	}
	s.addCertFunc(sha256.Sum224(cert.Raw), string(cert.RawSubject), func() (*x509.Certificate, error) {
		return cert, nil
	}, nil)
}

// addCertFunc adds metadata about a certificate to a pool, along with
// a func to fetch that certificate later when needed.
//
// The rawSubject is Certificate.RawSubject and must be non-empty.
// The getCert func may be called 0 or more times.
func (s *CertPool) addCertFunc(rawSum224 sum224, rawSubject string, getCert func() (*x509.Certificate, error), constraint func([]*x509.Certificate) error) {
	if getCert == nil {
		panic("getCert can't be nil")
	}

	// Check that the certificate isn't being added twice.
	if s.haveSum[rawSum224] {
		return
	}

	s.haveSum[rawSum224] = true
	s.lazyCerts = append(s.lazyCerts, lazyCert{
		rawSubject: []byte(rawSubject),
		getCert:    getCert,
		constraint: constraint,
	})
	s.byName[rawSubject] = append(s.byName[rawSubject], len(s.lazyCerts)-1)
}

// AppendCertsFromPEM attempts to parse a series of PEM encoded certificates.
// It appends any certificates found to s and reports whether any certificates
// were successfully parsed.
//
// On many Linux systems, /etc/ssl/cert.pem will contain the system wide set
// of root CAs in a format suitable for this function.
func (s *CertPool) AppendCertsFromPEM(pemCerts []byte) (ok bool) {
	for len(pemCerts) > 0 {
		var block *pem.Block
		block, pemCerts = pem.Decode(pemCerts)
		if block == nil {
			break
		}
		if block.Type != "CERTIFICATE" || len(block.Headers) != 0 {
			continue
		}

		certBytes := block.Bytes
		cert, err := x509.ParseCertificate(certBytes)
		if err != nil {
			continue
		}
		var lazyCert struct {
			sync.Once
			v *x509.Certificate
		}
		s.addCertFunc(sha256.Sum224(cert.Raw), string(cert.RawSubject), func() (*x509.Certificate, error) {
			lazyCert.Do(func() {
				// This can't fail, as the same bytes already parsed above.
				lazyCert.v, _ = x509.ParseCertificate(certBytes)
				certBytes = nil
			})
			return lazyCert.v, nil
		}, nil)
		ok = true
	}

	return ok
}

// Subjects returns a list of the DER-encoded subjects of
// all of the certificates in the pool.
// Deprecated in the original package: https://pkg.go.dev/crypto/x509#CertPool.Subjects.
// Undeprecated here since it's still used by some tests.
func (s *CertPool) Subjects() [][]byte {
	res := make([][]byte, s.len())
	for i, lc := range s.lazyCerts {
		res[i] = lc.rawSubject
	}
	return res
}

// Equal reports whether s and other are equal.
func (s *CertPool) Equal(other *CertPool) bool {
	if s == nil || other == nil {
		return s == other
	}
	if s.systemPool != other.systemPool || len(s.haveSum) != len(other.haveSum) {
		return false
	}
	for h := range s.haveSum {
		if !other.haveSum[h] {
			return false
		}
	}
	return true
}

// AddCertWithConstraint adds a certificate to the pool with the additional
// constraint. When Certificate.Verify builds a chain which is rooted by cert,
// it will additionally pass the whole chain to constraint to determine its
// validity. If constraint returns a non-nil error, the chain will be discarded.
// constraint may be called concurrently from multiple goroutines.
func (s *CertPool) AddCertWithConstraint(cert *x509.Certificate, constraint func([]*x509.Certificate) error) {
	if cert == nil {
		panic("adding nil Certificate to CertPool")
	}
	s.addCertFunc(sha256.Sum224(cert.Raw), string(cert.RawSubject), func() (*x509.Certificate, error) {
		return cert, nil
	}, constraint)
}
