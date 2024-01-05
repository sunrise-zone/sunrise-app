package testnode

import (
	"context"
	"encoding/json"
	"net"
	"os"
	"path/filepath"
	"testing"

	"github.com/cometbft/cometbft/config"
	"github.com/cometbft/cometbft/libs/log"
	"github.com/cometbft/cometbft/node"
	"github.com/cometbft/cometbft/p2p"
	"github.com/cometbft/cometbft/privval"
	tmproto "github.com/cometbft/cometbft/proto/tendermint/types"
	"github.com/cometbft/cometbft/proxy"
	"github.com/cosmos/cosmos-sdk/client/flags"
	"github.com/cosmos/cosmos-sdk/crypto/keyring"
	srvtypes "github.com/cosmos/cosmos-sdk/server/types"
	authtypes "github.com/cosmos/cosmos-sdk/x/auth/types"
	banktypes "github.com/cosmos/cosmos-sdk/x/bank/types"
	"github.com/cosmos/cosmos-sdk/x/genutil"
	"github.com/stretchr/testify/require"
	dbm "github.com/tendermint/tm-db"

	"github.com/sunrise-zone/sunrise-app/app"
	"github.com/sunrise-zone/sunrise-app/app/encoding"
	"github.com/sunrise-zone/sunrise-app/test/util/testfactory"
	qgbtypes "github.com/sunrise-zone/sunrise-app/x/qgb/types"
)

// NewCometNode creates a ready to use comet node that operates a single
// validator celestia-app network. It expects that all configuration files are
// already initialized and saved to the baseDir.
func NewCometNode(t testing.TB, baseDir string, cfg *Config) (*node.Node, srvtypes.Application, error) {
	var logger log.Logger
	if cfg.SupressLogs {
		logger = log.NewNopLogger()
	} else {
		logger = log.NewTMLogger(log.NewSyncWriter(os.Stdout))
		logger = log.NewFilter(logger, log.AllowError())
	}

	dbPath := filepath.Join(cfg.TmConfig.RootDir, "data")
	db, err := dbm.NewGoLevelDB("application", dbPath)
	require.NoError(t, err)

	cfg.AppOptions.Set(flags.FlagHome, baseDir)

	app := cfg.AppCreator(logger, db, nil, cfg.AppOptions)

	nodeKey, err := p2p.LoadOrGenNodeKey(cfg.TmConfig.NodeKeyFile())
	if err != nil {
		return nil, nil, err
	}

	tmNode, err := node.NewNode(
		cfg.TmConfig,
		privval.LoadOrGenFilePV(cfg.TmConfig.PrivValidatorKeyFile(), cfg.TmConfig.PrivValidatorStateFile()),
		nodeKey,
		proxy.NewLocalClientCreator(app),
		node.DefaultGenesisDocProviderFunc(cfg.TmConfig),
		node.DefaultDBProvider,
		node.DefaultMetricsProvider(cfg.TmConfig.Instrumentation),
		logger,
	)

	return tmNode, app, err
}

// InitFiles initializes the files for a new tendermint node with the provided
// genesis state and consensus parameters. The provided keyring is used to
// create a validator key and the chainID is used to initialize the genesis
// state. The keyring is returned with the validator account added.
func InitFiles(
	t testing.TB,
	cparams *tmproto.ConsensusParams,
	tmCfg *config.Config,
	genState map[string]json.RawMessage,
	kr keyring.Keyring,
	chainID string,
) (string, keyring.Keyring, error) {
	baseDir, err := initFileStructure(t, tmCfg)
	if err != nil {
		return baseDir, kr, err
	}

	encCfg := encoding.MakeConfig(app.ModuleEncodingRegisters...)

	nodeID, pubKey, err := genutil.InitializeNodeValidatorFiles(tmCfg)
	if err != nil {
		return baseDir, kr, err
	}

	err = createValidator(kr, encCfg, pubKey, "validator", nodeID, chainID, baseDir)
	if err != nil {
		return baseDir, kr, err
	}

	err = initGenFiles(cparams, genState, encCfg.Codec, tmCfg.GenesisFile(), chainID)
	if err != nil {
		return baseDir, kr, err
	}

	return baseDir, kr, collectGenFiles(tmCfg, encCfg, pubKey, nodeID, chainID, baseDir)
}

