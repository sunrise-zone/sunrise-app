package keeper_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	keepertest "github.com/sunrise-zone/sunrise-app/testutil/keeper"
	"github.com/sunrise-zone/sunrise-app/x/blobgrant/types"
)

func TestGetParams(t *testing.T) {
	k, ctx := keepertest.GrantKeeper(t)
	params := types.DefaultParams()

	require.NoError(t, k.SetParams(ctx, params))
	require.EqualValues(t, params, k.GetParams(ctx))
}
