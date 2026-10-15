# Launchpad contract: the launchpad precompile at 0x1017 in release v6.12.0

Version 1.0.0. This file binds the launchpad of the Paxeer X Network to the interface of the Launch-x contracts (github.com/Sidiora-Labs/Launch-x, commit c5eb7c7) of the manifest-active generation 2, served by the precompile at 0x1017 and its surfaces, with uusdl as the bank form of USDL, opticals, ERC721 fee rights and ERC20 pool tokens, and the v6.12.0 migration of every generation 1 and generation 2 pool.

Citations use `Launch-x:path:line` at c5eb7c7 and plain `path:line` for this repository at 211acbc6c. A "required change" names the current code and the target behaviour. The launchpad backend binds the precompile by manifest role.

## 1. Scope and addresses

| Role | Generation 1 | Generation 2 | Markets created after the upgrade |
|---|---|---|---|
| SidioraFactory | `0x8a1A09CEe72c1D39dF33B8284E38baeF8371f465` | `0x3993D004166738d1069B5C4e0a923f7B9D5D8830` | 0x1017 |
| Router | `0xCC7298801112682e10ee14b8a520309caD80336d` | `0x78FB06f525cD964388f2305A489035263E31C6e4` | 0x1017 |
| Quoter | `0xB768e183b6EfDeDf8b2AA7af732039D1C3c452d0` | `0x793006f4F321b8863F26DBD75b11bAa01DA181dc` | 0x1017 |
| FeesRouter | `0x02Df12a44F2658080E76fbcF7D6B34Baa97843b6` | `0x05681dA6f611C2dfFD3E92611F6dE2d247647927` | 0x1017 |
| FeeAccumulator | `0x50C69dF6637b3DCE6a7407C5A4b4F99E68514A76` | `0x7b14808b72f61664cDb79D25E6d1587655571a66` | 0x1017 |
| PoolRegistry | `0x7684382c89f79104574D8EF9b31eFf2eD2C2BA0b` | `0x1dC357d2B7B850332a91F06A9E66634F46fC4Ba7` | 0x1017 |
| ProtocolConfig | `0xEeDF5409cFD30bd14D0399318c7d2150265575e5` | `0xF3669eDc475Cb6E1F0d7635240ABD8e8FB7E20C6` | 0x1017 |
| SidioraNFT | `0xDF73b354ed9dcB473cc9D01541c46f507591e190` | `0xe8DcC844a8006dbf64f5Bb851bCad90292a6409B` | generation 2 collection |
| EventEmitter | `0x0E10286EE51F99c666CDcAb52451e58AbdA4048F` | `0x622E1bb90E0E2B6E6e5e9e2DbE1555389E8A3EF6` | 0x1017 (launchpad events) |

USDL: `0x85FcD13735F4309833A503EE804ea32395851479` (Launch-x:shared/config/deployment/current.json:86). After the upgrade this address is the ERC20 native pointer of the bank denom uusdl.

- The launchpad is served at 0x0000000000000000000000000000000000001017 (modules/launchpad/types/keys.go:26), registered at precompiles/setup.go:14.
- Ten contract roles are served with their Launch-x ABIs: SidioraPool 42 functions, SidioraFactory 23, SidioraERC20 19, SidioraNFT 37, Router 23, Quoter 19, FeesRouter 20, FeeAccumulator 54, PoolRegistry 22, ProtocolConfig 27 (the pool entry point at Launch-x:contracts/contracts/core/SidioraPool.sol:147).
- Direct calls to 0x1017 resolve through abi.json, 113 selectors, role order Router, SidioraFactory, Quoter, PoolRegistry, FeesRouter, FeeAccumulator, ProtocolConfig, replacing the method set of precompiles/launchpad/launchpad.go:46.
- Each market carries generation 1 (Launch-x:protocol/addresses.go:22), generation 2 (Launch-x:shared/config/deployment/current.json:37) or 3 (created after the upgrade, roles at 0x1017); role surfaces of a generation answer for that generation.
- Fee rights are ERC721 tokens in the generation 1 collection 0xDF73…e190 and the generation 2 collection 0xe8Dc…409B; markets created after the upgrade mint in the generation 2 collection from nextTokenId (base Launch-x:contracts/contracts/base/ERC721Base.sol:11).
- Pool tokens are ERC20 tokens whose bank denoms are tokenfactory denoms with subdenom lp<index> (modules/launchpad/keeper/market.go:18) and whose old addresses are native pointers or token surfaces.

## 2. Interface by role

Every function, event and custom error of each role, with its selector (function and error: 4 bytes; event: topic 0). The ABI goldens of task 2.0.1 freeze these tables.

### SidioraPool

42 functions, 6 events, 21 errors.

| Function | Selector | Mutability | Returns | Declared |
|---|---|---|---|---|
| `PAUSER_ROLE()` | `0xe63ab1e9` | view | `(bytes32)` | Launch-x:contracts/contracts/base/Pausable.sol:7 |
| `SNAPSHOT_INTERVAL()` | `0x6d2beef1` | view | `(uint256)` | Launch-x:contracts/contracts/core/SidioraPool.sol:80 |
| `SNAPSHOT_MIN_VOLUME_BPS()` | `0x923795a3` | view | `(uint256)` | Launch-x:contracts/contracts/core/SidioraPool.sol:81 |
| `acceptGuardian()` | `0xa5f31d61` | nonpayable | `()` | Launch-x:contracts/contracts/core/SidioraPool.sol:314 |
| `accumulatedTokenFees()` | `0x1e5a0998` | view | `(uint256)` | Launch-x:contracts/contracts/core/SidioraPool.sol:72 |
| `accumulatedUsdlFees()` | `0x61f63bf5` | view | `(uint256)` | Launch-x:contracts/contracts/core/SidioraPool.sol:71 |
| `creationTimestamp()` | `0xb3311086` | view | `(uint256)` | Launch-x:contracts/contracts/core/SidioraPool.sol:64 |
| `cumulativeBuyUsdlVolume()` | `0x1aa6f435` | view | `(uint256)` | Launch-x:contracts/contracts/core/SidioraPool.sol:78 |
| `cumulativeSellUsdlVolume()` | `0x00a8b4fe` | view | `(uint256)` | Launch-x:contracts/contracts/core/SidioraPool.sol:79 |
| `cumulativeVolume()` | `0xf4dd4156` | view | `(uint256)` | Launch-x:contracts/contracts/core/SidioraPool.sol:65 |
| `currentFeeBps()` | `0x975f8f46` | view | `(uint256)` | Launch-x:contracts/contracts/core/SidioraPool.sol:404 |
| `emergencyPaused()` | `0x27c830a9` | view | `(bool)` | Launch-x:contracts/contracts/base/Pausable.sol:10 |
| `eventEmitter()` | `0x9ff78c30` | view | `(address)` | Launch-x:contracts/contracts/core/SidioraPool.sol:57 |
| `factory()` | `0xc45a0155` | view | `(address)` | Launch-x:contracts/contracts/core/SidioraPool.sol:75 |
| `feeAccumulator()` | `0x5eaea35f` | view | `(address)` | Launch-x:contracts/contracts/core/SidioraPool.sol:56 |
| `getEffectiveReserves()` | `0x6e7f1e46` | view | `(uint256,uint256)` | Launch-x:contracts/contracts/core/SidioraPool.sol:338 |
| `getPoolInfo()` | `0x60246c88` | view | `((address,address,uint256,uint256,uint256,uint256,uint256))` | Launch-x:contracts/contracts/core/SidioraPool.sol:350 |
| `getPrice()` | `0x98d5fdca` | view | `(uint256)` | Launch-x:contracts/contracts/core/SidioraPool.sol:346 |
| `getPriceSnapshots()` | `0x680aef93` | view | `(uint256[8])` | Launch-x:contracts/contracts/core/SidioraPool.sol:362 |
| `getReserves()` | `0x0902f1ac` | view | `(uint256,uint256,uint256)` | Launch-x:contracts/contracts/core/SidioraPool.sol:328 |
| `guardian()` | `0x452a9320` | view | `(address)` | Launch-x:contracts/contracts/core/SidioraPool.sol:59 |
| `initialize(address,address,address,address,address,address,address,uint256,uint256)` | `0x9bb725be` | nonpayable | `()` | Launch-x:contracts/contracts/core/SidioraPool.sol:88 |
| `lastSnapshotTimestamp()` | `0x6f660492` | view | `(uint256)` | Launch-x:contracts/contracts/core/SidioraPool.sol:77 |
| `migrateFactory(address)` | `0x308efad5` | nonpayable | `()` | Launch-x:contracts/contracts/core/SidioraPool.sol:127 |
| `opticalAddress()` | `0xa2d02927` | view | `(address)` | Launch-x:contracts/contracts/core/SidioraPool.sol:55 |
| `pause()` | `0x8456cb59` | nonpayable | `()` | Launch-x:contracts/contracts/core/SidioraPool.sol:296 |
| `paused()` | `0x5c975abb` | view | `(bool)` | Launch-x:contracts/contracts/core/SidioraPool.sol:322 |
| `pendingGuardian()` | `0x762c31ba` | view | `(address)` | Launch-x:contracts/contracts/core/SidioraPool.sol:76 |
| `priceSnapshots(uint256)` | `0xcbd47f88` | view | `(uint256)` | Launch-x:contracts/contracts/core/SidioraPool.sol:67 |
| `protocolConfig()` | `0xf5efbb4f` | view | `(address)` | Launch-x:contracts/contracts/core/SidioraPool.sol:58 |
| `realUsdlBalance()` | `0xa8c98bde` | view | `(uint256)` | Launch-x:contracts/contracts/core/SidioraPool.sol:62 |
| `snapshotCount()` | `0x098ab6a1` | view | `(uint256)` | Launch-x:contracts/contracts/core/SidioraPool.sol:69 |
| `snapshotIndex()` | `0x234feaf2` | view | `(uint256)` | Launch-x:contracts/contracts/core/SidioraPool.sol:68 |
| `swap(uint256,uint256,bool,address,uint256)` | `0x704c4536` | nonpayable | `(uint256)` | Launch-x:contracts/contracts/core/SidioraPool.sol:147 |
| `swapFor(uint256,uint256,bool,address,address,uint256)` | `0x0602dc5b` | nonpayable | `(uint256)` | Launch-x:contracts/contracts/core/SidioraPool.sol:158 |
| `syncReserves()` | `0x5249f13e` | nonpayable | `(uint256,uint256)` | Launch-x:contracts/contracts/core/SidioraPool.sol:279 |
| `tokenAddress()` | `0x9d76ea58` | view | `(address)` | Launch-x:contracts/contracts/core/SidioraPool.sol:53 |
| `tokenReserve()` | `0xcbcb3171` | view | `(uint256)` | Launch-x:contracts/contracts/core/SidioraPool.sol:63 |
| `transferGuardian(address)` | `0x091954cd` | nonpayable | `()` | Launch-x:contracts/contracts/core/SidioraPool.sol:307 |
| `unpause()` | `0x3f4ba83a` | nonpayable | `()` | Launch-x:contracts/contracts/core/SidioraPool.sol:301 |
| `usdlAddress()` | `0x2e10f976` | view | `(address)` | Launch-x:contracts/contracts/core/SidioraPool.sol:54 |
| `virtualUsdlReserve()` | `0xe87ca810` | view | `(uint256)` | Launch-x:contracts/contracts/core/SidioraPool.sol:61 |

| Event | Topic 0 | Declared |
|---|---|---|
| `FactoryMigrated(address)` | `0xdc152171996ab31ea044d1f083998178a0515971a850651a133f8d13027bb277` | Launch-x:contracts/contracts/core/SidioraPool.sol:50 |
| `GuardianTransferStarted(address,address)` | `0x112d7ce5d0717de1e8d403be44596423dc61e85438d8ba49eaf0d31b1992bce0` | Launch-x:contracts/contracts/core/SidioraPool.sol:47 |
| `GuardianTransferred(address,address)` | `0x9fe65984b511eb7998b3cd481cfd80c807fcc7f28721a790d33c87dc9c879485` | Launch-x:contracts/contracts/core/SidioraPool.sol:48 |
| `Initialized(uint64)` | `0xc7f505b2f371ae2175ee4913f4499e1f2633a7b5936321eed1cdaeb6115181d2` | Launch-x:contracts/contracts/base/Initializable.sol:21 |
| `InputAdjustment(uint256,uint256)` | `0xc6dfd6050e713ed5ff35fd38843c7c60822498a3d56ae5dc062e563a5b157d5c` | Launch-x:contracts/contracts/core/SidioraPool.sol:49 |
| `PauseToggled(bool)` | `0x9077d36bc00859b5c3f320310707208543dd35092cb0a0fe117d0c6a558b148b` | Launch-x:contracts/contracts/base/Pausable.sol:19 |

| Error | Selector | Declared |
|---|---|---|
| `AlreadyInitialized()` | `0x0dc149f0` | Launch-x:contracts/contracts/base/Initializable.sol:18 |
| `DeadlineExpired()` | `0x1ab7da6b` | Launch-x:contracts/contracts/interfaces/ISidioraPool.sol:8 |
| `DivisionByZero()` | `0x23d359a3` | Launch-x:contracts/contracts/libraries/SidioraMath.sol:9 |
| `InsufficientInput()` | `0xf8b3bb61` | Launch-x:contracts/contracts/interfaces/ISidioraPool.sol:11 |
| `InsufficientInput()` | `0xf8b3bb61` | Launch-x:contracts/contracts/interfaces/ISidioraPool.sol:11 |
| `InsufficientLiquidity()` | `0xbb55fd27` | Launch-x:contracts/contracts/interfaces/ISidioraPool.sol:10 |
| `InsufficientLiquidity()` | `0xbb55fd27` | Launch-x:contracts/contracts/interfaces/ISidioraPool.sol:10 |
| `InvalidBalance()` | `0xc52e3eff` | Launch-x:contracts/contracts/core/SidioraPool.sol:45 |
| `InvalidConfiguration()` | `0xc52a9bd3` | Launch-x:contracts/contracts/core/SidioraPool.sol:44 |
| `InvalidHookResponse()` | `0x1e048e1d` | Launch-x:contracts/contracts/core/SidioraPool.sol:46 |
| `NotInitializing()` | `0xd7e6bcf8` | Launch-x:contracts/contracts/base/Initializable.sol:19 |
| `NotPaused()` | `0x6cd60201` | Launch-x:contracts/contracts/base/Pausable.sol:17 |
| `Overflow()` | `0x35278d12` | Launch-x:contracts/contracts/libraries/SidioraMath.sol:8 |
| `Paused()` | `0x9e87fac8` | Launch-x:contracts/contracts/base/Pausable.sol:16 |
| `ReentrancyGuardReentrantCall()` | `0x3ee5aeb5` | Launch-x:contracts/contracts/base/ReentrancyGuard.sol:15 |
| `SlippageExceeded()` | `0x8199f5f3` | Launch-x:contracts/contracts/interfaces/ISidioraPool.sol:9 |
| `TransferFailed()` | `0x90b8ec18` | Launch-x:contracts/contracts/libraries/TransferHelper.sol:8 |
| `Unauthorized()` | `0x82b42900` | Launch-x:contracts/contracts/core/SidioraPool.sol:43 |
| `VirtualFloorBreached()` | `0x04af467f` | Launch-x:contracts/contracts/core/SidioraPool.sol:42 |
| `ZeroAddress()` | `0xd92e233d` | Launch-x:contracts/contracts/interfaces/ISidioraPool.sol:12 |
| `ZeroAmount()` | `0x1f2a2005` | Launch-x:contracts/contracts/interfaces/ISidioraPool.sol:13 |

### SidioraFactory

23 functions, 6 events, 17 errors.

| Function | Selector | Mutability | Returns | Declared |
|---|---|---|---|---|
| `DEFAULT_ADMIN_ROLE()` | `0xa217fddf` | view | `(bytes32)` | Launch-x:contracts/contracts/base/AccessControl.sol:9 |
| `ROUTER_ROLE()` | `0x30d643b5` | view | `(bytes32)` | Launch-x:contracts/contracts/core/SidioraFactory.sol:23 |
| `createMarket(string,string,uint8,address)` | `0x57c0d7fd` | nonpayable | `(address,address,uint256)` | Launch-x:contracts/contracts/core/SidioraFactory.sol:72 |
| `createMarketFor(address,string,string,uint8,address)` | `0x5db85915` | nonpayable | `(address,address,uint256)` | Launch-x:contracts/contracts/core/SidioraFactory.sol:88 |
| `eventEmitter()` | `0x9ff78c30` | view | `(address)` | Launch-x:contracts/contracts/core/SidioraFactory.sol:28 |
| `feeAccumulator()` | `0x5eaea35f` | view | `(address)` | Launch-x:contracts/contracts/core/SidioraFactory.sol:31 |
| `getNonce(address)` | `0x2d0335ab` | view | `(uint256)` | Launch-x:contracts/contracts/core/SidioraFactory.sol:100 |
| `getRoleAdmin(bytes32)` | `0x248a9ca3` | view | `(bytes32)` | Launch-x:contracts/contracts/base/AccessControl.sol:53 |
| `grantRole(bytes32,address)` | `0x2f2ff15d` | nonpayable | `()` | Launch-x:contracts/contracts/base/AccessControl.sol:58 |
| `hasRole(bytes32,address)` | `0x91d14854` | view | `(bool)` | Launch-x:contracts/contracts/base/AccessControl.sol:48 |
| `initialize(address,address,address,address,address,address,address,address,address)` | `0xf5e95acb` | nonpayable | `()` | Launch-x:contracts/contracts/core/SidioraFactory.sol:41 |
| `nftContract()` | `0xd56d229d` | view | `(address)` | Launch-x:contracts/contracts/core/SidioraFactory.sol:26 |
| `poolBeacon()` | `0xc5a7b2ea` | view | `(address)` | Launch-x:contracts/contracts/core/SidioraFactory.sol:25 |
| `poolRegistry()` | `0xafcff50f` | view | `(address)` | Launch-x:contracts/contracts/core/SidioraFactory.sol:27 |
| `predictPoolAddress(address,address,address)` | `0x6992a1f5` | view | `(address)` | Launch-x:contracts/contracts/core/SidioraFactory.sol:167 |
| `protocolConfig()` | `0xf5efbb4f` | view | `(address)` | Launch-x:contracts/contracts/core/SidioraFactory.sol:29 |
| `proxiableUUID()` | `0x52d1902d` | view | `(bytes32)` | Launch-x:contracts/contracts/base/UUPSUpgradeable.sol:41 |
| `registeredDefaultAdminCount()` | `0xc0cb71b6` | view | `(uint256)` | Launch-x:contracts/contracts/base/AccessControl.sol:32 |
| `renounceRole(bytes32,address)` | `0x36568abe` | nonpayable | `()` | Launch-x:contracts/contracts/base/AccessControl.sol:68 |
| `revokeRole(bytes32,address)` | `0xd547741f` | nonpayable | `()` | Launch-x:contracts/contracts/base/AccessControl.sol:63 |
| `treasury()` | `0x61d027b3` | view | `(address)` | Launch-x:contracts/contracts/core/SidioraFactory.sol:30 |
| `upgradeToAndCall(address,bytes)` | `0x4f1ef286` | payable | `()` | Launch-x:contracts/contracts/base/UUPSUpgradeable.sol:48 |
| `usdlAddress()` | `0x2e10f976` | view | `(address)` | Launch-x:contracts/contracts/core/SidioraFactory.sol:32 |

| Event | Topic 0 | Declared |
|---|---|---|
| `Initialized(uint64)` | `0xc7f505b2f371ae2175ee4913f4499e1f2633a7b5936321eed1cdaeb6115181d2` | Launch-x:contracts/contracts/base/Initializable.sol:21 |
| `MarketCreated(address,address,address,uint256,address)` | `0xde3c5cd27ce87044b7bc6c8182270f302e537f55f8c943271a34e5163ef2b284` | Launch-x:contracts/contracts/interfaces/ISidioraFactory.sol:13 |
| `RoleAdminChanged(bytes32,bytes32,bytes32)` | `0xbd79b86ffe0ab8e8776151514217cd7cacd52c909f66475c3af44e129f0b00ff` | Launch-x:contracts/contracts/base/AccessControl.sol:40 |
| `RoleGranted(bytes32,address,address)` | `0x2f8788117e7eff1d82e926ec794901d17c78024a50270940304540a733656f0d` | Launch-x:contracts/contracts/base/AccessControl.sol:38 |
| `RoleRevoked(bytes32,address,address)` | `0xf6391f5c32d9c69d2a47ea670b442974b53935d1edc7fd64eb21e047a839171b` | Launch-x:contracts/contracts/base/AccessControl.sol:39 |
| `Upgraded(address)` | `0xbc7cd75a20ee27fd9adebab32041f755214dbc6bffa90cc0225b39da2e5c2d3b` | Launch-x:contracts/contracts/base/ERC1967Utils.sol:23 |

| Error | Selector | Declared |
|---|---|---|
| `AlreadyInitialized()` | `0x0dc149f0` | Launch-x:contracts/contracts/base/Initializable.sol:18 |
| `DuplicateToken()` | `0x464e3f6a` | Launch-x:contracts/contracts/interfaces/ISidioraFactory.sol:10 |
| `InsufficientCreationFee()` | `0x535b7470` | Launch-x:contracts/contracts/interfaces/ISidioraFactory.sol:9 |
| `InvalidImplementation()` | `0x68155f9a` | Launch-x:contracts/contracts/base/UUPSUpgradeable.sol:19 |
| `InvalidImplementation()` | `0x68155f9a` | Launch-x:contracts/contracts/base/UUPSUpgradeable.sol:19 |
| `InvalidRoleAccount()` | `0xeccda526` | Launch-x:contracts/contracts/base/AccessControl.sol:23 |
| `LastDefaultAdmin()` | `0x9c88180d` | Launch-x:contracts/contracts/base/AccessControl.sol:22 |
| `MissingRole(address,bytes32)` | `0x0161a64a` | Launch-x:contracts/contracts/base/AccessControl.sol:36 |
| `NonPayableUpgrade()` | `0x93738d49` | Launch-x:contracts/contracts/base/UUPSUpgradeable.sol:20 |
| `NotInitializing()` | `0xd7e6bcf8` | Launch-x:contracts/contracts/base/Initializable.sol:19 |
| `TransferFailed()` | `0x90b8ec18` | Launch-x:contracts/contracts/libraries/TransferHelper.sol:8 |
| `TransferFromFailed()` | `0x7939f424` | Launch-x:contracts/contracts/libraries/TransferHelper.sol:9 |
| `UUPSNotThroughActiveProxy()` | `0x45a93fc9` | Launch-x:contracts/contracts/base/UUPSUpgradeable.sol:18 |
| `UUPSNotThroughProxy()` | `0x4f083858` | Launch-x:contracts/contracts/base/UUPSUpgradeable.sol:17 |
| `UnauthorizedUpgrade()` | `0x3a617a54` | Launch-x:contracts/contracts/base/UUPSUpgradeable.sol:16 |
| `UpgradeInitializationFailed()` | `0xcc1cf440` | Launch-x:contracts/contracts/base/UUPSUpgradeable.sol:21 |
| `ZeroAddress()` | `0xd92e233d` | Launch-x:contracts/contracts/interfaces/ISidioraFactory.sol:8 |

