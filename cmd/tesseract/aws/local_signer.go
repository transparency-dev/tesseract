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
	"crypto"
	"crypto/ecdsa"
	"crypto/x509"
	"encoding/pem"
	"errors"
	"fmt"
	"os"
)

// NewLocalSigner creates a new signer that uses the ECDSA P-256 key pair from
// local disk files for signing digests.
func NewLocalSigner(publicKeyFile, privateKeyFile string) (*ECDSAWithSHA256Signer, error) {
	// Read public key
	publicKeyPEM, err := os.ReadFile(publicKeyFile)
	if err != nil {
		return nil, fmt.Errorf("failed to read public key file %s: %w", publicKeyFile, err)
	}

	publicPemBlock, rest := pem.Decode(publicKeyPEM)
	if publicPemBlock == nil {
		return nil, errors.New("failed to decode public key PEM")
	}
	if len(rest) > 0 {
		return nil, fmt.Errorf("extra data after decoding public key PEM: %v", rest)
	}

	var publicKey crypto.PublicKey
	switch publicPemBlock.Type {
	case "PUBLIC KEY":
		publicKey, err = x509.ParsePKIXPublicKey(publicPemBlock.Bytes)
	default:
		return nil, fmt.Errorf("unsupported public key PEM type: %s", publicPemBlock.Type)
	}
	if err != nil {
		return nil, err
	}

	ecdsaPublicKey, ok := publicKey.(*ecdsa.PublicKey)
	if !ok {
		return nil, fmt.Errorf("the public key is not an ECDSA key")
	}

	// Read private key
	privateKeyPEM, err := os.ReadFile(privateKeyFile)
	if err != nil {
		return nil, fmt.Errorf("failed to read private key file %s: %w", privateKeyFile, err)
	}

	privatePemBlock, rest := pem.Decode(privateKeyPEM)
	if privatePemBlock == nil {
		return nil, errors.New("failed to decode private key PEM")
	}
	if len(rest) > 0 {
		return nil, fmt.Errorf("extra data after decoding private key PEM: %v", rest)
	}

	var ecdsaPrivateKey *ecdsa.PrivateKey
	switch privatePemBlock.Type {
	case "EC PRIVATE KEY":
		ecdsaPrivateKey, err = x509.ParseECPrivateKey(privatePemBlock.Bytes)
	default:
		return nil, fmt.Errorf("unsupported private key PEM type: %s", privatePemBlock.Type)
	}
	if err != nil {
		return nil, err
	}

	// Verify the correctness of the signer key pair
	if !ecdsaPrivateKey.PublicKey.Equal(ecdsaPublicKey) {
		return nil, errors.New("signer key pair doesn't match")
	}

	return &ECDSAWithSHA256Signer{
		publicKey:  ecdsaPublicKey,
		privateKey: ecdsaPrivateKey,
	}, nil
}
