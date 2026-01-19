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

package ct

import (
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/asn1"
	"encoding/pem"
	"strings"
	"testing"
	"time"

	"github.com/transparency-dev/tesseract/internal/testdata"
	"github.com/transparency-dev/tesseract/internal/types/rfc6962"
	"github.com/transparency-dev/tesseract/internal/x509util"
)

func TestParseExtKeyUsages(t *testing.T) {
	for _, tc := range []struct {
		desc        string
		extKeyUsage []string
		wantEKU     []x509.ExtKeyUsage
		wantErr     bool
	}{
		{
			desc:        "empty",
			extKeyUsage: []string{},
			wantEKU:     []x509.ExtKeyUsage{},
			wantErr:     false,
		},
		{
			desc:        "valid-single",
			extKeyUsage: []string{"ServerAuth"},
			wantEKU:     []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
			wantErr:     false,
		},
		{
			desc:        "valid-multiple",
			extKeyUsage: []string{"ServerAuth", "ClientAuth"},
			wantEKU:     []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth, x509.ExtKeyUsageClientAuth},
			wantErr:     false,
		},
		{
			desc:        "invalid",
			extKeyUsage: []string{"InvalidUsage"},
			wantEKU:     nil,
			wantErr:     true,
		},
		{
			desc:        "mixed",
			extKeyUsage: []string{"ServerAuth", "InvalidUsage"},
			wantEKU:     nil,
			wantErr:     true,
		},
		{
			desc:        "any",
			extKeyUsage: []string{"Any"},
			wantEKU:     nil,
			wantErr:     false,
		},
		{
			desc:        "any-with-other-usages",
			extKeyUsage: []string{"Any", "ServerAuth"},
			wantEKU:     nil,
			wantErr:     false,
		},
	} {
		t.Run(tc.desc, func(t *testing.T) {
			got, err := ParseExtKeyUsages(tc.extKeyUsage)
			if tc.wantErr {
				if err == nil {
					t.Errorf("ParseExtKeyUsages(%v) = nil, want error", tc.extKeyUsage)
				}
				return
			}
			if err != nil {
				t.Errorf("ParseExtKeyUsages(%v) = %v, want nil", tc.extKeyUsage, err)
			}
			if len(got) != len(tc.wantEKU) {
				t.Errorf("ParseExtKeyUsages(%v) = %v, want %v", tc.extKeyUsage, got, tc.wantEKU)
			}
			for i, e := range tc.wantEKU {
				if got[i] != e {
					t.Errorf("ParseExtKeyUsages(%v) = %v, want %v", tc.extKeyUsage, got, tc.wantEKU)
				}
			}
		})
	}
}

func TestParseOIDs(t *testing.T) {
	for _, tc := range []struct {
		desc     string
		oids     []string
		wantOIDs []asn1.ObjectIdentifier
		wantErr  bool
	}{
		{
			desc:     "empty",
			oids:     []string{},
			wantOIDs: []asn1.ObjectIdentifier{},
			wantErr:  false,
		},
		{
			desc:     "valid-single",
			oids:     []string{"1.2.3"},
			wantOIDs: []asn1.ObjectIdentifier{[]int{1, 2, 3}},
			wantErr:  false,
		},
		{
			desc:     "valid-multiple",
			oids:     []string{"1.2.3", "4.5.6"},
			wantOIDs: []asn1.ObjectIdentifier{[]int{1, 2, 3}, []int{4, 5, 6}},
			wantErr:  false,
		},
		{
			desc:     "invalid",
			oids:     []string{"1.2.a"},
			wantOIDs: nil,
			wantErr:  true,
		},
		{
			desc:     "mixed-valid-invalid",
			oids:     []string{"1.2.3", "1.2.a"},
			wantOIDs: nil,
			wantErr:  true,
		},
	} {
		t.Run(tc.desc, func(t *testing.T) {
			got, err := ParseOIDs(tc.oids)
			if tc.wantErr {
				if err == nil {
					t.Errorf("ParseOIDs(%v) = nil, want error", tc.oids)
				}
				return
			}
			if err != nil {
				t.Fatalf("ParseOIDs(%v) = %v, want nil", tc.oids, err)
			}
			if len(got) != len(tc.wantOIDs) {
				t.Errorf("ParseOIDs(%v) = %v, want %v", tc.oids, got, tc.wantOIDs)
			}
			for i, e := range tc.wantOIDs {
				if !got[i].Equal(e) {
					t.Errorf("ParseOIDs(%v) = %v, want %v", tc.oids, got, tc.wantOIDs)
				}
			}
		})
	}
}

