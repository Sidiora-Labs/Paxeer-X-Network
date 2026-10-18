package keeper_test

import (
	"encoding/json"
	"math/big"
	"os"
	"path/filepath"
	"testing"

	"github.com/Sidiora-Labs/Paxeer-X-Network/modules/launchpad/keeper"
	"github.com/Sidiora-Labs/Paxeer-X-Network/modules/launchpad/types"
	sdk "github.com/Sidiora-Labs/Paxeer-X-Network/sdk/types"
	testkeeper "github.com/Sidiora-Labs/Paxeer-X-Network/testutil/keeper"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/stretchr/testify/require"
)

// treasuryRuntime is the runtime code of Treasury (Launch-x
// contracts/protocol/Treasury.sol, solc 0.8.27, optimizer 200 runs, via-IR).
const treasuryRuntime = "0x6080604052600436101561001257600080fd5b60003560e01c80631e578bae14610127578063248a9ca3146101225780632f2ff15d1461011d57806336568abe1461011857806347e7ef2414610113578063485cc9551461010e5780634f1ef2861461010957806352d1902d1461010457806372df7f5b146100ff57806391d14854146100fa5780639ff78c30146100f5578063a217fddf146100f0578063a3b0b5a3146100eb578063c0cb71b6146100e6578063d547741f146100e1578063d9caed12146100dc5763f8b2cb4f146100d757600080fd5b610ae9565b610a0d565b6109cd565b6109a2565b610967565b61094b565b610922565b6108cf565b6107c4565b610720565b6106a8565b6104df565b61037c565b61032d565b6102e4565b6102af565b61015d565b600435906001600160a01b038216820361014257565b600080fd5b602435906001600160a01b038216820361014257565b346101425760403660031901126101425761017661012c565b6024359061018333610d43565b6000805160206113918339815191525461029e57600160008051602061139183398151915255811561028d576101b881610ec1565b6001600160a01b038216600081815260016020526040902054909291908110908115610260575b5061024f576001600160a01b03166000908152600160205260409020610206838254610b84565b905560405191825233917f8752a472e571a816aea92eec8dae9baf628e840f4929fbcc2d155e6233ff68a79080602081015b0390a3600060008051602061139183398151915255005b631e9acf1760e31b60005260046000fd5b6001600160a01b038316600090815260016020526040902061028592505b5490610b72565b8311386101df565b631f2a200560e01b60005260046000fd5b6306fda65d60e31b60005260046000fd5b346101425760203660031901126101425760206102dc600435600052600060205260016040600020015490565b604051908152f35b346101425760403660031901126101425761032b600435610303610147565b9061032661031f82600052600060205260016040600020015490565b3390610e2d565b611097565b005b3461014257604036600319011261014257600435610349610147565b336001600160a01b03909116036103655761032b903390611178565b62b0d32560e11b6000523360045260245260446000fd5b346101425760403660031901126101425761039561012c565b6024356103a133610d43565b6000805160206113918339815191525461029e57600160008051602061139183398151915255801561028d576000806103d984610ec1565b9260405160208101916323b872dd60e01b835233602483015230604483015260648201526064815261040c608482610665565b519082865af161041a610e82565b833b159182156104d6575b5081156104be575b506104ad576104449061043f83610ec1565b610b72565b801561028d576001600160a01b038216600090815260016020526040902061046d828254610b84565b905560405190815233916001600160a01b0316907f8752a472e571a816aea92eec8dae9baf628e840f4929fbcc2d155e6233ff68a7908060208101610238565b631e4e7d0960e21b60005260046000fd5b6104d091506104cc90611321565b1590565b3861042d565b15915038610425565b34610142576040366003190112610142576104f861012c565b610500610147565b6000805160206113b183398151915254919067ffffffffffffffff61053560ff604086901c16159467ffffffffffffffff1690565b16600181101580610648575b61063057151580610640575b6106305761059391836105f4575b61058e600167ffffffffffffffff196000805160206113b18339815191525416176000805160206113b183398151915255565b610b91565b61059957005b6105c560ff60401b196000805160206113b183398151915254166000805160206113b183398151915255565b604051600181527fc7f505b2f371ae2175ee4913f4499e1f2633a7b5936321eed1cdaeb6115181d290602090a1005b61062b6801000000000000000060ff60401b196000805160206113b18339815191525416176000805160206113b183398151915255565b61055b565b62dc149f60e41b60005260046000fd5b50821561054d565b5083610541565b634e487b7160e01b600052604160045260246000fd5b90601f8019910116810190811067ffffffffffffffff82111761068757604052565b61064f565b67ffffffffffffffff811161068757601f01601f191660200190565b6040366003190112610142576106bc61012c565b6024359067ffffffffffffffff82116101425736602383011215610142578160040135906106e98261068c565b916106f76040519384610665565b808352366024828601011161014257602081600092602461032b97018387013784010152610bcd565b34610142576000366003190112610142577f00000000000000000000000000000000000000000000000000000000000000006001600160a01b031630036107795760206040516000805160206113d18339815191528152f35b6345a93fc960e01b60005260046000fd5b6060906003190112610142576004356001600160a01b038116810361014257906024356001600160a01b0381168103610142579060443590565b34610142576107d23661078a565b90916107dd33610dca565b6000805160206113918339815191525461029e576001600080516020611391833981519152556001600160a01b0383169283156108be57821561028d5761082382610ec1565b6001600160a01b0383166000908152600160205260409020548110908115610894575b5061024f578161087a84610238937ec571bcd43634ce4043e2bd7088ab9a1d36bd5dc21f5f4246e9eddda3c222f69561128e565b6040519384526001600160a01b0316929081906020820190565b6001600160a01b03841660009081526001602052604090206108b6925061027e565b831138610846565b63d92e233d60e01b60005260046000fd5b3461014257604036600319011261014257602060ff6109166004356108f2610147565b906000526000845260406000209060018060a01b0316600052602052604060002090565b54166040519015158152f35b34610142576000366003190112610142576002546040516001600160a01b039091168152602090f35b3461014257600036600319011261014257602060405160008152f35b346101425760003660031901126101425760206040517f8f4f2da22e8ac8f11e15f9fc141cddbb5deea8800186560abb6e68c5496619a98152f35b346101425760003660031901126101425760206000805160206113f183398151915254604051908152f35b346101425760403660031901126101425761032b6004356109ec610147565b90610a0861031f82600052600060205260016040600020015490565b611178565b3461014257610a1b3661078a565b90610a2533610dca565b6000805160206113918339815191525461029e576001600080516020611391833981519152556001600160a01b0381169283156108be57821561028d5760018060a01b03811692836000526001602052806040600020541061024f57610ad9817fd1c19fbcd4551a5edfb66d43d2e337c04837afda3482b42bdf569a8fccdae5fb9484610ac76102389660018060a01b03166000526001602052604060002090565b610ad2848254610b72565b905561128e565b6040519081529081906020820190565b34610142576020366003190112610142576001600160a01b03610b0a61012c565b1660005260016020526020604060002054604051908152f35b6001600160a01b031660009081527f2b88476a32ce493855cf9962818317d140ce1ca9ccfcfbae5b8ff8bf5e285cd06020526040902090565b634e487b7160e01b600052601160045260246000fd5b91908203918211610b7f57565b610b5c565b91908201809211610b7f57565b906001600160a01b038116156108be57610bca9160018060a01b03166bffffffffffffffffffffffff60a01b6002541617600255610f54565b50565b907f00000000000000000000000000000000000000000000000000000000000000006001600160a01b0316308114610d32576000805160206113d1833981519152546001600160a01b03160361077957610c2633610dca565b80511580610d29575b610d18576040516352d1902d60e01b81526020816004816001600160a01b0387165afa60009181610ce7575b50610c715763340aafcd60e11b60005260046000fd5b6000805160206113d183398151915203610cd657610c8e82611347565b805180610c9a57505050565b6000926020849301905af4610cad610e82565b9015610cb65750565b805115610cc557602081519101fd5b63033073d160e61b60005260046000fd5b63340aafcd60e11b60005260046000fd5b610d0a91925060203d602011610d11575b610d028183610665565b810190610eb2565b9038610c5b565b503d610cf8565b6393738d4960e01b60005260046000fd5b50341515610c2f565b6309e1070b60e31b60005260046000fd5b6001600160a01b03811660009081527fcd535922ec6598051cb9009e5385a1e92110f0406d6b811e10c8a3249b96d6dd602052604090205460ff1615610d865750565b62b0d32560e11b60009081526001600160a01b03919091166004527f8f4f2da22e8ac8f11e15f9fc141cddbb5deea8800186560abb6e68c5496619a9602452604490fd5b6001600160a01b03811660009081527fad3228b676f7d3cd4284a5443f17f1962b36e491b30a40b2405849e597ba5fb5602052604090205460ff1615610e0d5750565b62b0d32560e11b60005260018060a01b0316600452600060245260446000fd5b9081600052600060205260ff610e598260406000209060018060a01b0316600052602052604060002090565b541615610e64575050565b62b0d32560e11b60005260018060a01b031660045260245260446000fd5b3d15610ead573d90610e938261068c565b91610ea16040519384610665565b82523d6000602084013e565b606090565b90816020910312610142575190565b803b15610f28576040516370a0823160e01b602082019081523060248084019190915282526000928392909190610ef9604482610665565b51915afa610f05610e82565b90158015610f39575b610f285780602080610f2593518301019101610eb2565b90565b63c1ab6dc160e01b60005260046000fd5b50602081511415610f0e565b6000198114610b7f5760010190565b6001600160a01b0381169081156110865760ff610f7082610b23565b54161561104d575b6001600160a01b03811660009081527fad3228b676f7d3cd4284a5443f17f1962b36e491b30a40b2405849e597ba5fb560205260409020610fbf906104cc905b5460ff1690565b610fca575050600090565b600080805260205261102090611013907fad3228b676f7d3cd4284a5443f17f1962b36e491b30a40b2405849e597ba5fb55b9060018060a01b0316600052602052604060002090565b805460ff19166001179055565b339060007f2f8788117e7eff1d82e926ec794901d17c78024a50270940304540a733656f0d8180a4600190565b61105961101382610b23565b6110716000805160206113f183398151915254610f45565b6000805160206113f183398151915255610f78565b637666d29360e11b60005260046000fd5b906001600160a01b038116908115611086578215611121575b6110ce6104cc610fb883610ffc876000526000602052604060002090565b6110da57505050600090565b6110136110f591610ffc856000526000602052604060002090565b33917f2f8788117e7eff1d82e926ec794901d17c78024a50270940304540a733656f0d600080a4600190565b60ff61112c82610b23565b54166110b05761113e61101382610b23565b6111566000805160206113f183398151915254610f45565b6000805160206113f1833981519152556110b0565b8015610b7f576000190190565b6000818152602081815260408083206001600160a01b038616845290915290205460ff166111a7575050600090565b8015611209575b6111d36111c983610ffc846000526000602052604060002090565b805460ff19169055565b33916001600160a01b0316907ff6391f5c32d9c69d2a47ea670b442974b53935d1edc7fd64eb21e047a839171b600080a4600190565b6000805160206113f183398151915254611225610fb884610b23565b611280575b801561126f5761123c610fb884610b23565b611247575b506111ae565b611269906112576111c985610b23565b6000805160206113f183398151915255565b38611241565b639c88180d60e01b60005260046000fd5b6112899061116b565b61122a565b60405163a9059cbb60e01b602082019081526001600160a01b0390931660248201526044808201949094529283529160009182916112cd606482610665565b519082855af1906112dc610e82565b903b15918215611318575b508115611307575b506112f657565b6312171d8360e31b60005260046000fd5b6113119150611321565b15386112ef565b159150386112e7565b805180156113405760200361133a576020015160011490565b50600090565b5050600190565b803b15610cd6576000805160206113d18339815191528190556001600160a01b03167fbc7cd75a20ee27fd9adebab32041f755214dbc6bffa90cc0225b39da2e5c2d3b600080a256fe0f01dc38750987e5723c30902c27c5f0424bf3f14ddec28d8c4031114e7f59164377386e2795aba8c0ce23a1e6e67bac790e4b4166f14b7e9365049ab946f8c0360894a13ba1a3210667c828492db98dca3e2076cc3735a920a3ca505d382bbc2b88476a32ce493855cf9962818317d140ce1ca9ccfcfbae5b8ff8bf5e285ccfa2646970667358221220e01eaacdccad2eb56f9948087a625c7bfbd12b284dd52cc7a4572791163e66bd64736f6c634300081b0033"

