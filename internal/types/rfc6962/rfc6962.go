package rfc6962

import (
	"crypto/sha256"
	"crypto/x509"
	"encoding/asn1"
	"encoding/base64"
	"fmt"

	"github.com/transparency-dev/tesseract/internal/types/tls"
)

///////////////////////////////////////////////////////////////////////////////
// The following structures represent those outlined in RFC6962; any section
// numbers mentioned refer to that RFC.
///////////////////////////////////////////////////////////////////////////////

// LogEntryType represents the LogEntryType enum from section 3.1:
//
//	enum { x509_entry(0), precert_entry(1), (65535) } LogEntryType;
type LogEntryType tls.Enum // tls:"maxval:65535"

// LogEntryType constants from section 3.1.
const (
	X509LogEntryType    LogEntryType = 0
	PrecertLogEntryType LogEntryType = 1
)

func (e LogEntryType) String() string {
	switch e {
	case X509LogEntryType:
		return "X509LogEntryType"
	case PrecertLogEntryType:
		return "PrecertLogEntryType"
	default:
		return fmt.Sprintf("UnknownEntryType(%d)", e)
	}
}

// RFC6962 section 2.1 requires a prefix byte on hash inputs for second preimage resistance.
const (
	TreeLeafPrefix = byte(0x00)
	TreeNodePrefix = byte(0x01)
)

// Defined or referenced in RFC 6962 s3.1.
var (
	OIDExtAuthorityKeyId                  = asn1.ObjectIdentifier{2, 5, 29, 35}
	OIDExtKeyUsage                        = asn1.ObjectIdentifier{2, 5, 29, 37}
	OIDExtensionCTPoison                  = asn1.ObjectIdentifier{1, 3, 6, 1, 4, 1, 11129, 2, 4, 3}
	OIDExtKeyUsageCertificateTransparency = asn1.ObjectIdentifier{1, 3, 6, 1, 4, 1, 11129, 2, 4, 4}
)

// MerkleLeafType represents the MerkleLeafType enum from section 3.4:
//
//	enum { timestamped_entry(0), (255) } MerkleLeafType;
type MerkleLeafType tls.Enum // tls:"maxval:255"

// TimestampedEntryLeafType is the only defined MerkleLeafType constant from section 3.4.
const TimestampedEntryLeafType MerkleLeafType = 0 // Entry type for an SCT

func (m MerkleLeafType) String() string {
	switch m {
	case TimestampedEntryLeafType:
		return "TimestampedEntryLeafType"
	default:
		return fmt.Sprintf("UnknownLeafType(%d)", m)
	}
}

// Version represents the Version enum from section 3.2:
//
//	enum { v1(0), (255) } Version;
type Version tls.Enum // tls:"maxval:255"

// CT Version constants from section 3.2.
const (
	V1 Version = 0
)

func (v Version) String() string {
	switch v {
	case V1:
		return "V1"
	default:
		return fmt.Sprintf("UnknownVersion(%d)", v)
	}
}

// SignatureType differentiates STH signatures from SCT signatures, see section 3.2.
//
//	enum { certificate_timestamp(0), tree_hash(1), (255) } SignatureType;
type SignatureType tls.Enum // tls:"maxval:255"

// SignatureType constants from section 3.2.
const (
	CertificateTimestampSignatureType SignatureType = 0
	TreeHashSignatureType             SignatureType = 1
)

func (st SignatureType) String() string {
	switch st {
	case CertificateTimestampSignatureType:
		return "CertificateTimestamp"
	case TreeHashSignatureType:
		return "TreeHash"
	default:
		return fmt.Sprintf("UnknownSignatureType(%d)", st)
	}
}

// ASN1Cert type for holding the raw DER bytes of an ASN.1 Certificate
// (section 3.1).
type ASN1Cert struct {
	Data []byte `tls:"minlen:1,maxlen:16777215"`
}

// LogID holds the hash of the Log's public key (section 3.2).
type LogID struct {
	KeyID [sha256.Size]byte
}

// PreCert represents a Precertificate (section 3.2).
type PreCert struct {
	IssuerKeyHash  [sha256.Size]byte
	TBSCertificate []byte `tls:"minlen:1,maxlen:16777215"` // DER-encoded TBSCertificate
}

// CTExtensions is a representation of the raw bytes of any CtExtension
// structure (see section 3.2).
// nolint: revive
type CTExtensions []byte // tls:"minlen:0,maxlen:65535"`

// MerkleTreeNode represents an internal node in the CT tree.
type MerkleTreeNode []byte

