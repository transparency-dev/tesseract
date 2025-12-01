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

package ccadb

import (
	"crypto/x509"
	"encoding/csv"
	"encoding/pem"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"reflect"
	"strconv"
	"testing"

	"github.com/transparency-dev/tesseract/internal/testdata"
)

func SetupTestServer(t *testing.T) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Parse query params
		query := r.URL.Query()

		codeStr := query.Get("code")
		if codeStr != "" {
			if code, err := strconv.Atoi(codeStr); err == nil {
				w.WriteHeader(code)
			}
		}
		if codeStr != "200" {
			return
		}

		body := query.Get("body")
		cert := parsePEM(t, body)

		cw := csv.NewWriter(w)
		records := [][]string{
			{ColIssuer, ColSHA, ColSubject, ColPEM, ColUsecase},
			{cert.Issuer.String(), "sha", cert.Subject.String(), body, UsecaseServerAuth},
		}
		for _, record := range records {
			if err := cw.Write(record); err != nil {
				t.Fatalf("error writing record to csv: %v", err)
			}
		}
		cw.Flush()
	}))
}

func parsePEM(t *testing.T, pemCert string) *x509.Certificate {
	t.Helper()
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

func TestFetch(t *testing.T) {
	ts := SetupTestServer(t)
	tests := []struct {
		name string // description of this test case
		// Named input parameters for target function.
		url     string
		fields  []string
		want    [][][]byte
		wantErr bool
	}{
		{
			name:   "ok",
			url:    fmt.Sprintf("%s?&code=%d&body=%s", ts.URL, 200, url.QueryEscape(testdata.CACertPEM)),
			fields: []string{ColPEM},
			want:   [][][]byte{{[]byte(testdata.CACertPEM)}},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, gotErr := Fetch(tt.url, tt.fields)
			if gotErr != nil {
				if !tt.wantErr {
					t.Errorf("Fetch() failed: %v", gotErr)
				}
			}
			if tt.wantErr {
				t.Fatal("Fetch() succeeded unexpectedly")
			}
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("unequal: %s, %s", got, tt.want)
			}
		})
	}
}
