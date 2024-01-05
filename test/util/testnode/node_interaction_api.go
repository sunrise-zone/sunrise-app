package testnode

import (
	"context"
	"encoding/hex"
	"fmt"
	"strings"
	"time"

	abci "github.com/cometbft/cometbft/abci/types"
	tmconfig "github.com/cometbft/cometbft/config"
	tmrand "github.com/cometbft/cometbft/libs/rand"
	rpctypes "github.com/cometbft/cometbft/rpc/core/types"
	coretypes "github.com/cometbft/cometbft/types"
	"github.com/cosmos/cosmos-sdk/client"
	"github.com/cosmos/cosmos-sdk/client/flags"
	"github.com/cosmos/cosmos-sdk/crypto/keyring"
	sdk "github.com/cosmos/cosmos-sdk/types"
	authtypes "github.com/cosmos/cosmos-sdk/x/auth/types"
	"github.com/sunrise-zone/sunrise-app/app"
	"github.com/sunrise-zone/sunrise-app/app/encoding"
	"github.com/sunrise-zone/sunrise-app/pkg/appconsts"
	appns "github.com/sunrise-zone/sunrise-app/pkg/namespace"
	"github.com/sunrise-zone/sunrise-app/pkg/shares"
	"github.com/sunrise-zone/sunrise-app/x/blob/types"
)

const (
	DefaultTimeout = 30 * time.Second
)

type Context struct {
	rootCtx context.Context
	client.Context
}

func NewContext(goCtx context.Context, kr keyring.Keyring, tmCfg *tmconfig.Config, chainID string) Context {
	ecfg := encoding.MakeConfig(app.ModuleEncodingRegisters...)
	cctx := client.Context{}.
		WithKeyring(kr).
		WithHomeDir(tmCfg.RootDir).
		WithChainID(chainID).
		WithInterfaceRegistry(ecfg.InterfaceRegistry).
		WithCodec(ecfg.Codec).
		WithLegacyAmino(ecfg.Amino).
		WithTxConfig(ecfg.TxConfig).
		WithAccountRetriever(authtypes.AccountRetriever{})

	return Context{rootCtx: goCtx, Context: cctx}
}

func (c *Context) GoContext() context.Context {
	return c.rootCtx
}

// GenesisTime returns the genesis block time.
func (c *Context) GenesisTime() (time.Time, error) {
	height := int64(1)
	status, err := c.Client.Block(c.GoContext(), &height)
	if err != nil {
		return time.Unix(0, 0), err
	}

	return status.Block.Time, nil
}

// LatestHeight returns the latest height of the network or an error if the
// query fails.
func (c *Context) LatestHeight() (int64, error) {
	status, err := c.Client.Status(c.GoContext())
	if err != nil {
		return 0, err
	}

	return status.SyncInfo.LatestBlockHeight, nil
}

// LatestTimestamp returns the latest timestamp of the network or an error if the
// query fails.
func (c *Context) LatestTimestamp() (time.Time, error) {
	current, err := c.Client.Block(c.GoContext(), nil)
	if err != nil {
		return time.Unix(0, 0), err
	}
	return current.Block.Time, nil
}

// WaitForHeightWithTimeout is the same as WaitForHeight except the caller can
// provide a custom timeout.
func (c *Context) WaitForHeightWithTimeout(h int64, t time.Duration) (int64, error) {
	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()

	ctx, cancel := context.WithTimeout(c.rootCtx, t)
	defer cancel()

	var latestHeight int64
	for {
		select {
		case <-ctx.Done():
			return latestHeight, fmt.Errorf("timeout (%v) exceeded waiting for network to reach height", t)
		case <-ticker.C:
			latestHeight, err := c.LatestHeight()
			if err != nil {
				return 0, err
			}
			if latestHeight >= h {
				return latestHeight, nil
			}
		}
	}
}

// WaitForTimestampWithTimeout waits for a block with a timestamp greater than t.
func (c *Context) WaitForTimestampWithTimeout(t time.Time, d time.Duration) (time.Time, error) {
	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()

	ctx, cancel := context.WithTimeout(c.rootCtx, d)
	defer cancel()

	var latestTimestamp time.Time
	for {
		select {
		case <-ctx.Done():
			return latestTimestamp, fmt.Errorf("timeout %v exceeded waiting for network to reach block with timestamp %v", d, t)
		case <-ticker.C:
			latestTimestamp, err := c.LatestTimestamp()
			if err != nil {
				return time.Unix(0, 0), err
			}
			if latestTimestamp.After(t) {
				return latestTimestamp, nil
			}
		}
	}
}

// WaitForHeight performs a blocking check where it waits for a block to be
// committed after a given block. If that height is not reached within a timeout,
// an error is returned. Regardless, the latest height queried is returned.
func (c *Context) WaitForHeight(h int64) (int64, error) {
	return c.WaitForHeightWithTimeout(h, DefaultTimeout)
}

// WaitForTimestamp performs a blocking check where it waits for a block to be
// committed after a given timestamp. If that height is not reached within a timeout,
// an error is returned. Regardless, the latest timestamp queried is returned.
func (c *Context) WaitForTimestamp(t time.Time) (time.Time, error) {
	return c.WaitForTimestampWithTimeout(t, 10*time.Second)
}

