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
	"context"
	"crypto"
	"crypto/ecdsa"
	"crypto/x509"
	"encoding/pem"
	"errors"
	"fmt"
	"io"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/secretsmanager"
)

// TODO: Move ECDSAWithSHA256Signer to internal signer package.
// ECDSAWithSHA256Signer implements crypto.Signer using AWS Secrets Manager.
// Only crypto.SHA256 and ECDSA are supported.
type ECDSAWithSHA256Signer struct {
	publicKey  *ecdsa.PublicKey
	privateKey *ecdsa.PrivateKey
}

// Public returns the public key stored in the Signer object.
func (s *ECDSAWithSHA256Signer) Public() crypto.PublicKey {
	return s.publicKey
}

// Sign signs digest with the private key stored in AWS Secrets Manager.
func (s *ECDSAWithSHA256Signer) Sign(rand io.Reader, digest []byte, opts crypto.SignerOpts) ([]byte, error) {
	// Verify hash function and digest bytes length.
	if opts == nil {
		return nil, errors.New("opts cannot be nil")
	}
	if opts.HashFunc() != crypto.SHA256 {
		return nil, fmt.Errorf("unsupported hash func: %v", opts.HashFunc())
	}
	if len(digest) != opts.HashFunc().Size() {
		return nil, fmt.Errorf("digest bytes length %d does not match hash function bytes length %d", len(digest), opts.HashFunc().Size())
	}

	return ecdsa.SignASN1(rand, s.privateKey, digest)
}

// NewSecretsManagerSigner creates a new signer that uses the ECDSA P-256 key pair in
// AWS Secrets Manager for signing digests.
func NewSecretsManagerSigner(ctx context.Context, publicKeySecretName, privateKeySecretName string) (*ECDSAWithSHA256Signer, error) {
	sdkConfig, err := config.LoadDefaultConfig(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to load default AWS configuration: %v", err)
	}

	// Create Secrets Manager client
	client := secretsmanager.NewFromConfig(sdkConfig)

	// Public Key
	var publicKey crypto.PublicKey
	pemBlock, err := secretPEM(ctx, client, publicKeySecretName)
	if err != nil {
		return nil, fmt.Errorf("failed to get public key secret PEM (%s): %w", publicKeySecretName, err)
	}
	switch pemBlock.Type {
	case "PUBLIC KEY":
		publicKey, err = x509.ParsePKIXPublicKey(pemBlock.Bytes)
	default:
		return nil, fmt.Errorf("unsupported PEM type: %s", pemBlock.Type)
	}
	if err != nil {
		return nil, err
	}
	var ecdsaPublicKey *ecdsa.PublicKey
	ecdsaPublicKey, ok := publicKey.(*ecdsa.PublicKey)
	if !ok {
		return nil, fmt.Errorf("the public key stored in Secret Manager is not an ECDSA key")
	}

	// Private Key
	var ecdsaPrivateKey *ecdsa.PrivateKey
	pemBlock, err = secretPEM(ctx, client, privateKeySecretName)
	if err != nil {
		return nil, fmt.Errorf("failed to get private key secret PEM (%s): %w", privateKeySecretName, err)
	}
	switch pemBlock.Type {
	case "EC PRIVATE KEY":
		ecdsaPrivateKey, err = x509.ParseECPrivateKey(pemBlock.Bytes)
	default:
		return nil, fmt.Errorf("unsupported PEM type: %s", pemBlock.Type)
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

func secretPEM(ctx context.Context, client *secretsmanager.Client, secretName string) (*pem.Block, error) {
	input := &secretsmanager.GetSecretValueInput{
		SecretId: aws.String(secretName),
	}

	result, err := client.GetSecretValue(ctx, input)
	if err != nil {
		// For a list of exceptions thrown, see
		// https://<<{{DocsDomain}}>>/secretsmanager/latest/apireference/API_GetSecretValue.html
		return nil, fmt.Errorf("failed to get secret value: %w", err)
	}
	if result.SecretString == nil {
		return nil, fmt.Errorf("secretString is nil for secret %s", secretName)
	}

	var secretString = *result.SecretString

	pemBlock, rest := pem.Decode([]byte(secretString))
	if pemBlock == nil {
		return nil, errors.New("failed to decode PEM")
	}
	if len(rest) > 0 {
		return nil, fmt.Errorf("extra data after decoding PEM: %v", rest)
	}

	return pemBlock, nil
}
