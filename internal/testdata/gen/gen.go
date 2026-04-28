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

// gen is a tool to generate test ECDSA/RSA keys and test certificates.
package main

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/asn1"
	"encoding/pem"
	"flag"
	"fmt"
	"log/slog"
	"math/big"
	"os"
	"path"
	"strings"
	"time"
)

const (
	commonName   = "test.transparency.dev"
	organization = "TrustFabric Transparency.dev Test"
	country      = "GB"
)

var (
	outputPath      = flag.String("output_path", "./internal/testdata/", "Output path for private keys and certificates")
	notBeforeString = flag.String("not_before", "2024-12-05T18:05:50.000Z", "Start of the range of certs to be generated. RFC3339 format, e.g: 2024-01-02T15:04:05Z.")
)

var (
	// From RFC6962 Section 3.1. To identify pre-certs.
	ctPrecertPoisonOID = asn1.ObjectIdentifier{1, 3, 6, 1, 4, 1, 11129, 2, 4, 3}
	ctPoison           = pkix.Extension{
		Id:       ctPrecertPoisonOID,
		Critical: true,
		Value:    []byte{0x05, 0x00}, // ASN.1 NULL
	}
	// From RFC6962 Section 3.1. For intermediates to issue pre-certs.
	preIssuerEKUOID = asn1.ObjectIdentifier{1, 3, 6, 1, 4, 1, 11129, 2, 4, 4}
)

func main() {
	flag.Parse()
	notBefore, err := parseTime(*notBeforeString)
	if err != nil {
		slog.Error("Failed to parse start time", slog.Any("error", err))
		os.Exit(1)
	}
	// Generate root.
	// Generate a new EC root CA private key.
	rootPrivKey, err := ecdsa.GenerateKey(elliptic.P384(), rand.Reader)
	if err != nil {
		slog.Error("Failed to generate root CA private key", slog.Any("error", err))
		os.Exit(1)
	}
	if err := saveECDSAPrivateKeyPEM(rootPrivKey, path.Join(*outputPath, "test_root_ca_private_key.pem")); err != nil {
		slog.Error("Failed to save root CA private key", slog.Any("error", err))
		os.Exit(1)
	}

	// Generate a new root CA certificate.
	rootCert, err := rootCACert(rootPrivKey, *notBefore)
	if err != nil {
		slog.Error("Failed to generate root CA certificate", slog.Any("error", err))
		os.Exit(1)
	}
	if err := saveCertificatePEM(rootCert, path.Join(*outputPath, "test_root_ca_cert.pem")); err != nil {
		slog.Error("Failed to save root CA certificate", slog.Any("error", err))
		os.Exit(1)
	}

	// Generate CT server private keys (ECDSA, RSA).
	ctServerECDSAPrivKey, err := ecdsa.GenerateKey(elliptic.P384(), rand.Reader)
	if err != nil {
		slog.Error("Failed to generate CT server ECDSA private key", slog.Any("error", err))
		os.Exit(1)
	}
	if err := saveECDSAPrivateKeyPEM(ctServerECDSAPrivKey, path.Join(*outputPath, "test_ct_server_ecdsa_private_key.pem")); err != nil {
		slog.Error("Failed to save CT server ECDSA private key", slog.Any("error", err))
		os.Exit(1)
	}

	ctServerRSAPrivKey, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		slog.Error("Failed to generate CT server RSA private key", slog.Any("error", err))
		os.Exit(1)
	}
	if err := saveRSAPrivateKeyPEM(ctServerRSAPrivKey, path.Join(*outputPath, "test_ct_server_rsa_private_key.pem")); err != nil {
		slog.Error("Failed to save CT server RSA private key", slog.Any("error", err))
		os.Exit(1)
	}

	genLeaves(rootCert, rootPrivKey, *notBefore)
	genPreIssuerAndLeaves(rootCert, rootPrivKey, *notBefore)
	genIntermediateAndLeaves(rootCert, rootPrivKey, *notBefore)
}