// treasuryGen1Runtime is the runtime code of the generation 1 Treasury
// implementation 0xcdBB7923e9BE9255717704cf85Ee1B7fA201b52b at the halt height,
// which serves withdraw but not recoverSurplus.
const treasuryGen1Runtime = "0x6080604052600436101561001257600080fd5b60003560e01c8063248a9ca3146109935780632f2ff15d1461095357806336568abe1461090457806347e7ef241461078a578063485cc9551461061c5780634f1ef2861461040c57806352d1902d1461039057806391d14854146103435780639ff78c301461031a578063a217fddf146102fe578063a3b0b5a3146102c3578063d547741f1461027a578063d9caed12146100f55763f8b2cb4f146100b657600080fd5b346100f05760203660031901126100f0576001600160a01b036100d76109de565b1660005260016020526020604060002054604051908152f35b600080fd5b346100f05760603660031901126100f05761010e6109de565b6101166109c8565b906044359161012433610a48565b6001600160a01b03169182156102695780156102585760018060a01b0382169182600052600160205281604060002054106102475782600052600160205260406000208054918383039283116102315760009283925560405182602082019163a9059cbb60e01b8352886024820152866044820152604481526101a86064826109f4565b51925af16101b4610cc0565b9015908115610201575b506101f05760207fd1c19fbcd4551a5edfb66d43d2e337c04837afda3482b42bdf569a8fccdae5fb91604051908152a3005b6312171d8360e31b60005260046000fd5b8051801515925082610216575b5050846101be565b6102299250602080918301019101610cf0565b15848061020e565b634e487b7160e01b600052601160045260246000fd5b631e9acf1760e31b60005260046000fd5b631f2a200560e01b60005260046000fd5b63d92e233d60e01b60005260046000fd5b346100f05760403660031901126100f0576102c16004356102996109c8565b906102bc6102b582600052600060205260016040600020015490565b3390610aab565b610c38565b005b346100f05760003660031901126100f05760206040517f8f4f2da22e8ac8f11e15f9fc141cddbb5deea8800186560abb6e68c5496619a98152f35b346100f05760003660031901126100f057602060405160008152f35b346100f05760003660031901126100f0576002546040516001600160a01b039091168152602090f35b346100f05760403660031901126100f05761035c6109c8565b600435600052600060205260406000209060018060a01b0316600052602052602060ff604060002054166040519015158152f35b346100f05760003660031901126100f0577f000000000000000000000000cdbb7923e9be9255717704cf85ee1b7fa201b52b6001600160a01b031630036103fb5760206040517f360894a13ba1a3210667c828492db98dca3e2076cc3735a920a3ca505d382bbc8152f35b6345a93fc960e01b60005260046000fd5b60403660031901126100f0576104206109de565b6024359067ffffffffffffffff82116100f057366023830112156100f057816004013561044c81610a2c565b9061045a60405192836109f4565b808252602082019336602483830101116100f05781600092602460209301873783010152307f000000000000000000000000cdbb7923e9be9255717704cf85ee1b7fa201b52b6001600160a01b03161461060b576104b733610a48565b6040516352d1902d60e01b81526001600160a01b03831690602081600481855afa600091816105d7575b506104f75763340aafcd60e11b60005260046000fd5b7f360894a13ba1a3210667c828492db98dca3e2076cc3735a920a3ca505d382bbc036105c657823b156105c657827f360894a13ba1a3210667c828492db98dca3e2076cc3735a920a3ca505d382bbc557fbc7cd75a20ee27fd9adebab32041f755214dbc6bffa90cc0225b39da2e5c2d3b600080a2518061057457005b60009283925af4610583610cc0565b501561058b57005b60405162461bcd60e51b81526020600482015260136024820152721d5c19dc985919481a5b9a5d0819985a5b1959606a1b6044820152606490fd5b63340aafcd60e11b60005260046000fd5b9091506020813d602011610603575b816105f3602093836109f4565b810103126100f0575190866104e1565b3d91506105e6565b6309e1070b60e31b60005260046000fd5b346100f05760403660031901126100f0576106356109de565b61063d6109c8565b90600080516020610d09833981519152549160ff8360401c16159267ffffffffffffffff8116600181101580610783575b61076b5715158061077b575b61076b578361073e575b50600080516020610d09833981519152805467ffffffffffffffff191660011790556001600160a01b03811615610269576106de9160018060a01b03166bffffffffffffffffffffffff60a01b6002541617600255610af9565b506106e557005b68ff000000000000000019600080516020610d098339815191525416600080516020610d09833981519152557fc7f505b2f371ae2175ee4913f4499e1f2633a7b5936321eed1cdaeb6115181d2602060405160018152a1005b68ff000000000000000019166801000000000000000017600080516020610d098339815191525583610684565b62dc149f60e41b60005260046000fd5b50831561067a565b508461066e565b346100f05760403660031901126100f0576107a36109de565b3360009081527fcd535922ec6598051cb9009e5385a1e92110f0406d6b811e10c8a3249b96d6dd6020526040902054602435919060ff16156108cc5781156102585760008060405160208101906323b872dd60e01b82523360248201523060448201528560648201526064815261081b6084826109f4565b519082855af1610829610cc0565b901590811561089c575b5061088b576001600160a01b0316600081815260016020526040902080548381019190821061023157556040519182527f8752a472e571a816aea92eec8dae9baf628e840f4929fbcc2d155e6233ff68a760203393a3005b631e4e7d0960e21b60005260046000fd5b80518015159250826108b1575b505083610833565b6108c49250602080918301019101610cf0565b1583806108a9565b62b0d32560e11b600052336004527f8f4f2da22e8ac8f11e15f9fc141cddbb5deea8800186560abb6e68c5496619a960245260446000fd5b346100f05760403660031901126100f0576004356109206109c8565b336001600160a01b039091160361093c576102c1903390610c38565b62b0d32560e11b6000523360045260245260446000fd5b346100f05760403660031901126100f0576102c16004356109726109c8565b9061098e6102b582600052600060205260016040600020015490565b610bac565b346100f05760203660031901126100f05760206109c0600435600052600060205260016040600020015490565b604051908152f35b602435906001600160a01b03821682036100f057565b600435906001600160a01b03821682036100f057565b90601f8019910116810190811067ffffffffffffffff821117610a1657604052565b634e487b7160e01b600052604160045260246000fd5b67ffffffffffffffff8111610a1657601f01601f191660200190565b6001600160a01b03811660009081527fad3228b676f7d3cd4284a5443f17f1962b36e491b30a40b2405849e597ba5fb5602052604090205460ff1615610a8b5750565b62b0d32560e11b60005260018060a01b0316600452600060245260446000fd5b90816000526000602052604060002060018060a01b03821660005260205260ff6040600020541615610adb575050565b62b0d32560e11b60005260018060a01b031660045260245260446000fd5b6001600160a01b03811660009081527fad3228b676f7d3cd4284a5443f17f1962b36e491b30a40b2405849e597ba5fb5602052604090205460ff1615610b3f5750600090565b6001600160a01b031660008181527fad3228b676f7d3cd4284a5443f17f1962b36e491b30a40b2405849e597ba5fb560205260408120805460ff191660011790553391907f2f8788117e7eff1d82e926ec794901d17c78024a50270940304540a733656f0d8180a4600190565b6000818152602081815260408083206001600160a01b038616845290915290205460ff1615610bdc575050600090565b6000818152602081815260408083206001600160a01b0395909516808452949091528120805460ff19166001179055339291907f2f8788117e7eff1d82e926ec794901d17c78024a50270940304540a733656f0d9080a4600190565b6000818152602081815260408083206001600160a01b038616845290915290205460ff16610c67575050600090565b6000818152602081815260408083206001600160a01b0395909516808452949091528120805460ff19169055339291907ff6391f5c32d9c69d2a47ea670b442974b53935d1edc7fd64eb21e047a839171b9080a4600190565b3d15610ceb573d90610cd182610a2c565b91610cdf60405193846109f4565b82523d6000602084013e565b606090565b908160209103126100f0575180151581036100f0579056fe4377386e2795aba8c0ce23a1e6e67bac790e4b4166f14b7e9365049ab946f8c0a2646970667358221220422004e9f978c6236e748169778f7329d5e3446ad3b8b3cb1e4a7148e21ddec964736f6c634300081b0033"