func wipeExtensions(cert *x509.Certificate) *x509.Certificate {
	cert.Extensions = cert.Extensions[:0]
	return cert
}

func makePoisonNonCritical(cert *x509.Certificate) *x509.Certificate {
	// Invalid as a pre-cert because poison extension needs to be marked as critical.
	cert.Extensions = []pkix.Extension{{Id: rfc6962.OIDExtensionCTPoison, Critical: false, Value: asn1.NullBytes}}
	return cert
}

func makePoisonNonNull(cert *x509.Certificate) *x509.Certificate {
	// Invalid as a pre-cert because poison extension is not ASN.1 NULL value.
	cert.Extensions = []pkix.Extension{{Id: rfc6962.OIDExtensionCTPoison, Critical: false, Value: []byte{0x42, 0x42, 0x42}}}
	return cert
}

func TestIsPrecertificate(t *testing.T) {
	var tests = []struct {
		desc        string
		cert        *x509.Certificate
		wantPrecert bool
		wantErr     bool
	}{
		{
			desc:        "valid-precert",
			cert:        pemToCert(t, testdata.PrecertPEMValid),
			wantPrecert: true,
		},
		{
			desc:        "valid-cert",
			cert:        pemToCert(t, testdata.CACertPEM),
			wantPrecert: false,
		},
		{
			desc:        "remove-exts-from-precert",
			cert:        wipeExtensions(pemToCert(t, testdata.PrecertPEMValid)),
			wantPrecert: false,
		},
		{
			desc:        "poison-non-critical",
			cert:        makePoisonNonCritical(pemToCert(t, testdata.PrecertPEMValid)),
			wantPrecert: false,
			wantErr:     true,
		},
		{
			desc:        "poison-non-null",
			cert:        makePoisonNonNull(pemToCert(t, testdata.PrecertPEMValid)),
			wantPrecert: false,
			wantErr:     true,
		},
		{
			desc:        "nil-cert",
			cert:        nil,
			wantPrecert: false,
			wantErr:     true,
		},
	}

	for _, test := range tests {
		gotPrecert, err := isPrecertificate(test.cert)
		t.Run(test.desc, func(t *testing.T) {
			if err != nil {
				if !test.wantErr {
					t.Errorf("IsPrecertificate()=%v,%v; want %v,nil", gotPrecert, err, test.wantPrecert)
				}
				return
			}
			if test.wantErr {
				t.Errorf("IsPrecertificate()=%v,%v; want _,%v", gotPrecert, err, test.wantErr)
			}
			if gotPrecert != test.wantPrecert {
				t.Errorf("IsPrecertificate()=%v,%v; want %v,nil", gotPrecert, err, test.wantPrecert)
			}
		})
	}
}

