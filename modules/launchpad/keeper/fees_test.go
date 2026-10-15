package keeper_test

import (
	"math/big"
	"testing"

	"github.com/Sidiora-Labs/Paxeer-X-Network/modules/launchpad/keeper"
	"github.com/Sidiora-Labs/Paxeer-X-Network/modules/launchpad/types"
	sdk "github.com/Sidiora-Labs/Paxeer-X-Network/sdk/types"
	testkeeper "github.com/Sidiora-Labs/Paxeer-X-Network/testutil/keeper"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/stretchr/testify/require"
)

// feeSplitOpticalCreation is the creation code of an optical fee hook
// contract (solc 0.8.27, optimizer 200 runs) built from:
//
//	contract FeeSplitOptical {
//	    uint8 private immutable flags;
//	    uint256 private immutable keepBps;
//	    bytes4 private immutable ack;
//	    address private immutable accumulator;
//	    uint256 public afterPoolCut;
//	    constructor(uint8 flags_, uint256 keepBps_, bytes4 ack_, address accumulator_) { ... }
//	    function getFlags() external view returns (uint8) { return flags; }
//	    function beforeFeeDistribution(address, uint256 poolCut) external view returns (uint256) {
//	        require(msg.sender == accumulator);
//	        return (poolCut * keepBps) / 10000;
//	    }
//	    function afterFeeDistribution(address, uint256 poolCut) external returns (bytes4) {
//	        require(msg.sender == accumulator);
//	        afterPoolCut = poolCut;
//	        return ack;
//	    }
//	}
const feeSplitOpticalCreation = "0x610100604052348015610010575f5ffd5b5060405161035c38038061035c83398101604081905261002f9161005b565b60ff90931660805260a0919091526001600160e01b03191660c0526001600160a01b031660e0526100c9565b5f5f5f5f6080858703121561006e575f5ffd5b845160ff8116811461007e575f5ffd5b6020860151604087015191955093506001600160e01b0319811681146100a2575f5ffd5b60608601519092506001600160a01b03811681146100be575f5ffd5b939692955090935050565b60805160a05160c05160e05161025b6101015f395f818160e5015261014701525f61011501525f61017801525f6055015261025b5ff3fe608060405234801561000f575f5ffd5b506004361061004a575f3560e01c80636cc7149d1461004e578063ae3bd4bd14610084578063c77d2c4a146100b0578063f80d2cc7146100c6575b5f5ffd5b60405160ff7f00000000000000000000000000000000000000000000000000000000000000001681526020015b60405180910390f35b6100976100923660046101ae565b6100d9565b6040516001600160e01b0319909116815260200161007b565b6100b85f5481565b60405190815260200161007b565b6100b86100d43660046101ae565b61013b565b5f336001600160a01b037f0000000000000000000000000000000000000000000000000000000000000000161461010e575f5ffd5b505f8190557f00000000000000000000000000000000000000000000000000000000000000005b92915050565b5f336001600160a01b037f00000000000000000000000000000000000000000000000000000000000000001614610170575f5ffd5b61271061019d7f0000000000000000000000000000000000000000000000000000000000000000846101e3565b6101a79190610206565b9392505050565b5f5f604083850312156101bf575f5ffd5b82356001600160a01b03811681146101d5575f5ffd5b946020939093013593505050565b808202811582820484141761013557634e487b7160e01b5f52601160045260245ffd5b5f8261022057634e487b7160e01b5f52601260045260245ffd5b50049056fea264697066735822122043e7fc003e26e59f9e07e462bbeca64c54979c3c3b350e75995dfca0baca263464736f6c634300081b0033"

var afterFeeDistributionSelector = crypto.Keccak256([]byte("afterFeeDistribution(address,uint256)"))[:4]

// feeEnv is a generation 2 market over the custody environment whose fee
// right, id 1 of the generation 2 collection, is owned by holder.
type feeEnv struct {
	*custodyEnv
	denom       string
	pool        common.Address
	rightHolder common.Address
	accumulator common.Address
	fees        keeper.QuoteLedger
}

// opticalSpec deploys a FeeSplitOptical for the market when flags is set.
type opticalSpec struct {
	flags   uint8
	keepBps int64
	ack     []byte
}