var (
	usdlGoldenAddress = common.HexToAddress("0x85FcD13735F4309833A503EE804ea32395851479")
	treasuryGen1      = common.HexToAddress("0x15405D535ce533BfFb98c83e42f4DD242AA5e079")
	// treasuryGen1CodeHash is the code hash of the generation 1 Treasury
	// implementation at the halt height.
	treasuryGen1CodeHash = common.HexToHash("0x66fc1af36e20afd6ca9257c2364f7f7b465e2ec96a86219344d2bb358e998e2b")
	depositorRole        = crypto.Keccak256Hash([]byte("DEPOSITOR_ROLE"))
	// treasurySurplus is the USDL each Treasury holds above its getBalance
	// ledger; the rest of its usdl.json balance is deposited.
	treasurySurplus = map[common.Address]int64{
		common.HexToAddress("0x15405D535ce533BfFb98c83e42f4DD242AA5e079"): 21_131_511,
		common.HexToAddress("0xBb82d74E071e03d8E68F1d78Fae9EFe8b6b7A723"): 200_000,
	}
)

const (
	// goldenCustodySum is the USDL of every pool and FeeAccumulator in
	// usdl.json, goldenTreasurySum that of both Treasuries and
	// goldenTreasuryLedger the getBalance(USDL) of both Treasuries.
	goldenCustodySum     int64 = 6_451_794_928_571
	goldenTreasurySum    int64 = 375_726_331_511
	goldenTreasuryLedger int64 = 375_705_000_000
)

