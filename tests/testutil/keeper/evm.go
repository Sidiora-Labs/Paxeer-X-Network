package keeper

import (
	"encoding/hex"
	"sync"
	"testing"
	"time"

	"github.com/Sidiora-Labs/Paxeer-X-Network/sdk/crypto/hd"
	cryptotypes "github.com/Sidiora-Labs/Paxeer-X-Network/sdk/crypto/types"
	sdk "github.com/Sidiora-Labs/Paxeer-X-Network/sdk/types"
	"github.com/cosmos/go-bip39"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/crypto"

	evmkeeper "github.com/Sidiora-Labs/Paxeer-X-Network/modules/evm/keeper"
	evmtypes "github.com/Sidiora-Labs/Paxeer-X-Network/modules/evm/types"
	app "github.com/Sidiora-Labs/Paxeer-X-Network/node"
	"github.com/stretchr/testify/require"
)

// EVMTestApp is a chain past the v6.12.0 plan: its state records the plan as
// done at the first block, so it serves the v6.12.0 precompile set from that
// height on.
var EVMTestApp = v6120Applied(app.SetupWithDefaultHome(false, true, false))
var mockKeeper *evmkeeper.Keeper
var mockCtx sdk.Context
var mtx = &sync.Mutex{}

// v6120Applied records the v6.12.0 plan as done at height 1 in the deliver
// state of a and reselects its custom precompile set from that state.
func v6120Applied(a *app.App) *app.App {
	ctx := a.GetContextForDeliverTx([]byte{}).WithBlockHeight(1)
	a.UpgradeKeeper.SetDone(ctx, app.V6120Upgrade)
	a.RefreshPrecompiles(ctx)
	return a
}

func MockEVMKeeperWithPrecompiles() (*evmkeeper.Keeper, sdk.Context) {
	mtx.Lock()
	defer mtx.Unlock()
	if mockKeeper != nil {
		return mockKeeper, mockCtx
	}
	ctx := EVMTestApp.GetContextForDeliverTx([]byte{}).WithBlockHeight(8)
	k := EVMTestApp.EvmKeeper
	k.InitGenesis(ctx, *evmtypes.DefaultGenesis())

	// mint some coins to a pax address
	paxAddr, err := sdk.AccAddressFromHex(common.Bytes2Hex([]byte("paxAddr")))
	if err != nil {
		panic(err)
	}
	err = EVMTestApp.BankKeeper.MintCoins(ctx, "evm", sdk.NewCoins(sdk.NewCoin("uhpx", sdk.NewInt(10))))
	if err != nil {
		panic(err)
	}
	err = EVMTestApp.BankKeeper.SendCoinsFromModuleToAccount(ctx, "evm", paxAddr, sdk.NewCoins(sdk.NewCoin("uhpx", sdk.NewInt(10))))
	if err != nil {
		panic(err)
	}
	mockKeeper = &k
	mockCtx = ctx
	return &k, ctx
}

func MockEVMKeeper(t *testing.T) (*evmkeeper.Keeper, sdk.Context) {
	testApp := app.Setup(t, false, false, false)
	ctx := testApp.GetContextForDeliverTx([]byte{}).WithBlockHeight(8).WithBlockTime(time.Now())
	k := testApp.EvmKeeper
	k.InitGenesis(ctx, *evmtypes.DefaultGenesis())

	// mint some coins to a pax address
	paxAddr, err := sdk.AccAddressFromHex(common.Bytes2Hex([]byte("paxAddr")))
	if err != nil {
		panic(err)
	}
	err = testApp.BankKeeper.MintCoins(ctx, "evm", sdk.NewCoins(sdk.NewCoin("uhpx", sdk.NewInt(10))))
	if err != nil {
		panic(err)
	}
	err = testApp.BankKeeper.SendCoinsFromModuleToAccount(ctx, "evm", paxAddr, sdk.NewCoins(sdk.NewCoin("uhpx", sdk.NewInt(10))))
	if err != nil {
		panic(err)
	}
	return &k, ctx
}

