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

package tesseract

import (
	"crypto/x509"
	"encoding/csv"
	"encoding/pem"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/transparency-dev/tesseract/internal/ccadb"
	"github.com/transparency-dev/tesseract/internal/testdata"
)

func TestNewCertValidationOpts(t *testing.T) {
	t100 := time.Unix(100, 0)
	t200 := time.Unix(200, 0)

	for _, tc := range []struct {
		desc    string
		wantErr string
		cvCfg   ChainValidationConfig
	}{
		{
			desc:    "empty-rootsPemFile",
			wantErr: "empty rootsPemFile",
		},
		{
			desc:    "missing-root-cert",
			wantErr: "failed to read trusted roots",
			cvCfg: ChainValidationConfig{
				RootsPEMFile: "./internal/testdata/bogus.cert",
			},
		},
		{
			desc:    "rejecting-all",
			wantErr: "configuration would reject all certificates",
			cvCfg: ChainValidationConfig{
				RootsPEMFile:    "./internal/testdata/fake-ca.cert",
				RejectExpired:   true,
				RejectUnexpired: true},
		},
		{
			desc:    "unknown-ext-key-usage-1",
			wantErr: "unknown extended key usage",
			cvCfg: ChainValidationConfig{
				RootsPEMFile: "./internal/testdata/fake-ca.cert",
				ExtKeyUsages: "wrong_usage"},
		},
		{
			desc:    "unknown-ext-key-usage-2",
			wantErr: "unknown extended key usage",
			cvCfg: ChainValidationConfig{
				RootsPEMFile: "./internal/testdata/fake-ca.cert",
				ExtKeyUsages: "ClientAuth,ServerAuth,TimeStomping",
			},
		},
		{
			desc:    "unknown-ext-key-usage-3",
			wantErr: "unknown extended key usage",
			cvCfg: ChainValidationConfig{
				RootsPEMFile: "./internal/testdata/fake-ca.cert",
				ExtKeyUsages: "Any ",
			},
		},
		{
			desc:    "unknown-reject-ext",
			wantErr: "failed to parse RejectExtensions",
			cvCfg: ChainValidationConfig{
				RootsPEMFile:     "./internal/testdata/fake-ca.cert",
				RejectExtensions: "1.2.3.4,one.banana.two.bananas",
			},
		},
		{
			desc:    "limit-before-start",
			wantErr: "before start",
			cvCfg: ChainValidationConfig{
				RootsPEMFile:  "./internal/testdata/fake-ca.cert",
				NotAfterStart: &t200,
				NotAfterLimit: &t100,
			},
		},
		{
			desc: "ok",
			cvCfg: ChainValidationConfig{
				RootsPEMFile: "./internal/testdata/fake-ca.cert",
			},
		},
		{
			desc: "ok-ext-key-usages",
			cvCfg: ChainValidationConfig{
				RootsPEMFile: "./internal/testdata/fake-ca.cert",
				ExtKeyUsages: "ServerAuth,ClientAuth,OCSPSigning",
			},
		},
		{
			desc: "ok-reject-ext",
			cvCfg: ChainValidationConfig{
				RootsPEMFile:     "./internal/testdata/fake-ca.cert",
				RejectExtensions: "1.2.3.4,5.6.7.8",
			},
		},
		{
			desc: "ok-start-timestamp",
			cvCfg: ChainValidationConfig{
				RootsPEMFile:  "./internal/testdata/fake-ca.cert",
				NotAfterStart: &t100,
			},
		},
		{
			desc: "ok-limit-timestamp",
			cvCfg: ChainValidationConfig{
				RootsPEMFile:  "./internal/testdata/fake-ca.cert",
				NotAfterStart: &t200,
			},
		},
		{
			desc: "ok-range-timestamp",
			cvCfg: ChainValidationConfig{
				RootsPEMFile:  "./internal/testdata/fake-ca.cert",
				NotAfterStart: &t100,
				NotAfterLimit: &t200,
			},
		},
	} {
		t.Run(tc.desc, func(t *testing.T) {
			vc, err := newChainValidator(t.Context(), tc.cvCfg)
			if len(tc.wantErr) == 0 && err != nil {
				t.Errorf("ValidateLogConfig()=%v, want nil", err)
			}
			if len(tc.wantErr) > 0 && (err == nil || !strings.Contains(err.Error(), tc.wantErr)) {
				t.Errorf("ValidateLogConfig()=%v, want err containing %q", err, tc.wantErr)
			}
			if err == nil && vc == nil {
				t.Error("err and ValidatedLogConfig are both nil")
			}
		})
	}
}