// usdlStepEnv is the custody environment with USDL at its fixture address,
// every usdl.json balance funded at a contract address, each Treasury holding
// the Treasury runtime with an admin and a deposited ledger, and the golden
// of step 1 read from usdl.json.
type usdlStepEnv struct {
	*custodyEnv
	golden keeper.USDLGolden
}

func mappingSlot(key []byte, slot common.Hash) common.Hash {
	return crypto.Keccak256Hash(common.LeftPadBytes(key, 32), slot.Bytes())
}

func newUSDLStepEnv(t *testing.T) *usdlStepEnv {
	t.Helper()
	c := newCustodyEnv(t)
	evm := &testkeeper.EVMTestApp.EvmKeeper
	// USDL at its fixture address: the deployed runtime and the storage its
	// construction wrote (name, symbol, totalSupply and the holder balance).
	evm.SetCode(c.ctx, usdlGoldenAddress, evm.GetCode(c.ctx, c.usdl))
	for _, slot := range []common.Hash{common.BigToHash(big.NewInt(0)), common.BigToHash(big.NewInt(1)),
		common.BigToHash(big.NewInt(2)), mappingSlot(c.holder.Bytes(), common.BigToHash(big.NewInt(3)))} {
		evm.SetState(c.ctx, usdlGoldenAddress, slot, evm.GetState(c.ctx, c.usdl, slot))
	}
	c.usdl = usdlGoldenAddress
	params := c.k.GetParams(c.ctx)
	params.QuoteToken = usdlGoldenAddress.Hex()
	require.NoError(t, c.k.UpdateParams(c.ctx, c.k.Authority(), params))
	require.Equal(t, usdlSupply, c.totalSupply())
	require.Equal(t, usdlSupply, c.balanceOf(c.holder))

	raw, err := os.ReadFile(filepath.Join(v6120FixtureDir, "usdl.json"))
	require.NoError(t, err)
	var golden keeper.USDLGolden
	require.NoError(t, json.Unmarshal(raw, &golden))
	require.Equal(t, usdlGoldenAddress, golden.Address)
	require.Len(t, golden.Balances, 73)
	s := &usdlStepEnv{custodyEnv: c, golden: golden}
	for _, h := range golden.Balances {
		require.True(t, h.Balance.IsInt64())
		if h.Role == types.RoleTreasury {
			s.setUpTreasury(h.Address, h.Balance.Int64())
			continue
		}
		evm.SetCode(c.ctx, h.Address, []byte{0x00})
		if h.Balance.IsPositive() {
			c.transfer(c.holder, h.Address, h.Balance.Int64())
		}
		require.Equal(t, h.Balance.Int64(), c.balanceOf(h.Address))
	}
	require.Len(t, s.golden.Treasuries, 2)
	c.logStart = len(c.receiptLogs())
	return s
}

