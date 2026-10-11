package sequencer_test

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"math/big"
	"strings"
	"testing"
	"time"

	abci "github.com/Sidiora-Labs/Paxeer-X-Network/consensus/abci/types"
	tmproto "github.com/Sidiora-Labs/Paxeer-X-Network/consensus/proto/tendermint/types"
	"github.com/Sidiora-Labs/Paxeer-X-Network/modules/evm/config"
	evmtypes "github.com/Sidiora-Labs/Paxeer-X-Network/modules/evm/types"
	"github.com/Sidiora-Labs/Paxeer-X-Network/modules/evm/types/ethtx"
	app "github.com/Sidiora-Labs/Paxeer-X-Network/node"
	"github.com/Sidiora-Labs/Paxeer-X-Network/node/sequencer"
	"github.com/Sidiora-Labs/Paxeer-X-Network/occ_tests/utils"
	clienttx "github.com/Sidiora-Labs/Paxeer-X-Network/sdk/client/tx"
	serverconfig "github.com/Sidiora-Labs/Paxeer-X-Network/sdk/server/config"
	sdk "github.com/Sidiora-Labs/Paxeer-X-Network/sdk/types"
	sdkerrors "github.com/Sidiora-Labs/Paxeer-X-Network/sdk/types/errors"
	"github.com/Sidiora-Labs/Paxeer-X-Network/sdk/types/tx/signing"
	authsigning "github.com/Sidiora-Labs/Paxeer-X-Network/sdk/x/auth/signing"
	banktypes "github.com/Sidiora-Labs/Paxeer-X-Network/sdk/x/bank/types"
	"github.com/ethereum/go-ethereum/accounts"
	"github.com/ethereum/go-ethereum/accounts/abi"
	"github.com/ethereum/go-ethereum/common"
	ethtypes "github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/core/vm"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/holiman/uint256"
	"github.com/stretchr/testify/require"
)

const (
	floorGas     = 200_000
	minFeeWei    = 67_000_000
	baseFeeWei   = 70_000_000
	oneGweiInWei = 1_000_000_000
	sponsorGas   = 500_000
	deployGas    = 3_000_000
	sidRate      = 3_114_000
	quoteNonce   = 7
)

