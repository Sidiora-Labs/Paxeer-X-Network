// SPDX-License-Identifier: LicenseRef-Paxlabs-HyperPax-OS-Protocol
pragma solidity ^0.8.24;

import "../interfaces/IEventEmitter.sol";

/// @notice Simplified mock EventEmitter for testing contracts that emit events through it
contract MockEventEmitter {
    // Track calls for verification
    bytes32 public lastConfigKey;
    uint256 public lastConfigOldValue;
    uint256 public lastConfigNewValue;
    uint256 public configUpdateCount;

    mapping(address => bool) public authorizedEmitters;

    event ConfigUpdated(bytes32 indexed key, uint256 oldValue, uint256 newValue, uint256 timestamp, uint256 blockNumber);
    event MarketCreated(bytes32 indexed poolId, address indexed token, address indexed creator, address pool, address optical, uint256 timestamp, uint256 blockNumber);
    event Swap(bytes32 indexed poolId, address indexed sender, bool isBuy, uint256 amountIn, uint256 amountOut, uint256 fee, uint256 price, uint256 timestamp, uint256 blockNumber);
    event FeeRecorded(bytes32 indexed poolId, uint256 feeAmount, uint256 protocolCut, uint256 poolCut, uint256 timestamp, uint256 blockNumber);
    event FeeDistributed(bytes32 indexed poolId, uint256 nftId, uint8 strategy, uint256 amount, address recipient, uint256 timestamp, uint256 blockNumber);
    event FeeStrategyChanged(bytes32 indexed poolId, uint256 nftId, uint8 oldStrategy, uint8 newStrategy, uint256 timestamp, uint256 blockNumber);
    event PoolStateUpdated(bytes32 indexed poolId, uint256 virtualReserve, uint256 realReserve, uint256 tokenReserve, uint256 price, uint256 timestamp, uint256 blockNumber);

    function setAuthorizedEmitter(address emitter, bool authorized) external {
        authorizedEmitters[emitter] = authorized;
    }

    function isAuthorizedEmitter(address emitter) external view returns (bool) {
        return authorizedEmitters[emitter];
    }

    function emitConfigUpdated(bytes32 key, uint256 oldValue, uint256 newValue) external {
        lastConfigKey = key;
        lastConfigOldValue = oldValue;
        lastConfigNewValue = newValue;
        configUpdateCount++;
        emit ConfigUpdated(key, oldValue, newValue, block.timestamp, block.number);
    }

    function emitMarketCreated(bytes32 poolId, address token, address creator, address pool, address optical) external {
        emit MarketCreated(poolId, token, creator, pool, optical, block.timestamp, block.number);
    }

    function emitSwap(bytes32 poolId, address sender, bool isBuy, uint256 amountIn, uint256 amountOut, uint256 fee, uint256 price) external {
        emit Swap(poolId, sender, isBuy, amountIn, amountOut, fee, price, block.timestamp, block.number);
    }

    function emitFeeRecorded(bytes32 poolId, uint256 feeAmount, uint256 protocolCut, uint256 poolCut) external {
        emit FeeRecorded(poolId, feeAmount, protocolCut, poolCut, block.timestamp, block.number);
    }

    function emitFeeDistributed(bytes32 poolId, uint256 nftId, uint8 strategy, uint256 amount, address recipient) external {
        emit FeeDistributed(poolId, nftId, strategy, amount, recipient, block.timestamp, block.number);
    }

    function emitFeeStrategyChanged(bytes32 poolId, uint256 nftId, uint8 oldStrategy, uint8 newStrategy) external {
        emit FeeStrategyChanged(poolId, nftId, oldStrategy, newStrategy, block.timestamp, block.number);
    }

    function emitPoolStateUpdated(bytes32 poolId, uint256 virtualReserve, uint256 realReserve, uint256 tokenReserve, uint256 price) external {
        emit PoolStateUpdated(poolId, virtualReserve, realReserve, tokenReserve, price, block.timestamp, block.number);
    }

    event NftMint(uint256 indexed tokenId, address indexed creator, address indexed pool, uint8 strategy, uint256 timestamp, uint256 blockNumber);
    event NftTransfer(address indexed nft, address indexed from, address indexed to, uint256 tokenId, uint256 timestamp, uint256 blockNumber);
    event AssetApproval(address indexed asset, address indexed owner, address indexed spender, uint256 valueOrTokenId, bool isNft, uint256 timestamp, uint256 blockNumber);
    event TokenDeployed(address indexed token, address indexed pool, address indexed creator, bytes32 salt, string name, string symbol, uint8 decimals, uint256 totalSupply, uint256 timestamp, uint256 blockNumber);
    event RouterTrade(uint8 indexed kind, address indexed pool, address indexed sender, address tokenIn, address tokenOut, uint256 amountIn, uint256 amountOut, uint256 intermediateUsdl, uint256 timestamp, uint256 blockNumber);
    event FeeFlow(uint8 indexed kind, address indexed pool, address indexed party, uint256 amount, uint256 protocolCut, uint256 poolCut, uint256 epoch, uint256 timestamp, uint256 blockNumber);
    event PoolRegistered(address indexed pool, address indexed token, address indexed creator, address optical, uint256 nftId, uint256 timestamp, uint256 blockNumber);
    function emitNftMint(uint256 id, address creator, address pool, uint8 strategy) external { emit NftMint(id, creator, pool, strategy, block.timestamp, block.number); }
    function emitNftTransfer(address nft, address from, address to, uint256 id) external { emit NftTransfer(nft, from, to, id, block.timestamp, block.number); }
    function emitAssetApproval(address asset, address owner, address spender, uint256 id, bool nft) external { emit AssetApproval(asset, owner, spender, id, nft, block.timestamp, block.number); }
    function emitTokenDeployed(address token, address pool, address creator, bytes32 salt, string calldata name, string calldata symbol, uint8 decimals, uint256 supply) external { emit TokenDeployed(token, pool, creator, salt, name, symbol, decimals, supply, block.timestamp, block.number); }
    function emitRouterTrade(uint8 kind, address pool, address sender, address tokenIn, address tokenOut, uint256 amountIn, uint256 amountOut, uint256 middle) external { emit RouterTrade(kind, pool, sender, tokenIn, tokenOut, amountIn, amountOut, middle, block.timestamp, block.number); }
    function emitFeeFlow(uint8 kind, address pool, address party, uint256 amount, uint256 protocolCut, uint256 poolCut, uint256 epoch) external { emit FeeFlow(kind, pool, party, amount, protocolCut, poolCut, epoch, block.timestamp, block.number); }
    function emitPoolRegistered(address pool, address token, address creator, address optical, uint256 id) external { emit PoolRegistered(pool, token, creator, optical, id, block.timestamp, block.number); }
}