### SidioraERC20

19 functions, 2 events, 7 errors.

| Function | Selector | Mutability | Returns | Declared |
|---|---|---|---|---|
| `DOMAIN_SEPARATOR()` | `0x3644e515` | view | `(bytes32)` | Launch-x:contracts/contracts/base/ERC20Base.sol:48 |
| `PERMIT_TYPEHASH()` | `0x30adf81f` | view | `(bytes32)` | Launch-x:contracts/contracts/base/ERC20Base.sol:26 |
| `allowance(address,address)` | `0xdd62ed3e` | view | `(uint256)` | Launch-x:contracts/contracts/base/ERC20Base.sol:14 |
| `approve(address,uint256)` | `0x095ea7b3` | nonpayable | `(bool)` | Launch-x:contracts/contracts/base/ERC20Base.sol:93 |
| `balanceOf(address)` | `0x70a08231` | view | `(uint256)` | Launch-x:contracts/contracts/base/ERC20Base.sol:13 |
| `bindPool(address)` | `0x5a73fae2` | nonpayable | `()` | Launch-x:contracts/contracts/core/SidioraERC20.sol:21 |
| `decimals()` | `0x313ce567` | view | `(uint8)` | Launch-x:contracts/contracts/base/ERC20Base.sol:10 |
| `deploymentFactory()` | `0x8ea261cc` | view | `(address)` | Launch-x:contracts/contracts/core/SidioraERC20.sol:16 |
| `getPastBalance(address,uint256)` | `0x0df86999` | view | `(uint256)` | Launch-x:contracts/contracts/base/ERC20Base.sol:60 |
| `getPastTotalSupply(uint256)` | `0x8e539e8c` | view | `(uint256)` | Launch-x:contracts/contracts/base/ERC20Base.sol:68 |
| `getPastVotes(address,uint256)` | `0x3a46b1a8` | view | `(uint256)` | Launch-x:contracts/contracts/base/ERC20Base.sol:64 |
| `name()` | `0x06fdde03` | view | `(string)` | Launch-x:contracts/contracts/base/ERC20Base.sol:8 |
| `nonces(address)` | `0x7ecebe00` | view | `(uint256)` | Launch-x:contracts/contracts/base/ERC20Base.sol:15 |
| `permit(address,address,uint256,uint256,uint8,bytes32,bytes32)` | `0xd505accf` | nonpayable | `()` | Launch-x:contracts/contracts/base/ERC20Base.sol:116 |
| `pool()` | `0x16f0115b` | view | `(address)` | Launch-x:contracts/contracts/core/SidioraERC20.sol:17 |
| `symbol()` | `0x95d89b41` | view | `(string)` | Launch-x:contracts/contracts/base/ERC20Base.sol:9 |
| `totalSupply()` | `0x18160ddd` | view | `(uint256)` | Launch-x:contracts/contracts/base/ERC20Base.sol:11 |
| `transfer(address,uint256)` | `0xa9059cbb` | nonpayable | `(bool)` | Launch-x:contracts/contracts/base/ERC20Base.sol:98 |
| `transferFrom(address,address,uint256)` | `0x23b872dd` | nonpayable | `(bool)` | Launch-x:contracts/contracts/base/ERC20Base.sol:103 |

| Event | Topic 0 | Declared |
|---|---|---|
| `Approval(address,address,uint256)` | `0x8c5be1e5ebec7d5bd14f71427d1e84f3dd0314c0f7b2291e5b200ac8c7c3b925` | Launch-x:contracts/contracts/base/ERC20Base.sol:36 |
| `Transfer(address,address,uint256)` | `0xddf252ad1be2c89b69c2b068fc378daa952ba7f163c4a11628f55a4df523b3ef` | Launch-x:contracts/contracts/base/ERC20Base.sol:35 |

| Error | Selector | Declared |
|---|---|---|
| `FutureLookup()` | `0x0297ecc2` | Launch-x:contracts/contracts/base/ERC20Base.sol:25 |
| `InsufficientAllowance()` | `0x13be252b` | Launch-x:contracts/contracts/base/ERC20Base.sol:30 |
| `InsufficientBalance()` | `0xf4d678b8` | Launch-x:contracts/contracts/base/ERC20Base.sol:29 |
| `InvalidPermit()` | `0xddafbaef` | Launch-x:contracts/contracts/base/ERC20Base.sol:31 |
| `InvalidPoolBinding()` | `0xf77cf96c` | Launch-x:contracts/contracts/core/SidioraERC20.sol:18 |
| `PermitExpired()` | `0x1a15a3cc` | Launch-x:contracts/contracts/base/ERC20Base.sol:32 |
| `ZeroAddress()` | `0xd92e233d` | Launch-x:contracts/contracts/base/ERC20Base.sol:33 |

### SidioraNFT

37 functions, 10 events, 23 errors.

| Function | Selector | Mutability | Returns | Declared |
|---|---|---|---|---|
| `DEFAULT_ADMIN_ROLE()` | `0xa217fddf` | view | `(bytes32)` | Launch-x:contracts/contracts/base/AccessControl.sol:9 |
| `MINTER_ROLE()` | `0xd5391393` | view | `(bytes32)` | Launch-x:contracts/contracts/core/SidioraNFT.sol:15 |
| `STRATEGY_SETTER_ROLE()` | `0x7bb5cc58` | view | `(bytes32)` | Launch-x:contracts/contracts/core/SidioraNFT.sol:16 |
| `approve(address,uint256)` | `0x095ea7b3` | nonpayable | `()` | Launch-x:contracts/contracts/base/ERC721Base.sol:49 |
| `balanceOf(address)` | `0x70a08231` | view | `(uint256)` | Launch-x:contracts/contracts/base/ERC721Base.sol:39 |
| `eventEmitter()` | `0x9ff78c30` | view | `(address)` | Launch-x:contracts/contracts/core/SidioraNFT.sol:23 |
| `getApproved(uint256)` | `0x081812fc` | view | `(address)` | Launch-x:contracts/contracts/base/ERC721Base.sol:58 |
| `getFeeStrategy(uint256)` | `0xdea3579a` | view | `(uint8)` | Launch-x:contracts/contracts/core/SidioraNFT.sol:103 |
| `getPoolAddress(uint256)` | `0x00a5ae21` | view | `(address)` | Launch-x:contracts/contracts/core/SidioraNFT.sol:128 |
| `getRoleAdmin(bytes32)` | `0x248a9ca3` | view | `(bytes32)` | Launch-x:contracts/contracts/base/AccessControl.sol:53 |
| `grantRole(bytes32,address)` | `0x2f2ff15d` | nonpayable | `()` | Launch-x:contracts/contracts/base/AccessControl.sol:58 |
| `hasRole(bytes32,address)` | `0x91d14854` | view | `(bool)` | Launch-x:contracts/contracts/base/AccessControl.sol:48 |
| `initialize(string,string,address,address)` | `0x8f15b414` | nonpayable | `()` | Launch-x:contracts/contracts/core/SidioraNFT.sol:37 |
| `isApprovedForAll(address,address)` | `0xe985e9c5` | view | `(bool)` | Launch-x:contracts/contracts/base/ERC721Base.sol:68 |
| `migratePoolIndex(uint256)` | `0x0270182d` | nonpayable | `()` | Launch-x:contracts/contracts/core/SidioraNFT.sol:86 |
| `mint(address,address,uint8)` | `0xbd5a3396` | nonpayable | `(uint256)` | Launch-x:contracts/contracts/core/SidioraNFT.sol:53 |
| `name()` | `0x06fdde03` | view | `(string)` | Launch-x:contracts/contracts/base/ERC721Base.sol:8 |
| `nextTokenId()` | `0x75794a3c` | view | `(uint256)` | Launch-x:contracts/contracts/core/SidioraNFT.sol:18 |
| `ownerOf(uint256)` | `0x6352211e` | view | `(address)` | Launch-x:contracts/contracts/base/ERC721Base.sol:44 |
| `poolIndexMigrationCursor()` | `0xce808028` | view | `(uint256)` | Launch-x:contracts/contracts/core/SidioraNFT.sol:29 |
| `poolIndexReady()` | `0x3e747982` | view | `(bool)` | Launch-x:contracts/contracts/core/SidioraNFT.sol:30 |
| `poolTokenId(address)` | `0x8a5d6909` | view | `(uint256)` | Launch-x:contracts/contracts/core/SidioraNFT.sol:25 |
| `proxiableUUID()` | `0x52d1902d` | view | `(bytes32)` | Launch-x:contracts/contracts/base/UUPSUpgradeable.sol:41 |
| `registeredDefaultAdminCount()` | `0xc0cb71b6` | view | `(uint256)` | Launch-x:contracts/contracts/base/AccessControl.sol:32 |
| `renounceRole(bytes32,address)` | `0x36568abe` | nonpayable | `()` | Launch-x:contracts/contracts/base/AccessControl.sol:68 |
| `revokeRole(bytes32,address)` | `0xd547741f` | nonpayable | `()` | Launch-x:contracts/contracts/base/AccessControl.sol:63 |
| `safeTransferFrom(address,address,uint256)` | `0x42842e0e` | nonpayable | `()` | Launch-x:contracts/contracts/base/ERC721Base.sol:77 |
| `safeTransferFrom(address,address,uint256,bytes)` | `0xb88d4fde` | nonpayable | `()` | Launch-x:contracts/contracts/base/ERC721Base.sol:77 |
| `setApprovalForAll(address,bool)` | `0xa22cb465` | nonpayable | `()` | Launch-x:contracts/contracts/base/ERC721Base.sol:63 |
| `setFeeStrategy(uint256,uint8)` | `0x9fcf8f62` | nonpayable | `()` | Launch-x:contracts/contracts/core/SidioraNFT.sol:109 |
| `supportsInterface(bytes4)` | `0x01ffc9a7` | view | `(bool)` | Launch-x:contracts/contracts/core/SidioraNFT.sol:148 |
| `symbol()` | `0x95d89b41` | view | `(string)` | Launch-x:contracts/contracts/base/ERC721Base.sol:9 |
| `tokenByIndex(uint256)` | `0x4f6ccce7` | view | `(uint256)` | Launch-x:contracts/contracts/base/ERC721Enumerable.sol:29 |
| `tokenOfOwnerByIndex(address,uint256)` | `0x2f745c59` | view | `(uint256)` | Launch-x:contracts/contracts/base/ERC721Enumerable.sol:35 |
| `totalSupply()` | `0x18160ddd` | view | `(uint256)` | Launch-x:contracts/contracts/base/ERC721Enumerable.sol:24 |
| `transferFrom(address,address,uint256)` | `0x23b872dd` | nonpayable | `()` | Launch-x:contracts/contracts/base/ERC721Base.sol:72 |
| `upgradeToAndCall(address,bytes)` | `0x4f1ef286` | payable | `()` | Launch-x:contracts/contracts/base/UUPSUpgradeable.sol:48 |

| Event | Topic 0 | Declared |
|---|---|---|
| `Approval(address,address,uint256)` | `0x8c5be1e5ebec7d5bd14f71427d1e84f3dd0314c0f7b2291e5b200ac8c7c3b925` | Launch-x:contracts/contracts/base/ERC721Base.sol:24 |
| `ApprovalForAll(address,address,bool)` | `0x17307eab39ab6107e8899845ad3d59bd9653f200f220920489ca2b5937696c31` | Launch-x:contracts/contracts/base/ERC721Base.sol:25 |
| `FeeStrategyChanged(uint256,uint8,uint8)` | `0x773a9b32cf9f6ecdff8ee5e6f78159f15beb042f9da471ba934182412ea69568` | Launch-x:contracts/contracts/interfaces/ISidioraNFT.sol:11 |
| `Initialized(uint64)` | `0xc7f505b2f371ae2175ee4913f4499e1f2633a7b5936321eed1cdaeb6115181d2` | Launch-x:contracts/contracts/base/Initializable.sol:21 |
| `PoolNFTMinted(uint256,address,address)` | `0xa1ff1e0bedf4ae07c5f17ac3f0d64625d9c386b9681a705499c8595ebea1a732` | Launch-x:contracts/contracts/interfaces/ISidioraNFT.sol:12 |
| `RoleAdminChanged(bytes32,bytes32,bytes32)` | `0xbd79b86ffe0ab8e8776151514217cd7cacd52c909f66475c3af44e129f0b00ff` | Launch-x:contracts/contracts/base/AccessControl.sol:40 |
| `RoleGranted(bytes32,address,address)` | `0x2f8788117e7eff1d82e926ec794901d17c78024a50270940304540a733656f0d` | Launch-x:contracts/contracts/base/AccessControl.sol:38 |
| `RoleRevoked(bytes32,address,address)` | `0xf6391f5c32d9c69d2a47ea670b442974b53935d1edc7fd64eb21e047a839171b` | Launch-x:contracts/contracts/base/AccessControl.sol:39 |
| `Transfer(address,address,uint256)` | `0xddf252ad1be2c89b69c2b068fc378daa952ba7f163c4a11628f55a4df523b3ef` | Launch-x:contracts/contracts/base/ERC721Base.sol:23 |
| `Upgraded(address)` | `0xbc7cd75a20ee27fd9adebab32041f755214dbc6bffa90cc0225b39da2e5c2d3b` | Launch-x:contracts/contracts/base/ERC1967Utils.sol:23 |

| Error | Selector | Declared |
|---|---|---|
| `AlreadyInitialized()` | `0x0dc149f0` | Launch-x:contracts/contracts/base/Initializable.sol:18 |
| `AlreadyMinted()` | `0xddefae28` | Launch-x:contracts/contracts/base/ERC721Base.sol:20 |
| `DuplicatePool()` | `0xb82a4746` | Launch-x:contracts/contracts/core/SidioraNFT.sol:26 |
| `IndexOutOfBounds()` | `0x4e23d035` | Launch-x:contracts/contracts/base/ERC721Enumerable.sol:14 |
| `InvalidImplementation()` | `0x68155f9a` | Launch-x:contracts/contracts/base/UUPSUpgradeable.sol:19 |
| `InvalidImplementation()` | `0x68155f9a` | Launch-x:contracts/contracts/base/UUPSUpgradeable.sol:19 |
| `InvalidRoleAccount()` | `0xeccda526` | Launch-x:contracts/contracts/base/AccessControl.sol:23 |
| `InvalidStrategy()` | `0x4e236e9a` | Launch-x:contracts/contracts/interfaces/ISidioraNFT.sol:8 |
| `LastDefaultAdmin()` | `0x9c88180d` | Launch-x:contracts/contracts/base/AccessControl.sol:22 |
| `MissingRole(address,bytes32)` | `0x0161a64a` | Launch-x:contracts/contracts/base/AccessControl.sol:36 |
| `NonERC721Receiver()` | `0x80526d0c` | Launch-x:contracts/contracts/base/ERC721Base.sol:21 |
| `NonPayableUpgrade()` | `0x93738d49` | Launch-x:contracts/contracts/base/UUPSUpgradeable.sol:20 |
| `NotApproved()` | `0xc19f17a9` | Launch-x:contracts/contracts/base/ERC721Base.sol:18 |
| `NotInitializing()` | `0xd7e6bcf8` | Launch-x:contracts/contracts/base/Initializable.sol:19 |
| `NotTokenOwner()` | `0x59dc379f` | Launch-x:contracts/contracts/base/ERC721Base.sol:17 |
| `PoolIndexMigrationRequired()` | `0x9aee8140` | Launch-x:contracts/contracts/core/SidioraNFT.sol:28 |
| `TokenNotFound()` | `0xcbdb7b30` | Launch-x:contracts/contracts/base/ERC721Base.sol:16 |
| `UUPSNotThroughActiveProxy()` | `0x45a93fc9` | Launch-x:contracts/contracts/base/UUPSUpgradeable.sol:18 |
| `UUPSNotThroughProxy()` | `0x4f083858` | Launch-x:contracts/contracts/base/UUPSUpgradeable.sol:17 |
| `UnauthorizedUpgrade()` | `0x3a617a54` | Launch-x:contracts/contracts/base/UUPSUpgradeable.sol:16 |
| `UnsupportedSnapshotToken()` | `0xe3aa7e40` | Launch-x:contracts/contracts/core/SidioraNFT.sol:27 |
| `UpgradeInitializationFailed()` | `0xcc1cf440` | Launch-x:contracts/contracts/base/UUPSUpgradeable.sol:21 |
| `ZeroAddress()` | `0xd92e233d` | Launch-x:contracts/contracts/base/ERC721Base.sol:19 |

### Router

23 functions, 9 events, 22 errors.

| Function | Selector | Mutability | Returns | Declared |
|---|---|---|---|---|
| `DEFAULT_ADMIN_ROLE()` | `0xa217fddf` | view | `(bytes32)` | Launch-x:contracts/contracts/base/AccessControl.sol:9 |
| `buy(address,uint256,uint256,uint256)` | `0x1622dbe4` | nonpayable | `(uint256)` | Launch-x:contracts/contracts/periphery/Router.sol:68 |
| `buyWithPermit(address,uint256,uint256,uint256,(uint256,uint256,uint8,bytes32,bytes32))` | `0xffafb569` | nonpayable | `(uint256)` | Launch-x:contracts/contracts/periphery/Router.sol:103 |
| `createMarket(string,string,uint8,address)` | `0x57c0d7fd` | nonpayable | `(address,address,uint256)` | Launch-x:contracts/contracts/periphery/Router.sol:58 |
| `createMarketWithPermit(string,string,uint8,address,(uint256,uint256,uint8,bytes32,bytes32))` | `0xdc614295` | nonpayable | `(address,address,uint256)` | Launch-x:contracts/contracts/periphery/Router.sol:142 |
| `factory()` | `0xc45a0155` | view | `(address)` | Launch-x:contracts/contracts/periphery/Router.sol:24 |
| `getRoleAdmin(bytes32)` | `0x248a9ca3` | view | `(bytes32)` | Launch-x:contracts/contracts/base/AccessControl.sol:53 |
| `grantRole(bytes32,address)` | `0x2f2ff15d` | nonpayable | `()` | Launch-x:contracts/contracts/base/AccessControl.sol:58 |
| `hasRole(bytes32,address)` | `0x91d14854` | view | `(bool)` | Launch-x:contracts/contracts/base/AccessControl.sol:48 |
| `initialize(address,address,address,address,address)` | `0x1459457a` | nonpayable | `()` | Launch-x:contracts/contracts/periphery/Router.sol:34 |
| `multicall(bytes[])` | `0xac9650d8` | nonpayable | `(bytes[])` | Launch-x:contracts/contracts/base/Multicall.sol:13 |
| `poolRegistry()` | `0xafcff50f` | view | `(address)` | Launch-x:contracts/contracts/periphery/Router.sol:25 |
| `protocolConfig()` | `0xf5efbb4f` | view | `(address)` | Launch-x:contracts/contracts/periphery/Router.sol:26 |
| `proxiableUUID()` | `0x52d1902d` | view | `(bytes32)` | Launch-x:contracts/contracts/base/UUPSUpgradeable.sol:41 |
| `registeredDefaultAdminCount()` | `0xc0cb71b6` | view | `(uint256)` | Launch-x:contracts/contracts/base/AccessControl.sol:32 |
| `renounceRole(bytes32,address)` | `0x36568abe` | nonpayable | `()` | Launch-x:contracts/contracts/base/AccessControl.sol:68 |
| `revokeRole(bytes32,address)` | `0xd547741f` | nonpayable | `()` | Launch-x:contracts/contracts/base/AccessControl.sol:63 |
| `sell(address,uint256,uint256,uint256)` | `0x92cdbac5` | nonpayable | `(uint256)` | Launch-x:contracts/contracts/periphery/Router.sol:78 |
| `sellWithPermit(address,uint256,uint256,uint256,(uint256,uint256,uint8,bytes32,bytes32))` | `0x3aa73d77` | nonpayable | `(uint256)` | Launch-x:contracts/contracts/periphery/Router.sol:115 |
| `swapTokenForToken(address,address,uint256,uint256,uint256)` | `0x8ef5184b` | nonpayable | `(uint256,uint256)` | Launch-x:contracts/contracts/periphery/Router.sol:90 |
| `swapTokenForTokenWithPermit(address,address,uint256,uint256,uint256,(uint256,uint256,uint8,bytes32,bytes32))` | `0x1894fbf8` | nonpayable | `(uint256,uint256)` | Launch-x:contracts/contracts/periphery/Router.sol:129 |
| `upgradeToAndCall(address,bytes)` | `0x4f1ef286` | payable | `()` | Launch-x:contracts/contracts/base/UUPSUpgradeable.sol:48 |
| `usdlAddress()` | `0x2e10f976` | view | `(address)` | Launch-x:contracts/contracts/periphery/Router.sol:27 |

