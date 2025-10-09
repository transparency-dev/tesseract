// Copyright 2025 Google LLC. All Rights Reserved.
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

package main

import (
	"context"
	"crypto"
	"crypto/ecdsa"
	"crypto/elliptic"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/tink-crypto/tink-go-gcpkms/v2/integration/gcpkms"
	"github.com/tink-crypto/tink-go/v2/core/registry"
	"github.com/tink-crypto/tink-go/v2/keyset"
	"github.com/tink-crypto/tink-go/v2/tink"
	tinkUtils "github.com/transparency-dev/tesseract/internal/tink"
)

const TinkScheme = "tink"

// NewTinkSignerVerifier returns a crypto.Signer. Only ECDSA P-256 is supported.
// Provide a path to the encrypted keyset and GCP KMS key URI for decryption.
func NewTinkSignerVerifier(ctx context.Context, kekURI, keysetPath string) (crypto.Signer, error) {
	if kekURI == "" || keysetPath == "" {
		return nil, fmt.Errorf("key encryption key URI or keyset path unset")
	}
	kek, err := getKeyEncryptionKey(ctx, kekURI)
	if err != nil {
		return nil, err
	}

	f, err := os.Open(filepath.Clean(keysetPath))
	if err != nil {
		return nil, err
	}
	defer f.Close() //nolint: errcheck

	kh, err := keyset.Read(keyset.NewJSONReader(f), kek)
	if err != nil {
		return nil, err
	}
	signer, err := tinkUtils.KeyHandleToSigner(kh)
	if err != nil {
		return nil, err
	}

	// validate that key is ECDSA P-256
	pub, ok := signer.Public().(*ecdsa.PublicKey)
	if !ok {
		return nil, fmt.Errorf("key must be ECDSA")
	}
	if pub.Curve != elliptic.P256() {
		return nil, fmt.Errorf("elliptic curve must be P-256, was %s", pub.Curve.Params().Name)
	}

	return signer, err
}

// getKeyEncryptionKey returns a Tink AEAD encryption key from KMS
func getKeyEncryptionKey(ctx context.Context, kmsKey string) (tink.AEAD, error) {
	switch {
	case strings.HasPrefix(kmsKey, "gcp-kms://"):
		gcpClient, err := gcpkms.NewClientWithOptions(ctx, kmsKey)
		if err != nil {
			return nil, err
		}
		registry.RegisterKMSClient(gcpClient)
		return gcpClient.GetAEAD(kmsKey)
	default:
		return nil, fmt.Errorf("unsupported KMS key type for key %s", kmsKey)
	}
}
