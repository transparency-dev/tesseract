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

// gen is a tool to generate test ECDSA keys and test certificates for hammer.
package main

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"flag"
	"fmt"
	"math/big"
	"os"
	"path"
	"time"

	"k8s.io/klog/v2"
)

const (
	commonName   = "test.transparency.dev"
	organization = "TrustFabric Transparency.dev Test"
	country      = "GB"
)

var (
	outputPath = flag.String("output_path", "./internal/hammer/testdata/", "Output path for private keys and certificates")
)

func main() {
	klog.InitFlags(nil)
	flag.Parse()
	// Generate a new ECDSA root CA private key.
	rootPrivKey, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		klog.Fatalf("Failed to generate root CA private key: %v", err)
	}
	if err := saveECPrivateKeyPEM(rootPrivKey, path.Join(*outputPath, "test_root_ca_private_key.pem")); err != nil {
		klog.Fatalf("Failed to save root CA private key: %v", err)
	}

	// Generate a new root CA certificate.
	rootCert, err := rootCACert(rootPrivKey)
	if err != nil {
		klog.Fatalf("Failed to generate root CA certificate: %v", err)
	}
	if err := saveCertificatePEM(rootCert, path.Join(*outputPath, "test_root_ca_cert.pem")); err != nil {
		klog.Fatalf("Failed to save root CA certificate: %v", err)
	}

	// Generate a new EC intermediate CA private key.
	intermediatePrivKey, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		klog.Fatalf("Failed to generate intermediate CA private key: %v", err)
	}
	if err := saveECPrivateKeyPEM(intermediatePrivKey, path.Join(*outputPath, "test_intermediate_ca_private_key.pem")); err != nil {
		klog.Fatalf("Failed to save intermediate CA private key: %v", err)
	}

	// Generate a new Intermediate CA certificate.
	intermediateCert, err := intermediateCACert(rootCert, rootPrivKey, intermediatePrivKey)
	if err != nil {
		klog.Fatalf("Failed to generate intermediate CA certificate: %v", err)
	}
	if err := saveCertificatePEM(intermediateCert, path.Join(*outputPath, "test_intermediate_ca_cert.pem")); err != nil {
		klog.Fatalf("Failed to save intermediate CA certificate: %v", err)
	}

	// Generate a new EC leaf certificate signing private key.
	leafCertPrivateKey, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		klog.Fatalf("Failed to generate leaf certificate signing private key: %v", err)
	}
	if err := saveECPrivateKeyPEM(leafCertPrivateKey, path.Join(*outputPath, "test_leaf_cert_signing_private_key.pem")); err != nil {
		klog.Fatalf("Failed to save leaf certificate signing private key: %v", err)
	}
}

func rootCACert(privKey *ecdsa.PrivateKey) (*x509.Certificate, error) {
	template := x509.Certificate{
		SerialNumber: big.NewInt(1),
		Subject: pkix.Name{
			Organization: []string{fmt.Sprintf("%s Root Test CA", organization)},
			Country:      []string{country},
			CommonName:   fmt.Sprintf("%s Root Test CA", organization),
		},
		NotBefore:             time.Now(),
		NotAfter:              time.Now().AddDate(10, 0, 0),
		KeyUsage:              x509.KeyUsageCertSign | x509.KeyUsageCRLSign,
		BasicConstraintsValid: true,
		IsCA:                  true,
	}

	// Create the self-signed certificate.
	derBytes, err := x509.CreateCertificate(rand.Reader, &template, &template, privKey.Public(), privKey)
	if err != nil {
		return nil, fmt.Errorf("failed to create root certificate: %v", err)
	}

	// Parse the DER-encoded certificate.
	cert, err := x509.ParseCertificate(derBytes)
	if err != nil {
		return nil, fmt.Errorf("failed to parse root certificate: %v", err)
	}

	return cert, nil
}

func intermediateCACert(rootCACert *x509.Certificate, rootPrivKey, privKey *ecdsa.PrivateKey) (*x509.Certificate, error) {
	template := x509.Certificate{
		SerialNumber: big.NewInt(2),
		Subject: pkix.Name{
			Organization: []string{fmt.Sprintf("%s Intermediate Test CA", organization)},
			Country:      []string{country},
			CommonName:   fmt.Sprintf("%s Intermediate Test CA", organization),
		},
		NotBefore:             time.Now(),
		NotAfter:              time.Now().AddDate(5, 0, 0),
		KeyUsage:              x509.KeyUsageCertSign | x509.KeyUsageCRLSign,
		BasicConstraintsValid: true,
		IsCA:                  true,
		MaxPathLen:            0,
	}

	derBytes, err := x509.CreateCertificate(rand.Reader, &template, rootCACert, privKey.Public(), rootPrivKey)
	if err != nil {
		return nil, fmt.Errorf("failed to create intermediate certificate: %v", err)
	}

	cert, err := x509.ParseCertificate(derBytes)
	if err != nil {
		return nil, fmt.Errorf("failed to parse intermediate certificate: %v", err)
	}

	return cert, nil
}

func saveECPrivateKeyPEM(key *ecdsa.PrivateKey, filename string) error {
	derBytes, err := x509.MarshalECPrivateKey(key)
	if err != nil {
		return fmt.Errorf("failed to marshal EC key: %v", err)
	}

	// No encryption.
	block := &pem.Block{
		Type:  "EC TEST PRIVATE KEY",
		Bytes: derBytes,
	}

	// Encode the PEM block to memory.
	pemData := pem.EncodeToMemory(block)

	// Write the PEM data to the file with restrictive permissions.
	if err := os.WriteFile(filename, pemData, 0600); err != nil {
		return fmt.Errorf("failed to write PEM file: %w", err)
	}

	return nil
}

func saveCertificatePEM(cert *x509.Certificate, filename string) error {
	pemData := pem.EncodeToMemory(&pem.Block{
		Type:  "CERTIFICATE",
		Bytes: cert.Raw,
	})

	if err := os.WriteFile(filename, pemData, 0644); err != nil {
		return fmt.Errorf("failed to write PEM file: %w", err)
	}
	return nil
}
