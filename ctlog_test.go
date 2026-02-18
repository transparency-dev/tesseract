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
	"context"
	"crypto/sha256"
	"crypto/x509"
	"encoding/csv"
	"encoding/hex"
	"encoding/pem"
	"maps"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/transparency-dev/tesseract/internal/ccadb"
	"github.com/transparency-dev/tesseract/internal/testdata"
	"github.com/transparency-dev/tesseract/storage"
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
		{
			desc:    "invalid-reject-roots",
			wantErr: "failed to create roots pool",
			cvCfg: ChainValidationConfig{
				RootsPEMFile: "./internal/testdata/fake-ca.cert",
				RejectRoots:  []string{"invalid-hex"},
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

type memoryRootsStorage struct {
	mu sync.RWMutex
	m  map[string][]byte
}

func (m *memoryRootsStorage) AddIfNotExist(ctx context.Context, kvs []storage.KV) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, kv := range kvs {
		m.m[string(kv.K)] = kv.V
	}
	return nil
}

func (m *memoryRootsStorage) LoadAll(ctx context.Context) ([]storage.KV, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	kvs := make([]storage.KV, 0, len(m.m))
	for k, v := range m.m {
		kvs = append(kvs, storage.KV{K: []byte(k), V: v})
	}
	return kvs, nil
}

func TestNewChainValidatorRootsFiltering(t *testing.T) {
	fetchInterval := 20 * time.Millisecond
	fakeRoot := parsePEM(t, testdata.FakeRootCACertPEM)
	fakeRootDER := fakeRoot.Raw
	fakeRootSHA256 := sha256.Sum256(fakeRootDER)
	fakeRootFingerprint := hex.EncodeToString(fakeRootSHA256[:])

	caRoot := parsePEM(t, testdata.CACertPEM)
	caRootDER := caRoot.Raw
	caRootSHA256 := sha256.Sum256(caRootDER)
	caRootFingerprint := hex.EncodeToString(caRootSHA256[:])

	for _, tc := range []struct {
		desc              string
		cvCfg             ChainValidationConfig
		rsps              []ccadbRsp
		backupRoots       []storage.KV
		wantNRoots        int
		wantRootsFP       []string // Fingerprints we expect to be present
		wantBackupRootsFP []string // Fingerprints we expect to be present
	}{
		{
			desc: "reject-local-root",
			cvCfg: ChainValidationConfig{
				RootsPEMFile: "./internal/testdata/fake-ca.cert", // Contains FakeRootCACertPEM
				RejectRoots:  []string{fakeRootFingerprint},
			},
			wantNRoots:        0,
			wantRootsFP:       nil,
			wantBackupRootsFP: nil,
		},
		{
			desc: "reject-remote-root",
			cvCfg: ChainValidationConfig{
				RootsPEMFile:             "./internal/testdata/fake-ca.cert",
				RootsRemoteFetchInterval: fetchInterval,
				RejectRoots:              []string{caRootFingerprint}, // Reject the remote one, accept local
			},
			rsps: []ccadbRsp{
				{
					code: 200,
					crts: []string{
						testdata.CACertPEM, // This is the remote one
					},
				},
			},
			wantNRoots:        1, // Only local FakeRootCACertPEM
			wantRootsFP:       []string{fakeRootFingerprint},
			wantBackupRootsFP: []string{caRootFingerprint}, // remote roots are always backed up
		},
		{
			desc: "reject-both-roots",
			cvCfg: ChainValidationConfig{
				RootsPEMFile:             "./internal/testdata/fake-ca.cert",
				RootsRemoteFetchInterval: fetchInterval,
				RejectRoots:              []string{fakeRootFingerprint, caRootFingerprint},
			},
			rsps: []ccadbRsp{
				{
					code: 200,
					crts: []string{
						testdata.CACertPEM,
					},
				},
			},
			wantNRoots:        0,
			wantRootsFP:       []string{},
			wantBackupRootsFP: []string{caRootFingerprint}, // remote roots are always backed up
		},
		{
			desc: "reject-backup-root",
			cvCfg: ChainValidationConfig{
				RootsPEMFile:             "./internal/testdata/test_root_ca_cert.pem", // Just to satisfy non-empty check
				RootsRemoteFetchInterval: fetchInterval,
				RejectRoots:              []string{fakeRootFingerprint},
			},
			backupRoots: []storage.KV{
				{
					K: []byte(fakeRootFingerprint),
					V: []byte(testdata.FakeRootCACertPEM),
				},
			},
			wantNRoots:        1,                             // Only CACertPEM from file
			wantRootsFP:       []string{caRootFingerprint},   // CACertPEM fingerprint
			wantBackupRootsFP: []string{fakeRootFingerprint}, // remote roots are always backed up
		},
	} {
		t.Run(tc.desc, func(t *testing.T) {
			ts := newCCADBTestServer(t, tc.rsps)
			ts.Start()
			defer ts.Close()
			tc.cvCfg.RootsRemoteFetchURL = ts.URL
			tc.cvCfg.RootsRemoteFetchBackup = &memoryRootsStorage{m: make(map[string][]byte)}
			if err := tc.cvCfg.RootsRemoteFetchBackup.AddIfNotExist(t.Context(), tc.backupRoots); err != nil {
				t.Fatalf("Can't initialize root backup storage: %v", err)
			}
			cv, err := newChainValidator(t.Context(), tc.cvCfg)
			if err != nil {
				t.Fatalf("newChainValidator()=%v", err)
			}
			time.Sleep(10 * fetchInterval)

			roots := cv.Roots()
			if got := len(roots); got != tc.wantNRoots {
				t.Errorf("ChainValidator has %d roots, want %d", got, tc.wantNRoots)
			}
			// Verify presence of expected roots
			for _, wantFP := range tc.wantRootsFP {
				found := false
				for _, r := range roots {
					sha := sha256.Sum256(r.Raw)
					if hex.EncodeToString(sha[:]) == wantFP {
						found = true
						break
					}
				}
				if !found {
					t.Errorf("ChainValidator missing root with fingerprint %s", wantFP)
				}
			}
			// Verify presence of expected backed up roots
			gotBackupRoots, err := tc.cvCfg.RootsRemoteFetchBackup.LoadAll(t.Context())
			if err != nil {
				t.Fatalf("Couldn't read backed up roots: %v", err)
			}
			gotBackupRootsByFP := make(map[string][]byte)
			for _, root := range gotBackupRoots {
				gotBackupRootsByFP[string(root.K)] = root.V
			}
			for _, wantFP := range tc.wantBackupRootsFP {
				if _, exists := gotBackupRootsByFP[wantFP]; !exists {
					t.Errorf("Backed up roots missing root with fingerprint %s", wantFP)
					continue
				}
				delete(gotBackupRootsByFP, wantFP)
			}
			if len(gotBackupRootsByFP) > 0 {
				extra := slices.Collect(maps.Keys(gotBackupRootsByFP))
				t.Errorf("Backed up roots contains unexpected roots: %v", strings.Join(extra, ", "))
			}
		})
	}
}