// WaitForNextBlock waits for the next block to be committed, returning an error
// upon failure.
func (c *Context) WaitForNextBlock() error {
	return c.WaitForBlocks(1)
}

// WaitForBlocks waits until n blocks have been committed, returning an error
// upon failure.
func (c *Context) WaitForBlocks(n int64) error {
	lastBlock, err := c.LatestHeight()
	if err != nil {
		return err
	}

	_, err = c.WaitForHeight(lastBlock + n)
	if err != nil {
		return err
	}

	return err
}

func (c *Context) WaitForTx(hashHexStr string, blocks int) (*rpctypes.ResultTx, error) {
	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()

	hash, err := hex.DecodeString(hashHexStr)
	if err != nil {
		return nil, err
	}

	height, err := c.LatestHeight()
	if err != nil {
		return nil, err
	}

	ctx, cancel := context.WithTimeout(c.rootCtx, DefaultTimeout)
	defer cancel()

	for {
		latestHeight, err := c.LatestHeight()
		if err != nil {
			return nil, err
		}
		if blocks > 0 && latestHeight > height+int64(blocks) {
			return nil, fmt.Errorf("waited %d blocks for tx to be included in block", blocks)
		}

		<-ticker.C
		res, err := c.Client.Tx(ctx, hash, false)
		if err != nil {
			if strings.Contains(err.Error(), "not found") {
				continue
			}
			return nil, err
		}
		return res, nil
	}
}

// PostData will create and submit PFB transaction containing the namespace and
// blobData. This function blocks until the PFB has been included in a block and
// returns an error if the transaction is invalid or is rejected by the mempool.
func (c *Context) PostData(account, broadcastMode string, ns appns.Namespace, blobData []byte) (*sdk.TxResponse, error) {
	opts := []types.TxBuilderOption{
		types.SetGasLimit(100000000000000),
	}

	// use the key for accounts[i] to create a singer used for a single PFB
	signer := types.NewKeyringSigner(c.Keyring, account, c.ChainID)

	rec := signer.GetSignerInfo()
	addr, err := rec.GetAddress()
	if err != nil {
		return nil, err
	}

	acc, seq, err := c.AccountRetriever.GetAccountNumberSequence(c.Context, addr)
	if err != nil {
		return nil, err
	}

	signer.SetAccountNumber(acc)
	signer.SetSequence(seq)

	blob, err := types.NewBlob(ns, blobData, appconsts.ShareVersionZero)
	if err != nil {
		return nil, err
	}

	msg, err := types.NewMsgPayForBlobs(
		addr.String(),
		blob,
	)
	if err != nil {
		return nil, err
	}

	builder := signer.NewTxBuilder(opts...)
	stx, err := signer.BuildSignedTx(builder, msg)
	if err != nil {
		return nil, err
	}

	rawTx, err := signer.EncodeTx(stx)
	if err != nil {
		return nil, err
	}

	blobTx, err := coretypes.MarshalBlobTx(rawTx, blob)
	if err != nil {
		return nil, err
	}

	var res *sdk.TxResponse
	switch broadcastMode {
	case flags.BroadcastSync:
		res, err = c.BroadcastTxSync(blobTx)
	case flags.BroadcastAsync:
		res, err = c.BroadcastTxAsync(blobTx)
	case flags.BroadcastBlock:
		res, err = c.BroadcastTxCommit(blobTx)
	default:
		return nil, fmt.Errorf("unsupported broadcast mode %s; supported modes: sync, async, block", c.BroadcastMode)
	}
	if err != nil {
		return nil, err
	}
	if res.Code != abci.CodeTypeOK {
		return res, fmt.Errorf("failure to broadcast tx (%d): %s", res.Code, res.RawLog)
	}

	return res, nil
}

// FillBlock creates and submits a single transaction that is large enough to
// create a square of the desired size. broadcast mode indicates if the tx
// should be submitted async, sync, or block. (see flags.BroadcastModeSync). If
// broadcast mode is the string zero value, then it will be set to block.
func (c *Context) FillBlock(squareSize int, accounts []string, broadcastMode string) (*sdk.TxResponse, error) {
	if squareSize < appconsts.MinSquareSize+1 || (squareSize&(squareSize-1) != 0) {
		return nil, fmt.Errorf("unsupported squareSize: %d", squareSize)
	}

	if broadcastMode == "" {
		broadcastMode = flags.BroadcastBlock
	}

	// create the tx the size of the square minus one row
	shareCount := (squareSize - 1) * squareSize

	// we use a formula to guarantee that the tx is the exact size needed to force a specific square size.
	blobSize := shares.AvailableBytesFromSparseShares(shareCount)
	return c.PostData(accounts[0], broadcastMode, appns.RandomBlobNamespace(), tmrand.Bytes(blobSize))
}

// HeightForTimestamp returns the block height for the first block after a
// given timestamp.
func (c *Context) HeightForTimestamp(timestamp time.Time) (int64, error) {
	latestHeight, err := c.LatestHeight()
	if err != nil {
		return 0, err
	}

	for i := int64(1); i <= latestHeight; i++ {
		result, err := c.Client.Block(context.Background(), &i)
		if err != nil {
			return 0, err
		}
		if result.Block.Time.After(timestamp) {
			return i, nil
		}
	}
	return 0, fmt.Errorf("could not find block with timestamp after %v", timestamp)
}
