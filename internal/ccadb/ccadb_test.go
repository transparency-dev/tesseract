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
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"

	"github.com/transparency-dev/tesseract/internal/testdata"
)

var (
	testSHA256 = "dum"
)

type ccadbRsp struct {
	code    int
	crts    []string
	useCase string
}

func NewTestServer(t *testing.T, rsp ccadbRsp) *httptest.Server {
	t.Helper()

	return httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(rsp.code)
		if rsp.code != 200 {
			return
		}

		cw := csv.NewWriter(w)
		records := [][]string{{ColIssuer, ColSHA, ColSubject, ColPEM, ColUseCase}}
		for _, c := range rsp.crts {
			cert := parsePEM(t, c)
			records = append(records, []string{cert.Issuer.String(), testSHA256, cert.Subject.String(), c, rsp.useCase})
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
	tests := []struct {
		name    string
		rsp     ccadbRsp
		fields  []string
		want    [][][]byte
		wantErr bool
	}{
		{
			name: "ok-one-col",
			rsp: ccadbRsp{
				code: 200,
				crts: []string{
					testdata.CACertPEM,
				},
				useCase: UseCaseServerAuth,
			},
			fields: []string{ColPEM},
			want:   [][][]byte{{[]byte(testdata.CACertPEM)}},
		},
		{
			name: "ok-two-cols",
			rsp: ccadbRsp{
				code: 200,
				crts: []string{
					testdata.CACertPEM,
				},
				useCase: UseCaseServerAuth,
			},
			fields: []string{ColPEM, ColSHA},
			want:   [][][]byte{{[]byte(testdata.CACertPEM), []byte(testSHA256)}},
		},
		{
			name: "ok-upper-usecase",
			rsp: ccadbRsp{
				code: 200,
				crts: []string{
					testdata.CACertPEM,
				},
				useCase: strings.ToUpper(UseCaseServerAuth),
			},
			fields: []string{ColPEM},
			want:   [][][]byte{{[]byte(testdata.CACertPEM)}},
		},
		{
			name: "ok-two-certs",
			rsp: ccadbRsp{
				code: 200,
				crts: []string{
					testdata.CACertPEM,
					testdata.FakeRootCACertPEM,
				},
				useCase: UseCaseServerAuth,
			},
			fields: []string{ColPEM},
			want: [][][]byte{
				{[]byte(testdata.CACertPEM)},
				{[]byte(testdata.FakeRootCACertPEM)},
			},
		},
		{
			name: "wrong-usecase-two-certs",
			rsp: ccadbRsp{
				code: 200,
				crts: []string{
					testdata.CACertPEM,
					testdata.FakeRootCACertPEM,
				},
				useCase: "socks",
			},
			fields: []string{ColPEM},
			want:   [][][]byte{},
		},
		{
			name: "404",
			rsp: ccadbRsp{
				code: 404,
			},
			fields:  []string{ColPEM},
			wantErr: true,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			ts := NewTestServer(t, tc.rsp)
			ts.Start()
			defer ts.Close()
			got, gotErr := Fetch(t.Context(), ts.URL, tc.fields)
			if gotErr != nil {
				if !tc.wantErr {
					t.Errorf("Fetch() failed: %v", gotErr)
				}
				return
			}
			if tc.wantErr {
				t.Fatal("Fetch() succeeded unexpectedly")
			}
			if !reflect.DeepEqual(got, tc.want) {
				t.Errorf("unequal: %s, %s", got, tc.want)
			}
		})
	}
}