| Event | Topic 0 | Declared |
|---|---|---|
| `Buy(address,address,uint256,uint256)` | `0x89f5adc174562e07c9c9b1cae7109bbecb21cf9d1b2847e550042b8653c54a0e` | Launch-x:contracts/contracts/interfaces/IRouter.sol:34 |
| `Initialized(uint64)` | `0xc7f505b2f371ae2175ee4913f4499e1f2633a7b5936321eed1cdaeb6115181d2` | Launch-x:contracts/contracts/base/Initializable.sol:21 |
| `MarketCreated(address,address,address,uint256)` | `0xfe7e33f239f268e882861cae62e631558d814973588d97738cc084b8b3489599` | Launch-x:contracts/contracts/interfaces/IRouter.sol:27 |
| `MultihopSwap(address,address,address,uint256,uint256,uint256)` | `0xef2dce1faa347130cf25c6d059af1f6f181983e69a2fadd79ed8751a532d3d96` | Launch-x:contracts/contracts/interfaces/IRouter.sol:48 |
| `RoleAdminChanged(bytes32,bytes32,bytes32)` | `0xbd79b86ffe0ab8e8776151514217cd7cacd52c909f66475c3af44e129f0b00ff` | Launch-x:contracts/contracts/base/AccessControl.sol:40 |
| `RoleGranted(bytes32,address,address)` | `0x2f8788117e7eff1d82e926ec794901d17c78024a50270940304540a733656f0d` | Launch-x:contracts/contracts/base/AccessControl.sol:38 |
| `RoleRevoked(bytes32,address,address)` | `0xf6391f5c32d9c69d2a47ea670b442974b53935d1edc7fd64eb21e047a839171b` | Launch-x:contracts/contracts/base/AccessControl.sol:39 |
| `Sell(address,address,uint256,uint256)` | `0xa082022e93cfcd9f1da5f9236718053910f7e840da080c789c7845698dc032ff` | Launch-x:contracts/contracts/interfaces/IRouter.sol:41 |
| `Upgraded(address)` | `0xbc7cd75a20ee27fd9adebab32041f755214dbc6bffa90cc0225b39da2e5c2d3b` | Launch-x:contracts/contracts/base/ERC1967Utils.sol:23 |

| Error | Selector | Declared |
|---|---|---|
| `AlreadyInitialized()` | `0x0dc149f0` | Launch-x:contracts/contracts/base/Initializable.sol:18 |
| `DeadlineExpired()` | `0x1ab7da6b` | Launch-x:contracts/contracts/interfaces/IRouter.sol:10 |
| `InsufficientBalance()` | `0xf4d678b8` | Launch-x:contracts/contracts/interfaces/IRouter.sol:12 |
| `InvalidImplementation()` | `0x68155f9a` | Launch-x:contracts/contracts/base/UUPSUpgradeable.sol:19 |
| `InvalidImplementation()` | `0x68155f9a` | Launch-x:contracts/contracts/base/UUPSUpgradeable.sol:19 |
| `InvalidRoleAccount()` | `0xeccda526` | Launch-x:contracts/contracts/base/AccessControl.sol:23 |
| `LastDefaultAdmin()` | `0x9c88180d` | Launch-x:contracts/contracts/base/AccessControl.sol:22 |
| `MissingRole(address,bytes32)` | `0x0161a64a` | Launch-x:contracts/contracts/base/AccessControl.sol:36 |
| `MulticallFailed(uint256,bytes)` | `0x6cf73d14` | Launch-x:contracts/contracts/base/Multicall.sol:8 |
| `NonPayableUpgrade()` | `0x93738d49` | Launch-x:contracts/contracts/base/UUPSUpgradeable.sol:20 |
| `NotInitializing()` | `0xd7e6bcf8` | Launch-x:contracts/contracts/base/Initializable.sol:19 |
| `PoolNotFound()` | `0x76ecffc0` | Launch-x:contracts/contracts/interfaces/IRouter.sol:11 |
| `ReentrancyGuardReentrantCall()` | `0x3ee5aeb5` | Launch-x:contracts/contracts/base/ReentrancyGuard.sol:15 |
| `SameToken()` | `0x201b580a` | Launch-x:contracts/contracts/interfaces/IRouter.sol:13 |
| `TransferFailed()` | `0x90b8ec18` | Launch-x:contracts/contracts/libraries/TransferHelper.sol:8 |
| `TransferFromFailed()` | `0x7939f424` | Launch-x:contracts/contracts/libraries/TransferHelper.sol:9 |
| `UUPSNotThroughActiveProxy()` | `0x45a93fc9` | Launch-x:contracts/contracts/base/UUPSUpgradeable.sol:18 |
| `UUPSNotThroughProxy()` | `0x4f083858` | Launch-x:contracts/contracts/base/UUPSUpgradeable.sol:17 |
| `UnauthorizedUpgrade()` | `0x3a617a54` | Launch-x:contracts/contracts/base/UUPSUpgradeable.sol:16 |
| `UpgradeInitializationFailed()` | `0xcc1cf440` | Launch-x:contracts/contracts/base/UUPSUpgradeable.sol:21 |
| `ZeroAddress()` | `0xd92e233d` | Launch-x:contracts/contracts/interfaces/IRouter.sol:8 |
| `ZeroAmount()` | `0x1f2a2005` | Launch-x:contracts/contracts/interfaces/IRouter.sol:9 |

### Quoter

19 functions, 5 events, 19 errors.

| Function | Selector | Mutability | Returns | Declared |
|---|---|---|---|---|
| `DEFAULT_ADMIN_ROLE()` | `0xa217fddf` | view | `(bytes32)` | Launch-x:contracts/contracts/base/AccessControl.sol:9 |
| `getAllPools(uint256,uint256)` | `0xa7753f09` | view | `(address[])` | Launch-x:contracts/contracts/periphery/Quoter.sol:127 |
| `getMarketCap(address)` | `0xd146d31d` | view | `(uint256)` | Launch-x:contracts/contracts/periphery/Quoter.sol:118 |
| `getPoolPrice(address)` | `0x0a6de2c9` | view | `(uint256)` | Launch-x:contracts/contracts/periphery/Quoter.sol:95 |
| `getPoolStats(address)` | `0x8ae97595` | view | `((uint256,uint256,uint256,uint256,uint256,uint256,uint256,uint256))` | Launch-x:contracts/contracts/periphery/Quoter.sol:100 |
| `getPoolsByCreator(address)` | `0xc84728ef` | view | `(address[])` | Launch-x:contracts/contracts/periphery/Quoter.sol:132 |
| `getRoleAdmin(bytes32)` | `0x248a9ca3` | view | `(bytes32)` | Launch-x:contracts/contracts/base/AccessControl.sol:53 |
| `grantRole(bytes32,address)` | `0x2f2ff15d` | nonpayable | `()` | Launch-x:contracts/contracts/base/AccessControl.sol:58 |
| `hasRole(bytes32,address)` | `0x91d14854` | view | `(bool)` | Launch-x:contracts/contracts/base/AccessControl.sol:48 |
| `initialize(address,address,address)` | `0xc0c53b8b` | nonpayable | `()` | Launch-x:contracts/contracts/periphery/Quoter.sol:31 |
| `poolRegistry()` | `0xafcff50f` | view | `(address)` | Launch-x:contracts/contracts/periphery/Quoter.sol:23 |
| `protocolConfig()` | `0xf5efbb4f` | view | `(address)` | Launch-x:contracts/contracts/periphery/Quoter.sol:24 |
| `proxiableUUID()` | `0x52d1902d` | view | `(bytes32)` | Launch-x:contracts/contracts/base/UUPSUpgradeable.sol:41 |
| `quoteExactInput(address,uint256,bool)` | `0x4d9f71e4` | view | `((uint256,uint256,uint256))` | Launch-x:contracts/contracts/periphery/Quoter.sol:46 |
| `quoteMultihop(address,address,uint256)` | `0x7f1737ce` | view | `((uint256,uint256,uint256,uint256,uint256,uint256,uint256,address,address))` | Launch-x:contracts/contracts/periphery/Quoter.sol:137 |
| `registeredDefaultAdminCount()` | `0xc0cb71b6` | view | `(uint256)` | Launch-x:contracts/contracts/base/AccessControl.sol:32 |
| `renounceRole(bytes32,address)` | `0x36568abe` | nonpayable | `()` | Launch-x:contracts/contracts/base/AccessControl.sol:68 |
| `revokeRole(bytes32,address)` | `0xd547741f` | nonpayable | `()` | Launch-x:contracts/contracts/base/AccessControl.sol:63 |
| `upgradeToAndCall(address,bytes)` | `0x4f1ef286` | payable | `()` | Launch-x:contracts/contracts/base/UUPSUpgradeable.sol:48 |

| Event | Topic 0 | Declared |
|---|---|---|
| `Initialized(uint64)` | `0xc7f505b2f371ae2175ee4913f4499e1f2633a7b5936321eed1cdaeb6115181d2` | Launch-x:contracts/contracts/base/Initializable.sol:21 |
| `RoleAdminChanged(bytes32,bytes32,bytes32)` | `0xbd79b86ffe0ab8e8776151514217cd7cacd52c909f66475c3af44e129f0b00ff` | Launch-x:contracts/contracts/base/AccessControl.sol:40 |
| `RoleGranted(bytes32,address,address)` | `0x2f8788117e7eff1d82e926ec794901d17c78024a50270940304540a733656f0d` | Launch-x:contracts/contracts/base/AccessControl.sol:38 |
| `RoleRevoked(bytes32,address,address)` | `0xf6391f5c32d9c69d2a47ea670b442974b53935d1edc7fd64eb21e047a839171b` | Launch-x:contracts/contracts/base/AccessControl.sol:39 |
| `Upgraded(address)` | `0xbc7cd75a20ee27fd9adebab32041f755214dbc6bffa90cc0225b39da2e5c2d3b` | Launch-x:contracts/contracts/base/ERC1967Utils.sol:23 |

| Error | Selector | Declared |
|---|---|---|
| `AlreadyInitialized()` | `0x0dc149f0` | Launch-x:contracts/contracts/base/Initializable.sol:18 |
| `DivisionByZero()` | `0x23d359a3` | Launch-x:contracts/contracts/libraries/SidioraMath.sol:9 |
| `InsufficientInput()` | `0xf8b3bb61` | Launch-x:contracts/contracts/interfaces/ISidioraPool.sol:11 |
| `InsufficientLiquidity()` | `0xbb55fd27` | Launch-x:contracts/contracts/interfaces/ISidioraPool.sol:10 |
| `InvalidImplementation()` | `0x68155f9a` | Launch-x:contracts/contracts/base/UUPSUpgradeable.sol:19 |
| `InvalidImplementation()` | `0x68155f9a` | Launch-x:contracts/contracts/base/UUPSUpgradeable.sol:19 |
| `InvalidRoleAccount()` | `0xeccda526` | Launch-x:contracts/contracts/base/AccessControl.sol:23 |
| `LastDefaultAdmin()` | `0x9c88180d` | Launch-x:contracts/contracts/base/AccessControl.sol:22 |
| `MissingRole(address,bytes32)` | `0x0161a64a` | Launch-x:contracts/contracts/base/AccessControl.sol:36 |
| `NonPayableUpgrade()` | `0x93738d49` | Launch-x:contracts/contracts/base/UUPSUpgradeable.sol:20 |
| `NotInitializing()` | `0xd7e6bcf8` | Launch-x:contracts/contracts/base/Initializable.sol:19 |
| `OpticalQuoteUnsupported()` | `0x41d4e1d1` | Launch-x:contracts/contracts/periphery/Quoter.sol:21 |
| `Overflow()` | `0x35278d12` | Launch-x:contracts/contracts/libraries/SidioraMath.sol:8 |
| `UUPSNotThroughActiveProxy()` | `0x45a93fc9` | Launch-x:contracts/contracts/base/UUPSUpgradeable.sol:18 |
| `UUPSNotThroughProxy()` | `0x4f083858` | Launch-x:contracts/contracts/base/UUPSUpgradeable.sol:17 |
| `UnauthorizedUpgrade()` | `0x3a617a54` | Launch-x:contracts/contracts/base/UUPSUpgradeable.sol:16 |
| `UnexecutableQuote()` | `0xc1becfdb` | Launch-x:contracts/contracts/periphery/Quoter.sol:20 |
| `UpgradeInitializationFailed()` | `0xcc1cf440` | Launch-x:contracts/contracts/base/UUPSUpgradeable.sol:21 |
| `ZeroAddress()` | `0xd92e233d` | Launch-x:contracts/contracts/periphery/Quoter.sol:19 |

### FeesRouter

20 functions, 12 events, 17 errors.

| Function | Selector | Mutability | Returns | Declared |
|---|---|---|---|---|
| `DEFAULT_ADMIN_ROLE()` | `0xa217fddf` | view | `(bytes32)` | Launch-x:contracts/contracts/base/AccessControl.sol:9 |
| `claimAirdrop(uint256)` | `0xe30d4440` | nonpayable | `(uint256)` | Launch-x:contracts/contracts/periphery/FeesRouter.sol:94 |
| `claimAirdropEpoch(uint256,uint256)` | `0x13e615b9` | nonpayable | `(uint256)` | Launch-x:contracts/contracts/periphery/FeesRouter.sol:102 |
| `claimFees(uint256)` | `0xac68a748` | nonpayable | `(uint256)` | Launch-x:contracts/contracts/periphery/FeesRouter.sol:58 |
| `executeAirdrop(uint256)` | `0x9d4be5b2` | nonpayable | `(uint256)` | Launch-x:contracts/contracts/periphery/FeesRouter.sol:82 |
| `executeBurn(uint256)` | `0x15dec3cf` | nonpayable | `(uint256)` | Launch-x:contracts/contracts/periphery/FeesRouter.sol:70 |
| `executeLpRewards(uint256)` | `0x90d862a1` | nonpayable | `(uint256)` | Launch-x:contracts/contracts/periphery/FeesRouter.sol:109 |
| `feeAccumulator()` | `0x5eaea35f` | view | `(address)` | Launch-x:contracts/contracts/periphery/FeesRouter.sol:21 |
| `getRoleAdmin(bytes32)` | `0x248a9ca3` | view | `(bytes32)` | Launch-x:contracts/contracts/base/AccessControl.sol:53 |
| `grantRole(bytes32,address)` | `0x2f2ff15d` | nonpayable | `()` | Launch-x:contracts/contracts/base/AccessControl.sol:58 |
| `hasRole(bytes32,address)` | `0x91d14854` | view | `(bool)` | Launch-x:contracts/contracts/base/AccessControl.sol:48 |
| `initialize(address,address,address,address)` | `0xf8c8765e` | nonpayable | `()` | Launch-x:contracts/contracts/periphery/FeesRouter.sol:29 |
| `nftContract()` | `0xd56d229d` | view | `(address)` | Launch-x:contracts/contracts/periphery/FeesRouter.sol:20 |
| `poolRegistry()` | `0xafcff50f` | view | `(address)` | Launch-x:contracts/contracts/periphery/FeesRouter.sol:22 |
| `proxiableUUID()` | `0x52d1902d` | view | `(bytes32)` | Launch-x:contracts/contracts/base/UUPSUpgradeable.sol:41 |
| `registeredDefaultAdminCount()` | `0xc0cb71b6` | view | `(uint256)` | Launch-x:contracts/contracts/base/AccessControl.sol:32 |
| `renounceRole(bytes32,address)` | `0x36568abe` | nonpayable | `()` | Launch-x:contracts/contracts/base/AccessControl.sol:68 |
| `revokeRole(bytes32,address)` | `0xd547741f` | nonpayable | `()` | Launch-x:contracts/contracts/base/AccessControl.sol:63 |
| `setFeeStrategy(uint256,uint8)` | `0x9fcf8f62` | nonpayable | `()` | Launch-x:contracts/contracts/periphery/FeesRouter.sol:48 |
| `upgradeToAndCall(address,bytes)` | `0x4f1ef286` | payable | `()` | Launch-x:contracts/contracts/base/UUPSUpgradeable.sol:48 |

| Event | Topic 0 | Declared |
|---|---|---|
| `AirdropClaimed(uint256,address,uint256)` | `0x752d3a5de250652e9111e24fea2886c837dadfe50c8479ad3b5511f4c36f20a2` | Launch-x:contracts/contracts/interfaces/IFeesRouter.sol:17 |
| `AirdropExecuted(uint256,uint256)` | `0x0fa3c377438bb545a4c9ecaa3e574d96bb1b2c6416976df85919d2b83a57bd9f` | Launch-x:contracts/contracts/interfaces/IFeesRouter.sol:16 |
| `FeeStrategyChanged(uint256,uint8,uint8)` | `0x773a9b32cf9f6ecdff8ee5e6f78159f15beb042f9da471ba934182412ea69568` | Launch-x:contracts/contracts/interfaces/IFeesRouter.sol:19 |
| `FeesBurned(uint256,uint256)` | `0x9768e1fe058fe58a8022c0c5a88689f80a64c3a69cc5ba498c518407fc04ced5` | Launch-x:contracts/contracts/interfaces/IFeesRouter.sol:15 |
| `FeesClaimed(uint256,address,uint256)` | `0x2227733fc4c8a9034cb58087dcf6995128b9c0233b038b03366aaf30c92b92d6` | Launch-x:contracts/contracts/interfaces/IFeesRouter.sol:14 |
| `Initialized(uint64)` | `0xc7f505b2f371ae2175ee4913f4499e1f2633a7b5936321eed1cdaeb6115181d2` | Launch-x:contracts/contracts/base/Initializable.sol:21 |
| `LpRewardsExecuted(uint256,uint256)` | `0xf831aaeb2aae759a8843e359900e1b8a3fe4cdefa90fc390825acb66a4374408` | Launch-x:contracts/contracts/interfaces/IFeesRouter.sol:18 |
| `MirrorFailed(bytes4)` | `0x8694b6bbbf10ff6faa1b6ae45172474b03b0df0b4d87ac2113bbced3810eebdd` | Launch-x:contracts/contracts/periphery/FeesRouter.sol:19 |
| `RoleAdminChanged(bytes32,bytes32,bytes32)` | `0xbd79b86ffe0ab8e8776151514217cd7cacd52c909f66475c3af44e129f0b00ff` | Launch-x:contracts/contracts/base/AccessControl.sol:40 |
| `RoleGranted(bytes32,address,address)` | `0x2f8788117e7eff1d82e926ec794901d17c78024a50270940304540a733656f0d` | Launch-x:contracts/contracts/base/AccessControl.sol:38 |
| `RoleRevoked(bytes32,address,address)` | `0xf6391f5c32d9c69d2a47ea670b442974b53935d1edc7fd64eb21e047a839171b` | Launch-x:contracts/contracts/base/AccessControl.sol:39 |
| `Upgraded(address)` | `0xbc7cd75a20ee27fd9adebab32041f755214dbc6bffa90cc0225b39da2e5c2d3b` | Launch-x:contracts/contracts/base/ERC1967Utils.sol:23 |

| Error | Selector | Declared |
|---|---|---|
| `AlreadyInitialized()` | `0x0dc149f0` | Launch-x:contracts/contracts/base/Initializable.sol:18 |
| `InvalidImplementation()` | `0x68155f9a` | Launch-x:contracts/contracts/base/UUPSUpgradeable.sol:19 |
| `InvalidImplementation()` | `0x68155f9a` | Launch-x:contracts/contracts/base/UUPSUpgradeable.sol:19 |
| `InvalidRoleAccount()` | `0xeccda526` | Launch-x:contracts/contracts/base/AccessControl.sol:23 |
| `LastDefaultAdmin()` | `0x9c88180d` | Launch-x:contracts/contracts/base/AccessControl.sol:22 |
| `MissingRole(address,bytes32)` | `0x0161a64a` | Launch-x:contracts/contracts/base/AccessControl.sol:36 |
| `NonPayableUpgrade()` | `0x93738d49` | Launch-x:contracts/contracts/base/UUPSUpgradeable.sol:20 |
| `NotInitializing()` | `0xd7e6bcf8` | Launch-x:contracts/contracts/base/Initializable.sol:19 |
| `NotNftOwner()` | `0x4b64ae47` | Launch-x:contracts/contracts/interfaces/IFeesRouter.sol:9 |
| `ReentrancyGuardReentrantCall()` | `0x3ee5aeb5` | Launch-x:contracts/contracts/base/ReentrancyGuard.sol:15 |
| `UUPSNotThroughActiveProxy()` | `0x45a93fc9` | Launch-x:contracts/contracts/base/UUPSUpgradeable.sol:18 |
| `UUPSNotThroughProxy()` | `0x4f083858` | Launch-x:contracts/contracts/base/UUPSUpgradeable.sol:17 |
| `UnauthorizedUpgrade()` | `0x3a617a54` | Launch-x:contracts/contracts/base/UUPSUpgradeable.sol:16 |
| `UnsupportedSnapshotToken()` | `0xe3aa7e40` | Launch-x:contracts/contracts/interfaces/IFeesRouter.sol:7 |
| `UpgradeInitializationFailed()` | `0xcc1cf440` | Launch-x:contracts/contracts/base/UUPSUpgradeable.sol:21 |
| `WrongStrategy()` | `0xe181295c` | Launch-x:contracts/contracts/interfaces/IFeesRouter.sol:10 |
| `ZeroAddress()` | `0xd92e233d` | Launch-x:contracts/contracts/interfaces/IFeesRouter.sol:11 |

### FeeAccumulator

54 functions, 17 events, 24 errors.

