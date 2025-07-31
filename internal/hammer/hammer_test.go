// Copyright 2024 The Tessera authors. All Rights Reserved.
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
	"bytes"
	"encoding/json"
	"testing"
)

func TestLeafGenerator(t *testing.T) {
	// Load intermediate CA certificate from test data.
	intermediateCACert, err := loadIntermediateCACert("./testdata/test_intermediate_ca_cert.pem")
	if err != nil {
		t.Fatalf("Failed to load intermediate CA certificate: %v", err)
	}

	// Load intermediate CA private key from test data.
	caKey, err := loadPrivateKey("./testdata/test_intermediate_ca_private_key.pem")
	if err != nil {
		t.Fatalf("Failed to load intermediate CA private key: %v", err)
	}

	// Load leaf certificate signing private key.
	leafCertPrivateKey, err := loadPrivateKey("./testdata/test_leaf_cert_signing_private_key.pem")
	if err != nil {
		t.Fatalf("Failed to load private key: %v", err)
	}

	vs := make(map[string]bool)

	// Always generate new values.
	func() {
		gN := newLeafGenerator(0, 0, intermediateCACert, caKey, leafCertPrivateKey)
		for range 256 {
			l := gN()
			s, err := json.MarshalIndent(l, "", "  ")
			if err != nil {
				t.Fatalf("Failed to marshal leaf: %v", err)
			}
			vs[string(s)] = true
		}
	}()

	func() {
		// Always generate duplicate.
		gD := newLeafGenerator(256, 1.0, intermediateCACert, caKey, leafCertPrivateKey)
		for range 256 {
			l := gD()
			s, err := json.MarshalIndent(l, "", "  ")
			if err != nil {
				t.Fatalf("Failed to marshal leaf: %v", err)
			}
			if !vs[string(s)] {
				t.Error("Expected duplicate")
			}
		}
	}()
}

func TestCertificateGeneratorDeterministic(t *testing.T) {
	// Load intermediate CA certificate from test data.
	intermediateCACert, err := loadIntermediateCACert("./testdata/test_intermediate_ca_cert.pem")
	if err != nil {
		t.Fatalf("Failed to load intermediate CA certificate: %v", err)
	}

	// Load intermediate CA private key from test data.
	caKey, err := loadPrivateKey("./testdata/test_intermediate_ca_private_key.pem")
	if err != nil {
		t.Fatalf("Failed to load intermediate CA private key: %v", err)
	}

	// Load leaf certificate signing private key.
	leafCertPrivateKey, err := loadPrivateKey("./testdata/test_leaf_cert_signing_private_key.pem")
	if err != nil {
		t.Fatalf("Failed to load private key: %v", err)
	}

	certGen := newChainGenerator(intermediateCACert, caKey, publicKey(leafCertPrivateKey))

	cert0 := certGen.certificate(0)
	cert1 := certGen.certificate(0)

	if len(cert0) == 0 || len(cert1) == 0 {
		t.Error("Certificate is empty")
	}

	if !bytes.Equal(cert0, cert1) {
		t.Errorf("Certificates generator did not generate deterministic certificates")
	}
}