func MockEVMKeeperPrecompiles(t *testing.T) (*evmkeeper.Keeper, sdk.Context) {
	testApp := app.Setup(t, false, true, false)
	ctx := testApp.GetContextForDeliverTx([]byte{}).WithBlockHeight(8).WithBlockTime(time.Now())
	k := testApp.EvmKeeper
	k.InitGenesis(ctx, *evmtypes.DefaultGenesis())

	// mint some coins to a pax address
	paxAddr, err := sdk.AccAddressFromHex(common.Bytes2Hex([]byte("paxAddr")))
	if err != nil {
		panic(err)
	}
	err = testApp.BankKeeper.MintCoins(ctx, "evm", sdk.NewCoins(sdk.NewCoin("uhpx", sdk.NewInt(10))))
	if err != nil {
		panic(err)
	}
	err = testApp.BankKeeper.SendCoinsFromModuleToAccount(ctx, "evm", paxAddr, sdk.NewCoins(sdk.NewCoin("uhpx", sdk.NewInt(10))))
	if err != nil {
		panic(err)
	}
	return &k, ctx
}

func MockAddressPair() (sdk.AccAddress, common.Address) {
	return PrivateKeyToAddresses(MockPrivateKey())
}

func MockPrivateKey() cryptotypes.PrivKey {
	// Generate a new Pax private key
	entropySeed, _ := bip39.NewEntropy(256)
	mnemonic, _ := bip39.NewMnemonic(entropySeed)
	algo := hd.Secp256k1
	derivedPriv, _ := algo.Derive()(mnemonic, "", "")
	return algo.Generate()(derivedPriv)
}

func PrivateKeyToAddresses(privKey cryptotypes.PrivKey) (sdk.AccAddress, common.Address) {
	// Encode the private key to hex (i.e. what wallets do behind the scene when users reveal private keys)
	testPrivHex := hex.EncodeToString(privKey.Bytes())

	// Sign an Ethereum transaction with the hex private key
	key, _ := crypto.HexToECDSA(testPrivHex)
	msg := crypto.Keccak256([]byte("foo"))
	sig, _ := crypto.Sign(msg, key)

	// Recover the public keys from the Ethereum signature
	recoveredPub, _ := crypto.Ecrecover(msg, sig)
	pubKey, _ := crypto.UnmarshalPubkey(recoveredPub)

	return sdk.AccAddress(privKey.PubKey().Address()), crypto.PubkeyToAddress(*pubKey)
}

func UhpxCoins(amount int64) sdk.Coins {
	return sdk.NewCoins(sdk.NewCoin(sdk.MustGetBaseDenom(), sdk.NewInt(amount)))
}

func WaitForReceipt(t *testing.T, k *evmkeeper.Keeper, ctx sdk.Context, txHash common.Hash) *evmtypes.Receipt {
	t.Helper()
	var receipt *evmtypes.Receipt
	require.Eventually(t, func() bool {
		var err error
		receipt, err = k.GetReceipt(ctx, txHash)
		return err == nil
	}, 2*time.Second, 10*time.Millisecond)
	return receipt
}

func WaitForReceiptFromStore(t *testing.T, k *evmkeeper.Keeper, ctx sdk.Context, txHash common.Hash) *evmtypes.Receipt {
	t.Helper()
	var receipt *evmtypes.Receipt
	require.Eventually(t, func() bool {
		var err error
		receipt, err = k.GetReceiptFromReceiptStore(ctx, txHash)
		return err == nil
	}, 2*time.Second, 10*time.Millisecond)
	return receipt
}

func MustMockReceipt(t *testing.T, k *evmkeeper.Keeper, ctx sdk.Context, txHash common.Hash, receipt *evmtypes.Receipt) {
	t.Helper()
	require.NoError(t, k.MockReceipt(ctx, txHash, receipt))
	WaitForReceipt(t, k, ctx, txHash)
}
