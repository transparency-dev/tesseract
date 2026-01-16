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
	"bytes"
	"crypto/sha256"
	"crypto/x509"
	"encoding/asn1"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/transparency-dev/tesseract/internal/lax509"
	"github.com/transparency-dev/tesseract/internal/types/rfc6962"
	"github.com/transparency-dev/tesseract/internal/x509util"
	"k8s.io/klog/v2"
)

var stringToKeyUsage = map[string]x509.ExtKeyUsage{
	"Any":                        x509.ExtKeyUsageAny,
	"ServerAuth":                 x509.ExtKeyUsageServerAuth,
	"ClientAuth":                 x509.ExtKeyUsageClientAuth,
	"CodeSigning":                x509.ExtKeyUsageCodeSigning,
	"EmailProtection":            x509.ExtKeyUsageEmailProtection,
	"IPSECEndSystem":             x509.ExtKeyUsageIPSECEndSystem,
	"IPSECTunnel":                x509.ExtKeyUsageIPSECTunnel,
	"IPSECUser":                  x509.ExtKeyUsageIPSECUser,
	"TimeStamping":               x509.ExtKeyUsageTimeStamping,
	"OCSPSigning":                x509.ExtKeyUsageOCSPSigning,
	"MicrosoftServerGatedCrypto": x509.ExtKeyUsageMicrosoftServerGatedCrypto,
	"NetscapeServerGatedCrypto":  x509.ExtKeyUsageNetscapeServerGatedCrypto,
}

// ParseExtKeyUsages parses strings into x509ExtKeyUsage.
// Throws an error if the string does not match with a known key usage.
func ParseExtKeyUsages(kus []string) ([]x509.ExtKeyUsage, error) {
	lExtKeyUsages := make([]x509.ExtKeyUsage, 0, len(kus))
	// Validate the extended key usages list.
	for _, kuStr := range kus {
		if ku, ok := stringToKeyUsage[kuStr]; ok {
			// If "Any" is specified, then we can ignore the entire list and
			// just disable EKU checking.
			if ku == x509.ExtKeyUsageAny {
				klog.Info("Found ExtKeyUsageAny, allowing all EKUs")
				lExtKeyUsages = nil
				break
			}
			lExtKeyUsages = append(lExtKeyUsages, ku)
		} else {
			return nil, fmt.Errorf("unknown extended key usage: %s", kuStr)
		}
	}
	return lExtKeyUsages, nil
}

// ParseOIDs parses strings of dot separated numbers into OIDs.
func ParseOIDs(oids []string) ([]asn1.ObjectIdentifier, error) {
	ret := make([]asn1.ObjectIdentifier, 0, len(oids))
	for _, s := range oids {
		bits := strings.Split(s, ".")
		var oid asn1.ObjectIdentifier
		for _, n := range bits {
			p, err := strconv.Atoi(n)
			if err != nil {
				return nil, err
			}
			oid = append(oid, p)
		}
		ret = append(ret, oid)
	}
	return ret, nil
}

// chainValidator contains various parameters for certificate chain validation.
type chainValidator struct {
	// trustedRoots is a pool of certificates that defines the roots the CT log will accept.
	trustedRoots *x509util.PEMCertPool
	// currentTime is the time used for checking a certificate's validity period
	// against. If it's zero then time.Now() is used. Only for testing.
	// TODO(phboneff): check if I can remove this or align it with the other time definition.
	currentTime time.Time
	// rejectExpired indicates that expired certificates will be rejected.
	rejectExpired bool
	// rejectUnexpired indicates that certificates that are currently valid or not yet valid will be rejected.
	rejectUnexpired bool
	// notAfterStart is the earliest notAfter date which will be accepted.
	// nil means no lower bound on the accepted range.
	notAfterStart *time.Time
	// notAfterLimit defines the cut off point of notAfter dates - only notAfter
	// dates strictly *before* notAfterLimit will be accepted.
	// nil means no upper bound on the accepted range.
	notAfterLimit *time.Time
	// extKeyUsages contains the list of EKUs to use during chain verification.
	extKeyUsages []x509.ExtKeyUsage
	// rejectExtIds contains a list of X.509 extension IDs to reject during chain verification.
	rejectExtIds []asn1.ObjectIdentifier
	// acceptSHA1 specifies whether cert chains using SHA-1 based signing algorithms are allowed.
	acceptSHA1 bool
}