// setUpTreasury installs the Treasury runtime of its generation at treasury, initializes it
// with an admin, deposits its ledger from a DEPOSITOR_ROLE holder and sends
// the surplus straight to it, so it holds balance USDL.
func (s *usdlStepEnv) setUpTreasury(treasury common.Address, balance int64) {
	s.t.Helper()
	admin, depositor := address("treasury admin "+treasury.Hex()), address("treasury depositor "+treasury.Hex())
	surplus := treasurySurplus[treasury]
	require.Positive(s.t, surplus)
	ledger := balance - surplus
	runtime := treasuryRuntime
	if treasury == treasuryGen1 {
		runtime = treasuryGen1Runtime
	}
	testkeeper.EVMTestApp.EvmKeeper.SetCode(s.ctx, treasury, common.FromHex(runtime))
	s.call(admin, treasury, "initialize(address,address)", addressWord(s.k.Generation2().EventEmitter), addressWord(admin))
	s.call(admin, treasury, "grantRole(bytes32,address)", depositorRole.Bytes(), addressWord(depositor))
	s.transfer(s.holder, depositor, ledger)
	s.send(depositor, "approve(address,uint256)", addressWord(treasury), word(big.NewInt(ledger)))
	s.call(depositor, treasury, "deposit(address,uint256)", addressWord(s.usdl), word(big.NewInt(ledger)))
	s.transfer(s.holder, treasury, surplus)
	require.Equal(s.t, big.NewInt(ledger), s.view(treasury, "getBalance(address)", addressWord(s.usdl)))
	require.Equal(s.t, big.NewInt(1), s.view(treasury, "hasRole(bytes32,address)", make([]byte, 32), addressWord(admin)))
	require.Equal(s.t, balance, s.balanceOf(treasury))
	s.golden.Treasuries = append(s.golden.Treasuries,
		keeper.TreasuryDrain{Treasury: treasury, Admin: admin, Tokens: []common.Address{s.usdl}})
}

