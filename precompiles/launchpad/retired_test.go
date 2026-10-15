package launchpad_test

import (
	"encoding/hex"
	"os"
	"path/filepath"
	"testing"

	"github.com/Sidiora-Labs/Paxeer-X-Network/precompiles/launchpad"
	testkeeper "github.com/Sidiora-Labs/Paxeer-X-Network/testutil/keeper"
	"github.com/ethereum/go-ethereum/accounts/abi"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/vm"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/holiman/uint256"
	"github.com/stretchr/testify/require"
)

// retiredSet is the table of contracts/launchpad.md section 21; the Treasury
// replacement is the generation 2 Treasury of
// modules/launchpad/keeper/testdata/v6120/layout.json.
var retiredSet = []struct {
	role        string
	retired     common.Address
	replacement common.Address
}{
	{"SidioraFactory", common.HexToAddress("0x8a1A09CEe72c1D39dF33B8284E38baeF8371f465"), common.HexToAddress("0x3993D004166738d1069B5C4e0a923f7B9D5D8830")},
	{"Router", common.HexToAddress("0xCC7298801112682e10ee14b8a520309caD80336d"), common.HexToAddress("0x78FB06f525cD964388f2305A489035263E31C6e4")},
	{"Quoter", common.HexToAddress("0xB768e183b6EfDeDf8b2AA7af732039D1C3c452d0"), common.HexToAddress("0x793006f4F321b8863F26DBD75b11bAa01DA181dc")},
	{"ProtocolConfig", common.HexToAddress("0xEeDF5409cFD30bd14D0399318c7d2150265575e5"), common.HexToAddress("0xF3669eDc475Cb6E1F0d7635240ABD8e8FB7E20C6")},
	{"FeeAccumulator", common.HexToAddress("0x50C69dF6637b3DCE6a7407C5A4b4F99E68514A76"), common.HexToAddress("0x7b14808b72f61664cDb79D25E6d1587655571a66")},
	{"PoolRegistry", common.HexToAddress("0x7684382c89f79104574D8EF9b31eFf2eD2C2BA0b"), common.HexToAddress("0x1dC357d2B7B850332a91F06A9E66634F46fC4Ba7")},
	{"SidioraNFT", common.HexToAddress("0xDF73b354ed9dcB473cc9D01541c46f507591e190"), common.HexToAddress("0xe8DcC844a8006dbf64f5Bb851bCad90292a6409B")},
	{"FeesRouter", common.HexToAddress("0x02Df12a44F2658080E76fbcF7D6B34Baa97843b6"), common.HexToAddress("0x05681dA6f611C2dfFD3E92611F6dE2d247647927")},
	{"Treasury", common.HexToAddress("0x15405D535ce533BfFb98c83e42f4DD242AA5e079"), common.HexToAddress("0xBb82d74E071e03d8E68F1d78Fae9EFe8b6b7A723")},
	{"OpticalRegistry", common.HexToAddress("0x4CdA6e48632d51Ee4Fa735D81BF09F7543f644a1"), common.HexToAddress("0x0FB5E510372Bd3EddFED16A5Dd766d4473CffcF0")},
}

// retiredInputs returns every function selector of the role file of role
// with zero arguments appended, plus empty calldata, a short selector and an
// unknown selector; OpticalRegistry has no role file and gets the latter three.
func retiredInputs(t *testing.T, role string) [][]byte {
	t.Helper()
	inputs := [][]byte{{}, {0x01, 0x02, 0x03}, append([]byte{0xff, 0xff, 0xff, 0xff}, make([]byte, 64)...)}
	if role == "OpticalRegistry" {
		return inputs
	}
	file, err := os.Open(filepath.Join("abi", role+".json"))
	require.NoError(t, err, role)
	defer file.Close()
	parsed, err := abi.JSON(file)
	require.NoError(t, err, role)
	require.NotEmpty(t, parsed.Methods, role)
	for _, m := range parsed.Methods {
		inputs = append(inputs, append(append([]byte(nil), m.ID...), make([]byte, 32*len(m.Inputs))...))
	}
	return inputs
}

