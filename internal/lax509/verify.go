// Copyright 2011 The Go Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package lax509

import (
	"bytes"
	"crypto"
	"crypto/x509"
	"crypto/x509/pkix"
	"errors"
	"fmt"
	"slices"
	"strings"
)

// UnknownAuthorityError results when the certificate issuer is unknown
type UnknownAuthorityError struct {
	Cert *x509.Certificate
	// hintErr contains an error that may be helpful in determining why an
	// authority wasn't found.
	hintErr error
	// hintCert contains a possible authority certificate that was rejected
	// because of the error in hintErr.
	hintCert *x509.Certificate
}

func (e UnknownAuthorityError) Error() string {
	s := "x509: certificate signed by unknown authority"
	if e.hintErr != nil {
		certName := e.hintCert.Subject.CommonName
		if len(certName) == 0 {
			if len(e.hintCert.Subject.Organization) > 0 {
				certName = e.hintCert.Subject.Organization[0]
			} else {
				certName = "serial:" + e.hintCert.SerialNumber.String()
			}
		}
		s += fmt.Sprintf(" (possibly because of %q while trying to verify candidate authority certificate %q)", e.hintErr, certName)
	}
	return s
}

// errNotParsed is returned when a certificate without ASN.1 contents is
// verified. Platform-specific verification needs the ASN.1 contents.
var errNotParsed = errors.New("x509: missing ASN.1 contents; use ParseCertificate")

// VerifyOptions contains parameters for Certificate.Verify.
type VerifyOptions struct {
	// Intermediates is an optional pool of certificates that are not trust
	// anchors, but can be used to form a chain from the leaf certificate to a
	// root certificate.
	Intermediates *CertPool
	// Roots is the set of trusted root certificates the leaf certificate needs
	// to chain up to. If nil, the system roots or the platform verifier are used.
	Roots *CertPool
	// KeyUsages specifies which Extended Key Usage values are acceptable. A
	// chain is accepted if it allows any of the listed values. An empty list
	// means ExtKeyUsageServerAuth. To accept any key usage, include ExtKeyUsageAny.
	KeyUsages []x509.ExtKeyUsage
	// AcceptSHA1 specifies whether cert chains using SHA-1 based signing algorithms
	// are allowed.
	// CAUTION: This is a temporary solution and it will eventually be removed.
	// DO NOT depend on it.
	AcceptSHA1 bool
}

const (
	leafCertificate = iota
	intermediateCertificate
	rootCertificate
)

// isValid performs validity checks on c given that it is a candidate to append
// to the chain in currentChain.
func isValid(c *x509.Certificate, certType int, currentChain []*x509.Certificate) error {
	// UnhandledCriticalExtension check deleted.
	// Precertificates have the poison extension which the Go library code does
	// not recognize; also the Go library code does not support the standard
	// PolicyConstraints extension (which is required to be marked critical, RFC
	// 5280 s4.2.1.11)
	if len(currentChain) > 0 {
		child := currentChain[len(currentChain)-1]
		if !bytes.Equal(child.RawIssuer, c.RawSubject) {
			return x509.CertificateInvalidError{Cert: c, Reason: x509.NameMismatch, Detail: ""}
		}
	}

	// Expired checks disabled.
	// CT servers handle this at submission time, and accept certificates even
	// if they are expired.

	if certType == intermediateCertificate || certType == rootCertificate {
		if len(currentChain) == 0 {
			return errors.New("x509: internal error: empty chain when appending CA cert")
		}
	}

	// CANotAuthorizedForThisName check deleted.
	// Allow logging of all certificates, even if they have been issued by a CA that
	// is not authorized to issue certs for a given domain.

	// KeyUsage status flags are ignored. From Engineering Security, Peter
	// Gutmann: A European government CA marked its signing certificates as
	// being valid for encryption only, but no-one noticed. Another
	// European CA marked its signature keys as not being valid for
	// signatures. A different CA marked its own trusted root certificate
	// as being invalid for certificate signing. Another national CA
	// distributed a certificate to be used to encrypt data for the
	// country’s tax authority that was marked as only being usable for
	// digital signatures but not for encryption. Yet another CA reversed
	// the order of the bit flags in the keyUsage due to confusion over
	// encoding endianness, essentially setting a random keyUsage in
	// certificates that it issued. Another CA created a self-invalidating
	// certificate by adding a certificate policy statement stipulating
	// that the certificate had to be used strictly as specified in the
	// keyUsage, and a keyUsage containing a flag indicating that the RSA
	// encryption key could only be used for Diffie-Hellman key agreement.

	if certType == intermediateCertificate && (!c.BasicConstraintsValid || !c.IsCA) {
		return x509.CertificateInvalidError{Cert: c, Reason: x509.NotAuthorizedToSign, Detail: ""}
	}

	// TooManyIntermediates check deleted.
	// Path length checks get confused by the presence of an additional
	// pre-issuer intermediate.

	return nil
}

