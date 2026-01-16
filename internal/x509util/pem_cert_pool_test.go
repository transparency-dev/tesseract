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

package x509util_test

import (
	"crypto/x509"
	"encoding/pem"
	"testing"

	"github.com/transparency-dev/tesseract/internal/x509util"
)

func TestAppendCertsFromPEMs(t *testing.T) {
	tests := []struct {
		name       string
		pems       [][]byte
		wantParsed int
		wantAdded  int
	}{
		{
			name:       "single-cert-from-pem",
			pems:       [][]byte{[]byte(pemCACert)},
			wantParsed: 1,
			wantAdded:  1,
		},
		{
			name:       "single-cert-with-other-stuff",
			pems:       [][]byte{[]byte(pemCACertWithOtherStuff)},
			wantParsed: 1,
			wantAdded:  1,
		},
		{
			name:       "duplicate-cert-from-pems",
			pems:       [][]byte{[]byte(pemCACertDuplicated)},
			wantParsed: 2,
			wantAdded:  1,
		},
		{
			name:       "multiple-certs-from-pem",
			pems:       [][]byte{[]byte(pemCACertMultiple)},
			wantParsed: 2,
			wantAdded:  2,
		},
		{
			name:       "multiple-certs-from-pems",
			pems:       [][]byte{[]byte(pemCACert), []byte(pemCACertMultiple)},
			wantParsed: 3,
			wantAdded:  2,
		},
		{
			name:       "empty",
			pems:       [][]byte{},
			wantParsed: 0,
			wantAdded:  0,
		},
		{
			name:       "bad-from-pem",
			pems:       [][]byte{[]byte(pemCACertBad)},
			wantParsed: 0,
			wantAdded:  0,
		},
		{
			name:       "bad-and-empty-from-pems",
			pems:       [][]byte{[]byte(pemUnknownBlockType), []byte(pemCACertBad), []byte(pemCACert)},
			wantParsed: 1,
			wantAdded:  1,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p := x509util.NewPEMCertPool()
			gotParsed, gotAdded := p.AppendCertsFromPEMs(tt.pems...)
			poolLen := len(p.Subjects())
			if gotParsed != tt.wantParsed || gotAdded != tt.wantAdded {
				t.Errorf("AppendCertsFromPEMs() = (%v, %v), want (%v, %v)", gotParsed, gotAdded, tt.wantParsed, tt.wantAdded)
			}
			if gotAdded != poolLen {
				t.Errorf("AppendCertsFromPEMs added %v certs, but pool on has %v certs)", gotAdded, poolLen)
			}
		})
	}
}

func TestIncluded(t *testing.T) {
	certs := [2]*x509.Certificate{parsePEM(t, pemCACert), parsePEM(t, pemFakeCACert)}

	// Note: tests are cumulative
	tests := []struct {
		cert *x509.Certificate
		want [2]bool
	}{
		{cert: nil, want: [2]bool{false, false}},
		{cert: nil, want: [2]bool{false, false}},
		{cert: certs[0], want: [2]bool{true, false}},
		{cert: nil, want: [2]bool{true, false}},
		{cert: certs[0], want: [2]bool{true, false}},
		{cert: certs[1], want: [2]bool{true, true}},
		{cert: nil, want: [2]bool{true, true}},
		{cert: certs[1], want: [2]bool{true, true}},
	}

	pool := x509util.NewPEMCertPool()
	for _, test := range tests {
		if test.cert != nil {
			pool.AddCerts([]*x509.Certificate{test.cert})
		}
		for i, cert := range certs {
			got := pool.Included(cert)
			if got != test.want[i] {
				t.Errorf("pool.Included(cert[%d])=%v, want %v", i, got, test.want[i])
			}
		}
	}
}

func parsePEM(t *testing.T, pemCert string) *x509.Certificate {
	var block *pem.Block
	block, _ = pem.Decode([]byte(pemCert))
	if block == nil || block.Type != "CERTIFICATE" || len(block.Headers) != 0 {
		t.Fatal("No PEM data found")
	}

	cert, err := x509.ParseCertificate(block.Bytes)
	if err != nil {
		t.Fatalf("Failed to parse PEM certificate: %v", err)
	}
	return cert
}