func (s *usdlStepEnv) call(from, to common.Address, signature string, words ...[]byte) {
	s.t.Helper()
	_, err := testkeeper.EVMTestApp.EvmKeeper.CallEVM(s.ctx, from, &to, nil, calldata(signature, words...))
	require.NoError(s.t, err)
}

func (s *usdlStepEnv) view(to common.Address, signature string, words ...[]byte) *big.Int {
	s.t.Helper()
	ret, err := testkeeper.EVMTestApp.EvmKeeper.StaticCallEVM(s.ctx, s.k.ModuleAddress(), &to,
		calldata(signature, words...))
	require.NoError(s.t, err)
	require.Len(s.t, ret, 32)
	return new(big.Int).SetBytes(ret)
}

func (s *usdlStepEnv) run() error {
	ctx, err := s.k.MigrateV6120USDL(s.ctx, s.caller, s.golden)
	s.ctx = ctx
	return err
}

func (s *usdlStepEnv) treasuryLedger() int64 {
	s.t.Helper()
	amount, err := s.k.TreasuryGetBalance(s.ctx, s.usdl)
	require.NoError(s.t, err)
	require.True(s.t, amount.IsInt64())
	return amount.Int64()
}

func TestUsdlBalancesMustMatchGolden(t *testing.T) {
	s := newUSDLStepEnv(t)
	for i, h := range s.golden.Balances {
		s.golden.Balances[i].Balance = h.Balance.AddRaw(1)
		err := s.run()
		require.ErrorIs(t, err, keeper.ErrUSDLBalanceDiffersFromGolden)
		require.ErrorContains(t, err, "upgrade v6.12.0: launchpad: usdl balance differs from golden")
		require.ErrorContains(t, err, h.Address.Hex())
		require.Equal(t, int64(0), s.balanceOf(s.custody))
		s.golden.Balances[i].Balance = h.Balance
	}
	p1, outside := common.HexToAddress("0xEeA16aC55AaE47c484EBef02eb1186aD606e1BF9"), address("outside holder")
	require.Equal(t, p1, s.golden.Balances[0].Address)
	recorded := s.golden.Balances[0].Balance.Int64()
	// usdl.json records P1 one base unit above its balance.
	s.transfer(p1, outside, 1)
	before := len(s.logs())

	err := s.run()
	require.ErrorIs(t, err, keeper.ErrUSDLBalanceDiffersFromGolden)
	require.ErrorContains(t, err, "usdl balance differs from golden")
	require.ErrorContains(t, err, p1.Hex())
	require.Equal(t, recorded-1, s.balanceOf(p1))
	require.Equal(t, int64(0), s.balanceOf(s.custody))
	require.Equal(t, int64(0), s.ledger(keeper.HeldQuoteLedger(p1)))
	require.Equal(t, int64(0), s.ledger(keeper.TreasuryHeldLedger()))
	require.Equal(t, int64(0), s.treasuryLedger())
	require.Len(t, s.logs(), before)

	// A golden naming another token than the quote token is refused.
	golden := s.golden
	s.golden.Address = address("other token")
	require.ErrorIs(t, s.run(), types.ErrInvalidConfiguration)
	s.golden = golden

	// The refusal marked nothing: once P1 holds its golden balance the step runs.
	s.transfer(outside, p1, 1)
	require.NoError(t, s.run())
	require.Equal(t, recorded, s.ledger(keeper.HeldQuoteLedger(p1)))
	require.Equal(t, int64(0), s.balanceOf(p1))
}