// batchCallAndSponsorBin is the creation code of contracts/src/BatchCallAndSponsor.sol
// (solc 0.8.27, optimizer 200 runs, evm cancun, OpenZeppelin 5.3.0).
const batchCallAndSponsorBin = "60c060405234801561000f575f5ffd5b5060405161191438038061191483398101604081905261002e916100cc565b6001600160a01b038316610055576040516349e27cff60e01b815260040160405180910390fd5b815f0361007557604051636a43f8d160e01b815260040160405180910390fd5b805f0361009557604051631bd1807960e11b815260040160405180910390fd5b600480546001600160a01b0319166001600160a01b0394909416939093179092553060805260a0919091526002554260035561010b565b5f5f5f606084860312156100de575f5ffd5b83516001600160a01b03811681146100f4575f5ffd5b602085015160409095015190969495509392505050565b60805160a0516117be6101565f395f81816101d60152610d1501525f8181610279015281816104d50152818161060701528181610b5a01528181610c470152610c6e01526117be5ff3fe608060405260043610610120575f3560e01c80638a783d70116100a4578063b7848f321161006b578063b7848f3214610320578063e30c397814610353578063ea37412214610372578063f2fde38b14610399578063f7581615146103b8578063f9f8bdb7146103eb57005b80638a783d70146102685780638da5cb5b146102b3578063acaec56a146102d2578063affed0e0146102f1578063b1d69c461461030557005b8063454aca9e116100e8578063454aca9e146101b057806357bbdca3146101c55780636a3a4ac4146101f857806379ba5097146102415780637d7d5f5b1461025557005b806322fe2e14146101225780632675398b146101545780632c4e722e1461016957806334fcf4371461017e5780633f707e6b1461019d575b005b34801561012d575f5ffd5b5061014161013c366004611273565b6103ff565b6040519081526020015b60405180910390f35b34801561015f575f5ffd5b5061014160035481565b348015610174575f5ffd5b5061014160025481565b348015610189575f5ffd5b506101206101983660046112c5565b6104ca565b6101206101ab3660046112dc565b6105a6565b3480156101bb575f5ffd5b506101416101f481565b3480156101d0575f5ffd5b506101417f000000000000000000000000000000000000000000000000000000000000000081565b348015610203575f5ffd5b50610231610212366004611336565b600160209081525f928352604080842090915290825290205460ff1681565b604051901515815260200161014b565b34801561024c575f5ffd5b506101206105fc565b61012061026336600461139c565b6106c7565b348015610273575f5ffd5b5061029b7f000000000000000000000000000000000000000000000000000000000000000081565b6040516001600160a01b03909116815260200161014b565b3480156102be575f5ffd5b5060045461029b906001600160a01b031681565b3480156102dd575f5ffd5b506101416102ec366004611450565b610b0e565b3480156102fc575f5ffd5b506101415f5481565b348015610310575f5ffd5b50610141670de0b6b3a764000081565b34801561032b575f5ffd5b506101417f0b2b3120fa5b229cfbb5ade30f9bcced405d42c20fbb2e16868f153f2d17c21481565b34801561035e575f5ffd5b5060055461029b906001600160a01b031681565b34801561037d575f5ffd5b5061029b7321f7b20a555199fa73a238b1a91fd0f549068fee81565b3480156103a4575f5ffd5b506101206103b336600461146a565b610b4f565b3480156103c3575f5ffd5b506101417f67bf3393e6f77b3077bf27fd0604f1bf88341b2d90a493df21db3909fd12045981565b3480156103f6575f5ffd5b50610141610c3b565b5f6104c07f0b2b3120fa5b229cfbb5ade30f9bcced405d42c20fbb2e16868f153f2d17c2145f5486866040516020016104399291906114ab565b6040516020818303038152906040528051906020012061045886610b0e565b6040805160208101959095528401929092526060830152608082015260a0015b604051602081830303815290604052805190602001207f19457468657265756d205369676e6564204d6573736167653a0a3332000000005f908152601c91909152603c902090565b90505b9392505050565b306001600160a01b037f00000000000000000000000000000000000000000000000000000000000000001614610513576040516309ca431160e21b815260040160405180910390fd5b6004546001600160a01b0316331461053e5760405163de271cf560e01b815260040160405180910390fd5b805f0361055e57604051636a43f8d160e01b815260040160405180910390fd5b60028190554260038190556040805183815260208101929092527fb38780ddde1f073d91c150de2696f3f7085883648ba21cc5ef01029cb21d1916910160405180910390a150565b3330146105ee5760405162461bcd60e51b8152602060048201526011602482015270496e76616c696420617574686f7269747960781b60448201526064015b60405180910390fd5b6105f88282610d68565b5050565b306001600160a01b037f00000000000000000000000000000000000000000000000000000000000000001614610645576040516309ca431160e21b815260040160405180910390fd5b6005546001600160a01b031633146106705760405163de271cf560e01b815260040160405180910390fd5b60045460405133916001600160a01b0316907f8be0079c531659141344cd1fd0a4f28419497f9722a3daafe3b4186f6b6457e0905f90a3600480546001600160a01b03199081163317909155600580549091169055565b5f6106d5602087018761146a565b6001600160a01b031614806106fe5750306106f3602087018761146a565b6001600160a01b0316145b8061073557507321f7b20a555199fa73a238b1a91fd0f549068fee610729604087016020880161146a565b6001600160a01b031614155b80610742575060c0850135155b8061074f57506060850135155b1561076d57604051630f86180360e41b815260040160405180910390fd5b846080013542111561079257604051638727a7f960e01b815260040160405180910390fd5b8460400135856060013511156107bb57604051634af7792360e01b815260040160405180910390fd5b60015f6107cb602088018861146a565b6001600160a01b0316815260208082019290925260409081015f90812060a0890135825290925290205460ff1615610816576040516307387c7360e31b815260040160405180910390fd5b5f5f6108616108268a8a8a6103ff565b87878080601f0160208091040260200160405190810160405280939291908181526020018383808284375f92019190915250610dfc92505050565b5090925090505f81600381111561087a5761087a61158a565b14158061089057506001600160a01b0382163014155b156108ae57604051632237f6f560e11b815260040160405180910390fd5b5f5f6108bc6108268a610b0e565b5090925090505f8160038111156108d5576108d561158a565b14158061090057506108ea60208a018a61146a565b6001600160a01b0316826001600160a01b031614155b1561091e576040516340c32ca760e11b815260040160405180910390fd5b61092789610e45565b6001805f61093860208d018d61146a565b6001600160a01b0316815260208082019290925260409081015f90812060a08e013582529092529020805460ff191691151591909117905561097a8b8b610d68565b5f8061098c60408c0160208d0161146a565b6001600160a01b03166109a260208d018d61146a565b6040516001600160a01b03909116602482015260608d0135604482015260640160408051601f198184030181529181526020820180516001600160e01b031663a9059cbb60e01b179052516109f7919061159e565b5f604051808303815f865af19150503d805f8114610a30576040519150601f19603f3d011682016040523d82523d5f602084013e610a35565b606091505b5091509150811580610a4957508051602014155b80610a68575080806020019051810190610a6391906115b4565b600114155b15610a865760405163022e258160e11b815260040160405180910390fd5b610a9660408c0160208d0161146a565b6001600160a01b0316610aac60208d018d61146a565b6001600160a01b03167fec3e39c3c84c30bff1f79d2736cd5c684597be5ea62883a55ccd80153ddf8eb88d606001358e60a00135604051610af7929190918252602082015260400190565b60405180910390a350505050505050505050505050565b5f610b497f67bf3393e6f77b3077bf27fd0604f1bf88341b2d90a493df21db3909fd12045946308560405160200161047894939291906115cb565b92915050565b306001600160a01b037f00000000000000000000000000000000000000000000000000000000000000001614610b98576040516309ca431160e21b815260040160405180910390fd5b6004546001600160a01b03163314610bc35760405163de271cf560e01b815260040160405180910390fd5b6001600160a01b038116610bea576040516349e27cff60e01b815260040160405180910390fd5b600580546001600160a01b0319166001600160a01b03838116918217909255600454604051919216907f38d16b8cac22d99fc7c124b9cd0de2d3fa1faef420bfe791d8c362d765e22700905f90a350565b5f306001600160a01b037f00000000000000000000000000000000000000000000000000000000000000001614610cf1577f00000000000000000000000000000000000000000000000000000000000000006001600160a01b031663f9f8bdb76040518163ffffffff1660e01b8152600401602060405180830381865afa158015610cc8573d5f5f3e3d5ffd5b505050506040513d601f19601f82011682018060405250810190610cec91906115b4565b905090565b6002545f03610d1357604051636a43f8d160e01b815260040160405180910390fd5b7f000000000000000000000000000000000000000000000000000000000000000060035442610d42919061166c565b1115610d615760405163ec30f4ab60e01b815260040160405180910390fd5b5060025490565b5f805490819080610d788361167f565b909155505f90505b82811015610dbc57610db4848483818110610d9d57610d9d611697565b9050602002810190610daf91906116ab565b610edd565b600101610d80565b50807f280bb3599696acbf79fb8ffcde81a57337b52500f789600fbb1cff9b4cbaba398484604051610def9291906114ab565b60405180910390a2505050565b5f5f5f8351604103610e33576020840151604085015160608601515f1a610e2588828585610ff9565b955095509550505050610e3e565b505081515f91506002905b9250925092565b5f610e668260c00135610e56610c3b565b670de0b6b3a764000060016110c1565b90505f610e8582610e7b6101f461271061166c565b61271060016110c1565b90505f610ea283610e9a6101f46127106116c9565b61271061110c565b90508184606001351080610eb95750808460600135115b15610ed75760405163222878b360e01b815260040160405180910390fd5b50505050565b5f610eeb602083018361146a565b6001600160a01b03166020830135610f0660408501856116dc565b604051610f1492919061171f565b5f6040518083038185875af1925050503d805f8114610f4e576040519150601f19603f3d011682016040523d82523d5f602084013e610f53565b606091505b5050905080610f945760405162461bcd60e51b815260206004820152600d60248201526c10d85b1b081c995d995c9d1959609a1b60448201526064016105e5565b610fa1602083018361146a565b6001600160a01b0316337fed7e8f919df9cc0d0ad8b4057d084ebf319b630564d5da283e14751adc931f3a6020850135610fde60408701876116dc565b604051610fed9392919061172e565b60405180910390a35050565b5f80807f7fffffffffffffffffffffffffffffff5d576e7357a4501ddfe92f46681b20a084111561103257505f915060039050826110b7565b604080515f808252602082018084528a905260ff891692820192909252606081018790526080810186905260019060a0016020604051602081039080840390855afa158015611083573d5f5f3e3d5ffd5b5050604051601f1901519150506001600160a01b0381166110ae57505f9250600191508290506110b7565b92505f91508190505b9450945094915050565b5f6110ee6110ce836111bc565b80156110e957505f84806110e4576110e4611747565b868809115b151590565b6110f986868661110c565b61110391906116c9565b95945050505050565b5f5f5f61111986866111e8565b91509150815f0361113d5783818161113357611133611747565b04925050506104c3565b818411611154576111546003851502601118611204565b5f848688095f868103871696879004966002600389028118808a02820302808a02820302808a02820302808a02820302808a02820302808a02909103029181900381900460010185841190960395909502919093039390930492909217029150509392505050565b5f60028260038111156111d1576111d161158a565b6111db919061175b565b60ff166001149050919050565b5f805f1983850993909202808410938190039390930393915050565b634e487b715f52806020526024601cfd5b5f5f83601f840112611225575f5ffd5b50813567ffffffffffffffff81111561123c575f5ffd5b6020830191508360208260051b8501011115611256575f5ffd5b9250929050565b5f60e0828403121561126d575f5ffd5b50919050565b5f5f5f6101008486031215611286575f5ffd5b833567ffffffffffffffff81111561129c575f5ffd5b6112a886828701611215565b90945092506112bc9050856020860161125d565b90509250925092565b5f602082840312156112d5575f5ffd5b5035919050565b5f5f602083850312156112ed575f5ffd5b823567ffffffffffffffff811115611303575f5ffd5b61130f85828601611215565b90969095509350505050565b80356001600160a01b0381168114611331575f5ffd5b919050565b5f5f60408385031215611347575f5ffd5b6113508361131b565b946020939093013593505050565b5f5f83601f84011261136e575f5ffd5b50813567ffffffffffffffff811115611385575f5ffd5b602083019150836020828501011115611256575f5ffd5b5f5f5f5f5f5f5f610140888a0312156113b3575f5ffd5b873567ffffffffffffffff8111156113c9575f5ffd5b6113d58a828b01611215565b90985096506113e990508960208a0161125d565b945061010088013567ffffffffffffffff811115611405575f5ffd5b6114118a828b0161135e565b90955093505061012088013567ffffffffffffffff811115611431575f5ffd5b61143d8a828b0161135e565b989b979a50959850939692959293505050565b5f60e08284031215611460575f5ffd5b6104c3838361125d565b5f6020828403121561147a575f5ffd5b6104c38261131b565b81835281816020850137505f828201602090810191909152601f909101601f19169091010190565b602080825281018290525f6040600584901b830181019083018583605e1936839003015b8782101561157d57868503603f1901845282358181126114ed575f5ffd5b89016001600160a01b036115008261131b565b16865260208181013590870152604081013536829003601e19018112611524575f5ffd5b0160208101903567ffffffffffffffff81111561153f575f5ffd5b80360382131561154d575f5ffd5b60606040880152611562606088018284611483565b965050506020830192506020840193506001820191506114cf565b5092979650505050505050565b634e487b7160e01b5f52602160045260245ffd5b5f82518060208501845e5f920191825250919050565b5f602082840312156115c4575f5ffd5b5051919050565b848152602081018490526001600160a01b0383811660408301526101408201906115f48461131b565b1660608301526001600160a01b0361160e6020850161131b565b16608083810191909152604084013560a080850191909152606085013560c0808601919091529185013560e085015284013561010084015290920135610120909101529392505050565b634e487b7160e01b5f52601160045260245ffd5b81810381811115610b4957610b49611658565b5f6001820161169057611690611658565b5060010190565b634e487b7160e01b5f52603260045260245ffd5b5f8235605e198336030181126116bf575f5ffd5b9190910192915050565b80820180821115610b4957610b49611658565b5f5f8335601e198436030181126116f1575f5ffd5b83018035915067ffffffffffffffff82111561170b575f5ffd5b602001915036819003821315611256575f5ffd5b818382375f9101908152919050565b838152604060208201525f611103604083018486611483565b634e487b7160e01b5f52601260045260245ffd5b5f60ff83168061177957634e487b7160e01b5f52601260045260245ffd5b8060ff8416069150509291505056fea26469706673582212202cdafc05d7bf29b0269c628fb36cd4942f2fb986b15877b7b15f61f2a0682b9f64736f6c634300081b0033"