// DefaultGenesisState returns a default genesis state and a keyring with
// accounts that have coins. It adds a default "validator" account that is
// funded and used for the valop address of the single validator. The keyring
// accounts are based on the fundedAccounts parameter.
func DefaultGenesisState(fundedAccounts ...string) (map[string]json.RawMessage, keyring.Keyring, error) {
	encCfg := encoding.MakeConfig(app.ModuleEncodingRegisters...)
	state := app.ModuleBasics.DefaultGenesis(encCfg.Codec)
	fundedAccounts = append(fundedAccounts, "validator")
	kr, bankBals, authAccs := testfactory.FundKeyringAccounts(fundedAccounts...)

	// set the accounts in the genesis state
	var authGenState authtypes.GenesisState
	encCfg.Codec.MustUnmarshalJSON(state[authtypes.ModuleName], &authGenState)

	accounts, err := authtypes.PackAccounts(authAccs)
	if err != nil {
		return nil, nil, err
	}

	authGenState.Accounts = append(authGenState.Accounts, accounts...)
	state[authtypes.ModuleName] = encCfg.Codec.MustMarshalJSON(&authGenState)

	// set the balances in the genesis state
	var bankGenState banktypes.GenesisState
	encCfg.Codec.MustUnmarshalJSON(state[banktypes.ModuleName], &bankGenState)

	bankGenState.Balances = append(bankGenState.Balances, bankBals...)
	state[banktypes.ModuleName] = encCfg.Codec.MustMarshalJSON(&bankGenState)

	// use the minimum data commitment window (100)
	var qgbGenState qgbtypes.GenesisState
	encCfg.Codec.MustUnmarshalJSON(state[qgbtypes.ModuleName], &qgbGenState)
	qgbGenState.Params.DataCommitmentWindow = qgbtypes.MinimumDataCommitmentWindow
	state[qgbtypes.ModuleName] = encCfg.Codec.MustMarshalJSON(&qgbGenState)

	return state, kr, nil
}

// NewNetwork starts a single valiator celestia-app network using the provided
// configurations. Configured accounts will be funded and their keys can be
// accessed in keyring returned client.Context. All rpc, p2p, and grpc addresses
// in the provided configs are overwritten to use open ports. The node can be
// accessed via the returned client.Context or via the returned rpc and grpc
// addresses. Configured genesis options will be applied after all accounts have
// been initialized.
func NewNetwork(t testing.TB, cfg *Config) (cctx Context, rpcAddr, grpcAddr string) {
	t.Helper()

	genState, kr, err := DefaultGenesisState(cfg.Accounts...)
	require.NoError(t, err)

	for _, opt := range cfg.GenesisOptions {
		genState = opt(genState)
	}

	chainID := cfg.ChainID

	baseDir, kr, err := InitFiles(t, cfg.ConsensusParams, cfg.TmConfig, genState, kr, chainID)
	require.NoError(t, err)

	tmNode, app, err := NewCometNode(t, baseDir, cfg)
	require.NoError(t, err)

	cctx = NewContext(context.TODO(), kr, cfg.TmConfig, chainID)

	cctx, stopNode, err := StartNode(tmNode, cctx)
	require.NoError(t, err)

	cctx, cleanupGRPC, err := StartGRPCServer(app, cfg.AppConfig, cctx)
	require.NoError(t, err)

	t.Cleanup(func() {
		t.Log("tearing down testnode")
		require.NoError(t, stopNode())
		require.NoError(t, cleanupGRPC())
	})

	return cctx, cfg.TmConfig.RPC.ListenAddress, cfg.AppConfig.GRPC.Address
}

func GetFreePort() int {
	a, err := net.ResolveTCPAddr("tcp", "localhost:0")
	if err == nil {
		var l *net.TCPListener
		if l, err = net.ListenTCP("tcp", a); err == nil {
			defer l.Close()
			return l.Addr().(*net.TCPAddr).Port
		}
	}
	panic("while getting free port: " + err.Error())
}