func NewChainValidator(trustedRoots *x509util.PEMCertPool, rejectExpired, rejectUnexpired bool, notAfterStart, notAfterLimit *time.Time, extKeyUsages []x509.ExtKeyUsage, rejectExtIds []asn1.ObjectIdentifier, acceptSHA1 bool) *chainValidator {
	return &chainValidator{
		trustedRoots:    trustedRoots,
		rejectExpired:   rejectExpired,
		rejectUnexpired: rejectUnexpired,
		notAfterStart:   notAfterStart,
		notAfterLimit:   notAfterLimit,
		extKeyUsages:    extKeyUsages,
		rejectExtIds:    rejectExtIds,
		acceptSHA1:      acceptSHA1,
	}
}

// isPrecertificate tests if a certificate is a pre-certificate as defined in CT.
// An error is returned if the CT extension is present but is not ASN.1 NULL as defined
// by the spec.
func isPrecertificate(cert *x509.Certificate) (bool, error) {
	if cert == nil {
		return false, errors.New("nil certificate")
	}

	for _, ext := range cert.Extensions {
		if rfc6962.OIDExtensionCTPoison.Equal(ext.Id) {
			if !ext.Critical || !bytes.Equal(asn1.NullBytes, ext.Value) {
				return false, fmt.Errorf("CT poison ext is not critical or invalid: %v", ext)
			}

			return true, nil
		}
	}

	return false, nil
}

// parseChain parses the provided slice of DER certificates into a slice of Certificate structs.
func parseChain(rawChain [][]byte) ([]*x509.Certificate, error) {
	if len(rawChain) == 0 {
		return nil, errors.New("empty chain")
	}

	// First make sure the certs parse as X.509
	chain := make([]*x509.Certificate, 0, len(rawChain))

	for _, certBytes := range rawChain {
		cert, err := x509.ParseCertificate(certBytes)
		if err != nil {
			return nil, fmt.Errorf("x509.ParseCertificate(): %v", err)
		}

		chain = append(chain, cert)
	}
	return chain, nil
}