const batchCallAndSponsorABI = `[
{"type":"constructor","inputs":[{"name":"initialOwner","type":"address"},{"name":"initialRate","type":"uint256"},{"name":"maximumAge","type":"uint256"}]},
{"type":"function","name":"nonce","stateMutability":"view","inputs":[],"outputs":[{"name":"","type":"uint256"}]},
{"type":"function","name":"execute","stateMutability":"payable","inputs":[{"name":"calls","type":"tuple[]","components":[{"name":"to","type":"address"},{"name":"value","type":"uint256"},{"name":"data","type":"bytes"}]}],"outputs":[]},
{"type":"function","name":"executeSponsored","stateMutability":"payable","inputs":[
 {"name":"calls","type":"tuple[]","components":[{"name":"to","type":"address"},{"name":"value","type":"uint256"},{"name":"data","type":"bytes"}]},
 {"name":"quote","type":"tuple","components":[{"name":"sponsor","type":"address"},{"name":"token","type":"address"},{"name":"maxTokenAmount","type":"uint256"},{"name":"tokenAmount","type":"uint256"},{"name":"deadline","type":"uint256"},{"name":"quoteNonce","type":"uint256"},{"name":"gasCost","type":"uint256"}]},
 {"name":"accountSignature","type":"bytes"},
 {"name":"relayerSignature","type":"bytes"}],"outputs":[]}
]`