| Function | Selector | Mutability | Returns | Declared |
|---|---|---|---|---|
| `DEAD_ADDRESS()` | `0x4e6fd6c4` | view | `(address)` | Launch-x:contracts/contracts/data/FeeAccumulator.sol:44 |
| `DEFAULT_ADMIN_ROLE()` | `0xa217fddf` | view | `(bytes32)` | Launch-x:contracts/contracts/base/AccessControl.sol:9 |
| `FACTORY_ROLE()` | `0x04a0fb17` | view | `(bytes32)` | Launch-x:contracts/contracts/data/FeeAccumulator.sol:35 |
| `FEES_ROUTER_ROLE()` | `0xdfb76d70` | view | `(bytes32)` | Launch-x:contracts/contracts/data/FeeAccumulator.sol:34 |
| `OPTICAL_CLAIM_ROLE()` | `0x39df917a` | view | `(bytes32)` | Launch-x:contracts/contracts/data/FeeAccumulator.sol:36 |
| `OPTICAL_GRANTER_ROLE()` | `0x85f3d987` | view | `(bytes32)` | Launch-x:contracts/contracts/data/FeeAccumulator.sol:37 |
| `POOL_ROLE()` | `0x404ccd07` | view | `(bytes32)` | Launch-x:contracts/contracts/data/FeeAccumulator.sol:33 |
| `accountingInitialized()` | `0x2a2593e7` | view | `(bool)` | Launch-x:contracts/contracts/data/FeeAccumulator.sol:74 |
| `accountingMigrationCursor()` | `0xad37f674` | view | `(uint256)` | Launch-x:contracts/contracts/data/FeeAccumulator.sol:75 |
| `airdropClaimedSupply(address,uint256)` | `0x9deb3b11` | view | `(uint256)` | Launch-x:contracts/contracts/data/FeeAccumulator.sol:72 |
| `airdropEligibleSupply(address,uint256)` | `0xd71c7375` | view | `(uint256)` | Launch-x:contracts/contracts/data/FeeAccumulator.sol:70 |
| `airdropEpochRemaining(address,uint256)` | `0x81de2866` | view | `(uint256)` | Launch-x:contracts/contracts/data/FeeAccumulator.sol:71 |
| `airdropSnapshotBlock(address,uint256)` | `0xae1e143b` | view | `(uint256)` | Launch-x:contracts/contracts/data/FeeAccumulator.sol:69 |
| `allocateLegacyAirdrop(address,uint256,bytes32,uint256)` | `0xb01b2210` | nonpayable | `()` | Launch-x:contracts/contracts/data/FeeAccumulator.sol:436 |
| `authorizeOptical(address)` | `0x610edef8` | nonpayable | `()` | Launch-x:contracts/contracts/data/FeeAccumulator.sol:357 |
| `authorizePool(address)` | `0x3059b7c6` | nonpayable | `()` | Launch-x:contracts/contracts/data/FeeAccumulator.sol:40 |
| `burn(address)` | `0x89afcb44` | nonpayable | `(uint256)` | Launch-x:contracts/contracts/data/FeeAccumulator.sol:187 |
| `claim(address,address)` | `0x21c0b342` | nonpayable | `(uint256)` | Launch-x:contracts/contracts/data/FeeAccumulator.sol:169 |
| `claimAirdrop(address,address)` | `0x66e1b755` | nonpayable | `(uint256)` | Launch-x:contracts/contracts/data/FeeAccumulator.sol:233 |
| `claimAirdropEpoch(address,address,uint256)` | `0xd195ba7e` | nonpayable | `(uint256)` | Launch-x:contracts/contracts/data/FeeAccumulator.sol:241 |
| `claimLegacyAirdrop(address,uint256,address,uint256,bytes32[])` | `0x05fb334e` | nonpayable | `()` | Launch-x:contracts/contracts/data/FeeAccumulator.sol:448 |
| `claimOpticalSurplus(address,uint256,address)` | `0x613022fd` | nonpayable | `()` | Launch-x:contracts/contracts/data/FeeAccumulator.sol:376 |
| `eventEmitter()` | `0x9ff78c30` | view | `(address)` | Launch-x:contracts/contracts/data/FeeAccumulator.sol:49 |
| `getAccumulatedFees(address)` | `0x564201f9` | view | `(uint256)` | Launch-x:contracts/contracts/data/FeeAccumulator.sol:291 |
| `getAirdropBalance(address)` | `0xefa94d49` | view | `(uint256)` | Launch-x:contracts/contracts/data/FeeAccumulator.sol:301 |
| `getAirdropEpoch(address)` | `0x9eac5b39` | view | `(uint256)` | Launch-x:contracts/contracts/data/FeeAccumulator.sol:325 |
| `getLpRewardsBalance(address)` | `0x81fa4ef7` | view | `(uint256)` | Launch-x:contracts/contracts/data/FeeAccumulator.sol:306 |
| `getOpticalSurplus(address)` | `0xfc7ea90c` | view | `(uint256)` | Launch-x:contracts/contracts/data/FeeAccumulator.sol:396 |
| `getProtocolFeesPending()` | `0x52b2a385` | view | `(uint256)` | Launch-x:contracts/contracts/data/FeeAccumulator.sol:296 |
| `getRoleAdmin(bytes32)` | `0x248a9ca3` | view | `(bytes32)` | Launch-x:contracts/contracts/base/AccessControl.sol:53 |
| `grantRole(bytes32,address)` | `0x2f2ff15d` | nonpayable | `()` | Launch-x:contracts/contracts/base/AccessControl.sol:58 |
| `hasClaimedAirdrop(address,address,uint256)` | `0x2c959e18` | view | `(bool)` | Launch-x:contracts/contracts/data/FeeAccumulator.sol:330 |
| `hasRole(bytes32,address)` | `0x91d14854` | view | `(bool)` | Launch-x:contracts/contracts/base/AccessControl.sol:48 |
| `initialize(address,address,address,address,address,address)` | `0xcc2a9a5b` | nonpayable | `()` | Launch-x:contracts/contracts/data/FeeAccumulator.sol:88 |
| `legacyAirdropRemaining(address,uint256)` | `0xf10dec34` | view | `(uint256)` | Launch-x:contracts/contracts/data/FeeAccumulator.sol:79 |
| `legacyAirdropRoot(address,uint256)` | `0x45f34775` | view | `(bytes32)` | Launch-x:contracts/contracts/data/FeeAccumulator.sol:78 |
| `legacyAirdropUnallocated(address)` | `0x8e09d24b` | view | `(uint256)` | Launch-x:contracts/contracts/data/FeeAccumulator.sol:77 |
| `migrateAccounting(uint256)` | `0x479db4fd` | nonpayable | `()` | Launch-x:contracts/contracts/data/FeeAccumulator.sol:401 |
| `migrateAdditionalAccounting(address[])` | `0xdb6087ea` | nonpayable | `()` | Launch-x:contracts/contracts/data/FeeAccumulator.sol:421 |
| `poolRegistry()` | `0xafcff50f` | view | `(address)` | Launch-x:contracts/contracts/data/FeeAccumulator.sol:48 |
| `protocolConfig()` | `0xf5efbb4f` | view | `(address)` | Launch-x:contracts/contracts/data/FeeAccumulator.sol:46 |
| `proxiableUUID()` | `0x52d1902d` | view | `(bytes32)` | Launch-x:contracts/contracts/base/UUPSUpgradeable.sol:41 |
| `recordFee(address,uint256)` | `0x0a6e0fd4` | nonpayable | `()` | Launch-x:contracts/contracts/data/FeeAccumulator.sol:112 |
| `recordOpticalSurplus(address,uint256)` | `0x9605d79b` | nonpayable | `()` | Launch-x:contracts/contracts/data/FeeAccumulator.sol:364 |
| `registeredDefaultAdminCount()` | `0xc0cb71b6` | view | `(uint256)` | Launch-x:contracts/contracts/base/AccessControl.sol:32 |
| `renounceRole(bytes32,address)` | `0x36568abe` | nonpayable | `()` | Launch-x:contracts/contracts/base/AccessControl.sol:68 |
| `revokeRole(bytes32,address)` | `0xd547741f` | nonpayable | `()` | Launch-x:contracts/contracts/base/AccessControl.sol:63 |
| `sendLpRewards(address)` | `0xa48d7d07` | nonpayable | `(uint256)` | Launch-x:contracts/contracts/data/FeeAccumulator.sol:272 |
| `supportsAirdrop(address)` | `0x8c61de83` | view | `(bool)` | Launch-x:contracts/contracts/data/FeeAccumulator.sol:312 |
| `totalLiabilities()` | `0xf73579a9` | view | `(uint256)` | Launch-x:contracts/contracts/data/FeeAccumulator.sol:68 |
| `treasury()` | `0x61d027b3` | view | `(address)` | Launch-x:contracts/contracts/data/FeeAccumulator.sol:47 |
| `triggerAirdrop(address)` | `0xed374d4a` | nonpayable | `(uint256)` | Launch-x:contracts/contracts/data/FeeAccumulator.sol:203 |
| `upgradeToAndCall(address,bytes)` | `0x4f1ef286` | payable | `()` | Launch-x:contracts/contracts/base/UUPSUpgradeable.sol:48 |
| `usdlAddress()` | `0x2e10f976` | view | `(address)` | Launch-x:contracts/contracts/data/FeeAccumulator.sol:50 |

| Event | Topic 0 | Declared |
|---|---|---|
| `AccountingMigrated(uint256)` | `0x75b17ca7274960709421f611b666d569ceebe0cbb24821254bfe2e5e4fce9df2` | Launch-x:contracts/contracts/data/FeeAccumulator.sol:80 |
| `AirdropClaimed(address,address,uint256,uint256)` | `0xd399c6e7fad358fc300beda3f056717c94a04c7233ce92683de6500ba509022e` | Launch-x:contracts/contracts/interfaces/IFeeAccumulator.sol:43 |
| `AirdropTriggered(address,uint256,uint256)` | `0x8c39ea0d52c00d579184e8070640104228f1a618d77e55429e09cfbb049c2dc1` | Launch-x:contracts/contracts/interfaces/IFeeAccumulator.sol:37 |
| `FeeRecorded(address,uint256,uint256,uint256)` | `0xb4d4d3bd2f97a7d6f1657ee69f7191d7aa7dbd5b6864a2d7a9d14efc1322552f` | Launch-x:contracts/contracts/interfaces/IFeeAccumulator.sol:19 |
| `FeesBurned(address,uint256)` | `0x0d9575a73e2a7da16cfde907df749d23d901528ff2e7c832b731babdecca000b` | Launch-x:contracts/contracts/interfaces/IFeeAccumulator.sol:32 |
| `FeesClaimed(address,address,uint256)` | `0xfe3464cd748424446c37877c28ce5b700222c5bc9f90d908afcc4e5cb22707ff` | Launch-x:contracts/contracts/interfaces/IFeeAccumulator.sol:26 |
| `Initialized(uint64)` | `0xc7f505b2f371ae2175ee4913f4499e1f2633a7b5936321eed1cdaeb6115181d2` | Launch-x:contracts/contracts/base/Initializable.sol:21 |
| `LegacyAirdropAllocated(address,uint256,bytes32,uint256)` | `0x66f288a74d4384624c871c51e16635a78cbc49fc5c471db184fae9b5860ee216` | Launch-x:contracts/contracts/data/FeeAccumulator.sol:81 |
| `LpRewardsSent(address,uint256)` | `0x86b634da567bed9efe3e74597f5b2f7c9a880ed2e05e28f4c7c2fae1f0a9cfdb` | Launch-x:contracts/contracts/interfaces/IFeeAccumulator.sol:50 |
| `MirrorFailed(bytes4)` | `0x8694b6bbbf10ff6faa1b6ae45172474b03b0df0b4d87ac2113bbced3810eebdd` | Launch-x:contracts/contracts/data/FeeAccumulator.sol:32 |
| `OpticalSurplusClaimed(address,address,uint256)` | `0x879606fa2db265e0664b2683c3e39e5ca7c90eae54628f7af6b70f239af8d0a6` | Launch-x:contracts/contracts/interfaces/IFeeAccumulator.sol:64 |
| `OpticalSurplusRecorded(address,uint256)` | `0xc15bf759695c18fd9ddf60f8638036310ac80c3da55f86b717f102c53a44a9e3` | Launch-x:contracts/contracts/interfaces/IFeeAccumulator.sol:59 |
| `ProtocolFeeSwept(uint256)` | `0xe50aaa1965bcc0030c0dedb0b575635e1da17cf4989a55fa0f691ab35ff7ff13` | Launch-x:contracts/contracts/interfaces/IFeeAccumulator.sol:55 |
| `RoleAdminChanged(bytes32,bytes32,bytes32)` | `0xbd79b86ffe0ab8e8776151514217cd7cacd52c909f66475c3af44e129f0b00ff` | Launch-x:contracts/contracts/base/AccessControl.sol:40 |
| `RoleGranted(bytes32,address,address)` | `0x2f8788117e7eff1d82e926ec794901d17c78024a50270940304540a733656f0d` | Launch-x:contracts/contracts/base/AccessControl.sol:38 |
| `RoleRevoked(bytes32,address,address)` | `0xf6391f5c32d9c69d2a47ea670b442974b53935d1edc7fd64eb21e047a839171b` | Launch-x:contracts/contracts/base/AccessControl.sol:39 |
| `Upgraded(address)` | `0xbc7cd75a20ee27fd9adebab32041f755214dbc6bffa90cc0225b39da2e5c2d3b` | Launch-x:contracts/contracts/base/ERC1967Utils.sol:23 |

| Error | Selector | Declared |
|---|---|---|
| `AirdropNotTriggered()` | `0xceadfa6d` | Launch-x:contracts/contracts/interfaces/IFeeAccumulator.sol:14 |
| `AlreadyClaimed()` | `0x646cf558` | Launch-x:contracts/contracts/interfaces/IFeeAccumulator.sol:13 |
| `AlreadyInitialized()` | `0x0dc149f0` | Launch-x:contracts/contracts/base/Initializable.sol:18 |
| `ApproveFailed()` | `0x3e3f8f73` | Launch-x:contracts/contracts/libraries/TransferHelper.sol:10 |
| `InsufficientSurplus()` | `0x94290ab9` | Launch-x:contracts/contracts/interfaces/IFeeAccumulator.sol:15 |
| `InvalidImplementation()` | `0x68155f9a` | Launch-x:contracts/contracts/base/UUPSUpgradeable.sol:19 |
| `InvalidImplementation()` | `0x68155f9a` | Launch-x:contracts/contracts/base/UUPSUpgradeable.sol:19 |
| `InvalidRoleAccount()` | `0xeccda526` | Launch-x:contracts/contracts/base/AccessControl.sol:23 |
| `LastDefaultAdmin()` | `0x9c88180d` | Launch-x:contracts/contracts/base/AccessControl.sol:22 |
| `MissingRole(address,bytes32)` | `0x0161a64a` | Launch-x:contracts/contracts/base/AccessControl.sol:36 |
| `NoFeesAccumulated()` | `0xbe6fbbaf` | Launch-x:contracts/contracts/interfaces/IFeeAccumulator.sol:11 |
| `NonPayableUpgrade()` | `0x93738d49` | Launch-x:contracts/contracts/base/UUPSUpgradeable.sol:20 |
| `NotInitializing()` | `0xd7e6bcf8` | Launch-x:contracts/contracts/base/Initializable.sol:19 |
| `ReentrancyGuardReentrantCall()` | `0x3ee5aeb5` | Launch-x:contracts/contracts/base/ReentrancyGuard.sol:15 |
| `TransferFailed()` | `0x90b8ec18` | Launch-x:contracts/contracts/libraries/TransferHelper.sol:8 |
| `UUPSNotThroughActiveProxy()` | `0x45a93fc9` | Launch-x:contracts/contracts/base/UUPSUpgradeable.sol:18 |
| `UUPSNotThroughProxy()` | `0x4f083858` | Launch-x:contracts/contracts/base/UUPSUpgradeable.sol:17 |
| `Unauthorized()` | `0x82b42900` | Launch-x:contracts/contracts/interfaces/IFeeAccumulator.sol:8 |
| `UnauthorizedUpgrade()` | `0x3a617a54` | Launch-x:contracts/contracts/base/UUPSUpgradeable.sol:16 |
| `UnsupportedSnapshotToken()` | `0xe3aa7e40` | Launch-x:contracts/contracts/interfaces/IFeeAccumulator.sol:16 |
| `UpgradeInitializationFailed()` | `0xcc1cf440` | Launch-x:contracts/contracts/base/UUPSUpgradeable.sol:21 |
| `WrongStrategy()` | `0xe181295c` | Launch-x:contracts/contracts/interfaces/IFeeAccumulator.sol:12 |
| `ZeroAddress()` | `0xd92e233d` | Launch-x:contracts/contracts/interfaces/IFeeAccumulator.sol:9 |
| `ZeroAmount()` | `0x1f2a2005` | Launch-x:contracts/contracts/interfaces/IFeeAccumulator.sol:10 |

### PoolRegistry

22 functions, 7 events, 17 errors.

| Function | Selector | Mutability | Returns | Declared |
|---|---|---|---|---|
| `DEFAULT_ADMIN_ROLE()` | `0xa217fddf` | view | `(bytes32)` | Launch-x:contracts/contracts/base/AccessControl.sol:9 |
| `FACTORY_ROLE()` | `0x04a0fb17` | view | `(bytes32)` | Launch-x:contracts/contracts/data/PoolRegistry.sol:15 |
| `eventEmitter()` | `0x9ff78c30` | view | `(address)` | Launch-x:contracts/contracts/data/PoolRegistry.sol:25 |
| `getAllPools(uint256,uint256)` | `0xa7753f09` | view | `(address[])` | Launch-x:contracts/contracts/data/PoolRegistry.sol:94 |
| `getNftIdByPool(address)` | `0xd56ed6c4` | view | `(uint256)` | Launch-x:contracts/contracts/data/PoolRegistry.sol:84 |
| `getPoolByToken(address)` | `0xbbadc03b` | view | `(address)` | Launch-x:contracts/contracts/data/PoolRegistry.sol:74 |
| `getPoolCount()` | `0x8eec5d70` | view | `(uint256)` | Launch-x:contracts/contracts/data/PoolRegistry.sol:111 |
| `getPoolCountByCreator(address)` | `0x4598ed01` | view | `(uint256)` | Launch-x:contracts/contracts/data/PoolRegistry.sol:120 |
| `getPoolMetadata(address)` | `0x51de9d26` | view | `((address,address,address,uint256,uint256,uint256))` | Launch-x:contracts/contracts/data/PoolRegistry.sol:89 |
| `getPoolsByCreator(address)` | `0xc84728ef` | view | `(address[])` | Launch-x:contracts/contracts/data/PoolRegistry.sol:79 |
| `getPoolsByCreatorPage(address,uint256,uint256)` | `0xf27495af` | view | `(address[])` | Launch-x:contracts/contracts/data/PoolRegistry.sol:124 |
| `getRoleAdmin(bytes32)` | `0x248a9ca3` | view | `(bytes32)` | Launch-x:contracts/contracts/base/AccessControl.sol:53 |
| `grantRole(bytes32,address)` | `0x2f2ff15d` | nonpayable | `()` | Launch-x:contracts/contracts/base/AccessControl.sol:58 |
| `hasRole(bytes32,address)` | `0x91d14854` | view | `(bool)` | Launch-x:contracts/contracts/base/AccessControl.sol:48 |
| `initialize(address,address)` | `0x485cc955` | nonpayable | `()` | Launch-x:contracts/contracts/data/PoolRegistry.sol:32 |
| `isRegisteredPool(address)` | `0xc699e633` | view | `(bool)` | Launch-x:contracts/contracts/data/PoolRegistry.sol:116 |
| `proxiableUUID()` | `0x52d1902d` | view | `(bytes32)` | Launch-x:contracts/contracts/base/UUPSUpgradeable.sol:41 |
| `register(address,address,address,address,uint256)` | `0x43a6d1c3` | nonpayable | `()` | Launch-x:contracts/contracts/data/PoolRegistry.sol:39 |
| `registeredDefaultAdminCount()` | `0xc0cb71b6` | view | `(uint256)` | Launch-x:contracts/contracts/base/AccessControl.sol:32 |
| `renounceRole(bytes32,address)` | `0x36568abe` | nonpayable | `()` | Launch-x:contracts/contracts/base/AccessControl.sol:68 |
| `revokeRole(bytes32,address)` | `0xd547741f` | nonpayable | `()` | Launch-x:contracts/contracts/base/AccessControl.sol:63 |
| `upgradeToAndCall(address,bytes)` | `0x4f1ef286` | payable | `()` | Launch-x:contracts/contracts/base/UUPSUpgradeable.sol:48 |

| Event | Topic 0 | Declared |
|---|---|---|
| `Initialized(uint64)` | `0xc7f505b2f371ae2175ee4913f4499e1f2633a7b5936321eed1cdaeb6115181d2` | Launch-x:contracts/contracts/base/Initializable.sol:21 |
| `MirrorFailed(bytes4)` | `0x8694b6bbbf10ff6faa1b6ae45172474b03b0df0b4d87ac2113bbced3810eebdd` | Launch-x:contracts/contracts/data/PoolRegistry.sol:14 |
| `PoolRegistered(address,address,address,address,uint256,uint256)` | `0xdd85b374a142757b7745e1d5957bddced32d0fae0d5a038c41e7f79dc162b5ce` | Launch-x:contracts/contracts/interfaces/IPoolRegistry.sol:25 |
| `RoleAdminChanged(bytes32,bytes32,bytes32)` | `0xbd79b86ffe0ab8e8776151514217cd7cacd52c909f66475c3af44e129f0b00ff` | Launch-x:contracts/contracts/base/AccessControl.sol:40 |
| `RoleGranted(bytes32,address,address)` | `0x2f8788117e7eff1d82e926ec794901d17c78024a50270940304540a733656f0d` | Launch-x:contracts/contracts/base/AccessControl.sol:38 |
| `RoleRevoked(bytes32,address,address)` | `0xf6391f5c32d9c69d2a47ea670b442974b53935d1edc7fd64eb21e047a839171b` | Launch-x:contracts/contracts/base/AccessControl.sol:39 |
| `Upgraded(address)` | `0xbc7cd75a20ee27fd9adebab32041f755214dbc6bffa90cc0225b39da2e5c2d3b` | Launch-x:contracts/contracts/base/ERC1967Utils.sol:23 |

| Error | Selector | Declared |
|---|---|---|
| `AlreadyInitialized()` | `0x0dc149f0` | Launch-x:contracts/contracts/base/Initializable.sol:18 |
| `DuplicatePool()` | `0xb82a4746` | Launch-x:contracts/contracts/interfaces/IPoolRegistry.sol:11 |
| `DuplicateToken()` | `0x464e3f6a` | Launch-x:contracts/contracts/interfaces/IPoolRegistry.sol:10 |
| `InvalidImplementation()` | `0x68155f9a` | Launch-x:contracts/contracts/base/UUPSUpgradeable.sol:19 |
| `InvalidImplementation()` | `0x68155f9a` | Launch-x:contracts/contracts/base/UUPSUpgradeable.sol:19 |
| `InvalidRoleAccount()` | `0xeccda526` | Launch-x:contracts/contracts/base/AccessControl.sol:23 |
| `LastDefaultAdmin()` | `0x9c88180d` | Launch-x:contracts/contracts/base/AccessControl.sol:22 |
| `MissingRole(address,bytes32)` | `0x0161a64a` | Launch-x:contracts/contracts/base/AccessControl.sol:36 |
| `NonPayableUpgrade()` | `0x93738d49` | Launch-x:contracts/contracts/base/UUPSUpgradeable.sol:20 |
| `NotInitializing()` | `0xd7e6bcf8` | Launch-x:contracts/contracts/base/Initializable.sol:19 |
| `PoolNotFound()` | `0x76ecffc0` | Launch-x:contracts/contracts/interfaces/IPoolRegistry.sol:12 |
| `UUPSNotThroughActiveProxy()` | `0x45a93fc9` | Launch-x:contracts/contracts/base/UUPSUpgradeable.sol:18 |
| `UUPSNotThroughProxy()` | `0x4f083858` | Launch-x:contracts/contracts/base/UUPSUpgradeable.sol:17 |
| `Unauthorized()` | `0x82b42900` | Launch-x:contracts/contracts/interfaces/IPoolRegistry.sol:8 |
| `UnauthorizedUpgrade()` | `0x3a617a54` | Launch-x:contracts/contracts/base/UUPSUpgradeable.sol:16 |
| `UpgradeInitializationFailed()` | `0xcc1cf440` | Launch-x:contracts/contracts/base/UUPSUpgradeable.sol:21 |
| `ZeroAddress()` | `0xd92e233d` | Launch-x:contracts/contracts/interfaces/IPoolRegistry.sol:9 |