// validate ensures that there is a valid path from the end entity certificate in the chain to
// a trusted root cert, possibly using the intermediates supplied in the chain. Then applies the
// RFC requirement that the path must involve all the submitted chain in the order of
// submission.
func (cv chainValidator) validate(chain []*x509.Certificate) ([]*x509.Certificate, error) {
	if len(chain) == 0 {
		return nil, errors.New("empty certificate chain")
	}

	naStart := cv.notAfterStart
	naLimit := cv.notAfterLimit
	cert := chain[0]

	// Check whether the expiry date of the cert is within the acceptable range.
	if naStart != nil && cert.NotAfter.Before(*naStart) {
		return nil, fmt.Errorf("certificate NotAfter (%v) < %v", cert.NotAfter, *naStart)
	}
	if naLimit != nil && !cert.NotAfter.Before(*naLimit) {
		return nil, fmt.Errorf("certificate NotAfter (%v) >= %v", cert.NotAfter, *naLimit)
	}

	now := cv.currentTime
	if now.IsZero() {
		now = time.Now()
	}
	expired := now.After(cert.NotAfter)
	if cv.rejectExpired && expired {
		return nil, errors.New("rejecting expired certificate")
	}
	if cv.rejectUnexpired && !expired {
		return nil, errors.New("rejecting unexpired certificate")
	}

	// Check for unwanted extension types, if required.
	// TODO(al): Refactor CertValidationOpts c'tor to a builder pattern and
	// pre-calc this in there
	if len(cv.rejectExtIds) != 0 {
		badIDs := make(map[string]bool)
		for _, id := range cv.rejectExtIds {
			badIDs[id.String()] = true
		}
		for idx, ext := range cert.Extensions {
			extOid := ext.Id.String()
			if _, ok := badIDs[extOid]; ok {
				return nil, fmt.Errorf("rejecting certificate containing extension %v at index %d", extOid, idx)
			}
		}
	}

	// TODO(al): Refactor CertValidationOpts c'tor to a builder pattern and
	// pre-calc this in there too.
	if len(cv.extKeyUsages) > 0 {
		acceptEKUs := make(map[x509.ExtKeyUsage]bool)
		for _, eku := range cv.extKeyUsages {
			acceptEKUs[eku] = true
		}
		good := false
		for _, certEKU := range cert.ExtKeyUsage {
			if _, ok := acceptEKUs[certEKU]; ok {
				good = true
				break
			}
		}
		if !good {
			return nil, fmt.Errorf("rejecting certificate without EKU in %v", cv.extKeyUsages)
		}
	}

	intermediatePool := x509util.NewPEMCertPool()
	// All but the first cert form part of the intermediate pool
	if len(chain) > 1 {
		intermediatePool.AddCerts(chain[1:])
	}

	// We can now do the verification. Use lax509 with looser verification
	// constraints to:
	//  - allow pre-certificates and chains with pre-issuers
	//  - allow certificate without policing them since this is not CT's responsibility
	// See /internal/lax509/README.md for further information.
	verifyOpts := lax509.VerifyOptions{
		Roots:         cv.trustedRoots.CertPool(),
		Intermediates: intermediatePool.CertPool(),
		KeyUsages:     cv.extKeyUsages,
		AcceptSHA1:    cv.acceptSHA1,
	}

	crtsh := make([]string, len(chain))
	for i, c := range chain {
		crtsh[i] = fmt.Sprintf("https://crt.sh/?sha256=%x", sha256.Sum256(c.Raw))
	}

	verifiedChains, err := lax509.Verify(cert, verifyOpts)
	if err != nil {
		return nil, fmt.Errorf("failed to verify chain: %v: %s", err, strings.Join(crtsh, ", "))
	}

	if len(verifiedChains) == 0 {
		return nil, fmt.Errorf("no path to root found when trying to validate chains: %s", strings.Join(crtsh, ", "))
	}

	// Verify might have found multiple paths to roots. Now we check that we have a path that
	// uses all the certs in the order they were submitted so as to comply with RFC 6962
	// requirements detailed in Section 3.1.
	for _, verifiedChain := range verifiedChains {
		if chainsEquivalent(chain, verifiedChain) {
			return verifiedChain, nil
		}
	}

	return nil, fmt.Errorf("cert chains to an accepted root, but no RFC compliant path found when trying to validate chains: %s", strings.Join(crtsh, ", "))
}

// Validate is used by add-chain and add-pre-chain. It checks that the supplied
// cert is of the correct type, chains to a trusted root and satisties time
// constraints.
// TODO(phbnf): add tests
// TODO(phbnf): merge with validate
func (cv chainValidator) Validate(unverifiedChain []*x509.Certificate, expectingPrecert bool) ([]*x509.Certificate, error) {
	if len(unverifiedChain) == 0 {
		return nil, errors.New("empty chain")
	}
	validPath, err := cv.validate(unverifiedChain)
	if err != nil {
		// We rejected it because the cert failed checks or we could not find a path to a root etc.
		// Lots of possible causes for errors
		return nil, fmt.Errorf("chain failed to validate: %s", err)
	}

	isPrecert, err := isPrecertificate(validPath[0])
	if err != nil {
		return nil, fmt.Errorf("precert test failed: %s", err)
	}

	// The type of the leaf must match the one the handler expects
	if isPrecert != expectingPrecert {
		if expectingPrecert {
			klog.Warningf("Cert (or precert with invalid CT ext) submitted as precert chain: %v", unverifiedChain)
		} else {
			klog.Warningf("Precert (or cert with invalid CT ext) submitted as cert chain: %v", unverifiedChain)
		}
		return nil, fmt.Errorf("cert / precert mismatch: %T", expectingPrecert)
	}

	return validPath, nil
}

func (cv chainValidator) Roots() []*x509.Certificate {
	return cv.trustedRoots.RawCertificates()
}

func chainsEquivalent(inChain []*x509.Certificate, verifiedChain []*x509.Certificate) bool {
	// The verified chain includes a root, but the input chain may or may not include a
	// root (RFC 6962 s4.1/ s4.2 "the last [certificate] is either the root certificate
	// or a certificate that chains to a known root certificate").
	if len(inChain) != len(verifiedChain) && len(inChain) != (len(verifiedChain)-1) {
		return false
	}

	for i, certInChain := range inChain {
		if !certInChain.Equal(verifiedChain[i]) {
			return false
		}
	}
	return true
}