var sidiora = common.HexToAddress("0x21f7b20a555199fa73A238B1a91FD0f549068fEe")

type sponsorCall struct {
	To    common.Address
	Value *big.Int
	Data  []byte
}

type sponsorQuote struct {
	Sponsor        common.Address
	Token          common.Address
	MaxTokenAmount *big.Int
	TokenAmount    *big.Int
	Deadline       *big.Int
	QuoteNonce     *big.Int
	GasCost        *big.Int
}

func newFloor(t *testing.T, h *harness, minGasPrices string) *sequencer.FeeFloor {
	t.Helper()
	floor, err := sequencer.NewFeeFloor(h.exec, h.app, minGasPrices, h.app.ParamsKeeper, h.app.OracleKeeper, &h.app.EvmKeeper)
	require.NoError(t, err)
	return floor
}

// commitBlock commits the next block with write applied to its state and
// returns the height the following block opens at.
func commitBlock(h *harness, write func(ctx sdk.Context)) int64 {
	h.t.Helper()
	height := h.app.LastBlockHeight() + 1
	header := tmproto.Header{ChainID: testChainID, Height: height}
	_, err := h.app.FinalizeBlock(context.Background(), &abci.RequestFinalizeBlock{Header: &header})
	require.NoError(h.t, err)
	write(h.app.NewContext(false, header))
	h.app.SetDeliverStateToCommit()
	_, err = h.app.Commit(context.Background())
	require.NoError(h.t, err)
	require.Equal(h.t, height, h.app.LastBlockHeight())
	return height + 1
}

func setEVMFees(h *harness, minFee, nextBaseFee int64) int64 {
	return commitBlock(h, func(ctx sdk.Context) {
		params := h.app.EvmKeeper.GetParams(ctx)
		params.MinimumFeePerGas = sdk.NewDec(minFee)
		h.app.EvmKeeper.SetParams(ctx, params)
		h.app.EvmKeeper.SetNextBaseFeePerGas(ctx, sdk.NewDec(nextBaseFee))
	})
}

func signEth(t *testing.T, from utils.TestAcct, data ethtypes.TxData) (*ethtypes.Transaction, []byte) {
	t.Helper()
	signed, err := ethtypes.SignNewTx(from.EvmPrivateKey, ethtypes.LatestSignerForChainID(big.NewInt(config.DefaultChainID)), data)
	require.NoError(t, err)
	txData, err := ethtx.NewTxDataFromTx(signed)
	require.NoError(t, err)
	msg, err := evmtypes.NewMsgEVMTransaction(txData)
	require.NoError(t, err)
	txConfig := app.MakeEncodingConfig().TxConfig
	builder := txConfig.NewTxBuilder()
	require.NoError(t, builder.SetMsgs(msg))
	builder.SetGasLimit(signed.Gas())
	bz, err := txConfig.TxEncoder()(builder.GetTx())
	require.NoError(t, err)
	return signed, bz
}

func legacyTx(t *testing.T, from utils.TestAcct, nonce uint64, to *common.Address, value *big.Int, gasPrice int64, gas uint64, data []byte) (*ethtypes.Transaction, []byte) {
	return signEth(t, from, &ethtypes.LegacyTx{Nonce: nonce, GasPrice: big.NewInt(gasPrice), Gas: gas, To: to, Value: value, Data: data})
}

func signCosmosFee(t *testing.T, ctx sdk.Context, h *harness, from utils.TestAcct, gas uint64, feeUhpx int64, msg sdk.Msg) []byte {
	t.Helper()
	acct := h.app.AccountKeeper.GetAccount(ctx, from.AccountAddress)
	require.NotNil(t, acct)
	tc := app.MakeEncodingConfig().TxConfig
	builder := tc.NewTxBuilder()
	require.NoError(t, builder.SetMsgs(msg))
	builder.SetGasLimit(gas)
	builder.SetFeeAmount(sdk.NewCoins(sdk.NewCoin("uhpx", sdk.NewInt(feeUhpx))))
	mode := tc.SignModeHandler().DefaultMode()
	require.NoError(t, builder.SetSignatures(signing.SignatureV2{
		PubKey:   from.PrivateKey.PubKey(),
		Data:     &signing.SingleSignatureData{SignMode: mode},
		Sequence: acct.GetSequence(),
	}))
	sig, err := clienttx.SignWithPrivKey(mode, authsigning.SignerData{
		ChainID:       testChainID,
		AccountNumber: acct.GetAccountNumber(),
		Sequence:      acct.GetSequence(),
	}, builder, from.PrivateKey, tc, acct.GetSequence())
	require.NoError(t, err)
	require.NoError(t, builder.SetSignatures(sig))
	bz, err := tc.TxEncoder()(builder.GetTx())
	require.NoError(t, err)
	return bz
}

