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

package main

import (
	"crypto/rand"
	"crypto/x509"
	"crypto/x509/pkix"
	"math/big"
	"time"

	"github.com/transparency-dev/tesseract/internal/types/rfc6962"
	"k8s.io/klog/v2"
)

const (
	commonName         = "transparency.dev"
	organization       = "Transparency.dev"
	organizationalUnit = "TrustFabric"
	locality           = "London"
	state              = "London"
	country            = "GB"
)

type chainGenerator struct {
	intermediateCert  *x509.Certificate
	intermediateKey   any
	leafCertPublicKey any
}

// newChainGenerator creates the chainGenerator.
func newChainGenerator(intermediateCert *x509.Certificate, intermediateKey, leafCertPublicKey any) *chainGenerator {
	return &chainGenerator{
		intermediateCert:  intermediateCert,
		intermediateKey:   intermediateKey,
		leafCertPublicKey: leafCertPublicKey,
	}
}

// certificate generates a deterministic TLS certificate by using integer as the serial number.
// Note that deterministic signature algorithms are RSA and Ed25519.
func (g *chainGenerator) certificate(serialNumber int64) []byte {
	notBefore := time.Date(2023, 1, 1, 0, 0, 0, 0, time.UTC)
	notAfter := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)

	template := x509.Certificate{
		SerialNumber: big.NewInt(serialNumber),
		Subject: pkix.Name{
			CommonName:         commonName,
			Organization:       []string{organization},
			OrganizationalUnit: []string{organizationalUnit},
			Locality:           []string{locality},
			Province:           []string{state},
			Country:            []string{country},
		},
		NotBefore:             notBefore,
		NotAfter:              notAfter,
		KeyUsage:              x509.KeyUsageKeyEncipherment | x509.KeyUsageDigitalSignature,
		ExtKeyUsage:           []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		BasicConstraintsValid: true,
		DNSNames:              []string{commonName},
	}

	derBytes, err := x509.CreateCertificate(rand.Reader, &template, g.intermediateCert, g.leafCertPublicKey, g.intermediateKey)
	if err != nil {
		klog.Error(err)
		return nil
	}

	return derBytes
}

// addChainRequestBody generates the add-chain request body for submission.
func (g *chainGenerator) addChainRequestBody(serialNumber int64) rfc6962.AddChainRequest {
	var req rfc6962.AddChainRequest

	req.Chain = append(req.Chain, g.certificate(serialNumber))
	req.Chain = append(req.Chain, g.intermediateCert.Raw)

	return req
}
