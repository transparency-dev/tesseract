package lax509

import (
	"crypto/x509"
)

var (
	oidExtensionSubjectAltName = []int{2, 5, 29, 17}
)

// checkSignatureFrom verifies that the signature on c is a valid signature from parent.
//
// This is a low-level API that performs very limited checks, and not a full
// path verifier. Most users should use [Certificate.Verify] instead.
//
// lax509: this method has been forked to allow SHA-1 based signature algorithms.
// Old signature: func (c *Certificate) CheckSignatureFrom(parent *Certificate) error
func checkSignatureFrom(c *x509.Certificate, parent *x509.Certificate) error {
	// RFC 5280, 4.2.1.9:
	// "If the basic constraints extension is not present in a version 3
	// certificate, or the extension is present but the cA boolean is not
	// asserted, then the certified public key MUST NOT be used to verify
	// certificate signatures."
	if parent.Version == 3 && !parent.BasicConstraintsValid ||
		parent.BasicConstraintsValid && !parent.IsCA {
		return x509.ConstraintViolationError{}
	}

	if parent.KeyUsage != 0 && parent.KeyUsage&x509.KeyUsageCertSign == 0 {
		return x509.ConstraintViolationError{}
	}

	if parent.PublicKeyAlgorithm == x509.UnknownPublicKeyAlgorithm {
		return x509.ErrUnsupportedAlgorithm
	}

	// lax509: Here be dragons. Use parent.CheckSignature instead of
	// checksignature, since parent.CheckSignature allows SHA-1 signature for now.
	// checkSignature --> parent.CheckSignature
	return parent.CheckSignature(c.SignatureAlgorithm, c.RawTBSCertificate, c.Signature)
}
