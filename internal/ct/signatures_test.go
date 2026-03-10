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
	"crypto/ecdsa"
	"crypto/sha256"
	"crypto/x509"
	"encoding/hex"
	"encoding/pem"
	"testing"
	"time"

	"github.com/kylelemons/godebug/pretty"
	"github.com/transparency-dev/tesseract/internal/testdata"
	"github.com/transparency-dev/tesseract/internal/types/rfc6962"
	"github.com/transparency-dev/tesseract/internal/types/tls"
	"github.com/transparency-dev/tesseract/internal/x509util"
)

var (
	fixedTime       = time.Date(2017, 9, 7, 12, 15, 23, 0, time.UTC)
	fixedTimeMillis = uint64(fixedTime.UnixNano() / nanosPerMilli)
	demoLogID       = [32]byte{19, 56, 222, 93, 229, 36, 102, 128, 227, 214, 3, 121, 93, 175, 126, 236, 97, 217, 34, 32, 40, 233, 98, 27, 46, 179, 164, 251, 84, 10, 60, 57}
	fakeIndex       = uint8(8)
	fakeExtension   = []byte{0, 0, 5, 0, 0, 0, 0, fakeIndex}
	fakeSignature   = []byte("signed")
)

const (
	defaultSCTLogIDString          string = "iamapublickeyshatwofivesixdigest"
	defaultSCTTimestamp            uint64 = 1234
	defaultSCTSignatureString      string = "\x04\x03\x00\x09signature"
	defaultCertifictateString      string = "certificate"
	defaultPrecertIssuerHashString string = "iamapublickeyshatwofivesixdigest"
	defaultPrecertTBSString        string = "tbs"

	// TODO(phboneff): add extension and regenerate data
	defaultCertificateSCTSignatureInputHexString string =
	// version, 1 byte
	"00" +
		// signature type, 1 byte
		"00" +
		// timestamp, 8 bytes
		"00000000000004d2" +
		// entry type, 2 bytes
		"0000" +
		// leaf certificate length, 3 bytes
		"00000b" +
		// leaf certificate, 11 bytes
		"6365727469666963617465" +
		// extensions length, 2 bytes
		"0000" +
		// extensions, 0 bytes
		""

	defaultPrecertSCTSignatureInputHexString string =
	// version, 1 byte
	"00" +
		// signature type, 1 byte
		"00" +
		// timestamp, 8 bytes
		"00000000000004d2" +
		// entry type, 2 bytes
		"0001" +
		// issuer key hash, 32 bytes
		"69616d617075626c69636b657973686174776f66697665736978646967657374" +
		// tbs certificate length, 3 bytes
		"000003" +
		// tbs certificate, 3 bytes
		"746273" +
		// extensions length, 2 bytes
		"0000" +
		// extensions, 0 bytes
		""

	defaultSTHSignedHexString string =
	// version, 1 byte
	"00" +
		// signature type, 1 byte
		"01" +
		// timestamp, 8 bytes
		"0000000000000929" +
		// tree size, 8 bytes
		"0000000000000006" +
		// root hash, 32 bytes
		"696d757374626565786163746c7974686972747974776f62797465736c6f6e67"
)

func defaultSCTLogID() rfc6962.LogID {
	var id rfc6962.LogID
	copy(id.KeyID[:], defaultSCTLogIDString)
	return id
}

func defaultSCTSignature() rfc6962.DigitallySigned {
	var ds rfc6962.DigitallySigned
	if _, err := tls.Unmarshal([]byte(defaultSCTSignatureString), &ds); err != nil {
		panic(err)
	}
	return ds
}

func defaultSCT() rfc6962.SignedCertificateTimestamp {
	return rfc6962.SignedCertificateTimestamp{
		SCTVersion: rfc6962.V1,
		LogID:      defaultSCTLogID(),
		Timestamp:  defaultSCTTimestamp,
		Extensions: []byte{},
		Signature:  defaultSCTSignature()}
}

func defaultCertificate() []byte {
	return []byte(defaultCertifictateString)
}

func defaultCertificateSCTSignatureInput(t *testing.T) []byte {
	t.Helper()
	r, err := hex.DecodeString(defaultCertificateSCTSignatureInputHexString)
	if err != nil {
		t.Fatalf("failed to decode defaultCertificateSCTSignatureInputHexString: %v", err)
	}
	return r
}