func newFeeEnv(t *testing.T, strategy types.FeeStrategy, spec *opticalSpec) (*feeEnv, common.Address) {
	t.Helper()
	c := newCustodyEnv(t)
	roles := c.k.Generation2()
	var optical common.Address
	if spec != nil {
		deployer := address("optical deployer")
		args := append(append(append(word(big.NewInt(int64(spec.flags))), word(big.NewInt(spec.keepBps))...),
			common.RightPadBytes(spec.ack, 32)...), common.LeftPadBytes(roles.FeeAccumulator.Bytes(), 32)...)
		evm := &testkeeper.EVMTestApp.EvmKeeper
		nonce := evm.GetNonce(c.ctx, deployer)
		_, err := evm.CallEVM(c.ctx, deployer, nil, nil, append(common.FromHex(feeSplitOpticalCreation), args...))
		require.NoError(t, err)
		optical = crypto.CreateAddress(deployer, nonce)
		require.NotEmpty(t, evm.GetCode(c.ctx, optical))
	}
	gs := types.DefaultGenesis()
	gs.Params = c.k.GetParams(c.ctx)
	market := generation2Market(roles.SidioraNFT, 1, sdk.AccAddress(address("legacy holder").Bytes()))
	market.AccumulatedFees = sdk.ZeroInt()
	market.FeeStrategy = strategy
	market.Optical = optical.Hex()
	gs.Markets = []types.Market{market}
	c.k.InitGenesis(c.ctx, *gs)
	require.Equal(t, c.usdl.Hex(), c.k.GetParams(c.ctx).QuoteToken)
	pool, holder := common.HexToAddress(market.PoolAddress), address("fee right holder")
	id, err := c.k.MintFeeRight(c.ctx, roles.SidioraNFT, holder, pool, strategy)
	require.NoError(t, err)
	require.Equal(t, uint64(1), id)
	return &feeEnv{custodyEnv: c, denom: market.Denom, pool: pool, rightHolder: holder,
		accumulator: roles.FeeAccumulator, fees: keeper.FeeHeldLedger(roles.FeeAccumulator)}, optical
}

// fund puts amount of USDL from the holder into FeeHeld, as a pool's fee
// transfer does before recordFee.
func (f *feeEnv) fund(amount int64) {
	f.t.Helper()
	f.approve(f.holder, amount)
	f.pull(f.holder, f.fees, amount)
}

func (f *feeEnv) record(fee int64) (int64, int64) {
	f.t.Helper()
	ctx, protocolCut, poolCut, err := f.k.RecordFee(f.ctx, f.caller, f.denom, i(fee))
	require.NoError(f.t, err)
	f.ctx = ctx
	return protocolCut.Int64(), poolCut.Int64()
}

func (f *feeEnv) accumulated() int64 { return f.market(f.denom).AccumulatedFees.Int64() }

func (f *feeEnv) liabilities() int64 {
	f.t.Helper()
	total, err := f.k.TotalLiabilities(f.ctx)
	require.NoError(f.t, err)
	return total.Int64()
}

func (f *feeEnv) surplus() int64 {
	f.t.Helper()
	surplus, err := f.k.OpticalSurplus(f.ctx, f.pool)
	require.NoError(f.t, err)
	return surplus.Int64()
}

func (f *feeEnv) event(kind string) map[string]string {
	f.t.Helper()
	var found map[string]string
	for _, e := range f.ctx.EventManager().Events() {
		if e.Type != kind {
			continue
		}
		require.Nil(f.t, found, "second %s event", kind)
		found = map[string]string{}
		for _, a := range e.Attributes {
			found[string(a.Key)] = string(a.Value)
		}
	}
	require.NotNil(f.t, found, "no %s event", kind)
	return found
}

func (f *feeEnv) acc(a common.Address) sdk.AccAddress { return sdk.AccAddress(a.Bytes()) }