func TestValidate(t *testing.T) {
	fakeCARoots := x509util.NewPEMCertPool()
	if parsed, added := fakeCARoots.AppendCertsFromPEMs([]byte(testdata.FakeCACertPEM)); parsed <= 0 || parsed != added {
		t.Fatal("failed to load fake CA root")
	}
	if parsed, added := fakeCARoots.AppendCertsFromPEMs([]byte(testdata.FakeRootCACertPEM)); parsed <= 0 || parsed != added {
		t.Fatal("failed to load fake CA root")
	}
	if parsed, added := fakeCARoots.AppendCertsFromPEMs([]byte(testdata.CACertPEM)); parsed <= 0 || parsed != added {
		t.Fatal("failed to load CA root")
	}
	if parsed, added := fakeCARoots.AppendCertsFromPEMs([]byte(testdata.RealPrecertIntermediatePEM)); parsed <= 0 || parsed != added {
		t.Fatal("failed to load real intermediate")
	}
	cv := chainValidator{
		trustedRoots: fakeCARoots,
	}

	var tests = []struct {
		desc        string
		chain       [][]byte
		wantErr     bool
		wantPathLen int
		modifyCV    func(v *chainValidator)
	}{
		{
			desc:    "missing-intermediate-cert",
			chain:   pemsToDERChain(t, []string{testdata.LeafSignedByFakeIntermediateCertPEM}),
			wantErr: true,
		},
		{
			desc:    "wrong-cert-order",
			chain:   pemsToDERChain(t, []string{testdata.FakeIntermediateCertPEM, testdata.LeafSignedByFakeIntermediateCertPEM}),
			wantErr: true,
		},
		{
			desc:    "unrelated-cert-in-chain",
			chain:   pemsToDERChain(t, []string{testdata.FakeIntermediateCertPEM, testdata.TestCertPEM}),
			wantErr: true,
		},
		{
			desc:    "unrelated-cert-after-chain",
			chain:   pemsToDERChain(t, []string{testdata.LeafSignedByFakeIntermediateCertPEM, testdata.FakeIntermediateCertPEM, testdata.TestCertPEM}),
			wantErr: true,
		},
		{
			desc:        "valid-chain",
			chain:       pemsToDERChain(t, []string{testdata.LeafSignedByFakeIntermediateCertPEM, testdata.FakeIntermediateCertPEM}),
			wantPathLen: 3,
		},
		{
			desc:        "valid-chain-with-policyconstraints",
			chain:       pemsToDERChain(t, []string{testdata.LeafCertPEM, testdata.FakeIntermediateWithPolicyConstraintsCertPEM}),
			wantPathLen: 3,
		},
		{
			desc:        "valid-chain-with-policyconstraints-inc-root",
			chain:       pemsToDERChain(t, []string{testdata.LeafCertPEM, testdata.FakeIntermediateWithPolicyConstraintsCertPEM, testdata.FakeRootCACertPEM}),
			wantPathLen: 3,
		},
		{
			desc:        "valid-chain-with-nameconstraints",
			chain:       pemsToDERChain(t, []string{testdata.LeafCertPEM, testdata.FakeIntermediateWithNameConstraintsCertPEM}),
			wantPathLen: 3,
		},
		{
			desc:        "chain-with-invalid-nameconstraints",
			chain:       pemsToDERChain(t, []string{testdata.LeafCertPEM, testdata.FakeIntermediateWithInvalidNameConstraintsCertPEM}),
			wantPathLen: 3,
		},
		{
			desc:        "chain-of-len-4",
			chain:       pemFileToDERChain(t, "../testdata/subleaf.chain"),
			wantPathLen: 4,
		},
		{
			desc:    "misordered-chain-of-len-4",
			chain:   pemFileToDERChain(t, "../testdata/subleaf.misordered.chain"),
			wantErr: true,
		},
		{
			desc:  "reject-non-existent-ext-id",
			chain: pemsToDERChain(t, []string{testdata.LeafSignedByFakeIntermediateCertPEM, testdata.FakeIntermediateCertPEM}),
			modifyCV: func(v *chainValidator) {
				// reject SubjectKeyIdentifier extension
				v.rejectExtIds = []asn1.ObjectIdentifier{[]int{99, 99, 99, 99}}
			},
			wantPathLen: 3,
		},
		{
			desc:  "reject-non-existent-ext-id-precert",
			chain: pemsToDERChain(t, []string{testdata.PrecertPEMValid}),
			modifyCV: func(v *chainValidator) {
				// reject SubjectKeyIdentifier extension
				v.rejectExtIds = []asn1.ObjectIdentifier{[]int{99, 99, 99, 99}}
			},
			wantPathLen: 2,
		},
		{
			desc:    "reject-ext-id",
			chain:   pemsToDERChain(t, []string{testdata.LeafSignedByFakeIntermediateCertPEM, testdata.FakeIntermediateCertPEM}),
			wantErr: true,
			modifyCV: func(v *chainValidator) {
				// reject ExtendedKeyUsage extension
				v.rejectExtIds = []asn1.ObjectIdentifier{[]int{2, 5, 29, 37}}
			},
		},
		{
			desc:    "reject-ext-id-precert",
			chain:   pemsToDERChain(t, []string{testdata.PrecertPEMValid}),
			wantErr: true,
			modifyCV: func(v *chainValidator) {
				// reject ExtendedKeyUsage extension
				v.rejectExtIds = []asn1.ObjectIdentifier{[]int{2, 5, 29, 37}}
			},
		},
		{
			desc:    "reject-eku-not-present-in-cert",
			chain:   pemsToDERChain(t, []string{testdata.LeafSignedByFakeIntermediateCertPEM, testdata.FakeIntermediateCertPEM}),
			wantErr: true,
			modifyCV: func(v *chainValidator) {
				// reject cert without ExtKeyUsageEmailProtection
				v.extKeyUsages = []x509.ExtKeyUsage{x509.ExtKeyUsageEmailProtection}
			},
		},
		{
			desc:        "allow-eku-present-in-cert",
			chain:       pemsToDERChain(t, []string{testdata.LeafSignedByFakeIntermediateCertPEM, testdata.FakeIntermediateCertPEM}),
			wantPathLen: 3,
			modifyCV: func(v *chainValidator) {
				v.extKeyUsages = []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth}
			},
		},
		{
			desc:    "reject-eku-not-present-in-precert",
			chain:   pemsToDERChain(t, []string{testdata.RealPrecertWithEKUPEM}),
			wantErr: true,
			modifyCV: func(v *chainValidator) {
				// reject cert without ExtKeyUsageEmailProtection
				v.extKeyUsages = []x509.ExtKeyUsage{x509.ExtKeyUsageEmailProtection}
			},
		},
		{
			desc:        "allow-eku-present-in-precert",
			chain:       pemsToDERChain(t, []string{testdata.RealPrecertWithEKUPEM}),
			wantPathLen: 2,
			modifyCV: func(v *chainValidator) {
				v.extKeyUsages = []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth}
			},
		},
	}
	for _, test := range tests {
		t.Run(test.desc, func(t *testing.T) {
			cvv := cv
			if test.modifyCV != nil {
				test.modifyCV(&cvv)
			}
			chain, err := parseChain(test.chain)
			if err != nil {
				t.Fatalf("parseChain()=%v", err)
			}
			gotPath, err := cvv.validate(chain)
			if err != nil {
				if !test.wantErr {
					t.Errorf("chainValidator.validate()=%v,%v; want _,nil", gotPath, err)
				}
				return
			}
			if test.wantErr {
				t.Errorf("chainValidator.validate()=%v,%v; want _,non-nil", gotPath, err)
				return
			}
			if len(gotPath) != test.wantPathLen {
				t.Errorf("|chainValidator.validate()|=%d; want %d", len(gotPath), test.wantPathLen)
				for _, c := range gotPath {
					t.Logf("Subject: %s Issuer: %s", c.Subject, c.Issuer)
				}
			}
		})
	}
}

