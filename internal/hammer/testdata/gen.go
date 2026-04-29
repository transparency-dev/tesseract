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

// gen is a tool to generate test RSA keys and test certificates for hammer.
package main

import (
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"flag"
	"fmt"
	"log/slog"
	"math/big"
	"os"
	"path"
	"time"
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
	flag.Parse()
	// Generate a new RSA root CA private key.
	rootPrivKey, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		slog.Error("Failed to generate root CA private key", slog.Any("error", err))
		os.Exit(1)
	}
	if err := saveRSAPrivateKeyPEM(rootPrivKey, path.Join(*outputPath, "test_root_ca_private_key.pem")); err != nil {
		slog.Error("Failed to save root CA private key", slog.Any("error", err))
		os.Exit(1)
	}

	// Generate a new root CA certificate.
	rootCert, err := rootCACert(rootPrivKey)
	if err != nil {
		slog.Error("Failed to generate root CA certificate", slog.Any("error", err))
		os.Exit(1)
	}
	if err := saveCertificatePEM(rootCert, path.Join(*outputPath, "test_root_ca_cert.pem")); err != nil {
		slog.Error("Failed to save root CA certificate", slog.Any("error", err))
		os.Exit(1)
	}

	// Generate a new RSA intermediate CA private key.
	intermediatePrivKey, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		slog.Error("Failed to generate intermediate CA private key", slog.Any("error", err))
		os.Exit(1)
	}
	if err := saveRSAPrivateKeyPEM(intermediatePrivKey, path.Join(*outputPath, "test_intermediate_ca_private_key.pem")); err != nil {
		slog.Error("Failed to save intermediate CA private key", slog.Any("error", err))
		os.Exit(1)
	}

	// Generate a new Intermediate CA certificate.
	intermediateCert, err := intermediateCACert(rootCert, rootPrivKey, intermediatePrivKey)
	if err != nil {
		slog.Error("Failed to generate intermediate CA certificate", slog.Any("error", err))
		os.Exit(1)
	}
	if err := saveCertificatePEM(intermediateCert, path.Join(*outputPath, "test_intermediate_ca_cert.pem")); err != nil {
		slog.Error("Failed to save intermediate CA certificate", slog.Any("error", err))
		os.Exit(1)
	}

	// Generate a new RSA leaf certificate signing private key.
	leafCertPrivateKey, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		slog.Error("Failed to generate leaf certificate signing private key", slog.Any("error", err))
		os.Exit(1)
	}
	if err := saveRSAPrivateKeyPEM(leafCertPrivateKey, path.Join(*outputPath, "test_leaf_cert_signing_private_key.pem")); err != nil {
		slog.Error("Failed to save leaf certificate signing private key", slog.Any("error", err))
		os.Exit(1)
	}
}

func rootCACert(privKey *rsa.PrivateKey) (*x509.Certificate, error) {
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

func intermediateCACert(rootCACert *x509.Certificate, rootPrivKey, privKey *rsa.PrivateKey) (*x509.Certificate, error) {
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

func saveRSAPrivateKeyPEM(key *rsa.PrivateKey, filename string) error {
	// Marshal the private key to PKCS1 ASN.1 DER.
	derBytes := x509.MarshalPKCS1PrivateKey(key)

	// No encryption.
	block := &pem.Block{
		Type:  "RSA TEST PRIVATE KEY",
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
