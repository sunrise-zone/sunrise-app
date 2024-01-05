package types

import (
	"bytes"
	"fmt"
	math "math"

	"github.com/sunrise-zone/sunrise-app/pkg/appconsts"

	tmproto "github.com/cometbft/cometbft/proto/tendermint/types"
	core "github.com/cometbft/cometbft/types"
	"github.com/cosmos/cosmos-sdk/client"
	appns "github.com/sunrise-zone/sunrise-app/pkg/namespace"
	shares "github.com/sunrise-zone/sunrise-app/pkg/shares"
)

// Blob wraps the tendermint type so that users can simply import this one.
type Blob = tmproto.Blob

// NewBlob creates a new coretypes.Blob from the provided data after performing
// basic stateless checks over it.
func NewBlob(ns appns.Namespace, blob []byte, shareVersion uint8) (*Blob, error) {
	err := ValidateBlobNamespace(ns)
	if err != nil {
		return nil, err
	}

	if len(blob) == 0 {
		return nil, ErrZeroBlobSize
	}

	return &tmproto.Blob{
		NamespaceId:      ns.ID,
		Data:             blob,
		ShareVersion:     uint32(shareVersion),
		NamespaceVersion: uint32(ns.Version),
	}, nil
}

// ValidateBlobTx performs stateless checks on the BlobTx to ensure that the
// blobs attached to the transaction are valid.
func ValidateBlobTx(txcfg client.TxEncodingConfig, bTx tmproto.BlobTx) error {
	sdkTx, err := txcfg.TxDecoder()(bTx.Tx)
	if err != nil {
		return err
	}

	// TODO: remove this check once support for multiple sdk.Msgs in a BlobTx is
	// supported.
	msgs := sdkTx.GetMsgs()
	if len(msgs) != 1 {
		return ErrMultipleMsgsInBlobTx
	}
	msg := msgs[0]
	msgPFB, ok := msg.(*MsgPayForBlobs)
	if !ok {
		return ErrNoPFB
	}
	err = msgPFB.ValidateBasic()
	if err != nil {
		return err
	}

	// perform basic checks on the blobs
	sizes := make([]uint32, len(bTx.Blobs))
	for i, pblob := range bTx.Blobs {
		sizes[i] = uint32(len(pblob.Data))
	}
	err = ValidateBlobs(bTx.Blobs...)
	if err != nil {
		return err
	}

	// check that the sizes in the blobTx match the sizes in the msgPFB
	if !equalSlices(sizes, msgPFB.BlobSizes) {
		return ErrBlobSizeMismatch.Wrapf("actual %v declared %v", sizes, msgPFB.BlobSizes)
	}

	for i, ns := range msgPFB.Namespaces {
		msgPFBNamespace, err := appns.From(ns)
		if err != nil {
			return err
		}

		// this not only checks that the pfb namespaces match the ones in the blobs
		// but that the namespace version and namespace id are valid
		blobNamespace, err := appns.New(uint8(bTx.Blobs[i].NamespaceVersion), bTx.Blobs[i].NamespaceId)
		if err != nil {
			return err
		}

		if !bytes.Equal(blobNamespace.Bytes(), msgPFBNamespace.Bytes()) {
			return ErrNamespaceMismatch.Wrapf("%v %v", blobNamespace.Bytes(), msgPFB.Namespaces[i])
		}
	}

	// verify that the commitment of the blob matches that of the msgPFB
	for i, commitment := range msgPFB.ShareCommitments {
		calculatedCommit, err := CreateCommitment(bTx.Blobs[i])
		if err != nil {
			return ErrCalculateCommitment
		}
		if !bytes.Equal(calculatedCommit, commitment) {
			return ErrInvalidShareCommitment
		}
	}

	return nil
}

func BlobTxSharesUsed(btx tmproto.BlobTx) int {
	sharesUsed := 0
	for _, blob := range btx.Blobs {
		sharesUsed += shares.SparseSharesNeeded(uint32(len(blob.Data)))
	}
	return sharesUsed
}

func BlobFromProto(p *tmproto.Blob) (core.Blob, error) {
	if p == nil {
		return core.Blob{}, fmt.Errorf("nil blob")
	}

	if p.ShareVersion > math.MaxUint8 {
		return core.Blob{}, fmt.Errorf("invalid share version %d", p.ShareVersion)
	}

	if p.NamespaceVersion > appconsts.NamespaceVersionMaxValue {
		return core.Blob{}, fmt.Errorf("invalid namespace version %d", p.NamespaceVersion)
	}

	return core.Blob{
		NamespaceID:      p.NamespaceId,
		Data:             p.Data,
		ShareVersion:     uint8(p.ShareVersion),
		NamespaceVersion: uint8(p.NamespaceVersion),
	}, nil
}

func equalSlices[T comparable](a, b []T) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