type ccadbRsp struct {
	code int
	crts []string
}

func newCCADBTestServer(t *testing.T, rsps []ccadbRsp) *httptest.Server {
	t.Helper()

	if len(rsps) == 0 {
		rsps = append(rsps, ccadbRsp{code: 404})
	}
	i := 0
	next := func() ccadbRsp {
		idx := min(i, len(rsps)-1)
		i++
		return rsps[idx]
	}

	return httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		rsp := next()
		w.WriteHeader(rsp.code)
		if rsp.code != 200 {
			return
		}

		cw := csv.NewWriter(w)
		records := [][]string{
			{ccadb.ColIssuer, ccadb.ColSHA, ccadb.ColSubject, ccadb.ColPEM, ccadb.ColUseCase},
		}
		for _, c := range rsp.crts {
			cert := parsePEM(t, c)
			records = append(records, []string{cert.Issuer.String(), "dum", cert.Subject.String(), c, ccadb.UseCaseServerAuth})
		}

		for _, record := range records {
			if err := cw.Write(record); err != nil {
				t.Fatalf("error writing record to csv: %v", err)
			}
		}
		cw.Flush()
	}))
}

func TestNewChainValidatorRootsRemoteFetch(t *testing.T) {
	fetchInterval := 20 * time.Millisecond

	for _, tc := range []struct {
		desc       string
		cvCfg      ChainValidationConfig
		rsps       []ccadbRsp
		wantNRoots int
	}{
		{
			desc: "ok-no-remote",
			cvCfg: ChainValidationConfig{
				RootsPEMFile: "./internal/testdata/fake-ca.cert",
			},
			wantNRoots: 1,
		},
		{
			desc: "404",
			cvCfg: ChainValidationConfig{
				RootsPEMFile:             "./internal/testdata/fake-ca.cert",
				RootsRemoteFetchInterval: fetchInterval,
			},
			rsps: []ccadbRsp{
				{
					code: 404,
				},
			},
			wantNRoots: 1,
		},
		{
			desc: "404-then-200-new-root",
			cvCfg: ChainValidationConfig{
				RootsPEMFile:             "./internal/testdata/fake-ca.cert",
				RootsRemoteFetchInterval: fetchInterval,
			},
			rsps: []ccadbRsp{
				{
					code: 404,
				},
				{
					code: 404,
				},
				{
					code: 200,
					crts: []string{
						testdata.CACertPEM,
					},
				},
			},
			wantNRoots: 2,
		},
		{
			desc: "new-root-on-start",
			cvCfg: ChainValidationConfig{
				RootsPEMFile:             "./internal/testdata/fake-ca.cert",
				RootsRemoteFetchInterval: fetchInterval,
			},
			rsps: []ccadbRsp{
				{
					code: 200,
					crts: []string{
						testdata.CACertPEM,
					},
				},
			},
			wantNRoots: 2,
		},
		{
			desc: "no-new-root",
			cvCfg: ChainValidationConfig{
				RootsPEMFile:             "./internal/testdata/fake-ca.cert",
				RootsRemoteFetchInterval: fetchInterval,
			},
			rsps: []ccadbRsp{
				{
					code: 200,
					crts: []string{
						testdata.FakeRootCACertPEM,
					},
				},
			},
			wantNRoots: 1,
		},
		{
			desc: "root-removed",
			cvCfg: ChainValidationConfig{
				RootsPEMFile:             "./internal/testdata/fake-ca.cert",
				RootsRemoteFetchInterval: fetchInterval,
			},
			rsps: []ccadbRsp{
				{
					code: 200,
					crts: []string{
						testdata.CACertPEM,
						testdata.FakeRootCACertPEM,
					},
				},
				{
					code: 200,
					crts: []string{
						testdata.FakeRootCACertPEM,
					},
				},
			},
			wantNRoots: 2,
		},
	} {
		t.Run(tc.desc, func(t *testing.T) {
			ts := newCCADBTestServer(t, tc.rsps)
			ts.Start()
			defer ts.Close()
			tc.cvCfg.RootsRemoteFetchURL = ts.URL
			cv, err := newChainValidator(t.Context(), tc.cvCfg)
			if err == nil && cv == nil {
				t.Error("err and ValidatedLogConfig are both nil")
			}
			time.Sleep(10 * fetchInterval)
			if got := len(cv.Roots()); got != tc.wantNRoots {
				t.Errorf("ChainValidator has %d roots, want %d", got, tc.wantNRoots)
			}

		})
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
