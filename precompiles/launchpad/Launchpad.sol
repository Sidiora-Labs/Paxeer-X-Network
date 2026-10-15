// SPDX-License-Identifier: MIT
pragma solidity ^0.8.0;

address constant LAUNCHPAD_PRECOMPILE_ADDRESS = 0x0000000000000000000000000000000000001017;

ILaunchpad constant LAUNCHPAD_CONTRACT = ILaunchpad(LAUNCHPAD_PRECOMPILE_ADDRESS);

/// Direct calls to 0x1017: the functions of Router, SidioraFactory, Quoter,
/// PoolRegistry, FeesRouter, FeeAccumulator and ProtocolConfig, the first
/// role in that order serving a shared signature, and the twelve
/// EventEmitter launchpad events emitted at 0x1017.
interface ILaunchpad {
    struct PermitParams {
        uint256 value;
        uint256 deadline;
        uint8 v;
        bytes32 r;
        bytes32 s;
    }

    struct PoolStats {
        uint256 virtualUsdl;
        uint256 realUsdl;
        uint256 tokenReserve;
        uint256 cumulativeVolume;
        uint256 currentFeeBps;
        uint256 poolAge;
        uint256 marketCap;
        uint256 price;
    }

    struct QuoteResult {
        uint256 amountOut;
        uint256 feeAmount;
        uint256 priceImpactBps;
    }

    struct MultihopQuoteResult {
        uint256 amountOut;
        uint256 intermediateUsdl;
        uint256 sellFeeAmount;
        uint256 buyFeeAmount;
        uint256 sellPriceImpactBps;
        uint256 buyPriceImpactBps;
        uint256 combinedPriceImpactBps;
        address poolA;
        address poolB;
    }

    struct PoolMetadata {
        address creator;
        address token;
        address optical;
        uint256 nftId;
        uint256 createdAt;
        uint256 createdBlock;
    }

    event ConfigUpdated(bytes32 indexed key, uint256 oldValue, uint256 newValue, uint256 timestamp, uint256 blockNumber);
    event FeeDistributed(bytes32 indexed poolId, uint256 nftId, uint8 strategy, uint256 amount, address recipient, uint256 timestamp, uint256 blockNumber);
    event FeeFlow(uint8 indexed kind, address indexed pool, address indexed party, uint256 amount, uint256 protocolCut, uint256 poolCut, uint256 epoch, uint256 timestamp, uint256 blockNumber);
    event FeeStrategyChanged(bytes32 indexed poolId, uint256 nftId, uint8 oldStrategy, uint8 newStrategy, uint256 timestamp, uint256 blockNumber);
    event MarketCreated(bytes32 indexed poolId, address indexed token, address indexed creator, address pool, address optical, uint256 timestamp, uint256 blockNumber);
    event NftMint(uint256 indexed tokenId, address indexed creator, address indexed pool, uint8 strategy, uint256 timestamp, uint256 blockNumber);
    event NftTransfer(address indexed nft, address indexed from, address indexed to, uint256 tokenId, uint256 timestamp, uint256 blockNumber);
    event PoolRegistered(address indexed pool, address indexed token, address indexed creator, address optical, uint256 nftId, uint256 timestamp, uint256 blockNumber);
    event PoolStateUpdated(bytes32 indexed poolId, uint256 virtualReserve, uint256 realReserve, uint256 tokenReserve, uint256 price, uint256 timestamp, uint256 blockNumber);
    event RouterTrade(uint8 indexed kind, address indexed pool, address indexed sender, address tokenIn, address tokenOut, uint256 amountIn, uint256 amountOut, uint256 intermediateUsdl, uint256 timestamp, uint256 blockNumber);
    event Swap(bytes32 indexed poolId, address indexed sender, bool isBuy, uint256 amountIn, uint256 amountOut, uint256 fee, uint256 price, uint256 timestamp, uint256 blockNumber);
    event TokenDeployed(address indexed token, address indexed pool, address indexed creator, bytes32 salt, string name, string symbol, uint8 decimals, uint256 totalSupply, uint256 timestamp, uint256 blockNumber);