func TestParseChain(t *testing.T) {
	for _, test := range []struct {
		desc        string
		chain       [][]byte
		wantErr     bool
		wantPathLen int
	}{
		{
			desc:    "empty-chain",
			chain:   [][]byte{},
			wantErr: true,
		},
		{
			desc:    "nil-chain",
			chain:   nil,
			wantErr: true,
		},
	} {
		t.Run(test.desc, func(t *testing.T) {
			_, err := parseChain(test.chain)
			if gotErr := err != nil; gotErr != test.wantErr {
				t.Fatalf("parseChain()=%v; want err %t", err, test.wantErr)
			}
		})
	}
}

func TestNotAfterRange(t *testing.T) {
	fakeCARoots := x509util.NewPEMCertPool()
	if parsed, added := fakeCARoots.AppendCertsFromPEMs([]byte(testdata.FakeCACertPEM)); parsed <= 0 || parsed != added {
		t.Fatal("failed to load fake root")
	}
	cv := chainValidator{
		trustedRoots:  fakeCARoots,
		rejectExpired: false,
	}

	chain := pemsToDERChain(t, []string{testdata.LeafSignedByFakeIntermediateCertPEM, testdata.FakeIntermediateCertPEM})

	var tests = []struct {
		desc          string
		chain         [][]byte
		notAfterStart time.Time
		notAfterLimit time.Time
		wantErr       bool
	}{
		{
			desc:  "valid-chain, no range",
			chain: chain,
		},
		{
			desc:          "valid-chain, valid range",
			chain:         chain,
			notAfterStart: time.Date(2018, 1, 1, 0, 0, 0, 0, time.UTC),
			notAfterLimit: time.Date(2020, 7, 1, 0, 0, 0, 0, time.UTC),
		},
		{
			desc:          "before valid range",
			chain:         chain,
			notAfterStart: time.Date(2020, 1, 1, 0, 0, 0, 0, time.UTC),
			wantErr:       true,
		},
		{
			desc:          "after valid range",
			chain:         chain,
			notAfterLimit: time.Date(1999, 1, 1, 0, 0, 0, 0, time.UTC),
			wantErr:       true,
		},
	}
	for _, test := range tests {
		t.Run(test.desc, func(t *testing.T) {
			if !test.notAfterStart.IsZero() {
				cv.notAfterStart = &test.notAfterStart
			}
			if !test.notAfterLimit.IsZero() {
				cv.notAfterLimit = &test.notAfterLimit
			}
			chain, err := parseChain(test.chain)
			if err != nil {
				t.Fatalf("parseChain()=%v", err)
			}
			gotPath, err := cv.validate(chain)
			if err != nil {
				if !test.wantErr {
					t.Errorf("chainValidate.validate()=%v,%v; want _,nil", gotPath, err)
				}
				return
			}
			if test.wantErr {
				t.Errorf("chainValidate.validate()=%v,%v; want _,non-nil", gotPath, err)
			}
		})
	}
}