### ProtocolConfig

27 functions, 6 events, 14 errors.

| Function | Selector | Mutability | Returns | Declared |
|---|---|---|---|---|
| `DEFAULT_ADMIN_ROLE()` | `0xa217fddf` | view | `(bytes32)` | Launch-x:contracts/contracts/base/AccessControl.sol:9 |
| `baseFeeBps()` | `0xbf5a5940` | view | `(uint256)` | Launch-x:contracts/contracts/protocol/ProtocolConfig.sol:19 |
| `concentrationWeight()` | `0x2006e14c` | view | `(uint256)` | Launch-x:contracts/contracts/protocol/ProtocolConfig.sol:23 |
| `creationFee()` | `0xdce0b4e4` | view | `(uint256)` | Launch-x:contracts/contracts/protocol/ProtocolConfig.sol:24 |
| `eventEmitter()` | `0x9ff78c30` | view | `(address)` | Launch-x:contracts/contracts/protocol/ProtocolConfig.sol:26 |
| `feeDecayRate()` | `0x155d94c4` | view | `(uint256)` | Launch-x:contracts/contracts/protocol/ProtocolConfig.sol:21 |
| `getRoleAdmin(bytes32)` | `0x248a9ca3` | view | `(bytes32)` | Launch-x:contracts/contracts/base/AccessControl.sol:53 |
| `grantRole(bytes32,address)` | `0x2f2ff15d` | nonpayable | `()` | Launch-x:contracts/contracts/base/AccessControl.sol:58 |
| `hasRole(bytes32,address)` | `0x91d14854` | view | `(bool)` | Launch-x:contracts/contracts/base/AccessControl.sol:48 |
| `initialize(address,address,address)` | `0xc0c53b8b` | nonpayable | `()` | Launch-x:contracts/contracts/protocol/ProtocolConfig.sol:33 |
| `maxFeeBps()` | `0xbf769a3f` | view | `(uint256)` | Launch-x:contracts/contracts/protocol/ProtocolConfig.sol:18 |
| `minFeeBps()` | `0x071de20d` | view | `(uint256)` | Launch-x:contracts/contracts/protocol/ProtocolConfig.sol:17 |
| `protocolFeeBps()` | `0x35659fb8` | view | `(uint256)` | Launch-x:contracts/contracts/protocol/ProtocolConfig.sol:20 |
| `proxiableUUID()` | `0x52d1902d` | view | `(bytes32)` | Launch-x:contracts/contracts/base/UUPSUpgradeable.sol:41 |
| `registeredDefaultAdminCount()` | `0xc0cb71b6` | view | `(uint256)` | Launch-x:contracts/contracts/base/AccessControl.sol:32 |
| `renounceRole(bytes32,address)` | `0x36568abe` | nonpayable | `()` | Launch-x:contracts/contracts/base/AccessControl.sol:68 |
| `revokeRole(bytes32,address)` | `0xd547741f` | nonpayable | `()` | Launch-x:contracts/contracts/base/AccessControl.sol:63 |
| `setBaseFeeBps(uint256)` | `0xa389301a` | nonpayable | `()` | Launch-x:contracts/contracts/protocol/ProtocolConfig.sol:59 |
| `setCreationFee(uint256)` | `0xb7d86225` | nonpayable | `()` | Launch-x:contracts/contracts/protocol/ProtocolConfig.sol:73 |
| `setFeeWeights(uint256,uint256,uint256)` | `0xf1726fe7` | nonpayable | `()` | Launch-x:contracts/contracts/protocol/ProtocolConfig.sol:79 |
| `setProtocolFeeBps(uint256)` | `0xc0417e58` | nonpayable | `()` | Launch-x:contracts/contracts/protocol/ProtocolConfig.sol:66 |
| `setVirtualDefaults(uint256,uint256)` | `0xa83f1b4a` | nonpayable | `()` | Launch-x:contracts/contracts/protocol/ProtocolConfig.sol:96 |
| `upgradeToAndCall(address,bytes)` | `0x4f1ef286` | payable | `()` | Launch-x:contracts/contracts/base/UUPSUpgradeable.sol:48 |
| `usdlAddress()` | `0x2e10f976` | view | `(address)` | Launch-x:contracts/contracts/protocol/ProtocolConfig.sol:14 |
| `virtualTokenDefault()` | `0xd9fce324` | view | `(uint256)` | Launch-x:contracts/contracts/protocol/ProtocolConfig.sol:16 |
| `virtualUsdlDefault()` | `0x071ed6b5` | view | `(uint256)` | Launch-x:contracts/contracts/protocol/ProtocolConfig.sol:15 |
| `volatilityWeight()` | `0x294ae7f9` | view | `(uint256)` | Launch-x:contracts/contracts/protocol/ProtocolConfig.sol:22 |

| Event | Topic 0 | Declared |
|---|---|---|
| `ConfigUpdated(bytes32,uint256,uint256)` | `0xac2ccce3de9c0816ae772598f7f65fe69f9893b637f7c490497378cbb3ea043e` | Launch-x:contracts/contracts/interfaces/IProtocolConfig.sol:12 |
| `Initialized(uint64)` | `0xc7f505b2f371ae2175ee4913f4499e1f2633a7b5936321eed1cdaeb6115181d2` | Launch-x:contracts/contracts/base/Initializable.sol:21 |
| `RoleAdminChanged(bytes32,bytes32,bytes32)` | `0xbd79b86ffe0ab8e8776151514217cd7cacd52c909f66475c3af44e129f0b00ff` | Launch-x:contracts/contracts/base/AccessControl.sol:40 |
| `RoleGranted(bytes32,address,address)` | `0x2f8788117e7eff1d82e926ec794901d17c78024a50270940304540a733656f0d` | Launch-x:contracts/contracts/base/AccessControl.sol:38 |
| `RoleRevoked(bytes32,address,address)` | `0xf6391f5c32d9c69d2a47ea670b442974b53935d1edc7fd64eb21e047a839171b` | Launch-x:contracts/contracts/base/AccessControl.sol:39 |
| `Upgraded(address)` | `0xbc7cd75a20ee27fd9adebab32041f755214dbc6bffa90cc0225b39da2e5c2d3b` | Launch-x:contracts/contracts/base/ERC1967Utils.sol:23 |

| Error | Selector | Declared |
|---|---|---|
| `AlreadyInitialized()` | `0x0dc149f0` | Launch-x:contracts/contracts/base/Initializable.sol:18 |
| `FeeOutOfRange()` | `0x0c8249ad` | Launch-x:contracts/contracts/interfaces/IProtocolConfig.sol:8 |
| `InvalidImplementation()` | `0x68155f9a` | Launch-x:contracts/contracts/base/UUPSUpgradeable.sol:19 |
| `InvalidImplementation()` | `0x68155f9a` | Launch-x:contracts/contracts/base/UUPSUpgradeable.sol:19 |
| `InvalidRoleAccount()` | `0xeccda526` | Launch-x:contracts/contracts/base/AccessControl.sol:23 |
| `LastDefaultAdmin()` | `0x9c88180d` | Launch-x:contracts/contracts/base/AccessControl.sol:22 |
| `MissingRole(address,bytes32)` | `0x0161a64a` | Launch-x:contracts/contracts/base/AccessControl.sol:36 |
| `NonPayableUpgrade()` | `0x93738d49` | Launch-x:contracts/contracts/base/UUPSUpgradeable.sol:20 |
| `NotInitializing()` | `0xd7e6bcf8` | Launch-x:contracts/contracts/base/Initializable.sol:19 |
| `UUPSNotThroughActiveProxy()` | `0x45a93fc9` | Launch-x:contracts/contracts/base/UUPSUpgradeable.sol:18 |
| `UUPSNotThroughProxy()` | `0x4f083858` | Launch-x:contracts/contracts/base/UUPSUpgradeable.sol:17 |
| `UnauthorizedUpgrade()` | `0x3a617a54` | Launch-x:contracts/contracts/base/UUPSUpgradeable.sol:16 |
| `UpgradeInitializationFailed()` | `0xcc1cf440` | Launch-x:contracts/contracts/base/UUPSUpgradeable.sol:21 |
| `ZeroAddress()` | `0xd92e233d` | Launch-x:contracts/contracts/interfaces/IProtocolConfig.sol:9 |

### EventEmitter

63 functions, 39 events, 13 errors.

| Function | Selector | Mutability | Returns | Declared |
|---|---|---|---|---|
| `DEFAULT_ADMIN_ROLE()` | `0xa217fddf` | view | `(bytes32)` | Launch-x:contracts/contracts/base/AccessControl.sol:9 |
| `EMITTER_ADMIN_ROLE()` | `0xc2d11c38` | view | `(bytes32)` | Launch-x:contracts/contracts/data/EventEmitter.sol:54 |
| `EVENT_EMITTER_ROLE()` | `0xd7056c20` | view | `(bytes32)` | Launch-x:contracts/contracts/data/EventEmitter.sol:59 |
| `VERSION()` | `0xffa1ad74` | pure | `(string)` | Launch-x:contracts/contracts/data/EventEmitter.sol:341 |
| `deregisterToken(address)` | `0xbab2af1d` | nonpayable | `()` | Launch-x:contracts/contracts/data/EventEmitter.sol:326 |
| `emitAssetApproval(address,address,address,uint256,bool)` | `0x4f2bdd09` | nonpayable | `()` | Launch-x:contracts/contracts/data/EventEmitter.sol:710 |
| `emitBestRouteSwap(address,address,address,bytes32,uint256,uint256,address[],uint256)` | `0xb3defa3c` | nonpayable | `()` | Launch-x:contracts/contracts/data/EventEmitter.sol:555 |
| `emitCircuitBreaker(address,bytes32,uint256,uint256,uint256)` | `0xd6278d9c` | nonpayable | `()` | Launch-x:contracts/contracts/data/EventEmitter.sol:592 |
| `emitConfigUpdated(bytes32,uint256,uint256)` | `0x85ceceb5` | nonpayable | `()` | Launch-x:contracts/contracts/data/EventEmitter.sol:431 |
| `emitEventLog(string,(((string,address)[],(string,address[])[]),((string,uint256)[],(string,uint256[])[]),((string,int256)[],(string,int256[])[]),((string,bool)[],(string,bool[])[]),((string,bytes32)[],(string,bytes32[])[]),((string,bytes)[],(string,bytes[])[]),((string,string)[],(string,string[])[])))` | `0x906c49f6` | nonpayable | `()` | Launch-x:contracts/contracts/data/EventEmitter.sol:444 |
| `emitEventLog1(string,bytes32,(((string,address)[],(string,address[])[]),((string,uint256)[],(string,uint256[])[]),((string,int256)[],(string,int256[])[]),((string,bool)[],(string,bool[])[]),((string,bytes32)[],(string,bytes32[])[]),((string,bytes)[],(string,bytes[])[]),((string,string)[],(string,string[])[])))` | `0x24de01e4` | nonpayable | `()` | Launch-x:contracts/contracts/data/EventEmitter.sol:458 |
| `emitEventLog2(string,bytes32,bytes32,(((string,address)[],(string,address[])[]),((string,uint256)[],(string,uint256[])[]),((string,int256)[],(string,int256[])[]),((string,bool)[],(string,bool[])[]),((string,bytes32)[],(string,bytes32[])[]),((string,bytes)[],(string,bytes[])[]),((string,string)[],(string,string[])[])))` | `0x63d16363` | nonpayable | `()` | Launch-x:contracts/contracts/data/EventEmitter.sol:474 |
| `emitFeeDistributed(bytes32,uint256,uint8,uint256,address)` | `0x06a7cedb` | nonpayable | `()` | Launch-x:contracts/contracts/data/EventEmitter.sol:388 |
| `emitFeeFlow(uint8,address,address,uint256,uint256,uint256,uint256)` | `0xfa2e1d5e` | nonpayable | `()` | Launch-x:contracts/contracts/data/EventEmitter.sol:766 |
| `emitFeeRecorded(bytes32,uint256,uint256,uint256)` | `0xd6d28200` | nonpayable | `()` | Launch-x:contracts/contracts/data/EventEmitter.sol:378 |
| `emitFeeStrategyChanged(bytes32,uint256,uint8,uint8)` | `0xb2c8dc72` | nonpayable | `()` | Launch-x:contracts/contracts/data/EventEmitter.sol:399 |
| `emitGovernance(uint8,bytes32,address,bytes)` | `0x98616097` | nonpayable | `()` | Launch-x:contracts/contracts/data/EventEmitter.sol:641 |
| `emitMarketCreated(bytes32,address,address,address,address)` | `0xe4617ca5` | nonpayable | `()` | Launch-x:contracts/contracts/data/EventEmitter.sol:354 |
| `emitNftMint(uint256,address,address,uint8)` | `0x75c48038` | nonpayable | `()` | Launch-x:contracts/contracts/data/EventEmitter.sol:800 |
| `emitNftTransfer(address,address,address,uint256)` | `0xeeaf8bcd` | nonpayable | `()` | Launch-x:contracts/contracts/data/EventEmitter.sol:700 |
| `emitOpticalExecuted(bytes32,address,string,bytes)` | `0xd4756325` | nonpayable | `()` | Launch-x:contracts/contracts/data/EventEmitter.sol:420 |
| `emitOpticalLifecycle(uint8,address,address,bytes32,bytes)` | `0x82b678c3` | nonpayable | `()` | Launch-x:contracts/contracts/data/EventEmitter.sol:672 |
| `emitOracleAdapterLifecycle(bytes32,address,uint8,uint256)` | `0x176af80c` | nonpayable | `()` | Launch-x:contracts/contracts/data/EventEmitter.sol:606 |
| `emitPauseToggle(address,bool)` | `0xd4925be5` | nonpayable | `()` | Launch-x:contracts/contracts/data/EventEmitter.sol:754 |
| `emitPecorOrderCreated(uint256,address,uint8,uint8,address,address,uint256,uint256,uint256,uint256)` | `0x7944994b` | nonpayable | `()` | Launch-x:contracts/contracts/data/EventEmitter.sol:519 |
| `emitPecorOrderLifecycle(uint256,address,uint8,uint8,uint256,bytes)` | `0x1a7a34ef` | nonpayable | `()` | Launch-x:contracts/contracts/data/EventEmitter.sol:540 |
| `emitPecorSwap(address,address,address,uint256,uint256,uint256,uint256,uint256,uint256,uint256,uint256,uint8)` | `0xa6df9eea` | nonpayable | `()` | Launch-x:contracts/contracts/data/EventEmitter.sol:496 |
| `emitPoolRegistered(address,address,address,address,uint256)` | `0x25b14c33` | nonpayable | `()` | Launch-x:contracts/contracts/data/EventEmitter.sol:813 |
| `emitPoolStateUpdated(bytes32,uint256,uint256,uint256,uint256)` | `0xfa78f4ca` | nonpayable | `()` | Launch-x:contracts/contracts/data/EventEmitter.sol:409 |
| `emitPriceUpdated(address,uint256,address,uint256,uint256,bytes32)` | `0xd08aa55f` | nonpayable | `()` | Launch-x:contracts/contracts/data/EventEmitter.sol:577 |
| `emitRoleChange(uint8,bytes32,address,address,bytes32)` | `0xcdc0fc61` | nonpayable | `()` | Launch-x:contracts/contracts/data/EventEmitter.sol:728 |
| `emitRouterTrade(uint8,address,address,address,address,uint256,uint256,uint256)` | `0xe3cdc125` | nonpayable | `()` | Launch-x:contracts/contracts/data/EventEmitter.sol:782 |
| `emitSwap(bytes32,address,bool,uint256,uint256,uint256,uint256)` | `0x100fca4f` | nonpayable | `()` | Launch-x:contracts/contracts/data/EventEmitter.sol:365 |
| `emitTokenDeployed(address,address,address,bytes32,string,string,uint8,uint256)` | `0x3cced653` | nonpayable | `()` | Launch-x:contracts/contracts/data/EventEmitter.sol:831 |
| `emitTokenTransfer(address,address,address,uint256)` | `0xc6770db6` | nonpayable | `()` | Launch-x:contracts/contracts/data/EventEmitter.sol:690 |
| `emitTreasuryFlow(uint8,address,address,uint256)` | `0x9de5933b` | nonpayable | `()` | Launch-x:contracts/contracts/data/EventEmitter.sol:655 |
| `emitUpgraded(address,address,uint8)` | `0x37114247` | nonpayable | `()` | Launch-x:contracts/contracts/data/EventEmitter.sol:742 |
| `emitVaultFlow(uint8,address,address,uint256,uint256)` | `0x9e4a40ca` | nonpayable | `()` | Launch-x:contracts/contracts/data/EventEmitter.sol:623 |
| `getRoleAdmin(bytes32)` | `0x248a9ca3` | view | `(bytes32)` | Launch-x:contracts/contracts/base/AccessControl.sol:53 |
| `grantRole(bytes32,address)` | `0x2f2ff15d` | nonpayable | `()` | Launch-x:contracts/contracts/base/AccessControl.sol:58 |
| `hasRole(bytes32,address)` | `0x91d14854` | view | `(bool)` | Launch-x:contracts/contracts/base/AccessControl.sol:48 |
| `initialize(address)` | `0xc4d66de8` | nonpayable | `()` | Launch-x:contracts/contracts/data/EventEmitter.sol:106 |
| `isAuthorizedEmitter(address)` | `0x66706fd2` | view | `(bool)` | Launch-x:contracts/contracts/data/EventEmitter.sol:147 |
| `isRegisteredToken(address)` | `0x7639a230` | view | `(bool)` | Launch-x:contracts/contracts/data/EventEmitter.sol:156 |
| `isTypedEmitter(address,bytes4)` | `0x48c3830a` | view | `(bool)` | Launch-x:contracts/contracts/data/EventEmitter.sol:212 |
| `metaAGRouter()` | `0xfa0dced9` | view | `(address)` | Launch-x:contracts/contracts/data/EventEmitter.sol:81 |
| `opticalRegistry()` | `0x73b5708c` | view | `(address)` | Launch-x:contracts/contracts/data/EventEmitter.sol:79 |
| `poolRegistry()` | `0xafcff50f` | view | `(address)` | Launch-x:contracts/contracts/data/EventEmitter.sol:75 |
| `proxiableUUID()` | `0x52d1902d` | view | `(bytes32)` | Launch-x:contracts/contracts/base/UUPSUpgradeable.sol:41 |
| `registerToken(address,address)` | `0x4739f7e5` | nonpayable | `()` | Launch-x:contracts/contracts/data/EventEmitter.sol:313 |
| `registeredDefaultAdminCount()` | `0xc0cb71b6` | view | `(uint256)` | Launch-x:contracts/contracts/base/AccessControl.sol:32 |
| `reinitializeV2(address)` | `0xbe6b99e3` | nonpayable | `()` | Launch-x:contracts/contracts/data/EventEmitter.sol:118 |
| `renounceRole(bytes32,address)` | `0x36568abe` | nonpayable | `()` | Launch-x:contracts/contracts/base/AccessControl.sol:68 |
| `revokeRole(bytes32,address)` | `0xd547741f` | nonpayable | `()` | Launch-x:contracts/contracts/base/AccessControl.sol:63 |
| `setAuthorizedEmitter(address,bool)` | `0x27b81718` | nonpayable | `()` | Launch-x:contracts/contracts/data/EventEmitter.sol:249 |
| `setMetaAGRouter(address)` | `0xf4795b26` | nonpayable | `()` | Launch-x:contracts/contracts/data/EventEmitter.sol:272 |
| `setOpticalRegistry(address)` | `0xde2b7764` | nonpayable | `()` | Launch-x:contracts/contracts/data/EventEmitter.sol:261 |
| `setPoolRegistry(address)` | `0x7b77cd6a` | nonpayable | `()` | Launch-x:contracts/contracts/data/EventEmitter.sol:239 |
| `setSidioraFactory(address)` | `0xe53122ec` | nonpayable | `()` | Launch-x:contracts/contracts/data/EventEmitter.sol:283 |
| `setTokenRegistry(address)` | `0x35a5af92` | nonpayable | `()` | Launch-x:contracts/contracts/data/EventEmitter.sol:298 |
| `setTypedEmitter(address,bytes4,bool)` | `0xb2473503` | nonpayable | `()` | Launch-x:contracts/contracts/data/EventEmitter.sol:204 |
| `sidioraFactory()` | `0xb182ba64` | view | `(address)` | Launch-x:contracts/contracts/data/EventEmitter.sol:83 |
| `upgradeToAndCall(address,bytes)` | `0x4f1ef286` | payable | `()` | Launch-x:contracts/contracts/base/UUPSUpgradeable.sol:48 |