func retiredRevertData(replacement common.Address) []byte {
	return append(crypto.Keccak256([]byte("Retired(address)"))[:4], common.LeftPadBytes(replacement.Bytes(), 32)...)
}

func TestRetiredAddressesNameTheReplacement(t *testing.T) {
	h := newHarness(t)
	ctx := h.stateDB.Ctx()
	evmKeeper := &testkeeper.EVMTestApp.EvmKeeper
	for _, r := range retiredSet {
		require.NoError(t, launchpad.InstallRetired(ctx, evmKeeper, r.retired, r.replacement), r.role)
	}

	want := retiredRevertData(common.HexToAddress("0x78FB06f525cD964388f2305A489035263E31C6e4"))
	require.Equal(t, "da9d2e31"+"00000000000000000000000078fb06f525cd964388f2305a489035263e31c6e4", hex.EncodeToString(want))

	for _, r := range retiredSet {
		want := retiredRevertData(r.replacement)
		for _, input := range retiredInputs(t, r.role) {
			ret, _, err := h.evm.Call(h.caller, r.retired, input, 1_000_000, uint256.NewInt(0))
			require.ErrorIs(t, err, vm.ErrExecutionReverted, "%s %x", r.role, input)
			require.Equal(t, want, ret, "%s %x", r.role, input)
		}
		ret, _, err := h.evm.StaticCall(h.caller, r.retired, []byte{0xda, 0x9d, 0x2e, 0x31}, 1_000_000)
		require.ErrorIs(t, err, vm.ErrExecutionReverted, r.role)
		require.Equal(t, want, ret, r.role)
	}

	require.EqualError(t, launchpad.InstallRetired(ctx, evmKeeper, retiredSet[0].retired, common.Address{}),
		"retired address needs a non-zero replacement")
	require.EqualError(t, launchpad.InstallRetired(ctx, evmKeeper, retiredSet[0].retired, retiredSet[0].retired),
		"retired address cannot be its own replacement")
}

func TestRetiredStorageIsLeft(t *testing.T) {
	h := newHarness(t)
	ctx := h.stateDB.Ctx()
	evmKeeper := &testkeeper.EVMTestApp.EvmKeeper
	router := retiredSet[1].retired
	slot0 := common.Hash{}
	slot1 := common.BigToHash(common.Big1)
	value0 := common.HexToHash("0x0000000000000000000000000000000000000000000000000000000000000001")
	value1 := common.HexToHash("0x00000000000000000000000085fcd13735f4309833a503ee804ea32395851479")
	evmKeeper.SetCode(ctx, router, []byte{0x60, 0x00, 0x60, 0x00, 0xf3})
	evmKeeper.SetState(ctx, router, slot0, value0)
	evmKeeper.SetState(ctx, router, slot1, value1)

	eventEmitter := common.HexToAddress("0x0E10286EE51F99c666CDcAb52451e58AbdA4048F")
	poolBeacon := common.HexToAddress("0xf11f08afe33e020Cab22bCaffBbAfC471c75E9d4")
	emitterCode := []byte{0x60, 0x01, 0x60, 0x00, 0x52, 0x60, 0x20, 0x60, 0x00, 0xf3}
	beaconCode := []byte{0x60, 0x02, 0x60, 0x00, 0x52, 0x60, 0x20, 0x60, 0x00, 0xf3}
	evmKeeper.SetCode(ctx, eventEmitter, emitterCode)
	evmKeeper.SetCode(ctx, poolBeacon, beaconCode)

	for _, r := range retiredSet {
		require.NoError(t, launchpad.InstallRetired(ctx, evmKeeper, r.retired, r.replacement), r.role)
	}

	require.Equal(t, value0, evmKeeper.GetState(ctx, router, slot0))
	require.Equal(t, value1, evmKeeper.GetState(ctx, router, slot1))
	require.Equal(t, value0, h.stateDB.GetState(router, slot0))
	require.Equal(t, emitterCode, evmKeeper.GetCode(ctx, eventEmitter))
	require.Equal(t, beaconCode, evmKeeper.GetCode(ctx, poolBeacon))
	require.Equal(t, 40, evmKeeper.GetCodeSize(ctx, router))
}
