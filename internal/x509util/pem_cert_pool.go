// Copyright 2016 Google LLC. All Rights Reserved.
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

package x509util

import (
	"crypto/sha256"
	"crypto/x509"
	"encoding/pem"
	"errors"
	"fmt"
	"os"
	"sync"

	"github.com/transparency-dev/tesseract/internal/lax509"
	"k8s.io/klog/v2"
)

// String for certificate blocks in BEGIN / END PEM headers
const pemCertificateBlockType string = "CERTIFICATE"

// PEMCertPool is a wrapper / extension to x509.CertPool. It allows us to access the
// raw certs, which we need to serve get-roots request and has stricter handling on loading
// certs into the pool. CertPool ignores errors if at least one cert loads correctly but
// PEMCertPool requires all certs to load.
type PEMCertPool struct {
	mu sync.RWMutex
	// maps from sha-256 to certificate, used for dup detection
	fingerprintToCertMap map[[sha256.Size]byte]x509.Certificate
	rawCerts             []*x509.Certificate
	certPool             *lax509.CertPool
}

// NewPEMCertPool creates a new, empty, instance of PEMCertPool.
func NewPEMCertPool() *PEMCertPool {
	return &PEMCertPool{
		mu:                   sync.RWMutex{},
		fingerprintToCertMap: make(map[[sha256.Size]byte]x509.Certificate),
		certPool:             lax509.NewCertPool()}
}

// AddCerts adds certificates to a pool. certs must not be nil.
//
// It uses fingerprint to weed out duplicates and identify new certificates.
// If any new certificates is detected, the underlying certPool is cloned,
// new certs are added, and then pools are swapped.
func (p *PEMCertPool) AddCerts(certs []*x509.Certificate) {
	p.mu.Lock()
	defer p.mu.Unlock()

	newCerts := []*x509.Certificate{}
	for _, cert := range certs {
		fingerprint := sha256.Sum256(cert.Raw)
		_, ok := p.fingerprintToCertMap[fingerprint]

		if !ok {
			newCerts = append(newCerts, cert)
		}
	}

	if len(newCerts) > 0 {
		newPool := p.certPool.Clone()
		for _, cert := range newCerts {
			fingerprint := sha256.Sum256(cert.Raw)
			p.fingerprintToCertMap[fingerprint] = *cert
			p.rawCerts = append(p.rawCerts, cert)
			newPool.AddCert(cert)
		}
		p.certPool = newPool
	}
}

// Included indicates whether the given cert is included in the pool.
func (p *PEMCertPool) Included(cert *x509.Certificate) bool {
	p.mu.RLock()
	defer p.mu.RUnlock()
	fingerprint := sha256.Sum256(cert.Raw)
	_, ok := p.fingerprintToCertMap[fingerprint]
	return ok
}

// AppendCertsFromPEMs adds certs to the pool from byte slices assumed to contain PEM encoded data.
// Skips over non certificate blocks in the data. Returns true if all certificates in the
// data were parsed and added to the pool successfully and at least one certificate was found.
func (p *PEMCertPool) AppendCertsFromPEMs(pems ...[]byte) (ok bool) {
	certs := []*x509.Certificate{}
	for _, pemCerts := range pems {
		for len(pemCerts) > 0 {
			var block *pem.Block
			block, pemCerts = pem.Decode(pemCerts)
			if block == nil {
				break
			}
			if block.Type != pemCertificateBlockType || len(block.Headers) != 0 {
				continue
			}

			cert, err := x509.ParseCertificate(block.Bytes)
			if err != nil {
				klog.Warningf("error parsing PEM certificate: %v", err)
				return false
			}

			certs = append(certs, cert)
			ok = true
		}
	}
	p.AddCerts(certs)

	return
}

// AppendCertsFromPEMFile adds certs from a file that contains concatenated PEM data.
func (p *PEMCertPool) AppendCertsFromPEMFile(pemFile string) error {
	pemData, err := os.ReadFile(pemFile)
	if err != nil {
		return fmt.Errorf("failed to load PEM certs file: %v", err)
	}

	if !p.AppendCertsFromPEMs(pemData) {
		return errors.New("failed to parse PEM certs file")
	}
	return nil
}

// Subjects returns a list of the DER-encoded subjects of all of the certificates in the pool.
func (p *PEMCertPool) Subjects() (res [][]byte) {
	p.mu.RLock()
	defer p.mu.RUnlock()
	return p.certPool.Subjects()
}

// CertPool returns the underlying CertPool.
func (p *PEMCertPool) CertPool() *lax509.CertPool {
	p.mu.RLock()
	defer p.mu.RUnlock()
	return p.certPool
}

// RawCertificates returns a list of the raw bytes of certificates that are in this pool
func (p *PEMCertPool) RawCertificates() []*x509.Certificate {
	p.mu.RLock()
	defer p.mu.RUnlock()
	return p.rawCerts
}