// genLeaves generates a cert and a pre-cert.
func genLeaves(rootCert *x509.Certificate, rootPrivKey *ecdsa.PrivateKey, notBefore time.Time) {
	// Generate leaf certs chaining to root.
	// Generate a new ECDSA leaf certificate signing private key.
	leafCertPrivateKey, err := ecdsa.GenerateKey(elliptic.P384(), rand.Reader)
	if err != nil {
		slog.Error("Failed to generate leaf certificate signing private key", slog.Any("error", err))
		os.Exit(1)
	}
	if err := saveECDSAPrivateKeyPEM(leafCertPrivateKey, path.Join(*outputPath, "test_leaf_signed_by_root_signing_private_key.pem")); err != nil {
		slog.Error("Failed to save leaf certificate signing private key", slog.Any("error", err))
		os.Exit(1)
	}

	chainGenerator := newChainGenerator(rootCert, rootPrivKey, leafCertPrivateKey.Public())
	leafCert, err := chainGenerator.certificate(100, false, notBefore)
	if err != nil {
		slog.Error("Failed to generate leaf certificate", slog.Any("error", err))
		os.Exit(1)
	}
	if err := saveCertificatePEM(leafCert, path.Join(*outputPath, "test_leaf_cert_signed_by_root.pem")); err != nil {
		slog.Error("Failed to save leaf cert", slog.Any("error", err))
		os.Exit(1)
	}
	leafPreCert, err := chainGenerator.certificate(200, true, notBefore)
	if err != nil {
		slog.Error("Failed to generate leaf certificate", slog.Any("error", err))
		os.Exit(1)
	}
	if err := saveCertificatePEM(leafPreCert, path.Join(*outputPath, "test_leaf_pre_cert_signed_by_root.pem")); err != nil {
		slog.Error("Failed to save leaf cert", slog.Any("error", err))
		os.Exit(1)
	}

}

// genIntermediateAndLeaves generates an intermediate cert, a cert, a pre-cert.
func genIntermediateAndLeaves(rootCert *x509.Certificate, rootPrivKey *ecdsa.PrivateKey, notBefore time.Time) {
	// Generate a new ECDSA intermediate CA private key.
	intermediatePrivKey, err := ecdsa.GenerateKey(elliptic.P384(), rand.Reader)
	if err != nil {
		slog.Error("Failed to generate intermediate CA private key", slog.Any("error", err))
		os.Exit(1)
	}
	if err := saveECDSAPrivateKeyPEM(intermediatePrivKey, path.Join(*outputPath, "test_intermediate_ca_private_key.pem")); err != nil {
		slog.Error("Failed to save intermediate CA private key", slog.Any("error", err))
		os.Exit(1)
	}

	// Generate a new intermediate CA certificate with CT extension.
	intermediateCert, err := intermediateCACert(rootCert, rootPrivKey, intermediatePrivKey, false, notBefore)
	if err != nil {
		slog.Error("Failed to generate intermediate CA certificate", slog.Any("error", err))
		os.Exit(1)
	}
	if err := saveCertificatePEM(intermediateCert, path.Join(*outputPath, "test_intermediate_ca_cert.pem")); err != nil {
		slog.Error("Failed to save intermediate CA certificate", slog.Any("error", err))
		os.Exit(1)
	}

	// Generate a new ECDSA leaf certificate signing private key.
	leafCertPrivateKey, err := ecdsa.GenerateKey(elliptic.P384(), rand.Reader)
	if err != nil {
		slog.Error("Failed to generate leaf certificate signing private key", slog.Any("error", err))
		os.Exit(1)
	}
	if err := saveECDSAPrivateKeyPEM(leafCertPrivateKey, path.Join(*outputPath, "test_leaf_signed_by_intermediate_signing_private_key.pem")); err != nil {
		slog.Error("Failed to save leaf certificate signing private key", slog.Any("error", err))
		os.Exit(1)
	}

	chainGenerator := newChainGenerator(intermediateCert, intermediatePrivKey, leafCertPrivateKey.Public())
	leafCert, err := chainGenerator.certificate(100, false, notBefore)
	if err != nil {
		slog.Error("Failed to generate leaf certificate", slog.Any("error", err))
		os.Exit(1)
	}
	if err := saveCertificatePEM(leafCert, path.Join(*outputPath, "test_leaf_cert_signed_by_intermediate.pem")); err != nil {
		slog.Error("Failed to save leaf cert", slog.Any("error", err))
		os.Exit(1)
	}
	leafPreCert, err := chainGenerator.certificate(200, true, notBefore)
	if err != nil {
		slog.Error("Failed to generate leaf pre-certificate", slog.Any("error", err))
		os.Exit(1)
	}
	if err := saveCertificatePEM(leafPreCert, path.Join(*outputPath, "test_leaf_pre_cert_signed_by_intermediate.pem")); err != nil {
		slog.Error("Failed to save leaf pre-cert", slog.Any("error", err))
		os.Exit(1)
	}
}

