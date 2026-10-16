package keeper_test

import (
	"bytes"
	"errors"
	"math/big"
	"testing"

	"github.com/Sidiora-Labs/Paxeer-X-Network/modules/launchpad/keeper"
	"github.com/Sidiora-Labs/Paxeer-X-Network/modules/launchpad/types"
	sdk "github.com/Sidiora-Labs/Paxeer-X-Network/sdk/types"
	"github.com/Sidiora-Labs/Paxeer-X-Network/storage/ledger_db/receipt"
	testkeeper "github.com/Sidiora-Labs/Paxeer-X-Network/testutil/keeper"
	"github.com/ethereum/go-ethereum/accounts/abi"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/stretchr/testify/require"
)

type holding struct {
	holder common.Address
	amount int64
}

// poolToken deploys a SidioraERC20 pool token at the current height, minting
// supply to a distributor that sends every holding out in the same block.
func (c *custodyEnv) poolToken(name string, holdings ...holding) common.Address {
	c.t.Helper()
	evm := &testkeeper.EVMTestApp.EvmKeeper
	distributor := address(name + " distributor")
	var supply int64
	for _, h := range holdings {
		supply += h.amount
	}
	stringType, err := abi.NewType("string", "", nil)
	require.NoError(c.t, err)
	uintType, err := abi.NewType("uint256", "", nil)
	require.NoError(c.t, err)
	addressType, err := abi.NewType("address", "", nil)
	require.NoError(c.t, err)
	args, err := abi.Arguments{{Type: stringType}, {Type: stringType}, {Type: uintType}, {Type: addressType}}.
		Pack(name, "PT", big.NewInt(supply), distributor)
	require.NoError(c.t, err)
	nonce := evm.GetNonce(c.ctx, distributor)
	_, err = evm.CallEVM(c.ctx, distributor, nil, nil, append(common.FromHex(sidioraERC20Creation), args...))
	require.NoError(c.t, err)
	token := crypto.CreateAddress(distributor, nonce)
	for _, h := range holdings {
		input := append(crypto.Keccak256([]byte("transfer(address,uint256)"))[:4], common.LeftPadBytes(h.holder.Bytes(), 32)...)
		ret, err := evm.CallEVM(c.ctx, distributor, &token, nil, append(input, word(big.NewInt(h.amount))...))
		require.NoError(c.t, err)
		require.Equal(c.t, word(big.NewInt(1)), ret)
	}
	return token
}

func (c *custodyEnv) tokenTransfer(token, from, to common.Address, amount *big.Int) {
	c.t.Helper()
	input := append(crypto.Keccak256([]byte("transfer(address,uint256)"))[:4], common.LeftPadBytes(to.Bytes(), 32)...)
	ret, err := testkeeper.EVMTestApp.EvmKeeper.CallEVM(c.ctx, from, &token, nil, append(input, word(amount)...))
	require.NoError(c.t, err)
	require.Equal(c.t, word(big.NewInt(1)), ret)
}

func (c *custodyEnv) tokenRead(token common.Address, signature string, words ...[]byte) int64 {
	c.t.Helper()
	input := crypto.Keccak256([]byte(signature))[:4]
	for _, w := range words {
		input = append(input, w...)
	}
	ret, err := testkeeper.EVMTestApp.EvmKeeper.StaticCallEVM(c.ctx, c.k.ModuleAddress(), &token, input)
	require.NoError(c.t, err)
	require.Len(c.t, ret, 32)
	value := new(big.Int).SetBytes(ret)
	require.True(c.t, value.IsInt64())
	return value.Int64()
}

// quoteSince is the USDL Transfer logs written after the first before of
// them, leaving out the Transfer logs of the pool tokens.
func (c *custodyEnv) quoteSince(before int) []transferLog {
	return c.quoteLogs()[before:]
}

func (c *custodyEnv) quoteLogs() []transferLog {
	c.t.Helper()
	r, err := testkeeper.EVMTestApp.EvmKeeper.GetTransientReceipt(c.ctx, c.ctx.TxSum(), uint64(c.ctx.TxIndex()))
	if errors.Is(err, receipt.ErrNotFound) {
		return nil
	}
	require.NoError(c.t, err)
	var out []transferLog
	for _, l := range r.Logs {
		if common.HexToAddress(l.Address) != c.usdl || len(l.Topics) == 0 || common.HexToHash(l.Topics[0]) != transferTopic {
			continue
		}
		require.Len(c.t, l.Topics, 3)
		amount := new(big.Int).SetBytes(l.Data)
		require.True(c.t, amount.IsInt64())
		out = append(out, transferLog{from: common.HexToAddress(l.Topics[1]), to: common.HexToAddress(l.Topics[2]),
			amount: amount.Int64()})
	}
	return out
}

