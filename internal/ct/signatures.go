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
	"crypto"
	"crypto/rand"
	"crypto/sha256"
	"crypto/x509"
	"encoding/binary"
	"fmt"
	"time"

	tfl "github.com/transparency-dev/formats/log"
	"github.com/transparency-dev/tesseract/internal/types/rfc6962"
	"github.com/transparency-dev/tesseract/internal/types/tls"
	"golang.org/x/mod/sumdb/note"
)

const nanosPerMilli int64 = int64(time.Millisecond / time.Nanosecond)

type sctSigner struct {
	signer crypto.Signer
}

func (sctSigner *sctSigner) Sign(sctInput *rfc6962.CertificateTimestamp) (*rfc6962.SignedCertificateTimestamp, error) {
	data, err := tls.Marshal(*sctInput)
	if err != nil {
		return nil, fmt.Errorf("failed to serialize SCT data: %v", err)
	}

	h := sha256.Sum256(data)
	signature, err := sctSigner.signer.Sign(rand.Reader, h[:], crypto.SHA256)
	if err != nil {
		return nil, fmt.Errorf("failed to sign SCT data: %v", err)
	}

	digitallySigned := rfc6962.DigitallySigned{
		Algorithm: tls.SignatureAndHashAlgorithm{
			Hash:      tls.SHA256,
			Signature: tls.SignatureAlgorithmFromPubKey(sctSigner.signer.Public()),
		},
		Signature: signature,
	}

	logID, err := getCTLogID(sctSigner.signer.Public())
	if err != nil {
		return nil, fmt.Errorf("failed to get logID for signing: %v", err)
	}

	return &rfc6962.SignedCertificateTimestamp{
		SCTVersion: rfc6962.V1,
		LogID:      rfc6962.LogID{KeyID: logID},
		Timestamp:  sctInput.Timestamp,
		Extensions: sctInput.Extensions,
		Signature:  digitallySigned,
	}, nil
}

type rfc6962NoteSignature struct {
	Timestamp uint64
	Signature rfc6962.DigitallySigned
}

// serializeSTHSignatureInput serializes the passed in STH into the correct
// format for signing.
func serializeSTHSignatureInput(sth rfc6962.SignedTreeHead) ([]byte, error) {
	switch sth.Version {
	case rfc6962.V1:
		if len(sth.SHA256RootHash) != crypto.SHA256.Size() {
			return nil, fmt.Errorf("invalid TreeHash length, got %d expected %d", len(sth.SHA256RootHash), crypto.SHA256.Size())
		}

		input := rfc6962.TreeHeadSignature{
			Version:        sth.Version,
			SignatureType:  rfc6962.TreeHashSignatureType,
			Timestamp:      sth.Timestamp,
			TreeSize:       sth.TreeSize,
			SHA256RootHash: sth.SHA256RootHash,
		}
		return tls.Marshal(input)
	default:
		return nil, fmt.Errorf("unsupported STH version %d", sth.Version)
	}
}

// buildCp builds a https://c2sp.org/static-ct-api checkpoint.
func buildCp(signer crypto.Signer, size uint64, timeMilli uint64, hash []byte) ([]byte, error) {
	sth := rfc6962.SignedTreeHead{
		Version:   rfc6962.V1,
		TreeSize:  size,
		Timestamp: timeMilli,
	}
	copy(sth.SHA256RootHash[:], hash)

	sthBytes, err := serializeSTHSignatureInput(sth)
	if err != nil {
		return nil, fmt.Errorf("ct.SerializeSTHSignatureInput(): %v", err)
	}

	h := sha256.Sum256(sthBytes)
	signature, err := signer.Sign(rand.Reader, h[:], crypto.SHA256)
	if err != nil {
		return nil, err
	}

	rfc6962Note := rfc6962NoteSignature{
		Timestamp: sth.Timestamp,
		Signature: rfc6962.DigitallySigned{
			Algorithm: tls.SignatureAndHashAlgorithm{
				Hash:      tls.SHA256,
				Signature: tls.SignatureAlgorithmFromPubKey(signer.Public()),
			},
			Signature: signature,
		},
	}

	sig, err := tls.Marshal(rfc6962Note)
	if err != nil {
		return nil, fmt.Errorf("couldn't encode RFC6962NoteSignature: %w", err)
	}

	return sig, nil
}

// cpSigner implements note.Signer. It can generate https://c2sp.org/static-ct-api checkpoints.
type cpSigner struct {
	sthSigner  crypto.Signer
	origin     string
	keyHash    uint32
	timeSource TimeSource
}

// Sign takes an unsigned checkpoint, and signs it with a https://c2sp.org/static-ct-api signature.
// Returns an error if the message doesn't parse as a checkpoint, or if the
// checkpoint origin doesn't match with the Signer's origin.
// TODO(phboneff): add tests
func (cts *cpSigner) Sign(msg []byte) ([]byte, error) {
	ckpt := &tfl.Checkpoint{}
	rest, err := ckpt.Unmarshal(msg)

	if len(rest) != 0 {
		return nil, fmt.Errorf("checkpoint contains trailing data: %s", string(rest))
	} else if err != nil {
		return nil, fmt.Errorf("ckpt.Unmarshal: %v", err)
	} else if ckpt.Origin != cts.origin {
		return nil, fmt.Errorf("checkpoint's origin %s doesn't match signer's origin %s", ckpt.Origin, cts.origin)
	}

	t := uint64(cts.timeSource.Now().UnixMilli())
	sig, err := buildCp(cts.sthSigner, ckpt.Size, t, ckpt.Hash[:])
	if err != nil {
		return nil, fmt.Errorf("coudn't sign CT checkpoint: %v", err)
	}
	return sig, nil
}

func (cts *cpSigner) Name() string {
	return cts.origin
}

func (cts *cpSigner) KeyHash() uint32 {
	return cts.keyHash
}

// NewCpSigner returns a new note signer that can sign https://c2sp.org/static-ct-api checkpoints.
// TODO(phboneff): add tests
func NewCpSigner(cs crypto.Signer, origin string, timeSource TimeSource) (note.Signer, error) {
	logID, err := getCTLogID(cs.Public())
	if err != nil {
		return nil, fmt.Errorf("failed to get logID for signing: %v", err)
	}

	h := sha256.New()
	h.Write([]byte(origin))
	h.Write([]byte{0x0A}) // newline
	h.Write([]byte{0x05}) // signature type
	h.Write(logID[:])
	sum := h.Sum(nil)

	ns := &cpSigner{
		sthSigner:  cs,
		origin:     origin,
		keyHash:    binary.BigEndian.Uint32(sum),
		timeSource: timeSource,
	}

	return ns, nil
}

// getCTLogID takes a log public key and returns the LogID. (see RFC 6962 S3.2)
// In CT V1 the log id is a hash of the public key.
func getCTLogID(pk crypto.PublicKey) ([sha256.Size]byte, error) {
	pubBytes, err := x509.MarshalPKIXPublicKey(pk)
	if err != nil {
		return [sha256.Size]byte{}, err
	}
	return sha256.Sum256(pubBytes), nil
}