// DigitallySigned is a local alias for tls.DigitallySigned so that we can
// attach a Base64String() method.
type DigitallySigned tls.DigitallySigned

// Base64String returns the base64 representation of the DigitallySigned struct.
func (d DigitallySigned) Base64String() (string, error) {
	b, err := tls.Marshal(d)
	if err != nil {
		return "", err
	}
	return base64.StdEncoding.EncodeToString(b), nil
}

// RawLogEntry represents the (TLS-parsed) contents of an entry in a CT log.
type RawLogEntry struct {
	// Index is a position of the entry in the log.
	Index int64
	// Leaf is a parsed Merkle leaf hash input.
	Leaf MerkleTreeLeaf
	// Cert is:
	// - A certificate if Leaf.TimestampedEntry.EntryType is X509LogEntryType.
	// - A precertificate if Leaf.TimestampedEntry.EntryType is
	//   PrecertLogEntryType, in the form of a DER-encoded Certificate as
	//   originally added (which includes the poison extension and a signature
	//   generated over the pre-cert by the pre-cert issuer).
	// - Empty otherwise.
	Cert ASN1Cert
	// Chain is the issuing certificate chain starting with the issuer of Cert,
	// or an empty slice if Cert is empty.
	Chain []ASN1Cert
}

// LogEntry represents the (parsed) contents of an entry in a CT log.  This is described
// in section 3.1, but note that this structure does *not* match the TLS structure
// defined there (the TLS structure is never used directly in RFC6962).
type LogEntry struct {
	Index int64
	Leaf  MerkleTreeLeaf
	// Exactly one of the following three fields should be non-empty.
	X509Cert *x509.Certificate // Parsed X.509 certificate
	Precert  *Precertificate   // Extracted precertificate
	JSONData []byte

	// Chain holds the issuing certificate chain, starting with the
	// issuer of the leaf certificate / pre-certificate.
	Chain []ASN1Cert
}

// JSONDataEntry holds arbitrary data.
type JSONDataEntry struct {
	Data []byte `tls:"minlen:0,maxlen:1677215"`
}

// SHA256Hash represents the output from the SHA256 hash function.
type SHA256Hash [sha256.Size]byte

// Base64String returns the base64 representation of this SHA256Hash.
func (s SHA256Hash) Base64String() string {
	return base64.StdEncoding.EncodeToString(s[:])
}

// SignedTreeHead represents the structure returned by the get-sth CT method
// after base64 decoding; see sections 3.5 and 4.3.
type SignedTreeHead struct {
	Version           Version         // The version of the protocol to which the STH conforms
	TreeSize          uint64          // The number of entries in the new tree
	Timestamp         uint64          // The time at which the STH was created
	SHA256RootHash    SHA256Hash      // The root hash of the log's Merkle tree
	TreeHeadSignature DigitallySigned // Log's signature over a TLS-encoded TreeHeadSignature
	LogID             SHA256Hash      // The SHA256 hash of the log's public key
}

func (s SignedTreeHead) String() string {
	sigStr, err := s.TreeHeadSignature.Base64String()
	if err != nil {
		sigStr = tls.DigitallySigned(s.TreeHeadSignature).String()
	}

	// If the LogID field in the SignedTreeHead is empty, don't include it in
	// the string.
	var logIDStr string
	if id, empty := s.LogID, (SHA256Hash{}); id != empty {
		logIDStr = fmt.Sprintf("LogID:%s, ", id.Base64String())
	}

	return fmt.Sprintf("{%sTreeSize:%d, Timestamp:%d, SHA256RootHash:%q, TreeHeadSignature:%q}",
		logIDStr, s.TreeSize, s.Timestamp, s.SHA256RootHash.Base64String(), sigStr)
}

// TreeHeadSignature holds the data over which the signature in an STH is
// generated; see section 3.5
type TreeHeadSignature struct {
	Version        Version       `tls:"maxval:255"`
	SignatureType  SignatureType `tls:"maxval:255"` // == TreeHashSignatureType
	Timestamp      uint64
	TreeSize       uint64
	SHA256RootHash SHA256Hash
}

// SignedCertificateTimestamp represents the structure returned by the
// add-chain and add-pre-chain methods after base64 decoding; see sections
// 3.2, 4.1 and 4.2.
type SignedCertificateTimestamp struct {
	SCTVersion Version `tls:"maxval:255"`
	LogID      LogID
	Timestamp  uint64
	Extensions CTExtensions    `tls:"minlen:0,maxlen:65535"`
	Signature  DigitallySigned // Signature over TLS-encoded CertificateTimestamp
}