func (c *custodyEnv) height(h int64) { c.ctx = c.ctx.WithBlockHeight(h) }

// airdropMarket installs a migrated generation 2 market of token with the
// AIRDROP strategy, its pool and FeeAccumulator surfaces, FEES_ROUTER_ROLE
// held by the FeesRouter and FeeHeld funded with fees plus airdropBalance.
func (c *custodyEnv) airdropMarket(token common.Address, fees int64, epoch uint64, airdropBalance int64) types.Market {
	c.t.Helper()
	roles := c.k.Generation2()
	market := generation2Market(roles.SidioraNFT, 1, sdk.AccAddress(address("fee-right holder").Bytes()))
	market.FeeStrategy = types.FeeStrategyAirdrop
	market.TokenAddress = token.Hex()
	market.AccumulatedFees = i(fees)
	market.AirdropEpoch = epoch
	market.AirdropBalance = i(airdropBalance)
	gs := types.DefaultGenesis()
	gs.Params = c.k.GetParams(c.ctx)
	gs.Markets = []types.Market{market}
	c.k.InitGenesis(c.ctx, *gs)
	require.NoError(c.t, c.k.SetSurface(c.ctx, common.HexToAddress(market.PoolAddress),
		keeper.Surface{Role: types.RolePool, Generation: types.Generation2, Market: market.Denom}))
	require.NoError(c.t, c.k.SetSurface(c.ctx, roles.FeeAccumulator,
		keeper.Surface{Role: types.RoleFeeAccumulator, Generation: types.Generation2}))
	granted, err := c.k.GovernanceGrant(c.ctx, c.k.Authority(), roles.FeeAccumulator, keeper.FeesRouterRole, roles.FeesRouter)
	require.NoError(c.t, err)
	require.True(c.t, granted)
	if held := fees + airdropBalance; held > 0 {
		c.approve(c.holder, held)
		c.pull(c.holder, keeper.FeeHeldLedger(roles.FeeAccumulator), held)
	}
	return market
}

func (c *custodyEnv) airdropEpoch(pool common.Address, epoch uint64) types.PoolAirdropEpoch {
	c.t.Helper()
	record, found, err := c.k.GetPoolAirdropEpoch(c.ctx, pool, epoch)
	require.NoError(c.t, err)
	require.True(c.t, found)
	return record
}

func (c *custodyEnv) claim(pool, holder common.Address, epoch uint64) (sdk.Int, error) {
	c.t.Helper()
	ctx, amount, err := c.k.ClaimAirdropEpoch(c.ctx, c.caller, pool, holder, epoch)
	if err == nil {
		c.ctx = ctx
	}
	return amount, err
}