// genPreIssuerAndLeaves generates a pre-issuer intermediate cert, a cert,
// a pre-cert.
func genPreIssuerAndLeaves(rootCert *x509.Certificate, rootPrivKey *ecdsa.PrivateKey, notBefore time.Time) {
	// Generate a new ECDSA intermediate CA private key.
	preIntermediatePrivKey, err := ecdsa.GenerateKey(elliptic.P384(), rand.Reader)
	if err != nil {
		slog.Error("Failed to generate intermediate CA private key", slog.Any("error", err))
		os.Exit(1)
	}
	if err := saveECDSAPrivateKeyPEM(preIntermediatePrivKey, path.Join(*outputPath, "test_pre_intermediate_ca_private_key.pem")); err != nil {
		slog.Error("Failed to save intermediate CA private key", slog.Any("error", err))
		os.Exit(1)
	}

	// Generate a new intermediate CA certificate with CT extension.
	preIntermediateCert, err := intermediateCACert(rootCert, rootPrivKey, preIntermediatePrivKey, true, notBefore)
	if err != nil {
		slog.Error("Failed to generate intermediate CA certificate", slog.Any("error", err))
		os.Exit(1)
	}
	if err := saveCertificatePEM(preIntermediateCert, path.Join(*outputPath, "test_pre_intermediate_ca_cert.pem")); err != nil {
		slog.Error("Failed to save intermediate CA certificate", slog.Any("error", err))
		os.Exit(1)
	}

	// Generate a new ECDSA leaf certificate signing private key.
	leafCertPrivateKey, err := ecdsa.GenerateKey(elliptic.P384(), rand.Reader)
	if err != nil {
		slog.Error("Failed to generate leaf certificate signing private key", slog.Any("error", err))
		os.Exit(1)
	}
	if err := saveECDSAPrivateKeyPEM(leafCertPrivateKey, path.Join(*outputPath, "test_leaf_signed_by_pre_intermediate_signing_private_key.pem")); err != nil {
		slog.Error("Failed to save leaf certificate signing private key", slog.Any("error", err))
		os.Exit(1)
	}

	chainGenerator := newChainGenerator(preIntermediateCert, preIntermediatePrivKey, leafCertPrivateKey.Public())
	leafCert, err := chainGenerator.certificate(100, false, notBefore)
	if err != nil {
		slog.Error("Failed to generate leaf certificate", slog.Any("error", err))
		os.Exit(1)
	}
	if err := saveCertificatePEM(leafCert, path.Join(*outputPath, "test_leaf_cert_signed_by_pre_intermediate.pem")); err != nil {
		slog.Error("Failed to save leaf cert", slog.Any("error", err))
		os.Exit(1)
	}
	leafPreCert, err := chainGenerator.certificate(200, true, notBefore)
	if err != nil {
		slog.Error("Failed to generate leaf certificate", slog.Any("error", err))
		os.Exit(1)
	}
	if err := saveCertificatePEM(leafPreCert, path.Join(*outputPath, "test_leaf_pre_cert_signed_by_pre_intermediate.pem")); err != nil {
		slog.Error("Failed to save leaf cert", slog.Any("error", err))
		os.Exit(1)
	}
}