// Verify attempts to verify c by building one or more chains from c to a
// certificate in opts.Roots, using certificates in opts.Intermediates if
// needed. If successful, it returns one or more chains where the first
// element of the chain is c and the last element is from opts.Roots.
//
// If opts.Roots is nil, the platform verifier might be used, and
// verification details might differ from what is described below. If system
// roots are unavailable the returned error will be of type SystemRootsError.
//
// Name constraints in the intermediates will be applied to all names claimed
// in the chain, not just opts.DNSName. Thus it is invalid for a leaf to claim
// example.com if an intermediate doesn't permit it, even if example.com is not
// the name being validated. Note that DirectoryName constraints are not
// supported.
//
// Name constraint validation follows the rules from RFC 5280, with the
// addition that DNS name constraints may use the leading period format
// defined for emails and URIs. When a constraint has a leading period
// it indicates that at least one additional label must be prepended to
// the constrained name to be considered valid.
//
// Extended Key Usage values are enforced nested down a chain, so an intermediate
// or root that enumerates EKUs prevents a leaf from asserting an EKU not in that
// list. (While this is not specified, it is common practice in order to limit
// the types of certificates a CA can issue.)
//
// Certificates that use SHA1WithRSA and ECDSAWithSHA1 signatures are not supported,
// and will not be used to build chains.
//
// Certificates other than c in the returned chains should not be modified.
//
// WARNING: this function doesn't do any revocation checking.
func Verify(c *x509.Certificate, opts VerifyOptions) (chains [][]*x509.Certificate, err error) {
	// Platform-specific verification needs the ASN.1 contents so
	// this makes the behavior consistent across platforms.
	if len(c.Raw) == 0 {
		return nil, errNotParsed
	}
	for i := range opts.Intermediates.len() {
		c, _, err := opts.Intermediates.cert(i)
		if err != nil {
			return nil, fmt.Errorf("crypto/x509: error fetching intermediate: %w", err)
		}
		if len(c.Raw) == 0 {
			return nil, errNotParsed
		}
	}

	// CT server roots MUST not be empty.
	if opts.Roots == nil {
		return nil, fmt.Errorf("opts.Roots == nil, roots MUST be provided")
	}

	err = isValid(c, leafCertificate, nil)
	if err != nil {
		return
	}

	var candidateChains [][]*x509.Certificate
	if opts.Roots.contains(c) {
		candidateChains = [][]*x509.Certificate{{c}}
	} else {
		candidateChains, err = buildChains(c, []*x509.Certificate{c}, nil, &opts)
		if err != nil {
			return nil, err
		}
	}

	if len(opts.KeyUsages) == 0 {
		opts.KeyUsages = []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth}
	}

	if slices.Contains(opts.KeyUsages, x509.ExtKeyUsageAny) {
		// If any key usage is acceptable, no need to check the chain for
		// key usages.
		return candidateChains, nil
	}

	if len(candidateChains) == 0 {
		var details []string
		err = x509.CertificateInvalidError{Cert: c, Reason: x509.NoValidChains, Detail: strings.Join(details, ", ")}
		return nil, err
	}

	return candidateChains, nil
}