func TestAirdropEpochs(t *testing.T) {
	t.Run("opens epoch e+1 at the token checkpoints", func(t *testing.T) {
		c := newCustodyEnv(t)
		roles := c.k.Generation2()
		pool := common.HexToAddress(generation2Market(roles.SidioraNFT, 1, nil).PoolAddress)
		dead := common.HexToAddress(types.DeadAddress)
		a, b, holderC := address("holder A"), address("holder B"), address("holder C")
		h := c.ctx.BlockHeight() + 1
		c.height(h)
		token := c.poolToken("Pool Token", holding{pool, 600}, holding{dead, 100}, holding{a, 100}, holding{b, 200})
		market := c.airdropMarket(token, 50_000, 1, 0)

		c.height(h + 10)
		_, err := c.k.TriggerAirdrop(c.ctx, c.caller, address("stranger"), pool)
		require.ErrorIs(t, err, types.ErrMissingRole)
		amount, err := c.k.TriggerAirdrop(c.ctx, c.caller, roles.FeesRouter, pool)
		require.NoError(t, err)
		require.Equal(t, "50000", amount.String())

		got := c.market(market.Denom)
		require.Equal(t, uint64(2), got.AirdropEpoch)
		require.Equal(t, "0", got.AccumulatedFees.String())
		require.Equal(t, "50000", got.AirdropBalance.String())
		snapshot := uint64(h + 9)
		past := common.LeftPadBytes(new(big.Int).SetUint64(snapshot).Bytes(), 32)
		require.Equal(t, int64(1_000), c.tokenRead(token, "getPastTotalSupply(uint256)", past))
		require.Equal(t, int64(600), c.tokenRead(token, "getPastBalance(address,uint256)",
			common.LeftPadBytes(pool.Bytes(), 32), past))
		require.Equal(t, int64(100), c.tokenRead(token, "getPastBalance(address,uint256)",
			common.LeftPadBytes(dead.Bytes(), 32), past))
		sameJSON(t, types.PoolAirdropEpoch{Pool: pool.Hex(), Epoch: 2, Amount: i(50_000), SnapshotBlock: snapshot,
			EligibleSupply: i(300), Remaining: i(50_000), ClaimedSupply: i(0), LegacyRemaining: i(0)},
			c.airdropEpoch(pool, 2))
		require.Equal(t, int64(50_000), c.ledger(keeper.FeeHeldLedger(roles.FeeAccumulator)))

		// The pool's fees are spent, and the epoch before the upgrade has no
		// snapshot fields to claim against.
		_, err = c.k.TriggerAirdrop(c.ctx, c.caller, roles.FeesRouter, pool)
		require.ErrorIs(t, err, types.ErrNoFeesAccumulated)
		_, err = c.claim(pool, a, 1)
		require.ErrorIs(t, err, types.ErrAirdropNotTriggered)

		// Transfers after the snapshot do not move the entitlement.
		c.height(h + 11)
		c.tokenTransfer(token, a, holderC, big.NewInt(100))
		c.height(h + 12)
		before := len(c.quoteLogs())
		amount, err = c.claim(pool, a, 2)
		require.NoError(t, err)
		require.Equal(t, "16666", amount.String())
		_, err = c.claim(pool, holderC, 2)
		require.ErrorIs(t, err, types.ErrZeroAmount)
		amount, err = c.claim(pool, b, 2)
		require.NoError(t, err)
		require.Equal(t, "33334", amount.String())
		require.ElementsMatch(t, []transferLog{
			{from: c.custody, to: a, amount: 16_666},
			{from: c.custody, to: b, amount: 33_334},
		}, c.quoteSince(before))
		sameJSON(t, types.PoolAirdropEpoch{Pool: pool.Hex(), Epoch: 2, Amount: i(50_000), SnapshotBlock: snapshot,
			EligibleSupply: i(300), Remaining: i(0), ClaimedSupply: i(300), LegacyRemaining: i(0)},
			c.airdropEpoch(pool, 2))
		require.Equal(t, "0", c.market(market.Denom).AirdropBalance.String())
		require.Equal(t, int64(0), c.ledger(keeper.FeeHeldLedger(roles.FeeAccumulator)))
		require.True(t, c.k.HasClaimedPoolAirdrop(c.ctx, pool, a, 2))
		require.False(t, c.k.HasClaimedPoolAirdrop(c.ctx, pool, holderC, 2))
	})

	t.Run("token without checkpoints", func(t *testing.T) {
		c := newCustodyEnv(t)
		roles := c.k.Generation2()
		market := c.airdropMarket(address("codeless token"), 50_000, 0, 0)
		pool := common.HexToAddress(market.PoolAddress)
		_, err := c.k.TriggerAirdrop(c.ctx, c.caller, roles.FeesRouter, pool)
		require.ErrorIs(t, err, types.ErrUnsupportedSnapshotToken)
		sameJSON(t, market, c.market(market.Denom))
		_, found, err := c.k.GetPoolAirdropEpoch(c.ctx, pool, 1)
		require.NoError(t, err)
		require.False(t, found)
	})

	t.Run("nothing eligible", func(t *testing.T) {
		c := newCustodyEnv(t)
		roles := c.k.Generation2()
		pool := common.HexToAddress(generation2Market(roles.SidioraNFT, 1, nil).PoolAddress)
		h := c.ctx.BlockHeight() + 1
		c.height(h)
		token := c.poolToken("Held Token", holding{pool, 700}, holding{common.HexToAddress(types.DeadAddress), 300})
		market := c.airdropMarket(token, 50_000, 0, 0)
		c.height(h + 2)
		_, err := c.k.TriggerAirdrop(c.ctx, c.caller, roles.FeesRouter, pool)
		require.ErrorIs(t, err, types.ErrZeroAmount)
		sameJSON(t, market, c.market(market.Denom))
		_, err = c.k.TriggerAirdrop(c.ctx, c.caller, roles.FeesRouter, address("unregistered pool"))
		require.ErrorIs(t, err, types.ErrNoFeesAccumulated)
	})
}