func TestRejectExpiredUnexpired(t *testing.T) {
	fakeCARoots := x509util.NewPEMCertPool()
	// Validity period: Jul 11, 2016 - Jul 11, 2017.
	if parsed, added := fakeCARoots.AppendCertsFromPEMs([]byte(testdata.FakeCACertPEM)); parsed <= 0 || parsed != added {
		t.Fatal("failed to load fake root")
	}
	// Validity period: May 13, 2016 - Jul 12, 2019.
	derChain := pemsToDERChain(t, []string{testdata.LeafSignedByFakeIntermediateCertPEM, testdata.FakeIntermediateCertPEM})
	chain, err := parseChain(derChain)
	if err != nil {
		t.Fatalf("parseChain()=%v", err)
	}
	cv := chainValidator{
		trustedRoots: fakeCARoots,
		extKeyUsages: []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
	}
	beforeValidPeriod := time.Date(1999, 1, 1, 0, 0, 0, 0, time.UTC)
	currentValidPeriod := time.Date(2017, 1, 1, 0, 0, 0, 0, time.UTC)
	afterValidPeriod := time.Date(2020, 1, 1, 0, 0, 0, 0, time.UTC)

	for _, tc := range []struct {
		desc            string
		rejectExpired   bool
		rejectUnexpired bool
		now             time.Time
		wantErr         string
	}{
		// No flags: accept anything.
		{
			desc: "no-reject-current",
			now:  currentValidPeriod,
		},
		{
			desc: "no-reject-after",
			now:  afterValidPeriod,
		},
		{
			desc: "no-reject-before",
			now:  beforeValidPeriod,
		},
		// Reject-Expired: only allow currently-valid and not yet valid
		{
			desc:          "reject-expired-current",
			rejectExpired: true,
			now:           currentValidPeriod,
		},
		{
			desc:          "reject-expired-after",
			rejectExpired: true,
			now:           afterValidPeriod,
			wantErr:       "rejecting expired certificate",
		},
		{
			desc:          "reject-expired-before",
			rejectExpired: true,
			now:           beforeValidPeriod,
		},
		// Reject-Unexpired: only allow expired
		{
			desc:            "reject-non-expired-after",
			rejectUnexpired: true,
			now:             afterValidPeriod,
		},
		{
			desc:            "reject-non-expired-before",
			rejectUnexpired: true,
			now:             beforeValidPeriod,
			wantErr:         "rejecting unexpired certificate",
		},
		{
			desc:            "reject-non-expired-current",
			rejectUnexpired: true,
			now:             currentValidPeriod,
			wantErr:         "rejecting unexpired certificate",
		},
		// Reject-Expired AND Reject-Unexpired: nothing allowed
		{
			desc:            "reject-all-after",
			rejectExpired:   true,
			rejectUnexpired: true,
			now:             afterValidPeriod,
			wantErr:         "rejecting expired certificate",
		},
		{
			desc:            "reject-all-before",
			rejectExpired:   true,
			rejectUnexpired: true,
			now:             beforeValidPeriod,
			wantErr:         "rejecting unexpired certificate",
		},
		{
			desc:            "reject-all-current",
			rejectExpired:   true,
			rejectUnexpired: true,
			now:             currentValidPeriod,
			wantErr:         "rejecting unexpired certificate",
		},
	} {
		t.Run(tc.desc, func(t *testing.T) {
			cv.currentTime = tc.now
			cv.rejectExpired = tc.rejectExpired
			cv.rejectUnexpired = tc.rejectUnexpired
			_, err := cv.validate(chain)
			if err != nil {
				if len(tc.wantErr) == 0 {
					t.Errorf("chainValidate.validate()=_,%v; want _,nil", err)
				} else if !strings.Contains(err.Error(), tc.wantErr) {
					t.Errorf("chainValidate.validate()=_,%v; want err containing %q", err, tc.wantErr)
				}
			} else if len(tc.wantErr) != 0 {
				t.Errorf("chainValidate.validate()=_,nil; want err containing %q", tc.wantErr)
			}
		})
	}
}