    function DEFAULT_ADMIN_ROLE() external view returns (bytes32);
    function buy(address pool, uint256 usdlAmountIn, uint256 minTokensOut, uint256 deadline) external returns (uint256 amountOut);
    function buyWithPermit(address pool, uint256 usdlAmountIn, uint256 minTokensOut, uint256 deadline, PermitParams calldata permit) external returns (uint256 amountOut);
    function createMarket(string calldata name, string calldata symbol, uint8 feeStrategy, address optical) external returns (address tokenAddr, address poolAddr, uint256 nftId);
    function createMarketWithPermit(string calldata name, string calldata symbol, uint8 feeStrategy, address optical, PermitParams calldata permit) external returns (address tokenAddr, address poolAddr, uint256 nftId);
    function factory() external view returns (address);
    function getRoleAdmin(bytes32 role) external view returns (bytes32);
    function grantRole(bytes32 role, address account) external;
    function hasRole(bytes32 role, address account) external view returns (bool);
    function initialize(address _factory, address _poolRegistry, address _protocolConfig, address _usdlAddress, address _admin) external;
    function multicall(bytes[] calldata data) external returns (bytes[] memory results);
    function poolRegistry() external view returns (address);
    function protocolConfig() external view returns (address);
    function proxiableUUID() external view returns (bytes32);
    function registeredDefaultAdminCount() external view returns (uint256);
    function renounceRole(bytes32 role, address callerConfirmation) external;
    function revokeRole(bytes32 role, address account) external;
    function sell(address pool, uint256 tokenAmountIn, uint256 minUsdlOut, uint256 deadline) external returns (uint256 amountOut);
    function sellWithPermit(address pool, uint256 tokenAmountIn, uint256 minUsdlOut, uint256 deadline, PermitParams calldata permit) external returns (uint256 amountOut);
    function swapTokenForToken(address tokenIn, address tokenOut, uint256 amountIn, uint256 minAmountOut, uint256 deadline) external returns (uint256 amountOut, uint256 intermediateUsdl);
    function swapTokenForTokenWithPermit(address tokenIn, address tokenOut, uint256 amountIn, uint256 minAmountOut, uint256 deadline, PermitParams calldata permit) external returns (uint256 amountOut, uint256 intermediateUsdl);
    function upgradeToAndCall(address newImplementation, bytes calldata data) external payable;
    function usdlAddress() external view returns (address);
    function ROUTER_ROLE() external view returns (bytes32);
    function createMarketFor(address creator, string calldata name, string calldata symbol, uint8 feeStrategy, address optical) external returns (address tokenAddr, address poolAddr, uint256 nftId);
    function eventEmitter() external view returns (address);
    function feeAccumulator() external view returns (address);
    function getNonce(address creator) external view returns (uint256);
    function initialize(address _poolBeacon, address _nftContract, address _poolRegistry, address _eventEmitter, address _protocolConfig, address _treasury, address _feeAccumulator, address _usdlAddress, address _admin) external;
    function nftContract() external view returns (address);
    function poolBeacon() external view returns (address);
    function predictPoolAddress(address token, address creator, address optical) external view returns (address);
    function treasury() external view returns (address);
    function getAllPools(uint256 offset, uint256 limit) external view returns (address[] memory);
    function getMarketCap(address pool) external view returns (uint256);
    function getPoolPrice(address pool) external view returns (uint256);
    function getPoolStats(address pool) external view returns (PoolStats memory stats);
    function getPoolsByCreator(address creator) external view returns (address[] memory);
    function initialize(address _poolRegistry, address _protocolConfig, address _admin) external;
    function quoteExactInput(address pool, uint256 amountIn, bool isBuy) external view returns (QuoteResult memory result);
    function quoteMultihop(address tokenIn, address tokenOut, uint256 amountIn) external view returns (MultihopQuoteResult memory result);
    function FACTORY_ROLE() external view returns (bytes32);
    function getNftIdByPool(address pool) external view returns (uint256);
    function getPoolByToken(address token) external view returns (address);
    function getPoolCount() external view returns (uint256);
    function getPoolCountByCreator(address creator) external view returns (uint256);
    function getPoolMetadata(address pool) external view returns (PoolMetadata memory);
    function getPoolsByCreatorPage(address creator, uint256 offset, uint256 limit) external view returns (address[] memory result);
    function initialize(address _eventEmitter, address _admin) external;
    function isRegisteredPool(address pool) external view returns (bool);
    function register(address pool, address token, address creator, address optical, uint256 nftId) external;
    function claimAirdrop(uint256 nftId) external returns (uint256 amount);
    function claimAirdropEpoch(uint256 nftId, uint256 epoch) external returns (uint256 amount);
    function claimFees(uint256 nftId) external returns (uint256 amount);
    function executeAirdrop(uint256 nftId) external returns (uint256 amount);
    function executeBurn(uint256 nftId) external returns (uint256 amount);
    function executeLpRewards(uint256 nftId) external returns (uint256 amount);
    function initialize(address _nftContract, address _feeAccumulator, address _poolRegistry, address _admin) external;
    function setFeeStrategy(uint256 nftId, uint8 newStrategy) external;
    function DEAD_ADDRESS() external view returns (address);
    function FEES_ROUTER_ROLE() external view returns (bytes32);
    function OPTICAL_CLAIM_ROLE() external view returns (bytes32);
    function OPTICAL_GRANTER_ROLE() external view returns (bytes32);
    function POOL_ROLE() external view returns (bytes32);
    function accountingInitialized() external view returns (bool);
    function accountingMigrationCursor() external view returns (uint256);
    function airdropClaimedSupply(address, uint256) external view returns (uint256);
    function airdropEligibleSupply(address, uint256) external view returns (uint256);
    function airdropEpochRemaining(address, uint256) external view returns (uint256);
    function airdropSnapshotBlock(address, uint256) external view returns (uint256);
    function allocateLegacyAirdrop(address pool, uint256 epoch, bytes32 root, uint256 budget) external;
    function authorizeOptical(address optical) external;
    function authorizePool(address pool) external;
    function burn(address pool) external returns (uint256 amount);
    function claim(address pool, address recipient) external returns (uint256 amount);
    function claimAirdrop(address pool, address holder) external returns (uint256 amount);
    function claimAirdropEpoch(address pool, address holder, uint256 epoch) external returns (uint256 amount);
    function claimLegacyAirdrop(address pool, uint256 epoch, address holder, uint256 amount, bytes32[] calldata proof) external;
    function claimOpticalSurplus(address pool, uint256 amount, address recipient) external;
    function getAccumulatedFees(address pool) external view returns (uint256);
    function getAirdropBalance(address pool) external view returns (uint256);
    function getAirdropEpoch(address pool) external view returns (uint256);
    function getLpRewardsBalance(address pool) external view returns (uint256);
    function getOpticalSurplus(address pool) external view returns (uint256);
    function getProtocolFeesPending() external view returns (uint256);
    function hasClaimedAirdrop(address pool, address holder, uint256 epoch) external view returns (bool);
    function initialize(address _protocolConfig, address _treasury, address _poolRegistry, address _eventEmitter, address _usdlAddress, address _admin) external;
    function legacyAirdropRemaining(address, uint256) external view returns (uint256);
    function legacyAirdropRoot(address, uint256) external view returns (bytes32);
    function legacyAirdropUnallocated(address) external view returns (uint256);
    function migrateAccounting(uint256 maxPools) external;
    function migrateAdditionalAccounting(address[] calldata pools) external;
    function recordFee(address pool, uint256 feeAmount) external;
    function recordOpticalSurplus(address pool, uint256 amount) external;
    function sendLpRewards(address pool) external returns (uint256 amount);
    function supportsAirdrop(address pool) external view returns (bool);
    function totalLiabilities() external view returns (uint256);
    function triggerAirdrop(address pool) external returns (uint256 totalAmount);
    function baseFeeBps() external view returns (uint256);
    function concentrationWeight() external view returns (uint256);
    function creationFee() external view returns (uint256);
    function feeDecayRate() external view returns (uint256);
    function maxFeeBps() external view returns (uint256);
    function minFeeBps() external view returns (uint256);
    function protocolFeeBps() external view returns (uint256);
    function setBaseFeeBps(uint256 newBaseFeeBps) external;
    function setCreationFee(uint256 newCreationFee) external;
    function setFeeWeights(uint256 newDecayRate, uint256 newVolWeight, uint256 newConcWeight) external;
    function setProtocolFeeBps(uint256 newProtocolFeeBps) external;
    function setVirtualDefaults(uint256 newVirtualUsdl, uint256 newVirtualToken) external;
    function virtualTokenDefault() external view returns (uint256);
    function virtualUsdlDefault() external view returns (uint256);
    function volatilityWeight() external view returns (uint256);
}