func TestUsdlGoldenVector(t *testing.T) {
	t.Run("funded FeesRouters", func(t *testing.T) {
		s := newUSDLStepEnv(t)
		accumulators := map[uint8]common.Address{}
		balances := map[uint8]int64{}
		for _, h := range s.golden.Balances {
			if h.Role == types.RoleFeeAccumulator {
				accumulators[h.Generation] = h.Address
				balances[h.Generation] = h.Balance.Int64()
			}
		}
		before := len(s.logs())
		var routers []common.Address
		for i, h := range s.golden.Balances {
			if h.Role == types.RoleFeesRouter {
				s.transfer(s.holder, h.Address, 17)
				s.golden.Balances[i].Balance = sdk.NewInt(17)
				routers = append(routers, h.Address)
			}
		}
		require.Len(t, routers, 2)
		require.NoError(t, s.run())
		for _, router := range routers {
			require.Zero(t, s.balanceOf(router))
			require.Contains(t, s.since(before), transferLog{from: router, to: s.custody, amount: 17})
		}
		for generation, accumulator := range accumulators {
			require.Equal(t, balances[generation]+17, s.ledger(keeper.FeeHeldLedger(accumulator)))
		}
		require.Equal(t, goldenCustodySum+goldenTreasurySum+34, s.balanceOf(s.custody))
		require.Equal(t, usdlSupply, s.totalSupply())
		_, broken := keeper.CustodyInvariant(s.k)(s.ctx)
		require.False(t, broken)
	})
	s := newUSDLStepEnv(t)
	launchpad := common.HexToAddress(types.LaunchpadAddress)
	banks := s.bankBalances(s.custody, launchpad)
	before := len(s.logs())
	var want []transferLog
	var custodySum, treasurySum int64
	for _, h := range s.golden.Balances {
		amount := h.Balance.Int64()
		switch h.Role {
		case types.RolePool, types.RoleFeeAccumulator:
			custodySum += amount
			if amount > 0 {
				want = append(want, transferLog{from: h.Address, to: s.custody, amount: amount})
			}
		case types.RoleTreasury:
			treasurySum += amount
			surplus := treasurySurplus[h.Address]
			want = append(want, transferLog{from: h.Address, to: launchpad, amount: amount - surplus},
				transferLog{from: h.Address, to: launchpad, amount: surplus},
				transferLog{from: launchpad, to: s.custody, amount: amount})
		}
	}
	require.Equal(t, goldenCustodySum, custodySum)
	require.Equal(t, goldenTreasurySum, treasurySum)

	require.NoError(t, s.run())

	require.Equal(t, goldenCustodySum+goldenTreasurySum, s.balanceOf(s.custody))
	var held, fees int64
	for _, h := range s.golden.Balances {
		require.Equal(t, int64(0), s.balanceOf(h.Address), "%s %s", h.Role, h.Address.Hex())
		switch h.Role {
		case types.RolePool:
			require.Equal(t, h.Balance.Int64(), s.ledger(keeper.HeldQuoteLedger(h.Address)))
			held += h.Balance.Int64()
		case types.RoleFeeAccumulator:
			require.Equal(t, h.Balance.Int64(), s.ledger(keeper.FeeHeldLedger(h.Address)))
			fees += h.Balance.Int64()
		}
	}
	require.Equal(t, goldenCustodySum, held+fees)
	require.Equal(t, int64(2_414_497_551_549), fees)
	require.Equal(t, goldenTreasurySum, s.ledger(keeper.TreasuryHeldLedger()))
	require.Equal(t, int64(0), s.balanceOf(launchpad))
	require.Equal(t, usdlSupply, s.totalSupply())
	require.Equal(t, banks, s.bankBalances(s.custody, launchpad))
	require.Len(t, want, 48+6)
	require.ElementsMatch(t, want, s.since(before))
	_, broken := keeper.CustodyInvariant(s.k)(s.ctx)
	require.False(t, broken)
	_, broken = keeper.LiabilitiesInvariant(s.k)(s.ctx)
	require.False(t, broken)
}