func TestRecordFeeSplit(t *testing.T) {
	t.Run("protocol cut to the Treasury", func(t *testing.T) {
		f, _ := newFeeEnv(t, types.FeeStrategyClaim, nil)
		require.Equal(t, uint64(1000), f.k.GetParams(f.ctx).ProtocolFeeBps)
		f.fund(30_000)
		require.Equal(t, int64(30_000), f.ledger(f.fees))
		f.ctx = f.ctx.WithEventManager(sdk.NewEventManager())
		before := len(f.logs())

		protocolCut, poolCut := f.record(30_000)
		require.Equal(t, int64(3_000), protocolCut)
		require.Equal(t, int64(27_000), poolCut)
		require.Equal(t, int64(27_000), f.ledger(f.fees))
		require.Equal(t, int64(3_000), f.ledger(keeper.TreasuryHeldLedger()))
		treasury, err := f.k.TreasuryGetBalance(f.ctx, f.usdl)
		require.NoError(t, err)
		require.Equal(t, int64(3_000), treasury.Int64())
		require.Equal(t, int64(27_000), f.accumulated())
		require.Equal(t, int64(27_000), f.liabilities())
		require.Equal(t, int64(30_000), f.balanceOf(f.custody))
		require.Empty(t, f.since(before), "the split moves ledgers only")

		require.Equal(t, map[string]string{
			keeper.AttributeKeyContract:  common.HexToAddress("0xBb82d74E071e03d8E68F1d78Fae9EFe8b6b7A723").Hex(),
			keeper.AttributeKeyToken:     f.usdl.Hex(),
			keeper.AttributeKeyDepositor: common.HexToAddress("0x7b14808b72f61664cDb79D25E6d1587655571a66").Hex(),
			types.AttributeKeyAmount:     "3000",
		}, f.event(keeper.EventTypeDeposited))
		require.Equal(t, map[string]string{
			keeper.AttributeKeyContract:   f.accumulator.Hex(),
			types.AttributeKeyDenom:       f.denom,
			keeper.AttributeKeyPool:       f.pool.Hex(),
			types.AttributeKeyFee:         "30000",
			types.AttributeKeyProtocolCut: "3000",
			types.AttributeKeyPoolCut:     "27000",
		}, f.event(types.EventTypeFeeRecorded))

		_, _, _, err = f.k.RecordFee(f.ctx, f.caller, f.denom, sdk.ZeroInt())
		require.ErrorIs(t, err, types.ErrZeroAmount)
		_, _, _, err = f.k.RecordFee(f.ctx, f.caller, "factory/launchpad/missing", i(1))
		require.ErrorIs(t, err, types.ErrUnknownMarket)
	})

	t.Run("optical lowers the pool cut", func(t *testing.T) {
		f, optical := newFeeEnv(t, types.FeeStrategyClaim, &opticalSpec{flags: 12, keepBps: 8_000,
			ack: afterFeeDistributionSelector})
		f.fund(30_000)
		f.ctx = f.ctx.WithEventManager(sdk.NewEventManager())
		protocolCut, poolCut := f.record(30_000)
		require.Equal(t, int64(3_000), protocolCut)
		require.Equal(t, int64(21_600), poolCut)
		require.Equal(t, int64(21_600), f.accumulated())
		require.Equal(t, int64(5_400), f.surplus())
		require.Equal(t, int64(27_000), f.liabilities())
		require.Equal(t, int64(27_000), f.ledger(f.fees))
		require.Equal(t, int64(3_000), f.ledger(keeper.TreasuryHeldLedger()))
		ret, err := testkeeper.EVMTestApp.EvmKeeper.StaticCallEVM(f.ctx, f.k.ModuleAddress(), &optical,
			crypto.Keccak256([]byte("afterPoolCut()"))[:4])
		require.NoError(t, err)
		require.Equal(t, word(big.NewInt(21_600)), ret, "afterFeeDistribution sees the adjusted pool cut")
		require.Equal(t, map[string]string{
			keeper.AttributeKeyContract: f.accumulator.Hex(),
			keeper.AttributeKeyPool:     f.pool.Hex(),
			types.AttributeKeyAmount:    "5400",
		}, f.event(keeper.EventTypeOpticalSurplusRecorded))
		require.Equal(t, "21600", f.event(types.EventTypeFeeRecorded)[types.AttributeKeyPoolCut])
	})

	t.Run("optical raising the pool cut refuses", func(t *testing.T) {
		f, _ := newFeeEnv(t, types.FeeStrategyClaim, &opticalSpec{flags: 4, keepBps: 10_001,
			ack: afterFeeDistributionSelector})
		f.fund(30_000)
		_, _, _, err := f.k.RecordFee(f.ctx, f.caller, f.denom, i(30_000))
		require.ErrorIs(t, err, types.ErrInsufficientSurplus)
	})

	t.Run("afterFeeDistribution must return its selector", func(t *testing.T) {
		f, _ := newFeeEnv(t, types.FeeStrategyClaim, &opticalSpec{flags: 8, keepBps: 10_000,
			ack: []byte{0xde, 0xad, 0xbe, 0xef}})
		f.fund(30_000)
		_, _, _, err := f.k.RecordFee(f.ctx, f.caller, f.denom, i(30_000))
		require.ErrorIs(t, err, types.ErrUnauthorized)
	})

	t.Run("flags without fee hooks leave the split", func(t *testing.T) {
		f, _ := newFeeEnv(t, types.FeeStrategyClaim, &opticalSpec{flags: 3, keepBps: 0,
			ack: []byte{0xde, 0xad, 0xbe, 0xef}})
		f.fund(30_000)
		_, poolCut := f.record(30_000)
		require.Equal(t, int64(27_000), poolCut)
		require.Zero(t, f.surplus())
	})
}