| Event | Topic 0 | Declared |
|---|---|---|
| `AssetApproval(address,address,address,uint256,bool,uint256,uint256)` | `0xa9de58179a7ba626557514c856f27ad734eea61c56c3e9acb937f374ec63afe7` | Launch-x:contracts/contracts/interfaces/IEventEmitter.sol:491 |
| `BestRouteSwap(address,address,address,bytes32,uint256,uint256,address[],uint256,uint256,uint256)` | `0xf5041a4d1e1f497822428a20e208ea0ad2c90eb99518d0d9a1f598f8af04cd72` | Launch-x:contracts/contracts/interfaces/IEventEmitter.sol:275 |
| `CircuitBreakerTriggered(address,bytes32,uint256,uint256,uint256,uint256,uint256)` | `0xb08f12d79c5828345938b295337ebad6aa44441a33df36e9f766b0b7f253889b` | Launch-x:contracts/contracts/interfaces/IEventEmitter.sol:349 |
| `ConfigUpdated(bytes32,uint256,uint256,uint256,uint256)` | `0x2903b3642d2105b7fbd127326de888ab50e8d975bf819398337fb58407c1b10e` | Launch-x:contracts/contracts/interfaces/IEventEmitter.sol:80 |
| `ContractUpgraded(address,address,uint8,uint256,uint256)` | `0xb059389b6a1d06bc1833ca4b78798c2642d02b2d03d0f8fce5f4b8299737d3b7` | Launch-x:contracts/contracts/interfaces/IEventEmitter.sol:535 |
| `EventLog(address,bytes32,string,(((string,address)[],(string,address[])[]),((string,uint256)[],(string,uint256[])[]),((string,int256)[],(string,int256[])[]),((string,bool)[],(string,bool[])[]),((string,bytes32)[],(string,bytes32[])[]),((string,bytes)[],(string,bytes[])[]),((string,string)[],(string,string[])[])),uint256,uint256)` | `0x3f5c3dc69ea622ef6e129f03f630c1484849db8ae3e8f3d7fa164ca7fae08c27` | Launch-x:contracts/contracts/interfaces/IEventEmitter.sol:180 |
| `EventLog1(address,bytes32,bytes32,string,(((string,address)[],(string,address[])[]),((string,uint256)[],(string,uint256[])[]),((string,int256)[],(string,int256[])[]),((string,bool)[],(string,bool[])[]),((string,bytes32)[],(string,bytes32[])[]),((string,bytes)[],(string,bytes[])[]),((string,string)[],(string,string[])[])),uint256,uint256)` | `0xe67d7d5cff44135efdb2aa905b4fcd19c700c05482dd08ccb52a0b02b99f788a` | Launch-x:contracts/contracts/interfaces/IEventEmitter.sol:190 |
| `EventLog2(address,bytes32,bytes32,bytes32,string,(((string,address)[],(string,address[])[]),((string,uint256)[],(string,uint256[])[]),((string,int256)[],(string,int256[])[]),((string,bool)[],(string,bool[])[]),((string,bytes32)[],(string,bytes32[])[]),((string,bytes)[],(string,bytes[])[]),((string,string)[],(string,string[])[])),uint256,uint256)` | `0xfb0f08ac39934a2d9d9120c47f7079c97667c3a5495f5a6733b46b2c87cd2502` | Launch-x:contracts/contracts/interfaces/IEventEmitter.sol:201 |
| `FeeDistributed(bytes32,uint256,uint8,uint256,address,uint256,uint256)` | `0xf7ab826cedf9d005102eddf95e65f43597aef36a55ff09ee4f452204c83f4afc` | Launch-x:contracts/contracts/interfaces/IEventEmitter.sol:42 |
| `FeeFlow(uint8,address,address,uint256,uint256,uint256,uint256,uint256,uint256)` | `0x626e3fd5d9323ca4b7ffa3ca51c21c02766b638d15eb8896a36a4e0e97ae6de4` | Launch-x:contracts/contracts/interfaces/IEventEmitter.sol:573 |
| `FeeRecorded(bytes32,uint256,uint256,uint256,uint256,uint256)` | `0xe80e9e3e09460d716260f5ce6979a59f6cf4fbc1096e20f008d78b5cac7b7179` | Launch-x:contracts/contracts/interfaces/IEventEmitter.sol:33 |
| `FeeStrategyChanged(bytes32,uint256,uint8,uint8,uint256,uint256)` | `0x337fd9f9304e9da4c04886a5b8bf56e74032b459758212a860bc61d3544353dd` | Launch-x:contracts/contracts/interfaces/IEventEmitter.sol:52 |
| `Governance(uint8,bytes32,address,bytes,uint256,uint256)` | `0x637d9c5f725b51336e8338a6c85951604e8ee7bc041e8e2be8dc739abe9d2173` | Launch-x:contracts/contracts/interfaces/IEventEmitter.sol:414 |
| `Initialized(uint64)` | `0xc7f505b2f371ae2175ee4913f4499e1f2633a7b5936321eed1cdaeb6115181d2` | Launch-x:contracts/contracts/base/Initializable.sol:21 |
| `MarketCreated(bytes32,address,address,address,address,uint256,uint256)` | `0xb0a9c7605389f1127c2299d4a3ed85f0b2fc19894ab5411d76b453bb29f9f47b` | Launch-x:contracts/contracts/interfaces/IEventEmitter.sol:11 |
| `NftMint(uint256,address,address,uint8,uint256,uint256)` | `0xd2917d37031c0dfe5016665c7a83360cbf2ff32063096b2caf61ba66c06832e5` | Launch-x:contracts/contracts/interfaces/IEventEmitter.sol:600 |
| `NftTransfer(address,address,address,uint256,uint256,uint256)` | `0x021f03e8a1315704c74c03e8c022c12bc80c4a2bd4fec8bd36b217322c340ad3` | Launch-x:contracts/contracts/interfaces/IEventEmitter.sol:482 |
| `OpticalExecuted(bytes32,address,string,bytes,uint256,uint256)` | `0xfdf5ccbb26a02ea4efd5415e00b1d7782ca04cbb6b33a0d0a0d549ba284f9e57` | Launch-x:contracts/contracts/interfaces/IEventEmitter.sol:61 |
| `OpticalLifecycle(uint8,address,address,bytes32,bytes,uint256,uint256)` | `0xa3faac07b1221710e43ca60800fc18acd83007ed48055def89c1e69b79ba44cb` | Launch-x:contracts/contracts/interfaces/IEventEmitter.sol:452 |
| `OracleAdapterLifecycle(bytes32,address,uint8,uint256,uint256,uint256)` | `0x9f8c04497ebc19f8f7de480e55fdfa3780212516de2d597e6f793d6629017cec` | Launch-x:contracts/contracts/interfaces/IEventEmitter.sol:359 |
| `PauseToggle(address,bool,uint256,uint256)` | `0xd079c1af4038467292233cbd5af2e60da5c45ea48670f905aff3da18b483973c` | Launch-x:contracts/contracts/interfaces/IEventEmitter.sol:543 |
| `PecorOrderCreated(uint256,address,uint8,uint8,address,address,uint256,uint256,uint256,uint256,uint256,uint256)` | `0xa24904fab3b2e5505845a72e1cd3b798ad4fbd48a7e6cb662de4b0d0d5abb298` | Launch-x:contracts/contracts/interfaces/IEventEmitter.sol:249 |
| `PecorOrderLifecycle(uint256,address,uint8,uint8,uint256,bytes,uint256,uint256)` | `0xdbbb8b49248d8ae078049644637ec9edf159baa1c653f8274ddf3fa26f358d26` | Launch-x:contracts/contracts/interfaces/IEventEmitter.sol:264 |
| `PecorSwap(address,address,address,uint256,uint256,uint256,uint256,uint256,uint256,uint256,uint256,uint8,uint256,uint256)` | `0x9b4f5f387e6b6c21cc8d67c37c41d9d7b8d8b3f16494c84db2dd1fca812a644f` | Launch-x:contracts/contracts/interfaces/IEventEmitter.sol:232 |
| `PoolRegistered(address,address,address,address,uint256,uint256,uint256)` | `0xda5568642b67660133b1de538c867c58f3037a5722b0e231a959ee8ca11b2ed7` | Launch-x:contracts/contracts/interfaces/IEventEmitter.sol:609 |
| `PoolStateUpdated(bytes32,uint256,uint256,uint256,uint256,uint256,uint256)` | `0x8155a4b38a877d212b271811786986f8f7d78074c82fce4526c58defcc6d7ebd` | Launch-x:contracts/contracts/interfaces/IEventEmitter.sol:70 |
| `PriceUpdated(address,uint256,address,uint256,uint256,bytes32,uint256,uint256)` | `0x8d99dbd92dc7116e69aa285daa09f2da514d6b2ca202545c3d9a0385982a7a69` | Launch-x:contracts/contracts/interfaces/IEventEmitter.sol:338 |
| `RoleAdminChanged(bytes32,bytes32,bytes32)` | `0xbd79b86ffe0ab8e8776151514217cd7cacd52c909f66475c3af44e129f0b00ff` | Launch-x:contracts/contracts/base/AccessControl.sol:40 |
| `RoleChange(uint8,bytes32,address,address,bytes32,uint256,uint256)` | `0xdcb006502b86d8ebfada5d7606648e10555d5df4a42fe9a8fcd00999c25faaf4` | Launch-x:contracts/contracts/interfaces/IEventEmitter.sol:525 |
| `RoleGranted(bytes32,address,address)` | `0x2f8788117e7eff1d82e926ec794901d17c78024a50270940304540a733656f0d` | Launch-x:contracts/contracts/base/AccessControl.sol:38 |
| `RoleRevoked(bytes32,address,address)` | `0xf6391f5c32d9c69d2a47ea670b442974b53935d1edc7fd64eb21e047a839171b` | Launch-x:contracts/contracts/base/AccessControl.sol:39 |
| `RouterTrade(uint8,address,address,address,address,uint256,uint256,uint256,uint256,uint256)` | `0x17c1899a3fd431f2b2200b90e738d270bb856a4222040c5650bfa208acb3b650` | Launch-x:contracts/contracts/interfaces/IEventEmitter.sol:587 |
| `Swap(bytes32,address,bool,uint256,uint256,uint256,uint256,uint256,uint256)` | `0xa234ab85728295548823d8a26077ecc77aa5815f9ed8f4c5e6fd390ac851d4cc` | Launch-x:contracts/contracts/interfaces/IEventEmitter.sol:21 |
| `TokenDeployed(address,address,address,bytes32,string,string,uint8,uint256,uint256,uint256)` | `0xad187874b793634c55d977f35d4828526eb48170a9531c71d88f8fd10cf9c237` | Launch-x:contracts/contracts/interfaces/IEventEmitter.sol:619 |
| `TokenTransfer(address,address,address,uint256,uint256,uint256)` | `0xd1630e8dfeca8f80cedf002590087ccd08ffc4a5a1860fc02892098e7aa92de1` | Launch-x:contracts/contracts/interfaces/IEventEmitter.sol:473 |
| `TreasuryFlow(uint8,address,address,uint256,uint256,uint256)` | `0x589609a9576cbe900d4afeecbd83764a5db719055826446f88c74bee62af7f81` | Launch-x:contracts/contracts/interfaces/IEventEmitter.sol:434 |
| `TypedEmitterPermission(address,bytes4,bool)` | `0xac8cad594581b394b23d152f0d6fe73d7d6611afa3458ccc51e4659ff7dbc844` | Launch-x:contracts/contracts/interfaces/IEventEmitter.sol:681 |
| `Upgraded(address)` | `0xbc7cd75a20ee27fd9adebab32041f755214dbc6bffa90cc0225b39da2e5c2d3b` | Launch-x:contracts/contracts/base/ERC1967Utils.sol:23 |
| `VaultFlow(uint8,address,address,uint256,uint256,uint256,uint256)` | `0x4a928ce96ccd98c06c706d2dfc9e3be829d83d7df95db10dfe9d1c359ea54e78` | Launch-x:contracts/contracts/interfaces/IEventEmitter.sol:394 |

| Error | Selector | Declared |
|---|---|---|
| `AlreadyInitialized()` | `0x0dc149f0` | Launch-x:contracts/contracts/base/Initializable.sol:18 |
| `InvalidImplementation()` | `0x68155f9a` | Launch-x:contracts/contracts/base/UUPSUpgradeable.sol:19 |
| `InvalidImplementation()` | `0x68155f9a` | Launch-x:contracts/contracts/base/UUPSUpgradeable.sol:19 |
| `InvalidRoleAccount()` | `0xeccda526` | Launch-x:contracts/contracts/base/AccessControl.sol:23 |
| `LastDefaultAdmin()` | `0x9c88180d` | Launch-x:contracts/contracts/base/AccessControl.sol:22 |
| `MissingRole(address,bytes32)` | `0x0161a64a` | Launch-x:contracts/contracts/base/AccessControl.sol:36 |
| `NonPayableUpgrade()` | `0x93738d49` | Launch-x:contracts/contracts/base/UUPSUpgradeable.sol:20 |
| `NotInitializing()` | `0xd7e6bcf8` | Launch-x:contracts/contracts/base/Initializable.sol:19 |
| `UUPSNotThroughActiveProxy()` | `0x45a93fc9` | Launch-x:contracts/contracts/base/UUPSUpgradeable.sol:18 |
| `UUPSNotThroughProxy()` | `0x4f083858` | Launch-x:contracts/contracts/base/UUPSUpgradeable.sol:17 |
| `Unauthorized()` | `0x82b42900` | Launch-x:contracts/contracts/interfaces/IEventEmitter.sol:8 |
| `UnauthorizedUpgrade()` | `0x3a617a54` | Launch-x:contracts/contracts/base/UUPSUpgradeable.sol:16 |
| `UpgradeInitializationFailed()` | `0xcc1cf440` | Launch-x:contracts/contracts/base/UUPSUpgradeable.sol:21 |

### LaunchpadOptical

30 functions, 5 events, 10 errors.

| Function | Selector | Mutability | Returns | Declared |
|---|---|---|---|---|
| `MAX_CAPITAL_RAISE_BPS()` | `0x87993a4e` | view | `(uint256)` | Launch-x:contracts/contracts/opticals/presets/LaunchpadOptical.sol:45 |
| `MAX_CLIFF_DURATION()` | `0x5300a4a4` | view | `(uint256)` | Launch-x:contracts/contracts/opticals/presets/LaunchpadOptical.sol:46 |
| `MAX_RAISE_DURATION()` | `0xf43ae507` | view | `(uint256)` | Launch-x:contracts/contracts/opticals/presets/LaunchpadOptical.sol:48 |
| `MAX_TEAM_WALLETS()` | `0xd0fe7ca2` | view | `(uint256)` | Launch-x:contracts/contracts/opticals/presets/LaunchpadOptical.sol:49 |
| `MAX_VESTING_DURATION()` | `0x7cd0814b` | view | `(uint256)` | Launch-x:contracts/contracts/opticals/presets/LaunchpadOptical.sol:47 |
| `accumulatedUsdl(address)` | `0xa7746391` | view | `(uint256)` | Launch-x:contracts/contracts/opticals/presets/LaunchpadOptical.sol:90 |
| `afterFeeDistribution(address,uint256)` | `0xae3bd4bd` | nonpayable | `(bytes4)` | Launch-x:contracts/contracts/opticals/BaseOptical.sol:72 |
| `afterSwap(address,address,bool,uint256,uint256)` | `0xf74c1c1a` | nonpayable | `(bytes4)` | Launch-x:contracts/contracts/opticals/presets/LaunchpadOptical.sol:172 |
| `beforeFeeDistribution(address,uint256)` | `0xf80d2cc7` | nonpayable | `(uint256)` | Launch-x:contracts/contracts/opticals/presets/LaunchpadOptical.sol:208 |
| `beforeSwap(address,address,bool,uint256)` | `0xae7e71d9` | nonpayable | `(bool,int256)` | Launch-x:contracts/contracts/opticals/presets/LaunchpadOptical.sol:159 |
| `beforeTokenTransfer(address,address,address,uint256)` | `0x37be75a0` | nonpayable | `()` | Launch-x:contracts/contracts/opticals/presets/LaunchpadOptical.sol:179 |
| `capitalRaiseBps()` | `0xc6532ff3` | view | `(uint256)` | Launch-x:contracts/contracts/opticals/presets/LaunchpadOptical.sol:69 |
| `capitalRaiseDuration()` | `0x71f35332` | view | `(uint256)` | Launch-x:contracts/contracts/opticals/presets/LaunchpadOptical.sol:72 |
| `claimCapital(address)` | `0xa9b00985` | nonpayable | `()` | Launch-x:contracts/contracts/opticals/presets/LaunchpadOptical.sol:244 |
| `cliffDuration()` | `0xd85349f7` | view | `(uint256)` | Launch-x:contracts/contracts/opticals/presets/LaunchpadOptical.sol:63 |
| `creator()` | `0x02d05d3f` | view | `(address)` | Launch-x:contracts/contracts/opticals/presets/LaunchpadOptical.sol:54 |
| `feeAccumulator()` | `0x5eaea35f` | view | `(address)` | Launch-x:contracts/contracts/opticals/presets/LaunchpadOptical.sol:60 |
| `getCapitalRaiseInfo(address)` | `0x79ec5214` | view | `(uint256,uint256,uint256,bool)` | Launch-x:contracts/contracts/opticals/presets/LaunchpadOptical.sol:302 |
| `getFlags()` | `0x6cc7149d` | pure | `(uint8)` | Launch-x:contracts/contracts/opticals/presets/LaunchpadOptical.sol:151 |
| `getVestingInfo(address,address)` | `0xffc7b095` | view | `(bool,uint256,uint256,uint256,uint256,uint256)` | Launch-x:contracts/contracts/opticals/presets/LaunchpadOptical.sol:258 |
| `isTeamWallet(address)` | `0x4a5ecb36` | view | `(bool)` | Launch-x:contracts/contracts/opticals/presets/LaunchpadOptical.sol:319 |
| `isVested(address)` | `0x760a2e8a` | view | `(bool)` | Launch-x:contracts/contracts/opticals/presets/LaunchpadOptical.sol:80 |
| `owner()` | `0x8da5cb5b` | view | `(address)` | Launch-x:contracts/contracts/opticals/BaseOptical.sol:20 |
| `poolRegistry()` | `0xafcff50f` | view | `(address)` | Launch-x:contracts/contracts/opticals/BaseOptical.sol:17 |
| `poolStartTime(address)` | `0x56fcdcb4` | view | `(uint256)` | Launch-x:contracts/contracts/opticals/presets/LaunchpadOptical.sol:83 |
| `teamClaimAddress()` | `0x4ccb398e` | view | `(address)` | Launch-x:contracts/contracts/opticals/presets/LaunchpadOptical.sol:57 |
| `teamWalletCount()` | `0x2c420122` | view | `(uint256)` | Launch-x:contracts/contracts/opticals/presets/LaunchpadOptical.sol:75 |
| `tokensSold(address,address)` | `0x9f1245ad` | view | `(uint256)` | Launch-x:contracts/contracts/opticals/presets/LaunchpadOptical.sol:87 |
| `totalRaised(address)` | `0x30d373f5` | view | `(uint256)` | Launch-x:contracts/contracts/opticals/presets/LaunchpadOptical.sol:93 |
| `vestingDuration()` | `0x1514617e` | view | `(uint256)` | Launch-x:contracts/contracts/opticals/presets/LaunchpadOptical.sol:66 |

| Event | Topic 0 | Declared |
|---|---|---|
| `CapitalRaiseAccumulated(address,uint256,uint256)` | `0x48b3413afdad030150d68e8ed719b41dd32593b330f034158efe3134ffde0dae` | Launch-x:contracts/contracts/opticals/presets/LaunchpadOptical.sol:40 |
| `CapitalRaiseClaimed(address,uint256,address)` | `0xe8067320421baf056a2b16baecdc6fff89d3e90e2642fd0ab434b9845d8702c1` | Launch-x:contracts/contracts/opticals/presets/LaunchpadOptical.sol:41 |
| `PoolStartTimeRecorded(address,uint256)` | `0xe604181e9b1b472a9fd7581a81193c352a32c4a286112bcad619d427b19257c9` | Launch-x:contracts/contracts/opticals/presets/LaunchpadOptical.sol:37 |
| `SellBlockedCliff(address,address,uint256)` | `0x4acfead499d5bfd950d77435a2671732e5e612aba3b219ba1f129c48829b502b` | Launch-x:contracts/contracts/opticals/presets/LaunchpadOptical.sol:38 |
| `SellBlockedVesting(address,address,uint256,uint256)` | `0xb3966d45988315ed5a8dc1f14ca6aa97a4d2c0615f4a43cdc146990a715a4069` | Launch-x:contracts/contracts/opticals/presets/LaunchpadOptical.sol:39 |

| Error | Selector | Declared |
|---|---|---|
| `CapitalRaiseTooHigh()` | `0xf7fc11bd` | Launch-x:contracts/contracts/opticals/presets/LaunchpadOptical.sol:28 |
| `CliffActive()` | `0x1392d261` | Launch-x:contracts/contracts/opticals/presets/LaunchpadOptical.sol:26 |
| `DurationTooLong()` | `0x9529f506` | Launch-x:contracts/contracts/opticals/presets/LaunchpadOptical.sol:29 |
| `NoTeamWallets()` | `0x1985bc9e` | Launch-x:contracts/contracts/opticals/presets/LaunchpadOptical.sol:30 |
| `NotOwner()` | `0x30cd7471` | Launch-x:contracts/contracts/opticals/BaseOptical.sol:14 |
| `NotPool()` | `0x6f61f641` | Launch-x:contracts/contracts/opticals/BaseOptical.sol:13 |
| `NotTeam()` | `0xe9f3e974` | Launch-x:contracts/contracts/opticals/presets/LaunchpadOptical.sol:33 |
| `NothingToClaim()` | `0x969bf728` | Launch-x:contracts/contracts/opticals/presets/LaunchpadOptical.sol:32 |
| `TooManyTeamWallets()` | `0x1f4941f9` | Launch-x:contracts/contracts/opticals/presets/LaunchpadOptical.sol:31 |
| `VestingExceeded()` | `0x45446fb4` | Launch-x:contracts/contracts/opticals/presets/LaunchpadOptical.sol:27 |

- precompiles/launchpad/abi/ holds the compiled ABI of each of the ten served roles (SidioraPool, SidioraFactory, SidioraERC20, SidioraNFT, Router, Quoter, FeesRouter, FeeAccumulator, PoolRegistry, ProtocolConfig) copied from the Launch-x build at c5eb7c7 (the sources of Launch-x:contracts/contracts), and each file is loaded with MustGetABI (precompiles/common/precompiles.go:449) through the embed.FS of precompiles/launchpad/launchpad.go:94. (req 2.1.1)
- precompiles/launchpad/testdata/abi_golden.txt lists one line per function (role, signature, 4-byte selector), per event (role, signature, topic0) and per custom error (role, signature, selector), 547 lines for the ten roles plus the twelve EventEmitter launchpad events; any change to a role file that alters a line fails the test. (req 2.1.2)
- precompiles/launchpad/abi.json becomes the ABI of direct calls to 0x1017: the union of the Router, SidioraFactory, Quoter, PoolRegistry, FeesRouter, FeeAccumulator and ProtocolConfig functions taken in that order with the first role winning a shared signature (113 distinct selectors, the 22 shared signatures listed in contracts/launchpad.md section 3), plus the twelve EventEmitter launchpad events; the current abi.json and Launchpad.sol (precompiles/launchpad/launchpad.go:94) move to precompiles/launchpad/legacy/v610. (req 2.1.3)
- Two signatures with one selector never appear in one role or at 0x1017; the golden test asserts the selector set of each role and of 0x1017 has no duplicate. (req 2.1.4)