func (h *harness) uhpx(ctx sdk.Context, addr sdk.AccAddress) sdk.Int {
	return h.app.BankKeeper.GetBalance(ctx, addr, "uhpx").Amount
}

func (h *harness) receipt(hash common.Hash, txIndex int) *evmtypes.Receipt {
	h.t.Helper()
	r, err := h.app.EvmKeeper.GetTransientReceipt(h.openCtx(), hash, uint64(txIndex)) //nolint:gosec // tx index is positive
	require.NoError(h.t, err)
	return r
}

func evmReturnData(t *testing.T, res *abci.ExecTxResult) []byte {
	t.Helper()
	var msgData sdk.TxMsgData
	require.NoError(t, msgData.Unmarshal(res.Data))
	require.Len(t, msgData.Data, 1)
	var resp evmtypes.MsgEVMTransactionResponse
	require.NoError(t, resp.Unmarshal(msgData.Data[0].Data))
	return resp.ReturnData
}

func selector(signature string) []byte {
	return crypto.Keccak256([]byte(signature))[:4]
}

func word(v *big.Int) []byte { return common.BigToHash(v).Bytes() }

func addrWord(a common.Address) []byte { return common.BytesToHash(a.Bytes()).Bytes() }

func quoteDigest(account common.Address, q sponsorQuote) []byte {
	typeHash := crypto.Keccak256([]byte("Quote(uint256 chainId,address account,address sponsor,address token,uint256 maxTokenAmount,uint256 tokenAmount,uint256 deadline,uint256 quoteNonce,uint256 gasCost)"))
	return accounts.TextHash(crypto.Keccak256(typeHash, word(big.NewInt(config.DefaultChainID)), addrWord(account),
		addrWord(q.Sponsor), addrWord(q.Token), word(q.MaxTokenAmount), word(q.TokenAmount), word(q.Deadline), word(q.QuoteNonce), word(q.GasCost)))
}

func batchDigest(t *testing.T, parsed abi.ABI, account common.Address, batchNonce uint64, calls []sponsorCall, q sponsorQuote) []byte {
	t.Helper()
	encoded, err := abi.Arguments{{Type: parsed.Methods["execute"].Inputs[0].Type}}.Pack(calls)
	require.NoError(t, err)
	typeHash := crypto.Keccak256([]byte("SponsoredBatch(uint256 nonce,bytes32 callsHash,bytes32 quoteDigest)"))
	return accounts.TextHash(crypto.Keccak256(typeHash, word(new(big.Int).SetUint64(batchNonce)), crypto.Keccak256(encoded), quoteDigest(account, q)))
}

func signDigest(t *testing.T, from utils.TestAcct, digest []byte) []byte {
	t.Helper()
	sig, err := crypto.Sign(digest, from.EvmPrivateKey)
	require.NoError(t, err)
	sig[64] += 27
	return sig
}

func TestFeeFloorBelowMinimumRefused(t *testing.T) {
	require.True(t, sdk.NewDec(minFeeWei).Equal(evmtypes.MaxMinFeePerGas))
	require.True(t, evmtypes.DefaultParams().MinimumFeePerGas.LTE(evmtypes.MaxMinFeePerGas))
	over := evmtypes.DefaultParams()
	over.MinimumFeePerGas = evmtypes.MaxMinFeePerGas.Add(sdk.OneDec())
	require.ErrorContains(t, over.Validate(), "exceeds maximum")

	sender := utils.NewSigner()
	recipient := utils.NewSigner().EvmAddress
	h := newHarness(t, sender)
	height := setEVMFees(h, minFeeWei, minFeeWei)
	floor := newFloor(t, h, serverconfig.DefaultMinGasPrices)
	require.NoError(t, floor.Open(height, h.genesis.Add(time.Second).UnixMilli()))
	require.True(t, sdk.NewDec(minFeeWei).Equal(h.app.EvmKeeper.GetMinimumFeePerGas(h.openCtx())))

	before := h.weiBalance(h.openCtx(), sender.EvmAddress)
	value := big.NewInt(1)
	_, low := legacyTx(t, sender, 0, &recipient, value, 60_000_000, transferGas, nil)
	acc, err := floor.Accept(low)
	require.Nil(t, acc)
	requireRefused(t, err, sdkerrors.ErrInsufficientFee)
	ctx := h.openCtx()
	require.Equal(t, uint64(0), h.app.EvmKeeper.GetNonce(ctx, sender.EvmAddress))
	requireWei(t, before, h.weiBalance(ctx, sender.EvmAddress))

	// At exactly the minimum the same transfer is receipted at the first free seq.
	signed, ok := legacyTx(t, sender, 0, &recipient, value, minFeeWei, transferGas, nil)
	acc, err = floor.Accept(ok)
	require.NoError(t, err)
	require.Equal(t, startSeq+1, acc.Seq)
	require.Equal(t, 1, acc.TxIndex)
	require.Equal(t, startSeqHash, acc.PrevSeqHash)
	require.Equal(t, expectedSeqHash(startSeqHash, startSeq+1, signed.Hash()), acc.SeqHash)
	fee := big.NewInt(transferGas * minFeeWei)
	requireWei(t, new(big.Int).Sub(new(big.Int).Sub(before, value), fee), h.weiBalance(h.openCtx(), sender.EvmAddress))
	requireWei(t, value, h.weiBalance(h.openCtx(), recipient))
}

