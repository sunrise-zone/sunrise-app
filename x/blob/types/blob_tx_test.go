package types

import (
	"testing"

	tmrand "github.com/cometbft/cometbft/libs/rand"
	tmproto "github.com/cometbft/cometbft/proto/tendermint/types"
	"github.com/sunrise-zone/sunrise-app/app/encoding"
	"github.com/sunrise-zone/sunrise-app/pkg/appconsts"
	appns "github.com/sunrise-zone/sunrise-app/pkg/namespace"

	sdkmath "cosmossdk.io/math"
	coretypes "github.com/cometbft/cometbft/types"
	sdk "github.com/cosmos/cosmos-sdk/types"
	authsigning "github.com/cosmos/cosmos-sdk/x/auth/signing"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const (
	denom = "utia"
)

func TestNewBlob(t *testing.T) {
	rawBlob := []byte{1}
	validBlob, err := NewBlob(appns.RandomBlobNamespace(), rawBlob, appconsts.ShareVersionZero)
	require.NoError(t, err)
	require.Equal(t, validBlob.Data, rawBlob)

	_, err = NewBlob(appns.TxNamespace, rawBlob, appconsts.ShareVersionZero)
	require.Error(t, err)

	_, err = NewBlob(appns.RandomBlobNamespace(), []byte{}, appconsts.ShareVersionZero)
	require.Error(t, err)
}

func TestVerifySignature(t *testing.T) {
	_, addr, signer, encCfg := setupSigTest(t)
	coin := sdk.Coin{
		Denom:  denom,
		Amount: sdkmath.NewInt(10),
	}

	opts := []TxBuilderOption{
		SetFeeAmount(sdk.NewCoins(coin)),
		SetGasLimit(10000000),
	}

	msg, blob := randMsgPayForBlobsWithNamespaceAndSigner(
		t,
		addr.String(),
		appns.RandomBlobNamespace(),
		100,
	)
	builder := signer.NewTxBuilder(opts...)
	stx, err := signer.BuildSignedTx(builder, msg)
	require.NoError(t, err)

	rawTx, err := encCfg.TxConfig.TxEncoder()(stx)
	require.NoError(t, err)

	cTx, err := coretypes.MarshalBlobTx(rawTx, blob)
	require.NoError(t, err)

	uTx, isBlob := coretypes.UnmarshalBlobTx(cTx)
	require.True(t, isBlob)

	wTx, err := coretypes.MarshalIndexWrapper(uTx.Tx, 100)
	require.NoError(t, err)

	uwTx, isMal := coretypes.UnmarshalIndexWrapper(wTx)
	require.True(t, isMal)

	sTx, err := encCfg.TxConfig.TxDecoder()(uwTx.Tx)
	require.NoError(t, err)

	sigTx, ok := sTx.(authsigning.SigVerifiableTx)
	require.True(t, ok)

	sigs, err := sigTx.GetSignaturesV2()
	require.NoError(t, err)
	require.Equal(t, 1, len(sigs))
	sig := sigs[0]

	// verify the signatures of the prepared txs
	sdata, err := signer.GetSignerData()
	require.NoError(t, err)

	err = authsigning.VerifySignature(
		sdata.PubKey,
		sdata,
		sig.Data,
		encCfg.TxConfig.SignModeHandler(),
		sTx,
	)
	assert.NoError(t, err)
}

func setupSigTest(t *testing.T) (string, sdk.Address, *KeyringSigner, encoding.Config) {
	acc := "test account"
	signer := GenerateKeyringSigner(t, acc)
	encCfg := makeBlobEncodingConfig()
	addr, err := signer.GetSignerInfo().GetAddress()
	require.NoError(t, err)
	return acc, addr, signer, encCfg
}

func randMsgPayForBlobsWithNamespaceAndSigner(t *testing.T, signer string, ns appns.Namespace, size int) (*MsgPayForBlobs, *tmproto.Blob) {
	blob, err := NewBlob(ns, tmrand.Bytes(size), appconsts.ShareVersionZero)
	require.NoError(t, err)
	msg, err := NewMsgPayForBlobs(
		signer,
		blob,
	)
	if err != nil {
		panic(err)
	}
	return msg, blob
}