func defaultCertificateLogEntry() rfc6962.LogEntry {
	return rfc6962.LogEntry{
		Index: 1,
		Leaf: rfc6962.MerkleTreeLeaf{
			Version:  rfc6962.V1,
			LeafType: rfc6962.TimestampedEntryLeafType,
			TimestampedEntry: &rfc6962.TimestampedEntry{
				Timestamp: defaultSCTTimestamp,
				EntryType: rfc6962.X509LogEntryType,
				X509Entry: &rfc6962.ASN1Cert{Data: defaultCertificate()},
			},
		},
	}
}

func defaultPrecertSCTSignatureInput(t *testing.T) []byte {
	t.Helper()
	r, err := hex.DecodeString(defaultPrecertSCTSignatureInputHexString)
	if err != nil {
		t.Fatalf("failed to decode defaultPrecertSCTSignatureInputHexString: %v", err)
	}
	return r
}

func defaultPrecertTBS() []byte {
	return []byte(defaultPrecertTBSString)
}

func defaultPrecertIssuerHash() [32]byte {
	var b [32]byte
	copy(b[:], []byte(defaultPrecertIssuerHashString))
	return b
}

func defaultPrecertLogEntry() rfc6962.LogEntry {
	return rfc6962.LogEntry{
		Index: 1,
		Leaf: rfc6962.MerkleTreeLeaf{
			Version:  rfc6962.V1,
			LeafType: rfc6962.TimestampedEntryLeafType,
			TimestampedEntry: &rfc6962.TimestampedEntry{
				Timestamp: defaultSCTTimestamp,
				EntryType: rfc6962.PrecertLogEntryType,
				PrecertEntry: &rfc6962.PreCert{
					IssuerKeyHash:  defaultPrecertIssuerHash(),
					TBSCertificate: defaultPrecertTBS(),
				},
			},
		},
	}
}

func defaultSTH() rfc6962.SignedTreeHead {
	var root rfc6962.SHA256Hash
	copy(root[:], "imustbeexactlythirtytwobyteslong")
	return rfc6962.SignedTreeHead{
		TreeSize:       6,
		Timestamp:      2345,
		SHA256RootHash: root,
		TreeHeadSignature: rfc6962.DigitallySigned{
			Algorithm: tls.SignatureAndHashAlgorithm{
				Hash:      tls.SHA256,
				Signature: tls.ECDSA},
			Signature: []byte("tree_signature"),
		},
	}
}

func mustDehex(t *testing.T, h string) []byte {
	t.Helper()
	r, err := hex.DecodeString(h)
	if err != nil {
		t.Fatalf("Failed to decode hex string (%s): %v", h, err)
	}
	return r
}

func TestSerializeV1SCTSignatureInputForCertificateKAT(t *testing.T) {
	serialized, err := serializeSCTSignatureInput(defaultSCT(), defaultCertificateLogEntry())
	if err != nil {
		t.Fatalf("Failed to serialize SCT for signing: %v", err)
	}
	if !bytes.Equal(serialized, defaultCertificateSCTSignatureInput(t)) {
		t.Fatalf("Serialized certificate signature input doesn't match expected answer:\n%v\n%v", serialized, defaultCertificateSCTSignatureInput(t))
	}
}

func TestSerializeV1SCTSignatureInputForPrecertKAT(t *testing.T) {
	serialized, err := serializeSCTSignatureInput(defaultSCT(), defaultPrecertLogEntry())
	if err != nil {
		t.Fatalf("Failed to serialize SCT for signing: %v", err)
	}
	if !bytes.Equal(serialized, defaultPrecertSCTSignatureInput(t)) {
		t.Fatalf("Serialized precertificate signature input doesn't match expected answer:\n%v\n%v", serialized, defaultPrecertSCTSignatureInput(t))
	}
}

func TestSerializeV1STHSignatureKAT(t *testing.T) {
	b, err := serializeSTHSignatureInput(defaultSTH())
	if err != nil {
		t.Fatalf("Failed to serialize defaultSTH: %v", err)
	}
	if !bytes.Equal(b, mustDehex(t, defaultSTHSignedHexString)) {
		t.Fatalf("defaultSTH incorrectly serialized, expected:\n%v\ngot:\n%v", mustDehex(t, defaultSTHSignedHexString), b)
	}
}