func TestStrategyDistributions(t *testing.T) {
	stranger := address("stranger Y")
	recipient := address("recipient R")

	t.Run("claim", func(t *testing.T) {
		f, _ := newFeeEnv(t, types.FeeStrategyClaim, nil)
		f.fund(30_000)
		f.record(30_000)
		require.Equal(t, int64(27_000), f.ledger(f.fees))

		_, err := f.k.ClaimFees(f.ctx, f.acc(stranger), f.denom, f.acc(recipient))
		require.ErrorIs(t, err, types.ErrNotFeeRightsHolder)
		_, err = f.k.ExecuteBurn(f.ctx, f.acc(f.rightHolder), f.denom)
		require.ErrorIs(t, err, types.ErrWrongStrategy)
		_, err = f.k.ClaimFees(f.ctx, f.acc(f.rightHolder), f.denom, sdk.AccAddress{})
		require.ErrorIs(t, err, types.ErrZeroAddress)
		require.Equal(t, int64(27_000), f.ledger(f.fees))

		before := len(f.logs())
		amount, err := f.k.ClaimFees(f.ctx, f.acc(f.rightHolder), f.denom, f.acc(recipient))
		require.NoError(t, err)
		require.Equal(t, int64(27_000), amount.Int64())
		require.Equal(t, []transferLog{{from: f.custody, to: recipient, amount: 27_000}}, f.since(before))
		require.Equal(t, int64(27_000), f.balanceOf(recipient))
		require.Zero(t, f.ledger(f.fees))
		require.Zero(t, f.accumulated())
		require.Zero(t, f.liabilities())

		_, err = f.k.ClaimFees(f.ctx, f.acc(f.rightHolder), f.denom, f.acc(recipient))
		require.ErrorIs(t, err, types.ErrNoFeesAccumulated)
	})

	t.Run("burn after a strategy change", func(t *testing.T) {
		f, _ := newFeeEnv(t, types.FeeStrategyClaim, nil)
		f.fund(30_000)
		f.record(30_000)
		require.ErrorIs(t, f.k.SetFeeStrategy(f.ctx, f.acc(stranger), f.denom, types.FeeStrategyBurn),
			types.ErrNotFeeRightsHolder)
		require.NoError(t, f.k.SetFeeStrategy(f.ctx, f.acc(f.rightHolder), f.denom, types.FeeStrategyBurn))
		require.Equal(t, types.FeeStrategyBurn, f.market(f.denom).FeeStrategy)
		_, err := f.k.ClaimFees(f.ctx, f.acc(f.rightHolder), f.denom, f.acc(recipient))
		require.ErrorIs(t, err, types.ErrWrongStrategy)

		before := len(f.logs())
		amount, err := f.k.ExecuteBurn(f.ctx, f.acc(f.rightHolder), f.denom)
		require.NoError(t, err)
		require.Equal(t, int64(27_000), amount.Int64())
		dead := common.HexToAddress(types.DeadAddress)
		require.Equal(t, []transferLog{{from: f.custody, to: dead, amount: 27_000}}, f.since(before))
		require.Equal(t, int64(27_000), f.balanceOf(dead))
		require.Zero(t, f.ledger(f.fees))
		require.Zero(t, f.liabilities())
		_, err = f.k.ExecuteBurn(f.ctx, f.acc(f.rightHolder), f.denom)
		require.ErrorIs(t, err, types.ErrNoFeesAccumulated)
	})

	t.Run("lp rewards", func(t *testing.T) {
		f, _ := newFeeEnv(t, types.FeeStrategyLpRewards, nil)
		f.fund(30_000)
		f.record(30_000)
		realQuote := f.market(f.denom).RealQuoteBalance

		before := len(f.logs())
		amount, err := f.k.ExecuteLpRewards(f.ctx, f.acc(f.rightHolder), f.denom)
		require.NoError(t, err)
		require.Equal(t, int64(27_000), amount.Int64())
		require.Empty(t, f.since(before), "the rewards stay in custody")
		require.Zero(t, f.ledger(f.fees))
		require.Equal(t, int64(27_000), f.ledger(keeper.HeldQuoteLedger(f.pool)))
		require.Equal(t, realQuote.Add(i(27_000)).String(), f.market(f.denom).RealQuoteBalance.String())
		require.Zero(t, f.accumulated())
		require.Zero(t, f.liabilities())
		require.Equal(t, int64(30_000), f.balanceOf(f.custody))
		_, err = f.k.ExecuteLpRewards(f.ctx, f.acc(f.rightHolder), f.denom)
		require.ErrorIs(t, err, types.ErrNoFeesAccumulated)
	})
}