// CertificateTimestamp is the collection of data that the signature in an
// SCT is over; see section 3.2.
type CertificateTimestamp struct {
	SCTVersion    Version       `tls:"maxval:255"`
	SignatureType SignatureType `tls:"maxval:255"`
	Timestamp     uint64
	EntryType     LogEntryType   `tls:"maxval:65535"`
	X509Entry     *ASN1Cert      `tls:"selector:EntryType,val:0"`
	PrecertEntry  *PreCert       `tls:"selector:EntryType,val:1"`
	JSONEntry     *JSONDataEntry `tls:"selector:EntryType,val:32768"`
	Extensions    CTExtensions   `tls:"minlen:0,maxlen:65535"`
}

func (s SignedCertificateTimestamp) String() string {
	return fmt.Sprintf("{Version:%d LogId:%s Timestamp:%d Extensions:'%s' Signature:%v}", s.SCTVersion,
		base64.StdEncoding.EncodeToString(s.LogID.KeyID[:]),
		s.Timestamp,
		s.Extensions,
		s.Signature)
}

// TimestampedEntry is part of the MerkleTreeLeaf structure; see section 3.4.
type TimestampedEntry struct {
	Timestamp    uint64
	EntryType    LogEntryType   `tls:"maxval:65535"`
	X509Entry    *ASN1Cert      `tls:"selector:EntryType,val:0"`
	PrecertEntry *PreCert       `tls:"selector:EntryType,val:1"`
	JSONEntry    *JSONDataEntry `tls:"selector:EntryType,val:32768"`
	Extensions   CTExtensions   `tls:"minlen:0,maxlen:65535"`
}

// MerkleTreeLeaf represents the deserialized structure of the hash input for the
// leaves of a log's Merkle tree; see section 3.4.
type MerkleTreeLeaf struct {
	Version          Version           `tls:"maxval:255"`
	LeafType         MerkleLeafType    `tls:"maxval:255"`
	TimestampedEntry *TimestampedEntry `tls:"selector:LeafType,val:0"`
}

// Precertificate represents the parsed CT Precertificate structure.
type Precertificate struct {
	// DER-encoded pre-certificate as originally added, which includes a
	// poison extension and a signature generated over the pre-cert by
	// the pre-cert issuer (which might differ from the issuer of the final
	// cert, see RFC6962 s3.1).
	Submitted ASN1Cert
	// SHA256 hash of the issuing key
	IssuerKeyHash [sha256.Size]byte
	// Parsed TBSCertificate structure, held in an x509.Certificate for convenience.
	TBSCertificate *x509.Certificate
}

// APIEndpoint is a string that represents one of the Certificate Transparency
// Log API endpoints.
type APIEndpoint string

// Certificate Transparency Log API endpoints; see section 4.
// WARNING: Should match the URI paths without the "/ct/v1/" prefix.  If
// changing these constants, may need to change those too.
const (
	AddChainStr    APIEndpoint = "add-chain"
	AddPreChainStr APIEndpoint = "add-pre-chain"
	GetRootsStr    APIEndpoint = "get-roots"
)

// URI paths for Log requests; see section 4.
// WARNING: Should match the API endpoints, with the "/ct/v1/" prefix.  If
// changing these constants, may need to change those too.
const (
	AddChainPath    = "/ct/v1/add-chain"
	AddPreChainPath = "/ct/v1/add-pre-chain"
	GetRootsPath    = "/ct/v1/get-roots"
)

// AddChainRequest represents the JSON request body sent to the add-chain and
// add-pre-chain POST methods from sections 4.1 and 4.2.
type AddChainRequest struct {
	Chain [][]byte `json:"chain"`
}

// AddChainResponse represents the JSON response to the add-chain and
// add-pre-chain POST methods.
// An SCT represents a Log's promise to integrate a [pre-]certificate into the
// log within a defined period of time.
type AddChainResponse struct {
	SCTVersion Version `json:"sct_version"` // SCT structure version
	ID         []byte  `json:"id"`          // Log ID
	Timestamp  uint64  `json:"timestamp"`   // Timestamp of issuance
	Extensions string  `json:"extensions"`  // Holder for any CT extensions
	Signature  []byte  `json:"signature"`   // Log signature for this SCT
}

// GetRootsResponse represents the JSON response to the get-roots GET method from section 4.7.
type GetRootsResponse struct {
	Certificates []string `json:"certificates"`
}
