package malicious

import (
	"io"

	abci "github.com/cometbft/cometbft/abci/types"
	"github.com/cometbft/cometbft/libs/log"
	"github.com/cosmos/cosmos-sdk/baseapp"
	servertypes "github.com/cosmos/cosmos-sdk/server/types"
	"github.com/sunrise-zone/sunrise-app/app"
	"github.com/sunrise-zone/sunrise-app/app/encoding"
	dbm "github.com/tendermint/tm-db"
)

const (
	// BehaviorConfigKey is the key used to set the malicious config.
	BehaviorConfigKey = "behavior_config"

	// OutOfOrderHandlerKey is the key used to set the out of order prepare
	// proposal handler.
	OutOfOrderHandlerKey = "out_of_order"
)

// BehaviorConfig defines the malicious behavior for the application. It
// dictates the height at which the malicious behavior will start along with
// what type of malicious behavior will be performed.
type BehaviorConfig struct {
	// HandlerName is the name of the malicious handler to use. All known
	// handlers are defined in the PrepareProposalHandlerMap.
	HandlerName string `json:"handler_name"`
	// StartHeight is the height at which the malicious behavior will start.
	StartHeight int64 `json:"start_height"`
}

type PrepareProposalHandler func(req abci.RequestPrepareProposal) abci.ResponsePrepareProposal

// PrepareProposalHandlerMap is a map of all the known prepare proposal handlers.
func (a *App) PrepareProposalHandlerMap() map[string]PrepareProposalHandler {
	return map[string]PrepareProposalHandler{
		OutOfOrderHandlerKey: a.OutOfOrderPrepareProposal,
	}
}

type App struct {
	*app.App
	maliciousStartHeight      int64
	malPreparePropsoalHandler PrepareProposalHandler
}

func New(
	logger log.Logger,
	db dbm.DB,
	traceStore io.Writer,
	loadLatest bool,
	skipUpgradeHeights map[int64]bool,
	homePath string,
	invCheckPeriod uint,
	encodingConfig encoding.Config,
	appOpts servertypes.AppOptions,
	baseAppOptions ...func(*baseapp.BaseApp),
) *App {
	goodApp := app.New(logger, db, traceStore, loadLatest, skipUpgradeHeights, homePath, invCheckPeriod, encodingConfig, appOpts, baseAppOptions...)
	badApp := &App{App: goodApp}

	// set the malicious prepare proposal handler if it is set in the app options
	if malHanderName := appOpts.Get(BehaviorConfigKey); malHanderName != nil {
		badApp.SetMaliciousBehavior(malHanderName.(BehaviorConfig))
	}

	return badApp
}

func (a *App) SetMaliciousBehavior(mcfg BehaviorConfig) {
	// check if the handler is known
	if _, ok := a.PrepareProposalHandlerMap()[mcfg.HandlerName]; !ok {
		panic("unknown malicious prepare proposal handler")
	}
	a.malPreparePropsoalHandler = a.PrepareProposalHandlerMap()[mcfg.HandlerName]
	a.maliciousStartHeight = mcfg.StartHeight
}

// PrepareProposal overwrites the default app's method to use the configured
// malicious behavior after a given height.
func (a *App) PrepareProposal(req abci.RequestPrepareProposal) abci.ResponsePrepareProposal {
	if a.LastBlockHeight()+1 >= a.maliciousStartHeight {
		return a.malPreparePropsoalHandler(req)
	}
	return a.App.PrepareProposal(req)
}

// ProcessProposal overwrites the default app's method to auto accept any
// proposal.
func (a *App) ProcessProposal(_ abci.RequestProcessProposal) (resp abci.ResponseProcessProposal) {
	return abci.ResponseProcessProposal{
		Result: abci.ResponseProcessProposal_ACCEPT,
	}
}