// Builds a chain of DER-encoded certs.
// Note: ordering is important
func pemsToDERChain(t testing.TB, pemCerts []string) [][]byte {
	t.Helper()
	chain := make([][]byte, 0, len(pemCerts))
	for _, pemCert := range pemCerts {
		cert := pemToCert(t, pemCert)
		chain = append(chain, cert.Raw)
	}
	return chain
}

func pemToCert(t testing.TB, pemData string) *x509.Certificate {
	t.Helper()
	bytes, rest := pem.Decode([]byte(pemData))
	if len(rest) > 0 {
		t.Fatalf("Extra data after PEM: %v", rest)
		return nil
	}

	cert, err := x509.ParseCertificate(bytes.Bytes)
	if err != nil {
		t.Fatalf("x509.ParseCertificate(): %v", err)
	}

	return cert
}

func pemFileToDERChain(t *testing.T, filename string) [][]byte {
	t.Helper()
	rawChain, err := x509util.ReadPossiblePEMFile(filename, "CERTIFICATE")
	if err != nil {
		t.Fatalf("failed to load testdata: %v", err)
	}
	return rawChain
}

// chainFromPEMs builds a chain from a list of PEMs.
func chainFromPEMs(t *testing.T, pems ...string) [][]byte {
	t.Helper()
	var chain [][]byte
	for _, p := range pems {
		pb := []byte(p)
		for len(p) > 0 {
			var block *pem.Block
			block, pb = pem.Decode(pb)
			if block == nil {
				break
			}
			if block.Type != "CERTIFICATE" || len(block.Headers) != 0 {
				continue
			}
			chain = append(chain, block.Bytes)
		}
	}

	return chain
}

// Validate a chain including a pre-issuer.
func TestPreIssuedCert(t *testing.T) {
	rawChain := chainFromPEMs(t, []string{
		testdata.PreCertFromPreIntermediate,
		testdata.PreIntermediateFromRoot,
		testdata.CACertPEM}...)

	roots := x509util.NewPEMCertPool()
	if parsed, added := roots.AppendCertsFromPEMs([]byte(testdata.CACertPEM)); parsed <= 0 || parsed != added {
		t.Fatalf("failed to parse root cert")
	}

	for _, tc := range []struct {
		desc  string
		eku   []x509.ExtKeyUsage
		chain [][]byte
	}{
		{
			desc:  "no EKU specified",
			chain: rawChain,
		}, {
			desc:  "EKU ServerAuth",
			eku:   []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
			chain: rawChain,
		}, {
			// This shouldn't happen with production intermediates, but should
			// be transparently discoverable in CT logs if it ever happens, so
			// we allow them in.
			desc: "pre-intermediate issues end-cert",
			chain: chainFromPEMs(t, []string{
				testdata.CertFromPreIntermediate,
				testdata.PreIntermediateFromRoot,
				testdata.CACertPEM}...),
		},
	} {
		t.Run(tc.desc, func(t *testing.T) {
			cv := chainValidator{
				trustedRoots: roots,
				extKeyUsages: tc.eku,
			}
			chain, err := parseChain(tc.chain)
			if err != nil {
				t.Fatalf("parseChain()=%v", err)
			}
			chain, err = cv.validate(chain)
			if err != nil {
				t.Fatalf("failed to chainValidate.validate: %v", err)
			}
			for i, c := range chain {
				t.Logf("chain[%d] = \n%s", i, c.Subject)
			}
		})
	}
}