func TestAirdropRoundingResidue(t *testing.T) {
	t.Run("final claimant takes the residue", func(t *testing.T) {
		c := newCustodyEnv(t)
		roles := c.k.Generation2()
		pool := common.HexToAddress(generation2Market(roles.SidioraNFT, 1, nil).PoolAddress)
		dead := common.HexToAddress(types.DeadAddress)
		a, b := address("holder A"), address("holder B")
		h := c.ctx.BlockHeight() + 1
		c.height(h)
		token := c.poolToken("Residue Token", holding{pool, 5}, holding{dead, 2}, holding{a, 1}, holding{b, 2})
		market := c.airdropMarket(token, 10, 0, 0)
		c.height(h + 1)
		amount, err := c.k.TriggerAirdrop(c.ctx, c.caller, roles.FeesRouter, pool)
		require.NoError(t, err)
		require.Equal(t, "10", amount.String())
		require.Equal(t, "3", c.airdropEpoch(pool, 1).EligibleSupply.String())

		for _, refused := range []common.Address{pool, dead, {}} {
			_, err = c.claim(pool, refused, 1)
			require.ErrorIs(t, err, types.ErrUnauthorized)
		}
		for _, epoch := range []uint64{0, 2} {
			_, err = c.claim(pool, a, epoch)
			require.ErrorIs(t, err, types.ErrAirdropNotTriggered)
		}
		_, err = c.claim(address("unregistered pool"), a, 1)
		require.ErrorIs(t, err, types.ErrAirdropNotTriggered)

		before := len(c.quoteLogs())
		amount, err = c.claim(pool, a, 1)
		require.NoError(t, err)
		require.Equal(t, "3", amount.String())
		amount, err = c.claim(pool, b, 1)
		require.NoError(t, err)
		require.Equal(t, "7", amount.String())
		_, err = c.claim(pool, a, 1)
		require.ErrorIs(t, err, types.ErrAlreadyClaimed)
		require.ElementsMatch(t, []transferLog{
			{from: c.custody, to: a, amount: 3},
			{from: c.custody, to: b, amount: 7},
		}, c.quoteSince(before))
		require.Equal(t, int64(3), c.balanceOf(a))
		require.Equal(t, int64(7), c.balanceOf(b))
		require.Equal(t, int64(0), c.balanceOf(c.custody))
		record := c.airdropEpoch(pool, 1)
		require.Equal(t, "0", record.Remaining.String())
		require.Equal(t, "3", record.ClaimedSupply.String())
		require.Equal(t, "0", c.market(market.Denom).AirdropBalance.String())
	})

	t.Run("a share that rounds to zero", func(t *testing.T) {
		c := newCustodyEnv(t)
		roles := c.k.Generation2()
		pool := common.HexToAddress(generation2Market(roles.SidioraNFT, 1, nil).PoolAddress)
		a, b := address("holder A"), address("holder B")
		h := c.ctx.BlockHeight() + 1
		c.height(h)
		token := c.poolToken("Dust Token", holding{a, 1}, holding{b, 2})
		c.airdropMarket(token, 1, 0, 0)
		c.height(h + 1)
		_, err := c.k.TriggerAirdrop(c.ctx, c.caller, roles.FeesRouter, pool)
		require.NoError(t, err)
		before := len(c.quoteLogs())
		amount, err := c.claim(pool, a, 1)
		require.NoError(t, err)
		require.Equal(t, "0", amount.String())
		_, err = c.claim(pool, a, 1)
		require.ErrorIs(t, err, types.ErrAlreadyClaimed)
		amount, err = c.claim(pool, b, 1)
		require.NoError(t, err)
		require.Equal(t, "1", amount.String())
		require.ElementsMatch(t, []transferLog{
			{from: c.custody, to: a, amount: 0},
			{from: c.custody, to: b, amount: 1},
		}, c.quoteSince(before))
		require.Equal(t, int64(0), c.ledger(keeper.FeeHeldLedger(roles.FeeAccumulator)))
	})
}

// legacyLeaf is keccak256(abi.encode(pool, epoch, holder, amount)).
func legacyLeaf(t *testing.T, pool common.Address, epoch uint64, holder common.Address, amount int64) common.Hash {
	t.Helper()
	addressType, err := abi.NewType("address", "", nil)
	require.NoError(t, err)
	uintType, err := abi.NewType("uint256", "", nil)
	require.NoError(t, err)
	encoded, err := abi.Arguments{{Type: addressType}, {Type: uintType}, {Type: addressType}, {Type: uintType}}.
		Pack(pool, new(big.Int).SetUint64(epoch), holder, big.NewInt(amount))
	require.NoError(t, err)
	return crypto.Keccak256Hash(encoded)
}