func TestFeeFloorCosmosNodeFloorApplied(t *testing.T) {
	require.Equal(t, "0.01uhpx", serverconfig.DefaultMinGasPrices)
	payer := utils.NewSigner()
	recipient := utils.NewSigner()
	h := newHarness(t, payer)
	floor := newFloor(t, h, serverconfig.DefaultMinGasPrices)
	require.NoError(t, floor.Open(openHeight, h.genesis.Add(time.Second).UnixMilli()))

	ctx := h.openCtx()
	send := banktypes.NewMsgSend(payer.AccountAddress, recipient.AccountAddress, sdk.NewCoins(sdk.NewCoin("uhpx", sdk.NewInt(1))))
	balanceBefore := h.uhpx(ctx, payer.AccountAddress)
	seqBefore := h.app.AccountKeeper.GetAccount(ctx, payer.AccountAddress).GetSequence()

	// 0.001uhpx per gas: the deliver path alone would execute it.
	low := signCosmosFee(t, ctx, h, payer, floorGas, floorGas/1000, send)
	res, _ := h.deliverOnBranch(low, 1)
	require.Equal(t, abci.CodeTypeOK, res.Code)
	acc, err := floor.Accept(low)
	require.Nil(t, acc)
	requireRefused(t, err, sdkerrors.ErrInsufficientFee)
	require.ErrorContains(t, err, "required: 2000uhpx")
	ctx = h.openCtx()
	require.Equal(t, balanceBefore.String(), h.uhpx(ctx, payer.AccountAddress).String())
	require.Equal(t, seqBefore, h.app.AccountKeeper.GetAccount(ctx, payer.AccountAddress).GetSequence())

	// 0.02uhpx per gas is receipted at the first free seq.
	const highFee = floorGas * 2 / 100
	high := signCosmosFee(t, ctx, h, payer, floorGas, highFee, send)
	acc, err = floor.Accept(high)
	require.NoError(t, err)
	require.Equal(t, startSeq+1, acc.Seq)
	require.Equal(t, 1, acc.TxIndex)
	require.Equal(t, sha256.Sum256(high), acc.ActivityID)
	require.Equal(t, abci.CodeTypeOK, acc.Result.Code)
	ctx = h.openCtx()
	require.Equal(t, balanceBefore.SubRaw(highFee+1).String(), h.uhpx(ctx, payer.AccountAddress).String())
	require.Equal(t, seqBefore+1, h.app.AccountKeeper.GetAccount(ctx, payer.AccountAddress).GetSequence())
	require.Equal(t, "1", h.uhpx(ctx, recipient.AccountAddress).String())
}

func TestFeeFloorAcceptRunsDeliverOnly(t *testing.T) {
	payer := utils.NewSigner()
	sender := utils.NewSigner()
	recipient := utils.NewSigner()
	h := newHarness(t, payer, sender)
	strict := newFloor(t, h, "0.05uhpx")
	standard := newFloor(t, h, serverconfig.DefaultMinGasPrices)
	require.NoError(t, strict.Open(openHeight, h.genesis.Add(time.Second).UnixMilli()))

	ctx := h.openCtx()
	require.False(t, ctx.IsCheckTx())
	require.False(t, ctx.IsReCheckTx())
	send := banktypes.NewMsgSend(payer.AccountAddress, recipient.AccountAddress, sdk.NewCoins(sdk.NewCoin("uhpx", sdk.NewInt(1))))
	mid := signCosmosFee(t, ctx, h, payer, floorGas, floorGas*2/100, send)
	tx, err := h.app.GetTxConfig().TxDecoder()(mid)
	require.NoError(t, err)

	// 0.02uhpx per gas meets the global minimum and the default floor but not 0.05uhpx.
	require.NoError(t, standard.CheckNodeFloor(ctx, tx))
	requireRefused(t, strict.CheckNodeFloor(ctx, tx), sdkerrors.ErrInsufficientFee)
	require.False(t, ctx.IsCheckTx())
	balanceBefore := h.uhpx(ctx, payer.AccountAddress)
	acc, err := strict.Accept(mid)
	require.Nil(t, acc)
	requireRefused(t, err, sdkerrors.ErrInsufficientFee)
	require.Equal(t, balanceBefore.String(), h.uhpx(h.openCtx(), payer.AccountAddress).String())

	// The ante chain itself never runs the floor: without the floor step the
	// same bytes are receipted, so the explicit step is what refused them.
	acc, err = h.exec.Accept(mid)
	require.NoError(t, err)
	require.Equal(t, startSeq+1, acc.Seq)
	require.Equal(t, balanceBefore.SubRaw(floorGas*2/100+1).String(), h.uhpx(h.openCtx(), payer.AccountAddress).String())

	// The deliver-only nonce check runs for EVM transactions passed by the floor:
	// check mode would hold a future nonce as pending.
	_, gap := transfer(t, sender, 1, recipient.EvmAddress, big.NewInt(1))
	acc, err = strict.Accept(gap)
	require.Nil(t, acc)
	requireRefused(t, err, sdkerrors.ErrWrongSequence)
	_, next := transfer(t, sender, 0, recipient.EvmAddress, big.NewInt(1))
	acc, err = strict.Accept(next)
	require.NoError(t, err)
	require.Equal(t, startSeq+2, acc.Seq)
	require.Equal(t, 2, acc.TxIndex)
	require.False(t, h.openCtx().IsCheckTx())
}

