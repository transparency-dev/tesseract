// Copyright 2026 The Tessera authors. All Rights Reserved.
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

package x509util

import (
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/asn1"
	"fmt"
	"slices"
	"time"
)

var (
	oidExtensionNameConstraints     = asn1.ObjectIdentifier{2, 5, 29, 30}
	oidExtensionCertificatePolicies = asn1.ObjectIdentifier{2, 5, 29, 32}
	oidAnyPolicy                    = mustOIDFromInts([]uint64{2, 5, 29, 32, 0})
)

// RelaxedVerificationTime is the time to pass as x509.VerifyOptions.CurrentTime
// when verifying certificates returned by Relax: it is within the validity
// period that Relax sets on every certificate.
var RelaxedVerificationTime = time.UnixMilli(2)

// Relax returns a shallow copy of cert with the parsed fields that
// crypto/x509's Certificate.Verify enforces, and that CT logs must not police,
// neutralised:
//   - unhandled critical extensions, such as the CT precertificate poison,
//   - unknown extended key usages, such as the CT precertificate signing EKU,
//   - name constraints,
//   - validity period, replaced with [1ms, 3ms] after the epoch: verify with
//     RelaxedVerificationTime,
//   - path length constraints,
//   - certificate policies and policy constraints.
//
// cert itself is not modified, and neither is the copy's Raw field: use the
// original certificate for anything other than chain verification.
func Relax(cert *x509.Certificate) *x509.Certificate {
	c := *cert

	c.UnhandledCriticalExtensions = nil
	c.UnknownExtKeyUsage = nil

	// slices.DeleteFunc works in place: clone first so that cert's
	// Extensions are left untouched.
	c.Extensions = slices.DeleteFunc(slices.Clone(cert.Extensions), func(e pkix.Extension) bool {
		return e.Id.Equal(oidExtensionNameConstraints) || e.Id.Equal(oidExtensionCertificatePolicies)
	})

	// Name constraints.
	c.PermittedDNSDomainsCritical = false
	c.PermittedDNSDomains = nil
	c.ExcludedDNSDomains = nil
	c.PermittedIPRanges = nil
	c.ExcludedIPRanges = nil
	c.PermittedEmailAddresses = nil
	c.ExcludedEmailAddresses = nil
	c.PermittedURIDomains = nil
	c.ExcludedURIDomains = nil

	// Validity period.
	c.NotBefore = time.UnixMilli(1)
	c.NotAfter = time.UnixMilli(3)

	// Path length.
	c.MaxPathLen = -1
	c.MaxPathLenZero = false

	// Policies.
	c.Policies = []x509.OID{oidAnyPolicy}
	c.PolicyIdentifiers = nil
	c.PolicyMappings = nil
	c.InhibitAnyPolicy = -1
	c.InhibitAnyPolicyZero = false
	c.InhibitPolicyMapping = -1
	c.InhibitPolicyMappingZero = false
	c.RequireExplicitPolicy = -1
	c.RequireExplicitPolicyZero = false

	return &c
}

func mustOIDFromInts(ints []uint64) x509.OID {
	oid, err := x509.OIDFromInts(ints)
	if err != nil {
		panic(fmt.Sprintf("OIDFromInts(%v): %v", ints, err))
	}
	return oid
}
