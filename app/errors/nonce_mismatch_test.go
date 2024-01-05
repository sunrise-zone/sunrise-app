package errors_test

import (
	"fmt"
	"testing"

	tmproto "github.com/cometbft/cometbft/proto/tendermint/types"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/cosmos/cosmos-sdk/x/auth/ante"
	"github.com/stretchr/testify/require"
	"github.com/sunrise-zone/sunrise-app/app"
	"github.com/sunrise-zone/sunrise-app/app/encoding"
	apperr "github.com/sunrise-zone/sunrise-app/app/errors"
	"github.com/sunrise-zone/sunrise-app/pkg/appconsts"
	"github.com/sunrise-zone/sunrise-app/pkg/namespace"
	"github.com/sunrise-zone/sunrise-app/pkg/user"
	testutil "github.com/sunrise-zone/sunrise-app/test/util"
	"github.com/sunrise-zone/sunrise-app/test/util/testfactory"
	blob "github.com/sunrise-zone/sunrise-app/x/blob/types"
)

// This will detect any changes to the DeductFeeDecorator which may cause a
// different error message that does not match the regexp.
func TestNonceMismatchIntegration(t *testing.T) {
	account := "test"
	testApp, kr := testutil.SetupTestAppWithGenesisValSet(app.DefaultConsensusParams(), account)
	encCfg := encoding.MakeConfig(app.ModuleEncodingRegisters...)
	minGasPrice, err := sdk.ParseDecCoins(fmt.Sprintf("%v%s", appconsts.DefaultMinGasPrice, app.BondDenom))
	require.NoError(t, err)
	ctx := testApp.NewContext(true).WithMinGasPrices(minGasPrice)
	addr := testfactory.GetAddress(kr, account)
	enc := encoding.MakeConfig(app.ModuleEncodingRegisters...)
	acc := testutil.DirectQueryAccount(testApp, addr)
	// set the sequence to an incorrect value
	signer, err := user.NewSigner(kr, nil, addr, enc.TxConfig, testutil.ChainID, acc.GetAccountNumber(), 2)
	require.NoError(t, err)

	b, err := blob.NewBlob(namespace.RandomNamespace(), []byte("hello world"), 0)
	require.NoError(t, err)

	msg, err := blob.NewMsgPayForBlobs(signer.Address().String(), b)
	require.NoError(t, err)

	tx, err := signer.CreateTx([]sdk.Msg{msg})
	require.NoError(t, err)
	sdkTx, err := enc.TxConfig.TxDecoder()(tx)
	require.NoError(t, err)

	decorator := ante.NewSigVerificationDecorator(testApp.AccountKeeper, encCfg.TxConfig.SignModeHandler())
	anteHandler := sdk.ChainAnteDecorators(decorator)

	// We set simulate to true here to bypass having to initialize the
	// accounts public key.
	_, err = anteHandler(ctx, sdkTx, true)
	require.True(t, apperr.IsNonceMismatch(err), err)
	expectedNonce, err := apperr.ParseNonceMismatch(err)
	require.NoError(t, err)
	require.EqualValues(t, 0, expectedNonce, err)
}