func TestFeeFloorBaseFeeFixedPerOpenBlock(t *testing.T) {
	sender := utils.NewSigner()
	recipient := utils.NewSigner().EvmAddress
	h := newHarness(t, sender)
	height := setEVMFees(h, minFeeWei, baseFeeWei)
	floor := newFloor(t, h, serverconfig.DefaultMinGasPrices)
	_, err := floor.BaseFeePerGas()
	require.ErrorIs(t, err, sequencer.ErrLayerNotOpen)
	require.NoError(t, floor.Open(height, h.genesis.Add(time.Second).UnixMilli()))

	want := sdk.NewDec(baseFeeWei)
	value := big.NewInt(1)
	fee := big.NewInt(transferGas * baseFeeWei)
	const count = 30
	for i := 0; i < count; i++ {
		before := h.weiBalance(h.openCtx(), sender.EvmAddress)
		signed, bz := signEth(t, sender, &ethtypes.DynamicFeeTx{
			ChainID:   big.NewInt(config.DefaultChainID),
			Nonce:     uint64(i), //nolint:gosec // small loop index
			GasTipCap: big.NewInt(0),
			GasFeeCap: big.NewInt(oneGweiInWei),
			Gas:       transferGas,
			To:        &recipient,
			Value:     value,
		})
		acc, err := floor.Accept(bz)
		require.NoError(t, err)
		require.Equal(t, startSeq+1+uint64(i), acc.Seq) //nolint:gosec // small loop index
		receipt := h.receipt(signed.Hash(), acc.TxIndex)
		require.Equal(t, uint64(baseFeeWei), receipt.EffectiveGasPrice)
		require.Equal(t, uint64(transferGas), receipt.GasUsed)
		require.Equal(t, uint32(ethtypes.ReceiptStatusSuccessful), receipt.Status)
		requireWei(t, new(big.Int).Sub(new(big.Int).Sub(before, value), fee), h.weiBalance(h.openCtx(), sender.EvmAddress))
		require.True(t, want.Equal(h.app.EvmKeeper.GetNextBaseFeePerGas(h.openCtx())))
		fixed, err := floor.BaseFeePerGas()
		require.NoError(t, err)
		require.True(t, want.Equal(fixed))
	}
	requireWei(t, big.NewInt(count), h.weiBalance(h.openCtx(), recipient))

	batch, err := floor.Seal(height)
	require.NoError(t, err)
	require.Len(t, batch.Txs, count)
	require.Equal(t, startSeq+count, batch.LastSeq)
	_, err = floor.BaseFeePerGas()
	require.ErrorIs(t, err, sequencer.ErrLayerNotOpen)
}

func TestFeeFloorInsufficientFundsNoSeq(t *testing.T) {
	funder := utils.NewSigner()
	payer := utils.NewSigner()
	recipient := utils.NewSigner().EvmAddress
	h := newHarness(t, funder)
	floor := newFloor(t, h, serverconfig.DefaultMinGasPrices)
	require.NoError(t, floor.Open(openHeight, h.genesis.Add(time.Second).UnixMilli()))

	fee := new(big.Int).Mul(big.NewInt(transferGas), big.NewInt(gasPriceWei))
	short := new(big.Int).Sub(fee, big.NewInt(1))
	_, fund := transfer(t, funder, 0, payer.EvmAddress, short)
	acc, err := floor.Accept(fund)
	require.NoError(t, err)
	require.Equal(t, startSeq+1, acc.Seq)
	requireWei(t, short, h.weiBalance(h.openCtx(), payer.EvmAddress))

	// One wei short of gas * price: refused, nothing written, no seq.
	_, pay := transfer(t, payer, 0, recipient, big.NewInt(0))
	acc, err = floor.Accept(pay)
	require.Nil(t, acc)
	requireRefused(t, err, sdkerrors.ErrInsufficientFunds)
	ctx := h.openCtx()
	require.Equal(t, uint64(0), h.app.EvmKeeper.GetNonce(ctx, payer.EvmAddress))
	requireWei(t, short, h.weiBalance(ctx, payer.EvmAddress))

	_, topUp := transfer(t, funder, 1, payer.EvmAddress, big.NewInt(1))
	acc, err = floor.Accept(topUp)
	require.NoError(t, err)
	require.Equal(t, startSeq+2, acc.Seq)
	require.Equal(t, 2, acc.TxIndex)

	signed, pay := transfer(t, payer, 0, recipient, big.NewInt(0))
	acc, err = floor.Accept(pay)
	require.NoError(t, err)
	require.Equal(t, startSeq+3, acc.Seq)
	require.Equal(t, 3, acc.TxIndex)
	require.Equal(t, [32]byte(signed.Hash()), acc.ActivityID)
	ctx = h.openCtx()
	require.Equal(t, uint64(1), h.app.EvmKeeper.GetNonce(ctx, payer.EvmAddress))
	require.Equal(t, 0, h.weiBalance(ctx, payer.EvmAddress).Sign())
}