## 3. Direct calls to 0x1017

A call sent to 0x1017 itself resolves the selector in role order Router, SidioraFactory, Quoter, PoolRegistry, FeesRouter, FeeAccumulator, ProtocolConfig; the first role that declares it serves it. This gives 113 distinct selectors. 21 signatures are declared by several roles:

| Signature | Selector | Served by | Also declared by |
|---|---|---|---|
| `DEFAULT_ADMIN_ROLE()` | `0xa217fddf` | Router | SidioraFactory, Quoter, PoolRegistry, FeesRouter, FeeAccumulator, ProtocolConfig |
| `createMarket(string,string,uint8,address)` | `0x57c0d7fd` | Router | SidioraFactory |
| `getRoleAdmin(bytes32)` | `0x248a9ca3` | Router | SidioraFactory, Quoter, PoolRegistry, FeesRouter, FeeAccumulator, ProtocolConfig |
| `grantRole(bytes32,address)` | `0x2f2ff15d` | Router | SidioraFactory, Quoter, PoolRegistry, FeesRouter, FeeAccumulator, ProtocolConfig |
| `hasRole(bytes32,address)` | `0x91d14854` | Router | SidioraFactory, Quoter, PoolRegistry, FeesRouter, FeeAccumulator, ProtocolConfig |
| `poolRegistry()` | `0xafcff50f` | Router | SidioraFactory, Quoter, FeesRouter, FeeAccumulator |
| `protocolConfig()` | `0xf5efbb4f` | Router | SidioraFactory, Quoter, FeeAccumulator |
| `proxiableUUID()` | `0x52d1902d` | Router | SidioraFactory, Quoter, PoolRegistry, FeesRouter, FeeAccumulator, ProtocolConfig |
| `registeredDefaultAdminCount()` | `0xc0cb71b6` | Router | SidioraFactory, Quoter, PoolRegistry, FeesRouter, FeeAccumulator, ProtocolConfig |
| `renounceRole(bytes32,address)` | `0x36568abe` | Router | SidioraFactory, Quoter, PoolRegistry, FeesRouter, FeeAccumulator, ProtocolConfig |
| `revokeRole(bytes32,address)` | `0xd547741f` | Router | SidioraFactory, Quoter, PoolRegistry, FeesRouter, FeeAccumulator, ProtocolConfig |
| `upgradeToAndCall(address,bytes)` | `0x4f1ef286` | Router | SidioraFactory, Quoter, PoolRegistry, FeesRouter, FeeAccumulator, ProtocolConfig |
| `usdlAddress()` | `0x2e10f976` | Router | SidioraFactory, FeeAccumulator, ProtocolConfig |
| `eventEmitter()` | `0x9ff78c30` | SidioraFactory | PoolRegistry, FeeAccumulator, ProtocolConfig |
| `getAllPools(uint256,uint256)` | `0xa7753f09` | Quoter | PoolRegistry |
| `getPoolsByCreator(address)` | `0xc84728ef` | Quoter | PoolRegistry |
| `feeAccumulator()` | `0x5eaea35f` | SidioraFactory | FeesRouter |
| `nftContract()` | `0xd56d229d` | SidioraFactory | FeesRouter |
| `FACTORY_ROLE()` | `0x04a0fb17` | PoolRegistry | FeeAccumulator |
| `treasury()` | `0x61d027b3` | SidioraFactory | FeeAccumulator |
| `initialize(address,address,address)` | `0xc0c53b8b` | Quoter | ProtocolConfig |

No two different signatures share a selector.

## 4. Surfaces

- Every contract address of the launchpad holds the 41-byte runtime 0x363d3d373360601b36523d3d601436013d3d6110175af13d600060003e6024573d6000fd5b3d6000f3 after the upgrade, written with SetCode (modules/evm/keeper/code.go:23).
- The precompile reads the original caller from the last 20 bytes only when its EVM caller is a registered surface, otherwise the EVM caller (precompiles/launchpad/launchpad.go:232).
- Delegatecall reverts, writes under staticcall revert and value reverts (precompiles/common/precompiles.go:374).
- An unknown selector reverts with empty revert data.
- MigrateV6120 replaces the runtime code at every pool address of generations 1 and 2, at both fee-right collections and at the Factory, Router, Quoter, FeesRouter, FeeAccumulator, PoolRegistry and ProtocolConfig addresses of both generations (Launch-x:protocol/addresses.go:22, Launch-x:shared/config/deployment/current.json:37) with the 41-byte surface runtime 0x363d3d373360601b36523d3d601436013d3d6110175af13d600060003e6024573d6000fd5b3d6000f3, written with SetCode (modules/evm/keeper/code.go:23); the runtime copies calldata, appends CALLER and CALLs 0x1017 with all gas, returning or reverting with the callee data. (req 2.2.1)
- The precompile treats the last 20 bytes of calldata as the original caller only when its own caller is a registered surface address (a keeper set written by MigrateV6120 and by market creation, keyed as MarketPointerPrefix is in modules/launchpad/types/keys.go:41); for any other caller the calldata is decoded whole and the caller is the EVM caller, so no account can claim another caller by appending bytes. (req 2.2.2)
- A call arriving from a surface selects the role ABI of that surface (pool, collection, token, or the generation role of the address) and the market generation of the address, so the same selector resolves per role; direct calls to 0x1017 resolve through abi.json in the order Router, SidioraFactory, Quoter, PoolRegistry, FeesRouter, FeeAccumulator, ProtocolConfig. (req 2.2.3)
- Delegatecall into 0x1017 reverts and a write under staticcall reverts, as precompiles/launchpad/launchpad.go:232 does today; a view selector under staticcall is served; value attached to any call reverts, matching ValidateNonPayable (precompiles/common/precompiles.go:374) and the non-payable functions of the contracts (the only payable function, upgradeToAndCall, reverts for every caller). (req 2.2.4)
- An unknown selector at a surface or at 0x1017 reverts with empty revert data, as a contract without a fallback does; the registered surface set is exported and imported by genesis (modules/launchpad/keeper/genesis.go:53). (req 2.2.5)

## 5. Quote asset

- The quote denom is uusdl (modules/launchpad/types/params.go:31), six decimals (Launch-x:shared/config/deployment/current.json:98).
- The ERC20 native pointer of uusdl is 0x85FcD13735F4309833A503EE804ea32395851479 (Launch-x:shared/config/deployment/current.json:86), returned by GetERC20NativePointer (precompiles/utils/expected_keepers.go:127).
- Each EVM address holds uusdl in its bank account from GetPaxAddressOrDefault (modules/evm/keeper/address.go:58); pools keep reserves in the bank account of the pool address.
- Quote moves with BankKeeper.SendCoins (modules/launchpad/types/expected_keepers.go:16) between the accounts of the contract flow.
- Router and Factory surfaces move the caller's own funds without an allowance; allowances held on the old USDL contract are not carried.
- Params.QuoteDenom is "uusdl" (modules/launchpad/types/params.go:31) from H+1: MigrateV6120 sets it back from the base coin that the v6.10 handler wrote (node/upgrades.go:351), through UpdateParams (modules/launchpad/keeper/keeper.go:76). (req 2.18.1)
- uusdl has six decimals, the settlement asset of the deployment manifest (Launch-x:shared/config/deployment/current.json:97-Launch-x:shared/config/deployment/current.json:98); its bank metadata is set with display "USDL" and exponent 6, and GetERC20NativePointer(ctx, "uusdl") (precompiles/utils/expected_keepers.go:127) returns 0x85FcD13735F4309833A503EE804ea32395851479. (req 2.18.2)
- Every pool, FeeAccumulator, Treasury and account holds its USDL as uusdl in the bank account of its EVM address (GetPaxAddressOrDefault, modules/evm/keeper/address.go:58), so the pay-first balance check of the pool (Launch-x:contracts/contracts/core/SidioraPool.sol:178) reads the bank balance of the pool address and contracts that call transfer on USDL keep working through the pointer. (req 2.18.3)
- The launchpad keeper moves quote and pool tokens with BankKeeper.SendCoins (modules/launchpad/types/expected_keepers.go:16) between the bank accounts of the EVM addresses of the contract flow (caller, pool, FeeAccumulator, recipient, Treasury, `0x000000000000000000000000000000000000dEaD`); no escrow in the module account is kept for pool reserves. (req 2.18.4)
- Router and Factory surfaces move the caller's own uusdl and pool tokens without consulting an allowance, because the caller is authenticated by the surface; the contract transferFrom calls (Launch-x:contracts/contracts/periphery/Router.sol:188, Launch-x:contracts/contracts/periphery/Router.sol:165) become bank sends from the caller, and the *WithPermit variants decode the permit tuple and then run as their plain variant. (req 2.18.5)

## 6. Swap

- Swap checks in contract order: deadline (Launch-x:contracts/contracts/core/SidioraPool.sol:171), zero input (Launch-x:contracts/contracts/core/SidioraPool.sol:172), zero recipient (Launch-x:contracts/contracts/core/SidioraPool.sol:173), prepaid input (Launch-x:contracts/contracts/core/SidioraPool.sol:178), then the optical beforeSwap reduction rules (Launch-x:contracts/contracts/core/SidioraPool.sol:186-195), each reverting with the same custom error. (req 2.19.1)
- The fee is taken on the input with mulDiv(amountIn, feeBps, 10000) (Launch-x:contracts/contracts/core/SidioraPool.sol:201); a buy prices amountIn minus fee against virtual plus real USDL (Launch-x:contracts/contracts/core/SidioraPool.sol:208-213), moves the fee to the FeeAccumulator of the market generation and records it (Launch-x:contracts/contracts/core/SidioraPool.sol:220-224); a sell keeps fee tokens in the pool (Launch-x:contracts/contracts/core/SidioraPool.sol:241-244). (req 2.19.2)
- A sell whose output exceeds real USDL reverts VirtualFloorBreached() (Launch-x:contracts/contracts/core/SidioraPool.sol:232); a buy whose output exceeds the token reserve reverts InsufficientLiquidity() (Launch-x:contracts/contracts/core/SidioraPool.sol:211); zero output reverts InsufficientInput() and output below minAmountOut reverts SlippageExceeded() (Launch-x:contracts/contracts/core/SidioraPool.sol:247-248). (req 2.19.3)
- USDL volume is paidInput on buys and amountOut on sells, added to cumulativeVolume and to the buy or sell cumulative field (Launch-x:contracts/contracts/core/SidioraPool.sol:250-254); the price snapshot is taken only when volume is at least 1 bps of effective USDL and 60 seconds passed since the last snapshot (Launch-x:contracts/contracts/core/SidioraPool.sol:406-415), into an eight-slot ring. (req 2.19.4)
- After the swap the precompile emits InputAdjustment when paidInput differs from amountIn (Launch-x:contracts/contracts/core/SidioraPool.sol:257), calls afterSwap and requires its selector (Launch-x:contracts/contracts/core/SidioraPool.sol:259-263), checks the bank balances of the pool against realUsdlBalance and tokenReserve (Launch-x:contracts/contracts/core/SidioraPool.sol:268), and emits Swap at 0x1017 (Launch-x:contracts/contracts/core/SidioraPool.sol:273). (req 2.19.5)
- The dynamic fee uses ProtocolConfig parameters with the holder concentration term fixed at 0 (Launch-x:contracts/contracts/core/SidioraPool.sol:375-396) through FeeLib (Launch-x:contracts/contracts/libraries/FeeLib.sol:22), which the keeper implements in CalculateDynamicFee (modules/launchpad/types/math.go:179). (req 2.19.6)
- syncReserves sets realUsdlBalance and tokenReserve to the bank balances of the pool address and emits PoolStateUpdated at 0x1017 (Launch-x:contracts/contracts/core/SidioraPool.sol:279-291). (req 2.19.7)

## 7. Market creation

- createMarket through the Router or Factory surface or 0x1017 validates name and symbol (modules/launchpad/types/market.go:36), charges the creation fee in uusdl from the caller to the Treasury address of the generation 2 Factory (Factory slot 6, the field at Launch-x:contracts/contracts/core/SidioraFactory.sol:30), and reverts with the contract error when the fee cannot be paid. (req 2.20.1)
- The pool token is a tokenfactory denom with subdenom "lp"+index (modules/launchpad/keeper/market.go:18) minted to the pool account with the virtual token default supply; its ERC20 address is the native pointer from UpsertERCNativePointer (modules/evm/keeper/pointer_upgrade.go:49) when the optical has no BEFORE_TOKEN_TRANSFER flag, and a token surface at the CREATE2 address of 0x1017 with salt keccak256(name,symbol,creator,nonce) (Launch-x:contracts/contracts/core/SidioraFactory.sol:121) otherwise. (req 2.20.2)
- The pool address of a new market is the CREATE2 address of 0x1017 with salt keccak256(token) (Launch-x:contracts/contracts/core/SidioraFactory.sol:143) and the surface runtime hash, registered as a surface and in the PoolRegistry role at 0x1017 with metadata creator, token, optical, nftId, createdAt and createdBlock (Launch-x:contracts/contracts/interfaces/IPoolRegistry.sol:15). (req 2.20.3)
- The fee right is minted in the generation 2 collection with id nextTokenId (collection slot 11) and its fee strategy (slot 12) and pool (slot 13); NftMint, TokenDeployed, MarketCreated and PoolRegistered are emitted at 0x1017 in the contract order (Launch-x:contracts/contracts/core/SidioraFactory.sol:159, Launch-x:contracts/contracts/core/SidioraFactory.sol:160). (req 2.20.4)

## 8. Migration record

- MigrateV6120 runs step 1 (USDL), step 2 (pools and pool tokens), step 3 (fee rights, roles, accounting) in one cached context.
- Step 1 reads holder balances with GetState (modules/evm/keeper/state.go:10), mints uusdl, clears the contract storage and installs the native pointer at 0x85FcD13735F4309833A503EE804ea32395851479 (modules/evm/keeper/pointer.go:29).
- Step 2 enumerates pools from each PoolRegistry (Launch-x:contracts/contracts/data/PoolRegistry.sol:23) and reads every pool slot.
- Allowances, permit nonces, token checkpoint arrays (beyond open airdrop entitlements) and DEFAULT_ADMIN_ROLE memberships are not carried.

### Step 1, USDL

- migrateUSDL reads the holder list of modules/launchpad/keeper/testdata/v6120/holders.json, reads each balance from the USDL contract storage with GetState (modules/evm/keeper/state.go:10) at keccak256(holder, balanceSlot), with the balance slot and total-supply slot taken from modules/launchpad/keeper/testdata/v6120/usdl.json, and refuses with "upgrade v6.12.0: launchpad: usdl holder list incomplete" when the sum differs from the total-supply slot. (req 2.23.1)
- For each holder with a nonzero balance it mints the balance in uusdl to the launchpad module account and sends it to the bank account of the holder address (modules/evm/keeper/address.go:58); the bank supply of uusdl after the step equals the total-supply slot exactly. (req 2.23.2)
- The step clears the storage of the old USDL address with IterateState and SetState (modules/evm/keeper/state.go:39, modules/evm/keeper/state.go:30), records 0x85FcD13735F4309833A503EE804ea32395851479 as the native pointer of uusdl with SetERC20NativePointer (modules/evm/keeper/pointer.go:29) and then runs UpsertERCNativePointer, whose existing-pointer branch writes the pointer runtime at that address (modules/evm/keeper/pointer_upgrade.go:101-108). (req 2.23.3)
- Allowances and permit nonces of the USDL contract are not migrated: `allowance(address,address)` returns 0 after the step and holders approve again; the migration record of contracts/launchpad.md lists both. (req 2.23.4)
- The step runs before pool migration inside MigrateV6120, its module account needs Minter permission and BankKeeper needs MintCoins (required change to modules/launchpad/types/expected_keepers.go:15), and the uusdl bank metadata (base uusdl, display USDL, exponent 6) is written once. (req 2.23.5)

### Step 2, pools and pool tokens

- migratePools lists the pools of each generation from the PoolRegistry _allPools array (slot 6, Launch-x:contracts/contracts/data/PoolRegistry.sol:23) of 0x7684…BA0b and 0x1dC3…4Ba7 and refuses when a pool code hash is not the code hash of a generation pool proxy recorded in modules/launchpad/keeper/testdata/v6120/layout.json. (req 2.24.1)
- Each pool state field is read from its slot (tokenAddress 0, usdlAddress 1, opticalAddress 2, feeAccumulator 3, eventEmitter 4, protocolConfig 5, guardian 6, virtualUsdlReserve 7, realUsdlBalance 8, tokenReserve 9, creationTimestamp 10, cumulativeVolume 11, priceSnapshots 12-19, snapshotIndex 20, snapshotCount 21, accumulatedUsdlFees 22, accumulatedTokenFees 23, factory 24, pendingGuardian 25, lastSnapshotTimestamp 26, cumulativeBuyUsdlVolume 27, cumulativeSellUsdlVolume 28) as layout.json records them for the generation implementation, into the keeper Market record. (req 2.24.2)
- Each pool token becomes the tokenfactory denom of its market: holders from holders.json are verified against balanceOf (SidioraERC20 slot 3, Launch-x:contracts/contracts/base/ERC20Base.sol:13) with their sum equal to totalSupply (slot 2), the balances are minted to the holder accounts, and the old token address becomes the native pointer of the denom or, for markets with the BEFORE_TOKEN_TRANSFER flag, a token surface. (req 2.24.3)
- The uusdl balance of each pool address after step 1 equals its realUsdlBalance plus any surplus the contract held, and the pool-token balance of the pool address equals its tokenReserve plus surplus, so InvalidBalance (Launch-x:contracts/contracts/core/SidioraPool.sol:268) holds for every migrated pool. (req 2.24.4)
- Pool-token allowances (slot 4), permit nonces (slot 5) and the checkpoint arrays (slots 6 and 7) are not carried as storage; past balances needed by open airdrop epochs are written as entitlements, and getPastBalance, getPastTotalSupply, getPastVotes, permit, nonces, DOMAIN_SEPARATOR and PERMIT_TYPEHASH are listed as not served in the migration record. (req 2.24.5)

### Step 3, fee rights, roles and accounting

- Fee rights of both collections migrate keyed by (collection, id): holder from _owners (slot 2), approval from _tokenApprovals (slot 4), operator approvals from _operatorApprovals (slot 5) for the operators listed in the fixture, strategy from _feeStrategy (slot 12), pool from _poolAddress (slot 13) and nextTokenId (slot 11), declared at Launch-x:contracts/contracts/core/SidioraNFT.sol:30 and its base Launch-x:contracts/contracts/base/ERC721Base.sol:11. (req 2.25.1)
- Role memberships other than DEFAULT_ADMIN_ROLE migrate from each role _roles mapping (slot 0, or slot 10 in the collection) for the accounts listed in the fixture and verified by storage read; DEFAULT_ADMIN_ROLE is not carried, so registeredDefaultAdminCount() returns 0 at every role address. (req 2.25.2)
- FeeAccumulator accounting migrates per generation: _accumulatedFees, _protocolFeesPending, _airdropBalance, _airdropEpoch, _airdropEpochAmount, _airdropClaimed (for the fixture claimants), _lpRewardsBalance, _opticalSurplus, totalLiabilities, the airdrop snapshot fields (slots 15 to 18), accountingInitialized and the legacy airdrop fields (slots 22 to 24). (req 2.25.3)
- Each pool keeps its optical address; optical contracts stay deployed and unchanged and are called by the precompile with the callers they accept (Launch-x:contracts/contracts/opticals/BaseOptical.sol:86), and the PoolRegistry metadata (slot 4) of every pool migrates with nftId, createdAt and createdBlock. (req 2.25.4)
- MigrateV6120 returns its first error wrapped as "upgrade v6.12.0: launchpad: <error>" by the caller in the upgrade handler; it writes nothing outside the launchpad, bank and EVM stores, and the whole migration is one cached context committed only on success. (req 2.25.5)

### Not migrated

| Item | Replacement |
|---|---|
| USDL and pool-token allowances | holders approve again; Router and Factory surfaces move the caller funds without an allowance |
| permit nonces and permit signatures | holders sign again; the *WithPermit surfaces decode the permit tuple and act as the plain variant |
| pool-token checkpoint arrays | past balances for airdrops come from the holding history |
| DEFAULT_ADMIN_ROLE memberships | parameters change through MsgUpdateParams; admin-gated writes revert MissingRole(caller, 0x00) |
| `permit`, `nonces`, `DOMAIN_SEPARATOR`, `PERMIT_TYPEHASH`, `getPastBalance`, `getPastTotalSupply`, `getPastVotes`, `pool`, `deploymentFactory`, `bindPool` of SidioraERC20 | not served by the native pointer |

## 9. Fees and airdrops

- recordFee splits the buy fee into protocol cut protocolFeeBps/10000 and pool cut, applies the optical beforeFeeDistribution adjustment with InsufficientSurplus when it raises the pool cut (Launch-x:contracts/contracts/data/FeeAccumulator.sol:135), calls afterFeeDistribution and requires its selector (Launch-x:contracts/contracts/data/FeeAccumulator.sol:161) and emits FeeRecorded (Launch-x:contracts/contracts/data/FeeAccumulator.sol:164) at the generation FeeAccumulator address. (req 2.21.1)
- claim pays the accumulated pool fees to the recipient and reverts NoFeesAccumulated() at zero (Launch-x:contracts/contracts/data/FeeAccumulator.sol:169-184); burn sends them to `0x000000000000000000000000000000000000dEaD` (Launch-x:contracts/contracts/data/FeeAccumulator.sol:187-200); sendLpRewards moves them to the pool reserve (Launch-x:contracts/contracts/data/FeeAccumulator.sol:272-286). (req 2.21.2)
- The FeesRouter role checks the caller holds the fee right of the pool in the collection of its market (ownerOf) and the strategy of that fee right, and emits FeeDistributed at 0x1017 (Launch-x:contracts/contracts/periphery/FeesRouter.sol:124); the keeper requireFeeRightsHolder (modules/launchpad/keeper/fees.go:12) reads the fee-right store keyed by collection and id. (req 2.21.3)
- Each FeeAccumulator role keeps totalLiabilities and requires its uusdl bank balance to cover it plus the new amount (Launch-x:contracts/contracts/data/FeeAccumulator.sol:478-480); the mirror FeeFlow event is emitted at 0x1017 (Launch-x:contracts/contracts/data/FeeAccumulator.sol:470). (req 2.21.4)
- triggerAirdrop opens epoch e+1 for the pool with the airdrop balance, the eligible supply and the snapshot height (Launch-x:contracts/contracts/data/FeeAccumulator.sol:203-230); the keeper records the epoch through OpenHoldingHistory (modules/launchpad/keeper/history.go:83). (req 2.22.1)
- claimAirdrop pays holder balance at the snapshot times the epoch amount over the eligible supply, refuses the pool, the `0x000000000000000000000000000000000000dEaD` address and zero (Launch-x:contracts/contracts/data/FeeAccumulator.sol:250), refuses a second claim with AlreadyClaimed() (Launch-x:contracts/contracts/data/FeeAccumulator.sol:251) and gives the rounding residue to the final claimant (Launch-x:contracts/contracts/data/FeeAccumulator.sol:248-269); balances at the snapshot come from EpochBalance (modules/launchpad/keeper/history.go:155). (req 2.22.2)
- Open airdrop epochs of migrated pools keep their snapshot: MigrateV6120 writes each holder entitlement from the token checkpoint array (SidioraERC20 slot 6) at airdropSnapshotBlock (FeeAccumulator slot 15) into the airdrop basis store (modules/launchpad/keeper/keeper.go:302), so a claim after the upgrade pays what the contract would have paid. (req 2.22.3)
- claimLegacyAirdrop verifies the Merkle proof over keccak256(pool, epoch, holder, amount) (Launch-x:contracts/contracts/data/FeeAccumulator.sol:453) against legacyAirdropRoot (slot 23) and legacyAirdropRemaining (slot 24), both migrated. (req 2.22.4)