func TestTreasuryWithdrawnIntoCustody(t *testing.T) {
	s := newUSDLStepEnv(t)
	drains := s.golden.Treasuries
	evm := &testkeeper.EVMTestApp.EvmKeeper

	// The generation 1 Treasury runs the halt-height generation 1
	// implementation, which answers getBalance and hasRole but has no
	// recoverSurplus.
	require.Equal(t, treasuryGen1CodeHash, crypto.Keccak256Hash(common.FromHex(treasuryGen1Runtime)))
	require.Equal(t, treasuryGen1CodeHash, evm.GetCodeHash(s.ctx, treasuryGen1))
	require.NotEqual(t, treasuryGen1, s.k.TreasuryContract())
	gen1 := treasuryGen1
	_, err := evm.StaticCallEVM(s.ctx, s.k.ModuleAddress(), &gen1, calldata("recoverSurplus(address,address,uint256)",
		addressWord(s.usdl), addressWord(common.HexToAddress(types.LaunchpadAddress)), word(big.NewInt(1))))
	require.Error(t, err)
	require.Positive(t, s.view(treasuryGen1, "getBalance(address)", addressWord(s.usdl)).Sign())
	for _, d := range drains {
		if d.Treasury == treasuryGen1 {
			require.Equal(t, big.NewInt(1), s.view(treasuryGen1, "hasRole(bytes32,address)", make([]byte, 32),
				addressWord(d.Admin)))
		}
	}

	// A drain from an account without DEFAULT_ADMIN_ROLE at the Treasury is
	// refused by the hasRole check.
	s.golden.Treasuries = []keeper.TreasuryDrain{drains[0], drains[1]}
	s.golden.Treasuries[1].Admin = address("not an admin")
	cacheCtx, _ := s.ctx.CacheContext()
	_, err = s.k.MigrateV6120USDL(cacheCtx, s.caller, s.golden)
	require.ErrorIs(t, err, types.ErrMissingRole)
	require.ErrorContains(t, err, drains[1].Treasury.Hex())
	// A drain list that omits a Treasury is refused before any call.
	s.golden.Treasuries = drains[:1]
	cacheCtx, _ = s.ctx.CacheContext()
	_, err = s.k.MigrateV6120USDL(cacheCtx, s.caller, s.golden)
	require.ErrorIs(t, err, types.ErrInvalidConfiguration)
	s.golden.Treasuries = drains

	require.NoError(t, s.run())
	for _, d := range drains {
		require.Equal(t, int64(0), s.balanceOf(d.Treasury))
		require.Zero(t, s.view(d.Treasury, "getBalance(address)", addressWord(s.usdl)).Sign())
	}
	require.Equal(t, goldenTreasurySum, s.ledger(keeper.TreasuryHeldLedger()))
	require.Equal(t, goldenTreasuryLedger, s.treasuryLedger())
	held, err := s.k.TreasuryHeld(s.ctx, s.usdl)
	require.NoError(t, err)
	require.Equal(t, goldenTreasurySum, held.Int64())
	require.Equal(t, int64(0), s.balanceOf(common.HexToAddress(types.LaunchpadAddress)))
}

func TestUsdlStorageUntouched(t *testing.T) {
	s := newUSDLStepEnv(t)
	evm := &testkeeper.EVMTestApp.EvmKeeper
	a, spender := address("holder A"), address("spender S")
	s.transfer(s.holder, a, 1_000)
	s.send(a, "approve(address,uint256)", addressWord(spender), word(big.NewInt(500)))
	code, codeHash := evm.GetCode(s.ctx, s.usdl), evm.GetCodeHash(s.ctx, s.usdl)
	slots := []common.Hash{
		common.BigToHash(big.NewInt(0)), common.BigToHash(big.NewInt(1)), common.BigToHash(big.NewInt(2)),
		common.BigToHash(big.NewInt(7)), common.BigToHash(big.NewInt(8)),
		mappingSlot(a.Bytes(), common.BigToHash(big.NewInt(3))),
		mappingSlot(s.holder.Bytes(), common.BigToHash(big.NewInt(3))),
		mappingSlot(spender.Bytes(), mappingSlot(a.Bytes(), common.BigToHash(big.NewInt(4)))),
		mappingSlot(a.Bytes(), common.BigToHash(big.NewInt(5))),
	}
	values := make([]common.Hash, len(slots))
	for i, slot := range slots {
		values[i] = evm.GetState(s.ctx, s.usdl, slot)
	}
	require.Equal(t, common.BigToHash(big.NewInt(500)), values[7])
	nonce := s.read("nonces(address)", addressWord(a))

	require.NoError(t, s.run())

	require.Equal(t, int64(500), s.allowance(a, spender))
	require.Equal(t, nonce, s.read("nonces(address)", addressWord(a)))
	require.Equal(t, code, evm.GetCode(s.ctx, s.usdl))
	require.Equal(t, codeHash, evm.GetCodeHash(s.ctx, s.usdl))
	for i, slot := range slots {
		require.Equal(t, values[i], evm.GetState(s.ctx, s.usdl, slot), "slot %s", slot.Hex())
	}
	require.Equal(t, usdlSupply, s.totalSupply())
}

func TestUsdlStepRunsOnce(t *testing.T) {
	s := newUSDLStepEnv(t)
	require.NoError(t, s.run())
	custody, logs := s.balanceOf(s.custody), len(s.logs())
	p1 := s.golden.Balances[0].Address
	held := s.ledger(keeper.HeldQuoteLedger(p1))

	err := s.run()
	require.ErrorIs(t, err, keeper.ErrAlreadyMigrated)
	require.EqualError(t, err, "upgrade v6.12.0: launchpad: already migrated")
	require.Equal(t, custody, s.balanceOf(s.custody))
	require.Len(t, s.logs(), logs)
	require.Equal(t, held, s.ledger(keeper.HeldQuoteLedger(p1)))
	require.Equal(t, goldenTreasurySum, s.ledger(keeper.TreasuryHeldLedger()))
	require.Equal(t, goldenTreasuryLedger, s.treasuryLedger())
}