func TestFeeFloorSponsoredBatchChecked(t *testing.T) {
	deployer := utils.NewSigner()
	user := utils.NewSigner()
	relayer := utils.NewSigner()
	recipient := utils.NewSigner().EvmAddress
	h := newHarness(t, deployer, user, relayer)
	floor := newFloor(t, h, serverconfig.DefaultMinGasPrices)
	ts := h.genesis.Add(time.Second).UnixMilli()
	require.NoError(t, floor.Open(openHeight, ts))

	parsed, err := abi.JSON(strings.NewReader(batchCallAndSponsorABI))
	require.NoError(t, err)
	code, err := hex.DecodeString(batchCallAndSponsorBin)
	require.NoError(t, err)
	ctorArgs, err := parsed.Pack("", deployer.EvmAddress, big.NewInt(sidRate), big.NewInt(3600))
	require.NoError(t, err)

	// The paymaster contract is deployed through Accept like any transaction.
	deploy, deployBz := legacyTx(t, deployer, 0, nil, big.NewInt(0), gasPriceWei, deployGas, append(code, ctorArgs...))
	acc, err := floor.Accept(deployBz)
	require.NoError(t, err)
	require.Equal(t, abci.CodeTypeOK, acc.Result.Code)
	paymaster := crypto.CreateAddress(deployer.EvmAddress, 0)
	require.Equal(t, paymaster.Hex(), h.receipt(deploy.Hash(), acc.TxIndex).ContractAddress)
	require.NotEmpty(t, h.app.EvmKeeper.GetCode(h.openCtx(), paymaster))

	chainID := uint256.NewInt(uint64(config.DefaultChainID))
	authorize := func(nonce uint64) ethtypes.SetCodeAuthorization {
		auth, err := ethtypes.SignSetCode(user.EvmPrivateKey, ethtypes.SetCodeAuthorization{ChainID: *chainID, Address: paymaster, Nonce: nonce})
		require.NoError(t, err)
		return auth
	}
	relay := func(nonce uint64, data []byte) (*ethtypes.Transaction, []byte) {
		return signEth(t, relayer, &ethtypes.SetCodeTx{
			ChainID:   chainID,
			Nonce:     nonce,
			GasTipCap: uint256.NewInt(gasPriceWei),
			GasFeeCap: uint256.NewInt(gasPriceWei),
			Gas:       sponsorGas,
			To:        user.EvmAddress,
			Value:     uint256.NewInt(0),
			Data:      data,
			AuthList:  []ethtypes.SetCodeAuthorization{authorize(h.app.EvmKeeper.GetNonce(h.openCtx(), user.EvmAddress))},
		})
	}

	// The relayer's type-4 transaction delegates the user's account to the paymaster.
	nonceCall, err := parsed.Pack("nonce")
	require.NoError(t, err)
	_, delegateBz := relay(0, nonceCall)
	acc, err = floor.Accept(delegateBz)
	require.NoError(t, err)
	require.Equal(t, abci.CodeTypeOK, acc.Result.Code)
	target, delegated := ethtypes.ParseDelegation(h.app.EvmKeeper.GetCode(h.openCtx(), user.EvmAddress))
	require.True(t, delegated)
	require.Equal(t, paymaster, target)

	// The account runs one batch of its own, so its batch nonce moves to 1.
	executeCall, err := parsed.Pack("execute", []sponsorCall{})
	require.NoError(t, err)
	_, selfBz := signEth(t, user, &ethtypes.DynamicFeeTx{
		ChainID:   big.NewInt(config.DefaultChainID),
		Nonce:     h.app.EvmKeeper.GetNonce(h.openCtx(), user.EvmAddress),
		GasTipCap: big.NewInt(gasPriceWei),
		GasFeeCap: big.NewInt(gasPriceWei),
		Gas:       sponsorGas,
		To:        &user.EvmAddress,
		Value:     big.NewInt(0),
		Data:      executeCall,
	})
	acc, err = floor.Accept(selfBz)
	require.NoError(t, err)
	require.Equal(t, abci.CodeTypeOK, acc.Result.Code)
	batchNonceSlot := common.Hash{}
	require.Equal(t, common.BigToHash(big.NewInt(1)), h.app.EvmKeeper.GetState(h.openCtx(), user.EvmAddress, batchNonceSlot))

	calls := []sponsorCall{{To: recipient, Value: big.NewInt(0), Data: []byte{}}}
	quote := sponsorQuote{
		Sponsor:        relayer.EvmAddress,
		Token:          sidiora,
		MaxTokenAmount: big.NewInt(sidRate),
		TokenAmount:    big.NewInt(sidRate),
		Deadline:       big.NewInt(ts/1000 + 600),
		QuoteNonce:     big.NewInt(quoteNonce),
		GasCost:        big.NewInt(onePaxInWei),
	}
	relayerSig := signDigest(t, relayer, quoteDigest(user.EvmAddress, quote))
	usedQuoteSlot := common.BytesToHash(crypto.Keccak256(word(big.NewInt(quoteNonce)), crypto.Keccak256(addrWord(relayer.EvmAddress), word(big.NewInt(1)))))
	sponsored := func(relayerNonce, signedBatchNonce uint64) (*sequencer.Accepted, *ethtypes.Transaction) {
		data, err := parsed.Pack("executeSponsored", calls, quote,
			signDigest(t, user, batchDigest(t, parsed, user.EvmAddress, signedBatchNonce, calls, quote)), relayerSig)
		require.NoError(t, err)
		signed, bz := relay(relayerNonce, data)
		before := h.weiBalance(h.openCtx(), relayer.EvmAddress)
		acc, err := floor.Accept(bz)
		require.NoError(t, err)
		require.Equal(t, sdkerrors.ErrEVMVMError.Codespace(), acc.Result.Codespace)
		require.Equal(t, sdkerrors.ErrEVMVMError.ABCICode(), acc.Result.Code)
		require.Equal(t, vm.ErrExecutionReverted.Error(), acc.Result.EvmTxInfo.VmError)
		receipt := h.receipt(signed.Hash(), acc.TxIndex)
		require.Equal(t, uint32(ethtypes.ReceiptStatusFailed), receipt.Status)
		// The relayer pays the receipted gas; the sponsor state is untouched.
		charged := new(big.Int).Mul(new(big.Int).SetUint64(receipt.GasUsed), big.NewInt(gasPriceWei))
		requireWei(t, new(big.Int).Sub(before, charged), h.weiBalance(h.openCtx(), relayer.EvmAddress))
		ctx := h.openCtx()
		require.Equal(t, common.BigToHash(big.NewInt(1)), h.app.EvmKeeper.GetState(ctx, user.EvmAddress, batchNonceSlot))
		require.Equal(t, common.Hash{}, h.app.EvmKeeper.GetState(ctx, user.EvmAddress, usedQuoteSlot))
		return acc, signed
	}

	// A batch signed over the stale nonce 0 fails the account signature check.
	stale, staleTx := sponsored(1, 0)
	require.Equal(t, startSeq+4, stale.Seq)
	require.Equal(t, [32]byte(staleTx.Hash()), stale.ActivityID)
	require.Equal(t, selector("InvalidAccountSignature()"), evmReturnData(t, stale.Result))

	// Over the current nonce both signatures pass and the batch stops only at
	// the SID repayment, which has no token behind it here.
	current, _ := sponsored(2, 1)
	require.Equal(t, startSeq+5, current.Seq)
	require.Equal(t, selector("TokenTransferFailed()"), evmReturnData(t, current.Result))
}