func sortedPair(x, y common.Hash) common.Hash {
	if bytes.Compare(x.Bytes(), y.Bytes()) < 0 {
		return crypto.Keccak256Hash(x.Bytes(), y.Bytes())
	}
	return crypto.Keccak256Hash(y.Bytes(), x.Bytes())
}

func TestLegacyAirdropProof(t *testing.T) {
	c := newCustodyEnv(t)
	roles := c.k.Generation2()
	fees := keeper.FeeHeldLedger(roles.FeeAccumulator)
	a, b, holderC, d := address("holder A"), address("holder B"), address("holder C"), address("holder D")
	market := c.airdropMarket(address("migrated token"), 0, 1, 1_000)
	pool := common.HexToAddress(market.PoolAddress)

	leaves := []common.Hash{legacyLeaf(t, pool, 1, a, 400), legacyLeaf(t, pool, 1, b, 350),
		legacyLeaf(t, pool, 1, holderC, 250), legacyLeaf(t, pool, 1, d, 2_000)}
	left, right := sortedPair(leaves[0], leaves[1]), sortedPair(leaves[2], leaves[3])
	root := sortedPair(left, right)
	proofA, proofB, proofD := []common.Hash{leaves[1], right}, []common.Hash{leaves[0], right}, []common.Hash{leaves[2], left}

	record := types.PoolAirdropEpoch{Pool: pool.Hex(), Epoch: 1, Amount: i(1_000), EligibleSupply: i(0), Remaining: i(0),
		ClaimedSupply: i(0), LegacyRoot: root.Hex(), LegacyRemaining: i(1_000)}
	unopened := record
	unopened.Epoch = 2
	require.ErrorIs(t, c.k.SetPoolAirdropEpoch(c.ctx, unopened), types.ErrAirdropBasis)
	malformed := record
	malformed.EligibleSupply = i(1)
	require.ErrorIs(t, c.k.SetPoolAirdropEpoch(c.ctx, malformed), types.ErrAirdropBasis)
	require.NoError(t, c.k.SetPoolAirdropEpoch(c.ctx, record))

	legacy := func(holder common.Address, amount int64, proof []common.Hash) error {
		ctx, err := c.k.ClaimLegacyAirdrop(c.ctx, c.caller, pool, 1, holder, i(amount), proof)
		if err == nil {
			c.ctx = ctx
		}
		return err
	}
	_, err := c.claim(pool, a, 1)
	require.ErrorIs(t, err, types.ErrAirdropNotTriggered)
	require.ErrorIs(t, legacy(a, 401, proofA), types.ErrUnauthorized)
	require.ErrorIs(t, legacy(a, 400, proofB), types.ErrUnauthorized)
	require.ErrorIs(t, legacy(a, 0, proofA), types.ErrUnauthorized)
	require.ErrorIs(t, legacy(common.Address{}, 400, proofA), types.ErrUnauthorized)
	require.ErrorIs(t, legacy(d, 2_000, proofD), types.ErrUnauthorized)
	_, err = c.k.ClaimLegacyAirdrop(c.ctx, c.caller, address("unregistered pool"), 1, a, i(400), proofA)
	require.ErrorIs(t, err, types.ErrUnauthorized)

	before := len(c.quoteLogs())
	require.NoError(t, legacy(a, 400, proofA))
	require.Equal(t, []transferLog{{from: c.custody, to: a, amount: 400}}, c.quoteSince(before))
	require.Equal(t, int64(400), c.balanceOf(a))
	require.Equal(t, int64(600), c.ledger(fees))
	require.Equal(t, "600", c.airdropEpoch(pool, 1).LegacyRemaining.String())
	require.Equal(t, "600", c.market(market.Denom).AirdropBalance.String())
	require.ErrorIs(t, legacy(a, 400, proofA), types.ErrAlreadyClaimed)

	require.NoError(t, legacy(b, 350, proofB))
	require.Equal(t, int64(350), c.balanceOf(b))
	require.Equal(t, int64(250), c.ledger(fees))
	require.Equal(t, "250", c.airdropEpoch(pool, 1).LegacyRemaining.String())
	require.Len(t, c.quoteLogs(), before+2)
}