func TestBuildV1MerkleTreeLeafForCert(t *testing.T) {
	cert, err := x509util.CertificateFromPEM([]byte(testdata.LeafSignedByFakeIntermediateCertPEM))
	if err != nil {
		t.Fatalf("failed to set up test cert: %v", err)
	}

	sctSigner, err := setupSCTSigner(fakeSignature)
	if err != nil {
		t.Fatalf("could not create signer: %v", err)
	}

	// Use the same cert as the issuer for convenience.
	entry, err := x509util.EntryFromChain([]*x509.Certificate{cert, cert}, false, fixedTimeMillis)
	if err != nil {
		t.Fatalf("buildV1MerkleTreeLeafForCert()=nil,%v; want _,nil", err)
	}
	defer x509util.ReturnEntry(entry)

	var leaf rfc6962.MerkleTreeLeaf
	leafValue := entry.MerkleTreeLeaf(uint64(fakeIndex))
	if rest, err := tls.Unmarshal(leafValue, &leaf); err != nil {
		t.Fatalf("failed to reconstruct MerkleTreeLeaf: %s", err)
	} else if len(rest) > 0 {
		t.Fatalf("extra data (%d bytes) on reconstructing MerkleTreeLeaf", len(rest))
	}
	got, err := sctSigner.Sign(&leaf)
	if err != nil {
		t.Fatalf("buildV1SCT()=nil,%v; want _,nil", err)
	}

	expected := rfc6962.SignedCertificateTimestamp{
		SCTVersion: 0,
		LogID:      rfc6962.LogID{KeyID: demoLogID},
		Timestamp:  fixedTimeMillis,
		Extensions: rfc6962.CTExtensions(fakeExtension),
		Signature: rfc6962.DigitallySigned{
			Algorithm: tls.SignatureAndHashAlgorithm{
				Hash:      tls.SHA256,
				Signature: tls.ECDSA},
			Signature: fakeSignature,
		},
	}

	if diff := pretty.Compare(*got, expected); diff != "" {
		t.Fatalf("Mismatched SCT (cert), diff:\n%v", diff)
	}

	// Additional checks that the MerkleTreeLeaf we built is correct
	if got, want := leaf.Version, rfc6962.V1; got != want {
		t.Fatalf("Got a %v leaf, expected a %v leaf", got, want)
	}
	if got, want := leaf.LeafType, rfc6962.TimestampedEntryLeafType; got != want {
		t.Fatalf("Got leaf type %v, expected %v", got, want)
	}
	if got, want := leaf.TimestampedEntry.EntryType, rfc6962.X509LogEntryType; got != want {
		t.Fatalf("Got entry type %v, expected %v", got, want)
	}
	if got, want := leaf.TimestampedEntry.Timestamp, got.Timestamp; got != want {
		t.Fatalf("Entry / sct timestamp mismatch; got %v, expected %v", got, want)
	}
	if got, want := leaf.TimestampedEntry.X509Entry.Data, cert.Raw; !bytes.Equal(got, want) {
		t.Fatalf("Cert bytes mismatch, got %x, expected %x", got, want)
	}
}

func TestSignV1SCTForPrecertificate(t *testing.T) {
	cert, err := x509util.CertificateFromPEM([]byte(testdata.PrecertPEMValid))
	if err != nil {
		t.Fatalf("failed to set up test precert: %v", err)
	}

	sctSigner, err := setupSCTSigner(fakeSignature)
	if err != nil {
		t.Fatalf("could not create signer: %v", err)
	}

	// Use the same cert as the issuer for convenience.
	entry, err := x509util.EntryFromChain([]*x509.Certificate{cert, cert}, true, fixedTimeMillis)
	if err != nil {
		t.Fatalf("buildV1MerkleTreeLeafForCert()=nil,%v; want _,nil", err)
	}
	defer x509util.ReturnEntry(entry)

	var leaf rfc6962.MerkleTreeLeaf
	leafValue := entry.MerkleTreeLeaf(uint64(fakeIndex))
	if rest, err := tls.Unmarshal(leafValue, &leaf); err != nil {
		t.Fatalf("failed to reconstruct MerkleTreeLeaf: %s", err)
	} else if len(rest) > 0 {
		t.Fatalf("extra data (%d bytes) on reconstructing MerkleTreeLeaf", len(rest))
	}

	got, err := sctSigner.Sign(&leaf)
	if err != nil {
		t.Fatalf("buildV1SCT()=nil,%v; want _,nil", err)
	}

	expected := rfc6962.SignedCertificateTimestamp{
		SCTVersion: 0,
		LogID:      rfc6962.LogID{KeyID: demoLogID},
		Timestamp:  fixedTimeMillis,
		Extensions: rfc6962.CTExtensions(fakeExtension),
		Signature: rfc6962.DigitallySigned{
			Algorithm: tls.SignatureAndHashAlgorithm{
				Hash:      tls.SHA256,
				Signature: tls.ECDSA},
			Signature: fakeSignature}}

	if diff := pretty.Compare(*got, expected); diff != "" {
		t.Fatalf("Mismatched SCT (precert), diff:\n%v", diff)
	}

	// Additional checks that the MerkleTreeLeaf we built is correct
	if got, want := leaf.Version, rfc6962.V1; got != want {
		t.Fatalf("Got a %v leaf, expected a %v leaf", got, want)
	}
	if got, want := leaf.LeafType, rfc6962.TimestampedEntryLeafType; got != want {
		t.Fatalf("Got leaf type %v, expected %v", got, want)
	}
	if got, want := leaf.TimestampedEntry.EntryType, rfc6962.PrecertLogEntryType; got != want {
		t.Fatalf("Got entry type %v, expected %v", got, want)
	}
	if got, want := got.Timestamp, leaf.TimestampedEntry.Timestamp; got != want {
		t.Fatalf("Entry / sct timestamp mismatch; got %v, expected %v", got, want)
	}
	keyHash := sha256.Sum256(cert.RawSubjectPublicKeyInfo)
	if got, want := keyHash[:], leaf.TimestampedEntry.PrecertEntry.IssuerKeyHash[:]; !bytes.Equal(got, want) {
		t.Fatalf("Issuer key hash bytes mismatch, got %v, expected %v", got, want)
	}
	defangedTBS, _ := x509util.RemoveCTPoison(cert.RawTBSCertificate)
	if got, want := leaf.TimestampedEntry.PrecertEntry.TBSCertificate, defangedTBS; !bytes.Equal(got, want) {
		t.Fatalf("TBS cert mismatch, got %v, expected %v", got, want)
	}
}