func rootCACert(privKey *ecdsa.PrivateKey, notBefore time.Time) (*x509.Certificate, error) {
	template := x509.Certificate{
		SerialNumber: big.NewInt(1),
		Subject: pkix.Name{
			Organization: []string{fmt.Sprintf("%s Root Test CA", organization)},
			Country:      []string{country},
			CommonName:   fmt.Sprintf("%s Root Test CA", organization),
		},
		NotBefore:             notBefore,
		NotAfter:              notBefore.AddDate(10, 0, 0),
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

func intermediateCACert(rootCACert *x509.Certificate, rootPrivKey, privKey *ecdsa.PrivateKey, preIntermediate bool, notBefore time.Time) (*x509.Certificate, error) {
	template := x509.Certificate{
		SerialNumber: big.NewInt(2),
		Subject: pkix.Name{
			Organization: []string{fmt.Sprintf("%s Intermediate Test CA", organization)},
			Country:      []string{country},
			CommonName:   fmt.Sprintf("%s Intermediate Test CA", organization),
		},
		NotBefore:             notBefore,
		NotAfter:              notBefore.AddDate(5, 0, 0),
		KeyUsage:              x509.KeyUsageCertSign | x509.KeyUsageCRLSign,
		BasicConstraintsValid: true,
		IsCA:                  true,
		MaxPathLen:            0,
	}

	if preIntermediate {
		preIssuerExtension := pkix.Extension{
			Id: preIssuerEKUOID,
		}
		template.ExtraExtensions = append(template.ExtraExtensions, preIssuerExtension)
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

const (
	organizationalUnit = "TrustFabric"
	locality           = "London"
	state              = "London"
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
func (g *chainGenerator) certificate(serialNumber int64, preCert bool, notBefore time.Time) (*x509.Certificate, error) {
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
		NotAfter:              notBefore.AddDate(1, 0, 0),
		KeyUsage:              x509.KeyUsageKeyEncipherment | x509.KeyUsageDigitalSignature,
		ExtKeyUsage:           []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		BasicConstraintsValid: true,
		DNSNames:              []string{commonName},
	}

	ctPoison := pkix.Extension{
		Id:       ctPrecertPoisonOID,
		Critical: true,
		Value:    []byte{0x05, 0x00}, // ASN.1 NULL
	}

	if preCert {
		template.ExtraExtensions = append(template.ExtraExtensions, ctPoison)
	}

	derBytes, err := x509.CreateCertificate(rand.Reader, &template, g.intermediateCert, g.leafCertPublicKey, g.intermediateKey)
	if err != nil {
		return nil, fmt.Errorf("failed to create leaf certificate: %v", err)
	}

	// Parse the DER-encoded certificate.
	cert, err := x509.ParseCertificate(derBytes)
	if err != nil {
		return nil, fmt.Errorf("failed to parse root certificate: %v", err)
	}

	return cert, nil
}

func saveECDSAPrivateKeyPEM(key *ecdsa.PrivateKey, filename string) error {
	// Marshal the private key to SEC1 ASN.1 DER.
	derBytes, err := x509.MarshalECPrivateKey(key)
	if err != nil {
		return fmt.Errorf("failed to marshal EC private key: %v", err)
	}

	// No encryption.
	block := &pem.Block{
		Type:  "EC TESTING KEY",
		Bytes: derBytes,
	}

	// Encode the PEM block to memory.
	pemData := pem.EncodeToMemory(block)

	// Write the PEM data to the file with restrictive permissions.
	if err := os.WriteFile(filename, pemData, 0600); err != nil {
		return fmt.Errorf("failed to write PEM file: %v", err)
	}

	return nil
}

func saveRSAPrivateKeyPEM(key *rsa.PrivateKey, filename string) error {
	// Marshal the private key to PKCS1 ASN.1 DER.
	derBytes := x509.MarshalPKCS1PrivateKey(key)

	// No encryption.
	block := &pem.Block{
		Type:  "RSA TESTING KEY",
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
		return fmt.Errorf("failed to write PEM file: %v", err)
	}
	// TODO(phboneff): automate cert printing in certificate.go
	fmt.Println("Don't forget to update certificate.go with the matching openssl text output.")
	return nil
}

func testingKey(s string) string {
	return strings.ReplaceAll(s, "TESTING KEY", "PRIVATE KEY")
}

type timestampFlag struct {
	t *time.Time
}

func (t *timestampFlag) String() string {
	if t.t != nil {
		return t.t.Format(time.RFC3339)
	}
	return "2024-12-05T18:05:50.000Z"
}

func parseTime(w string) (*time.Time, error) {
	tt, err := time.Parse(time.RFC3339, w)
	if err != nil {
		return nil, fmt.Errorf("can't parse %q as RFC3339 timestamp: %v", w, err)
	}
	return &tt, nil
}