## 10. Parameters

- Parameters equal ProtocolConfig.initialize (Launch-x:contracts/contracts/protocol/ProtocolConfig.sol:45-54); ConcentrationWeight 0 and CreationFee 1000000 are required changes to modules/launchpad/types/params.go:34.
- Validate enforces the ProtocolConfig setter bounds (Launch-x:contracts/contracts/protocol/ProtocolConfig.sol:84, Launch-x:contracts/contracts/protocol/ProtocolConfig.sol:100, Launch-x:contracts/contracts/protocol/ProtocolConfig.sol:67).
- Parameters change through MsgUpdateParams only (modules/launchpad/keeper/msg_server.go:14).

| Parameter | ProtocolConfig.initialize | DefaultParams today | v6.12.0 |
|---|---|---|---|
| VirtualQuoteDefault | Launch-x:contracts/contracts/protocol/ProtocolConfig.sol:45 | modules/launchpad/types/params.go:37 | 10000000000 |
| VirtualTokenDefault | Launch-x:contracts/contracts/protocol/ProtocolConfig.sol:46 | modules/launchpad/types/params.go:38 | 1000000000000000 |
| MinFeeBps | Launch-x:contracts/contracts/protocol/ProtocolConfig.sol:47 | modules/launchpad/types/params.go:39 | 10 |
| MaxFeeBps | Launch-x:contracts/contracts/protocol/ProtocolConfig.sol:48 | modules/launchpad/types/params.go:40 | 300 |
| BaseFeeBps | Launch-x:contracts/contracts/protocol/ProtocolConfig.sol:49 | modules/launchpad/types/params.go:41 | 30 |
| ProtocolFeeBps | Launch-x:contracts/contracts/protocol/ProtocolConfig.sol:50 | modules/launchpad/types/params.go:42 | 1000 |
| FeeDecayRate | Launch-x:contracts/contracts/protocol/ProtocolConfig.sol:51 | modules/launchpad/types/params.go:43 | 500 |
| VolatilityWeight | Launch-x:contracts/contracts/protocol/ProtocolConfig.sol:52 | modules/launchpad/types/params.go:44 | 100 |
| ConcentrationWeight | Launch-x:contracts/contracts/protocol/ProtocolConfig.sol:53 | modules/launchpad/types/params.go:45 | 0 (required change) |
| CreationFee | Launch-x:contracts/contracts/protocol/ProtocolConfig.sol:54 | modules/launchpad/types/params.go:46 | 1000000 (required change) |

- DefaultParams (modules/launchpad/types/params.go:34) sets ConcentrationWeight 0 and CreationFee 1000000, the ProtocolConfig values (Launch-x:contracts/contracts/protocol/ProtocolConfig.sol:45-54); today it holds 100 and 100000000, a required change. (req 2.29.1)
- MigrateV6120 writes the parameters read from the generation 2 ProtocolConfig storage (slots 2 to 11) and refuses when they fail Validate (modules/launchpad/types/params.go:50). (req 2.29.2)
- Validate adds the ProtocolConfig bounds: concentrationWeight must be 0 (Launch-x:contracts/contracts/protocol/ProtocolConfig.sol:84), virtual defaults fit uint112 (Launch-x:contracts/contracts/protocol/ProtocolConfig.sol:100), protocolFeeBps at most 5000 (modules/launchpad/types/params.go:11) and baseFeeBps within min and max (Launch-x:contracts/contracts/protocol/ProtocolConfig.sol:60). (req 2.29.3)
- Parameters change only through MsgUpdateParams from the governance authority (modules/launchpad/keeper/msg_server.go:14), which emits ConfigUpdated at 0x1017 for each changed key. (req 2.29.4)

## 11. Events

- Contract-native events are emitted at the address that emitted them before, through EmitEVMLog (precompiles/common/evm_events.go:16).
- The twelve EventEmitter launchpad events are emitted at 0x1017 with block timestamp and number appended (Launch-x:contracts/contracts/data/EventEmitter.sol:365); the EventEmitter contracts stay deployed for the other systems.
- Logs of one call appear in the contract order of the call.

## 12. Errors

- Every refusal returns the contract custom error selector and ABI arguments as revert data; keeper errors keep their LXP_LAUNCHPAD_ codes (modules/launchpad/types/errors.go:7).
- Admin-gated writes revert MissingRole(caller, 0x00) (0x0161a64a, Launch-x:contracts/contracts/base/AccessControl.sol:42); initialize reverts AlreadyInitialized() (0x0dc149f0).
- Guardian functions revert Unauthorized() (0x82b42900, Launch-x:contracts/contracts/core/SidioraPool.sol:297) for other callers.

## 13. Optical hooks

- The precompile calls `getFlags()` (selector 0x6cc7149d, implemented at Launch-x:contracts/contracts/opticals/presets/LaunchpadOptical.sol:151) on the market optical through evm.Call with caller the pool address, at the point of the contract flow Launch-x:contracts/contracts/core/SidioraPool.sol:179, only when the optical flags carry every flag (Launch-x:contracts/contracts/libraries/BitFlag.sol:16); the optical code stays deployed unchanged and its sender checks pass (Launch-x:contracts/contracts/opticals/BaseOptical.sol:86). (req 2.17.1)
- The precompile calls `beforeSwap(address,address,bool,uint256)` (selector 0xae7e71d9, implemented at Launch-x:contracts/contracts/opticals/presets/LaunchpadOptical.sol:159) on the market optical through evm.Call with caller the pool address, at the point of the contract flow Launch-x:contracts/contracts/core/SidioraPool.sol:188, only when the optical flags carry BEFORE_SWAP (1) (Launch-x:contracts/contracts/libraries/BitFlag.sol:16); the optical code stays deployed unchanged and its sender checks pass (Launch-x:contracts/contracts/opticals/BaseOptical.sol:86). (req 2.17.2)
- The precompile calls `afterSwap(address,address,bool,uint256,uint256)` (selector 0xf74c1c1a, implemented at Launch-x:contracts/contracts/opticals/presets/LaunchpadOptical.sol:172) on the market optical through evm.Call with caller the pool address, at the point of the contract flow Launch-x:contracts/contracts/core/SidioraPool.sol:262, only when the optical flags carry AFTER_SWAP (2) (Launch-x:contracts/contracts/libraries/BitFlag.sol:16); the optical code stays deployed unchanged and its sender checks pass (Launch-x:contracts/contracts/opticals/BaseOptical.sol:86). (req 2.17.3)
- The precompile calls `beforeFeeDistribution(address,uint256)` (selector 0xf80d2cc7, implemented at Launch-x:contracts/contracts/opticals/presets/LaunchpadOptical.sol:208) on the market optical through evm.Call with caller the market generation FeeAccumulator address, at the point of the contract flow Launch-x:contracts/contracts/data/FeeAccumulator.sol:134, only when the optical flags carry BEFORE_FEE_DISTRIBUTION (4) (Launch-x:contracts/contracts/libraries/BitFlag.sol:16); the optical code stays deployed unchanged and its sender checks pass (Launch-x:contracts/contracts/opticals/BaseOptical.sol:86). (req 2.17.4)
- The precompile calls `afterFeeDistribution(address,uint256)` (selector 0xae3bd4bd, implemented at Launch-x:contracts/contracts/opticals/BaseOptical.sol:72) on the market optical through evm.Call with caller the market generation FeeAccumulator address, at the point of the contract flow Launch-x:contracts/contracts/data/FeeAccumulator.sol:161, only when the optical flags carry AFTER_FEE_DISTRIBUTION (8) (Launch-x:contracts/contracts/libraries/BitFlag.sol:16); the optical code stays deployed unchanged and its sender checks pass (Launch-x:contracts/contracts/opticals/BaseOptical.sol:86). (req 2.17.5)
- The precompile calls `beforeTokenTransfer(address,address,address,uint256)` (selector 0x37be75a0, implemented at Launch-x:contracts/contracts/opticals/presets/LaunchpadOptical.sol:179) on the market optical through evm.Call with caller the pool-token address, at the point of the contract flow Launch-x:contracts/contracts/core/SidioraERC20.sol:30, only when the optical flags carry BEFORE_TOKEN_TRANSFER (16) (Launch-x:contracts/contracts/libraries/BitFlag.sol:16); the optical code stays deployed unchanged and its sender checks pass (Launch-x:contracts/contracts/opticals/BaseOptical.sol:86). (req 2.17.6)

## 14. Storage

- Market records extend modules/launchpad/types/market.go:58 under MarketPrefix (modules/launchpad/types/keys.go:39).
- Surface and role records are new prefixes beside modules/launchpad/types/keys.go:48.
- Holding history stays under HoldingHistoryPrefix (modules/launchpad/types/keys.go:45) and backs past balances of airdrops.
- Pool storage slots read by the migration are those of contracts/launchpad.md section 14, from the Launch-x build of each generation implementation.

Storage slots of the generation 2 implementations read by the migration (slot, field):


- SidioraERC20: 0 `name`, 1 `symbol`, 2 `totalSupply`, 3 `balanceOf`, 4 `allowance`, 5 `nonces`, 6 `_balanceCheckpoints`, 7 `_supplyCheckpoints`, 8 `pool`.

- SidioraFactory: 0 `_roles`, 1 `poolBeacon`, 2 `nftContract`, 3 `poolRegistry`, 4 `eventEmitter`, 5 `protocolConfig`, 6 `treasury`, 7 `feeAccumulator`, 8 `usdlAddress`, 9 `_nonces`.

- SidioraNFT: 0 `name`, 1 `symbol`, 2 `_owners`, 3 `_balances`, 4 `_tokenApprovals`, 5 `_operatorApprovals`, 6 `_allTokens`, 7 `_allTokensIndex`, 8 `_ownedTokens`, 9 `_ownedTokensIndex`, 10 `_roles`, 11 `nextTokenId`, 12 `_feeStrategy`, 13 `_poolAddress`, 14 `eventEmitter`, 15 `poolTokenId`, 16 `poolIndexMigrationCursor`, 17 `poolIndexReady`.

- SidioraPool: 0 `tokenAddress`, 1 `usdlAddress`, 2 `opticalAddress`, 3 `feeAccumulator`, 4 `eventEmitter`, 5 `protocolConfig`, 6 `guardian`, 7 `virtualUsdlReserve`, 8 `realUsdlBalance`, 9 `tokenReserve`, 10 `creationTimestamp`, 11 `cumulativeVolume`, 12 `priceSnapshots`, 20 `snapshotIndex`, 21 `snapshotCount`, 22 `accumulatedUsdlFees`, 23 `accumulatedTokenFees`, 24 `factory`, 25 `pendingGuardian`, 26 `lastSnapshotTimestamp`, 27 `cumulativeBuyUsdlVolume`, 28 `cumulativeSellUsdlVolume`.

- FeeAccumulator: 0 `_roles`, 1 `protocolConfig`, 2 `treasury`, 3 `poolRegistry`, 4 `eventEmitter`, 5 `usdlAddress`, 6 `_accumulatedFees`, 7 `_protocolFeesPending`, 8 `_airdropBalance`, 9 `_airdropEpoch`, 10 `_airdropEpochAmount`, 11 `_airdropClaimed`, 12 `_lpRewardsBalance`, 13 `_opticalSurplus`, 14 `totalLiabilities`, 15 `airdropSnapshotBlock`, 16 `airdropEligibleSupply`, 17 `airdropEpochRemaining`, 18 `airdropClaimedSupply`, 19 `accountingInitialized`, 20 `accountingMigrationCursor`, 21 `_accountingMigratedPool`, 22 `legacyAirdropUnallocated`, 23 `legacyAirdropRoot`, 24 `legacyAirdropRemaining`.

- PoolRegistry: 0 `_roles`, 1 `_tokenToPool`, 2 `_poolToNftId`, 3 `_creatorToPools`, 4 `_poolMetadata`, 5 `_registeredPools`, 6 `_allPools`, 7 `eventEmitter`.

- FeesRouter: 0 `_roles`, 1 `nftContract`, 2 `feeAccumulator`, 3 `poolRegistry`.

- Quoter: 0 `_roles`, 1 `poolRegistry`, 2 `protocolConfig`.

- Router: 0 `_roles`, 1 `factory`, 2 `poolRegistry`, 3 `protocolConfig`, 4 `usdlAddress`.

- ProtocolConfig: 0 `_roles`, 1 `usdlAddress`, 2 `virtualUsdlDefault`, 3 `virtualTokenDefault`, 4 `minFeeBps`, 5 `maxFeeBps`, 6 `baseFeeBps`, 7 `protocolFeeBps`, 8 `feeDecayRate`, 9 `volatilityWeight`, 10 `concentrationWeight`, 11 `creationFee`, 12 `eventEmitter`.

The generation 1 layout is a release input in modules/launchpad/keeper/testdata/v6120/layout.json; the migration refuses a pool whose code hash is not listed there.

## 15. Gas

- RequiredGas is Gas(inputBytes, writes, pointers) with BaseGas 3000, GasPerByte 16, GasPerWrite 5000, GasPerPointer 9000000 (precompiles/launchpad/launchpad.go:15-18).
- Optical calls are nested EVM calls metered by the EVM (precompiles/xweb/xweb.go:482).
- RequiredGas stays Gas(inputBytes, writes, pointers) with BaseGas 3000, GasPerByte 16, GasPerWrite 5000 and GasPerPointer 9000000 (precompiles/launchpad/launchpad.go:15-18, precompiles/launchpad/launchpad.go:224), and the Writes table (precompiles/launchpad/launchpad.go:196) lists every state-changing method of the ten roles. (req 2.26.1)
- Pointers counts 1 for createMarket and createMarketWithPermit, the methods that register a new pointer (precompiles/launchpad/launchpad.go:216); nested evm.Call gas for optical hooks is metered by the EVM, as the xweb precompile meters its callback (precompiles/xweb/xweb.go:482). (req 2.26.2)
- The current pinned gas test (precompiles/launchpad/launchpad_test.go:177) moves to the legacy version with its precompile and keeps its values there. (req 2.26.3)

## 16. Activation

- 0x1017 stays behind activationPrecompileGate (node/upgrades.go:498).
- GetVersioned (precompiles/launchpad/setup.go:10) keeps the current precompile under v6.10 in legacy/v610.
- The v6.12.0 handler calls MigrateV6120(ctx, k, evm) first and wraps its error as "upgrade v6.12.0: launchpad: <error>".
- GetVersioned (precompiles/launchpad/setup.go:10) returns the new precompile for the latest upgrade and the current one under "v6.10" from precompiles/launchpad/legacy/v610, following the pattern of precompiles/p256/setup.go:22; precompiles/launchpad/versions gains v6.12.0 after v6.5 (precompiles/launchpad/versions:1). (req 2.27.1)
- 0x1017 stays behind activationPrecompileGate (node/upgrades.go:498), the registration of precompiles/setup.go:14 is unchanged, and the plan names v6.9 (node/upgrades.go:93) and v6.10 (node/upgrades.go:102) stay as they are. (req 2.27.2)
- The v6.12.0 handler calls launchpad MigrateV6120 first and then pricefeed InitV6120, perps SeedV6120 and spot InitV6120, wrapping each error as "upgrade v6.12.0: <hook>: <error>"; this domain supplies MigrateV6120 and its EVMReader interface only. (req 2.27.3)
- Task 2.3.99 runs go test ./modules/launchpad/... ./precompiles/launchpad/... once after every other task of the domain is done. (req 2.33.1)
- TestLaunchpadDomainWiring builds the keeper and the precompile together and runs createMarket, buy, sell, claimFees and a migrated pool read in one store. (req 2.33.2)

## 17. Invariants

- Bank balances of each pool cover realUsdlBalance and tokenReserve (Launch-x:contracts/contracts/core/SidioraPool.sol:268, modules/launchpad/keeper/invariants.go:20).
- Bank uusdl of each FeeAccumulator covers totalLiabilities (Launch-x:contracts/contracts/data/FeeAccumulator.sol:478-480).
- The uusdl supply after step 1 equals the USDL total-supply slot at the halt height.
- SolvencyInvariant (modules/launchpad/keeper/invariants.go:20) checks, for every market, that the uusdl bank balance of the pool address is at least realUsdlBalance and the pool-token balance is at least tokenReserve, the condition of Launch-x:contracts/contracts/core/SidioraPool.sol:268. (req 2.28.1)
- A second invariant checks that the uusdl bank balance of each FeeAccumulator address is at least its totalLiabilities, the condition of Launch-x:contracts/contracts/data/FeeAccumulator.sol:478-480, and is registered in RegisterInvariants (modules/launchpad/keeper/invariants.go:13). (req 2.28.2)
- The pool-token supply of every market equals the sum of holder balances, and the bank supply of uusdl changes only by mint in step 1 of MigrateV6120 and by sends afterwards. (req 2.28.3)

## 18. Dependents and backend binding

- Decision 2 is met by building to the generation 2 interface and migrating both generations (req 2.24, req 2.25).
- Decision 3 is met by uusdl as the bank form of the USDL ERC20 with its pointer at the USDL address (req 2.18, req 2.23), opticals (req 2.17), ERC721 fee rights (req 2.11).
- Programme req 1 ac 1 is met by req 2.1 to req 2.17.
- Programme req 1 ac 2 is met by req 2.18 to req 2.21; graduation is not part of the contracts or the manifest (Launch-x:shared/config/deployment/current.json:107), so no graduation path is served.
- Programme req 1 ac 3 is met by req 2.23 to req 2.25.
- Programme req 1 ac 4 is met by req 2.11 and req 2.12.
- Programme req 1 ac 5 is met by req 2.30 to req 2.32.
- The launchpad decode of the intents crate (human/crates/layerx-intents/src/precompile.rs:557) decodes the twelve EventEmitter launchpad events emitted at LAUNCHPAD_PRECOMPILE (human/crates/layerx-intents/src/precompile.rs:24) and PrecompileEventKind (human/crates/layerx-intents/src/precompile.rs:28) lists them with their topic0. (req 2.30.1)
- The intent map (human/crates/layerx-intents/src/precompile.rs:1005-1014) maps Swap to LaunchpadSwap, MarketCreated to LaunchpadCreate, FeeStrategyChanged to LaunchpadFeeStrategy and FeeDistributed by strategy to LaunchpadFeesClaim, LaunchpadFeesBurn, LaunchpadAirdropExecute and LaunchpadLpRewards, with the vocabulary variants of human/crates/layerx-intents/src/vocabulary.rs:134-145. (req 2.30.2)
- every_abi_event_is_declared_with_its_computed_topic (human/crates/layerx-intents/tests/precompile_router.rs:203) keeps passing with LAUNCHPAD_ABI (human/crates/layerx-intents/tests/precompile_router.rs:26) now the new abi.json. (req 2.30.3)
- The hosted indexer registry entry ("launchpad", 0x1017) (platform/hosted/indexer/src/abi.rs:41) loads the new abi.json, and signatures_and_topics_match_solidity (platform/hosted/indexer/src/abi.rs:637) covers the twelve launchpad events. (req 2.31.1)
- The gateway launchpad probe changes from getMarketCount() (platform/hosted/gateway/src/capabilities.rs:40) to getPoolCount() of the new interface, selector 0x8eec5d70, sent to LAUNCHPAD_PRECOMPILE (platform/hosted/gateway/src/evm.rs:47), and its answer still decodes as one word (platform/hosted/gateway/src/capabilities.rs:87). (req 2.31.2)
- The indexer ingest test replays a Swap and a MarketCreated log at 0x1017 and stores one row each. (req 2.31.3)
- The deployment manifest gains role kind "precompile" for roles served at 0x1017: Validate (Launch-x:shared/config/deployment/manifest.go:113) accepts it for the nine contract roles, exempts it from role_address_conflict (Launch-x:shared/config/deployment/manifest.go:160) and from the runtime hash check, and requires the implementation fields to be empty as for "direct" (Launch-x:shared/config/deployment/manifest.go:177). (req 2.32.1)
- current.json (Launch-x:shared/config/deployment/current.json:107) gains a third generation entry with the nine roles at 0x1017 kind "precompile", SidioraNFT kept at the generation 2 collection with kind "surface", USDL kept at 0x85FcD13735F4309833A503EE804ea32395851479 kind "direct" (Launch-x:shared/config/deployment/current.json:86), and settlement USDL six decimals unchanged. (req 2.32.2)
- The decode registry (Launch-x:core/indexer/internal/decode/registry.go:158) accepts several roles at one address with overlapping epochs when the deployment kind is "precompile" and dispatches each log by topic0 to its role, instead of refusing (Launch-x:core/indexer/internal/decode/registry.go:234). (req 2.32.3)
- parseContractAddresses (Launch-x:shared/chain/contracts.go:67) accepts the same address for several roles, and CreateContracts (Launch-x:shared/chain/contracts.go:93) binds each role to 0x1017 with its role ABI. (req 2.32.4)
- protocol/addresses.go keeps the generation 1 addresses (Launch-x:protocol/addresses.go:23) for historical decoding and adds the precompile address as the active address of the nine roles. (req 2.32.5)