func TestLiabilityBacking(t *testing.T) {
	f, optical := newFeeEnv(t, types.FeeStrategyClaim, &opticalSpec{flags: 4, keepBps: 8_000,
		ack: afterFeeDistributionSelector})
	recipient := address("surplus recipient")
	f.fund(30_000)
	f.record(30_000)
	require.Equal(t, int64(5_400), f.surplus())
	require.Equal(t, f.ledger(f.fees), f.liabilities(), "FeeHeld equals totalLiabilities")
	held := f.ledger(f.fees)

	_, _, _, err := f.k.RecordFee(f.ctx, f.caller, f.denom, i(1))
	require.ErrorIs(t, err, types.ErrInsufficientSurplus)
	require.ErrorIs(t, f.k.RecordOpticalSurplus(f.ctx, f.denom, i(1)), types.ErrInsufficientSurplus)

	_, err = f.k.ClaimOpticalSurplus(f.ctx, f.caller, optical, f.denom, i(5_401), recipient)
	require.ErrorIs(t, err, types.ErrInsufficientSurplus)
	_, err = f.k.ClaimOpticalSurplus(f.ctx, f.caller, f.rightHolder, f.denom, i(1), recipient)
	require.ErrorIs(t, err, types.ErrUnauthorized)
	_, err = f.k.ClaimOpticalSurplus(f.ctx, f.caller, optical, f.denom, i(1), common.Address{})
	require.ErrorIs(t, err, types.ErrZeroAddress)
	_, err = f.k.ClaimOpticalSurplus(f.ctx, f.caller, optical, f.denom, sdk.ZeroInt(), recipient)
	require.ErrorIs(t, err, types.ErrZeroAmount)
	require.Equal(t, held, f.ledger(f.fees))
	require.Equal(t, held, f.liabilities())
	require.Equal(t, int64(5_400), f.surplus())

	before := len(f.logs())
	ctx, err := f.k.ClaimOpticalSurplus(f.ctx, f.caller, optical, f.denom, i(5_400), recipient)
	require.NoError(t, err)
	f.ctx = ctx
	require.Equal(t, []transferLog{{from: f.custody, to: recipient, amount: 5_400}}, f.since(before))
	require.Zero(t, f.surplus())
	require.Equal(t, int64(21_600), f.ledger(f.fees))
	require.Equal(t, int64(21_600), f.liabilities())

	require.NoError(t, f.k.RecordOpticalSurplus(f.ctx, f.denom, sdk.ZeroInt()))
	require.Equal(t, int64(21_600), f.liabilities())
	f.fund(1_000)
	require.NoError(t, f.k.RecordOpticalSurplus(f.ctx, f.denom, i(1_000)))
	require.Equal(t, int64(1_000), f.surplus())
	require.Equal(t, int64(22_600), f.liabilities())
	require.Equal(t, f.ledger(f.fees), f.liabilities())
	require.ErrorIs(t, f.k.RecordOpticalSurplus(f.ctx, f.denom, i(1)), types.ErrInsufficientSurplus)
}