func appendToFreshChain(chain []*x509.Certificate, cert *x509.Certificate) []*x509.Certificate {
	n := make([]*x509.Certificate, len(chain)+1)
	copy(n, chain)
	n[len(chain)] = cert
	return n
}

// alreadyInChain checks whether a candidate certificate is present in a chain.
// Rather than doing a direct byte for byte equivalency check, we check if the
// subject, public key, and SAN, if present, are equal. This prevents loops that
// are created by mutual cross-signatures, or other cross-signature bridge
// oddities.
func alreadyInChain(candidate *x509.Certificate, chain []*x509.Certificate) bool {
	type pubKeyEqual interface {
		Equal(crypto.PublicKey) bool
	}

	var candidateSAN *pkix.Extension
	for _, ext := range candidate.Extensions {
		if ext.Id.Equal(oidExtensionSubjectAltName) {
			candidateSAN = &ext
			break
		}
	}

	for _, cert := range chain {
		if !bytes.Equal(candidate.RawSubject, cert.RawSubject) {
			continue
		}
		if !candidate.PublicKey.(pubKeyEqual).Equal(cert.PublicKey) {
			continue
		}
		var certSAN *pkix.Extension
		for _, ext := range cert.Extensions {
			if ext.Id.Equal(oidExtensionSubjectAltName) {
				certSAN = &ext
				break
			}
		}
		if candidateSAN == nil && certSAN == nil {
			return true
		} else if candidateSAN == nil || certSAN == nil {
			return false
		}
		if bytes.Equal(candidateSAN.Value, certSAN.Value) {
			return true
		}
	}
	return false
}

// maxChainSignatureChecks is the maximum number of CheckSignatureFrom calls
// that an invocation of buildChains will (transitively) make. Most chains are
// less than 15 certificates long, so this leaves space for multiple chains and
// for failed checks due to different intermediates having the same Subject.
const maxChainSignatureChecks = 100

func buildChains(c *x509.Certificate, currentChain []*x509.Certificate, sigChecks *int, opts *VerifyOptions) (chains [][]*x509.Certificate, err error) {
	var (
		hintErr  error
		hintCert *x509.Certificate
	)

	considerCandidate := func(certType int, candidate potentialParent) {
		if candidate.cert.PublicKey == nil || alreadyInChain(candidate.cert, currentChain) {
			return
		}

		if sigChecks == nil {
			sigChecks = new(int)
		}
		*sigChecks++
		if *sigChecks > maxChainSignatureChecks {
			err = errors.New("x509: signature check attempts limit reached while verifying certificate chain")
			return
		}

		if opts.AcceptSHA1 {
			err = checkSignatureFrom(c, candidate.cert)
		} else {
			err = c.CheckSignatureFrom(candidate.cert)
		}
		if err != nil {
			if hintErr == nil {
				hintErr = err
				hintCert = candidate.cert
			}
			return
		}

		err = isValid(candidate.cert, certType, currentChain)
		if err != nil {
			if hintErr == nil {
				hintErr = err
				hintCert = candidate.cert
			}
			return
		}

		if candidate.constraint != nil {
			if err := candidate.constraint(currentChain); err != nil {
				if hintErr == nil {
					hintErr = err
					hintCert = candidate.cert
				}
				return
			}
		}

		switch certType {
		case rootCertificate:
			chains = append(chains, appendToFreshChain(currentChain, candidate.cert))
		case intermediateCertificate:
			var childChains [][]*x509.Certificate
			childChains, err = buildChains(candidate.cert, appendToFreshChain(currentChain, candidate.cert), sigChecks, opts)
			chains = append(chains, childChains...)
		}
	}

	for _, root := range opts.Roots.findPotentialParents(c) {
		considerCandidate(rootCertificate, root)
	}
	for _, intermediate := range opts.Intermediates.findPotentialParents(c) {
		considerCandidate(intermediateCertificate, intermediate)
	}

	if len(chains) > 0 {
		err = nil
	}
	if len(chains) == 0 && err == nil {
		err = UnknownAuthorityError{c, hintErr, hintCert}
	}

	return
}