func TestGetCTLogID(t *testing.T) {
	block, _ := pem.Decode([]byte(testdata.DemoPublicKey))
	pk, err := x509.ParsePKIXPublicKey(block.Bytes)
	if err != nil {
		t.Fatalf("unexpected error loading public key: %v", err)
	}

	got, err := getCTLogID(pk)
	if err != nil {
		t.Fatalf("error getting logid: %v", err)
	}

	if want := demoLogID; got != want {
		t.Errorf("logID: \n%v want \n%v", got, want)
	}
}

// Creates a fake signer for use in interaction tests.
// It will always return fakeSig when asked to sign something.
func setupSCTSigner(fakeSig []byte) (*sctSigner, error) {
	block, _ := pem.Decode([]byte(testdata.DemoPublicKey))
	key, err := x509.ParsePKIXPublicKey(block.Bytes)
	if err != nil {
		return nil, err
	}

	return &sctSigner{testdata.NewSignerWithFixedSig(key, fakeSig)}, nil
}

func TestBuildCp(t *testing.T) {
	// Create a test signer.
	ecdsaSigner, err := loadPEMPrivateKey("../testdata/test_ct_server_ecdsa_private_key.pem")
	if err != nil {
		t.Fatalf("Can't open key: %v", err)
	}

	// Define test data.
	size := uint64(12345)
	hash := []byte("test_hash_value_12345678901234567890")

	// Build the checkpoint which is in the RFC6962NoteSignature format.
	checkpoint, err := buildCp(ecdsaSigner, size, fixedTimeMillis, hash)
	if err != nil {
		t.Errorf("buildCp failed: %v", err)
	}

	// Verify whether the checkpoint is empty.
	if len(checkpoint) == 0 {
		t.Errorf("buildCp returned an empty checkpoint")
	}

	// Verify that the checkpoint can be parsed.
	var sig rfc6962NoteSignature
	_, err = tls.Unmarshal(checkpoint, &sig)
	if err != nil {
		t.Errorf("failed to unmarshal checkpoint: %v", err)
	}
	// Verify the timestamp in the note signature.
	if sig.Timestamp != fixedTimeMillis {
		t.Errorf("buildCp returned wrong timestamp, got %d, want %d", sig.Timestamp, fixedTimeMillis)
	}

	// Verify the signature using the public key.
	sth := rfc6962.SignedTreeHead{
		Version:   rfc6962.V1,
		TreeSize:  size,
		Timestamp: fixedTimeMillis,
	}
	copy(sth.SHA256RootHash[:], hash)

	sthBytes, err := serializeSTHSignatureInput(sth)
	if err != nil {
		t.Fatalf("serializeSTHSignatureInput(): %v", err)
	}

	h := sha256.Sum256(sthBytes)
	valid := ecdsa.VerifyASN1(ecdsaSigner.Public().(*ecdsa.PublicKey), h[:], sig.Signature.Signature)
	if !valid {
		t.Errorf("buildCp returned an invalid signature")
	}
}
