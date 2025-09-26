package ct

import (
	"context"
	"crypto"
	"crypto/ecdsa"
	"crypto/x509"
	"errors"
	"fmt"

	"github.com/transparency-dev/tessera"
	"github.com/transparency-dev/tessera/ctonly"
	"github.com/transparency-dev/tesseract/internal/types/rfc6962"
	"github.com/transparency-dev/tesseract/storage"
	"k8s.io/klog/v2"
)

// log provides objects and functions to implement static-ct-api write api.
// TODO(phboneff): consider moving to methods.
type log struct {
	// origin identifies the log. It will be used in its checkpoint, and
	// is also its submission prefix, as per https://c2sp.org/static-ct-api.
	origin string
	// signSCT Signs SCTs.
	signSCT signSCT
	// chainValidator validates incoming chains.
	chainValidator ChainValidator
	// storage stores certificate data.
	storage Storage
}

// signSCT builds an SCT for a leaf.
type signSCT func(leaf *rfc6962.MerkleTreeLeaf) (*rfc6962.SignedCertificateTimestamp, error)

// Storage provides functions to store certificates in a static-ct-api log.
type Storage interface {
	// Add assigns an index to the provided Entry, stages the entry for integration, and returns a future for the assigned index.
	Add(context.Context, *ctonly.Entry) (tessera.IndexFuture, error)
	// DedupFuture fetches a duplicate tessera ctlog entry from the log and extracts its timestamp.
	DedupFuture(context.Context, tessera.IndexFuture) (uint64, error)
	// AddIssuerChain stores every the chain certificate in a content-addressable store under their sha256 hash.
	AddIssuerChain(context.Context, []*x509.Certificate) error
}

// ChainValidator provides functions to validate incoming chains.
type ChainValidator interface {
	Validate(chain []*x509.Certificate, expectingPrecert bool) ([]*x509.Certificate, error)
	Roots() []*x509.Certificate
}

// NewLog instantiates a new log instance, with write endpoints.
// It initiates:
//   - checkpoint signer
//   - SCT signer
//   - storage, used to persist chains
func NewLog(ctx context.Context, origin string, signer crypto.Signer, cv ChainValidator, cs storage.CreateStorage, ts TimeSource) (*log, error) {
	log := &log{}

	if origin == "" {
		return nil, errors.New("empty origin")
	}
	log.origin = origin

	// Validate signer that only ECDSA is supported.
	if signer == nil {
		return nil, errors.New("empty signer")
	}
	switch keyType := signer.Public().(type) {
	case *ecdsa.PublicKey:
	default:
		return nil, fmt.Errorf("unsupported key type: %v", keyType)
	}

	sctSigner := &sctSigner{signer: signer}
	log.signSCT = sctSigner.Sign

	log.chainValidator = cv

	cpSigner, err := NewCpSigner(signer, origin, ts)
	if err != nil {
		klog.Exitf("failed to create checkpoint Signer: %v", err)
	}

	storage, err := cs(ctx, cpSigner)
	if err != nil {
		klog.Exitf("failed to initiate storage backend: %v", err)
	}
	log.storage = storage

	return log, nil
}
