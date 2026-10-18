// SPDX-License-Identifier: LicenseRef-Paxlabs-HyperPax-OS-Protocol
pragma solidity ^0.8.24;

import "../base/Initializable.sol";
import "../base/UUPSUpgradeable.sol";
import "../base/AccessControl.sol";
import "../interfaces/ISidioraFactory.sol";
import "../interfaces/IProtocolConfig.sol";
import "../interfaces/IPoolRegistry.sol";
import "../interfaces/ISidioraNFT.sol";
import "../interfaces/IEventEmitter.sol";
import "../interfaces/IFeeAccumulator.sol";
import "../interfaces/ISidioraPool.sol";
import "../libraries/TransferHelper.sol";
import "../interfaces/ITreasury.sol";
import "./SidioraERC20.sol";
import "../libraries/PoolAddress.sol";

/// @title SidioraFactory
/// @notice Market creation orchestrator. One transaction creates token + pool + NFT.
/// @dev UUPS proxy. CREATE2 deploys SidioraERC20, BeaconProxy deploys SidioraPool.
contract SidioraFactory is ISidioraFactory, Initializable, UUPSUpgradeable, AccessControl {
    bytes32 public constant ROUTER_ROLE = keccak256("ROUTER_ROLE");

    address public override poolBeacon;
    address public override nftContract;
    address public override poolRegistry;
    address public override eventEmitter;
    address public override protocolConfig;
    address public override treasury;
    address public feeAccumulator;
    address public usdlAddress;

    mapping(address => uint256) private _nonces;

    /// @custom:oz-upgrades-unsafe-allow constructor
    constructor() {
        _disableInitializers();
    }

    function initialize(
        address _poolBeacon,
        address _nftContract,
        address _poolRegistry,
        address _eventEmitter,
        address _protocolConfig,
        address _treasury,
        address _feeAccumulator,
        address _usdlAddress,
        address _admin
    ) external initializer {
        if (_admin == address(0)) revert ZeroAddress();
        if (_poolBeacon == address(0)) revert ZeroAddress();
        if (_usdlAddress.code.length == 0 || _poolBeacon.code.length == 0 ||
            _nftContract.code.length == 0 || _poolRegistry.code.length == 0 ||
            _protocolConfig.code.length == 0 || _treasury.code.length == 0 ||
            _feeAccumulator.code.length == 0) revert ZeroAddress();

        poolBeacon = _poolBeacon;
        nftContract = _nftContract;
        poolRegistry = _poolRegistry;
        eventEmitter = _eventEmitter;
        protocolConfig = _protocolConfig;
        treasury = _treasury;
        feeAccumulator = _feeAccumulator;
        usdlAddress = _usdlAddress;

        _grantRole(DEFAULT_ADMIN_ROLE, _admin);
    }

    /// @inheritdoc ISidioraFactory
    function createMarket(
        string calldata name,
        string calldata symbol,
        uint8 feeStrategy,
        address optical
    ) external override returns (address tokenAddr, address poolAddr, uint256 nftId) {
        // Charge creation fee from caller
        uint256 creationFee = IProtocolConfig(protocolConfig).creationFee();
        if (creationFee > 0) {
            TransferHelper.safeTransferFrom(usdlAddress, msg.sender, treasury, creationFee);
            ITreasury(treasury).recordDeposit(usdlAddress, creationFee);
        }
        return _createMarket(msg.sender, name, symbol, feeStrategy, optical);
    }

    /// @inheritdoc ISidioraFactory
    function createMarketFor(
        address creator,
        string calldata name,
        string calldata symbol,
        uint8 feeStrategy,
        address optical
    ) external override onlyRole(ROUTER_ROLE) returns (address tokenAddr, address poolAddr, uint256 nftId) {
        if (creator == address(0)) revert ZeroAddress();
        return _createMarket(creator, name, symbol, feeStrategy, optical);
    }

    /// @inheritdoc ISidioraFactory
    function getNonce(address creator) external view override returns (uint256) {
        return _nonces[creator];
    }

    function _createMarket(
        address creator,
        string calldata name,
        string calldata symbol,
        uint8 feeStrategy,
        address optical
    ) internal returns (address tokenAddr, address poolAddr, uint256 nftId) {
        IProtocolConfig config = IProtocolConfig(protocolConfig);
        uint256 virtualUsdl = config.virtualUsdlDefault();
        uint256 virtualToken = config.virtualTokenDefault();

        if (virtualUsdl == 0 || virtualToken == 0 || virtualUsdl > type(uint128).max ||
            virtualToken > type(uint128).max || feeStrategy > 3 ||
            (optical != address(0) && optical.code.length == 0)) revert ZeroAddress();

        // CREATE2 deploy SidioraERC20
        uint256 nonce = _nonces[creator]++;
        bytes32 salt = keccak256(abi.encodePacked(creator, name, symbol, nonce));

        tokenAddr = address(
            new SidioraERC20{salt: salt}(name, symbol, virtualToken, address(this))
        );

        // Deploy BeaconProxy for SidioraPool
        bytes memory poolInitData = abi.encodeCall(
            ISidioraPool.initialize,
            (
                tokenAddr,
                usdlAddress,
                optical,
                feeAccumulator,
                eventEmitter,
                protocolConfig,
                creator, // creator is guardian initially
                virtualUsdl,
                virtualToken
            )
        );

        bytes32 poolSalt = keccak256(abi.encodePacked(tokenAddr));
        poolAddr = _deployBeaconProxy(poolSalt, poolInitData);

        // Authorize pool on FeeAccumulator to record fees
        IFeeAccumulator(feeAccumulator).authorizePool(poolAddr);

        // Mint fee rights, then publish authoritative registry attachment before
        // the token's transfer-policy callback observes initial liquidity.
        nftId = ISidioraNFT(nftContract).mint(creator, poolAddr, feeStrategy);
        IPoolRegistry(poolRegistry).register(poolAddr, tokenAddr, creator, optical, nftId);
        SidioraERC20(tokenAddr).bindPool(poolAddr);
        TransferHelper.safeTransfer(tokenAddr, poolAddr, virtualToken);

        // Emit MarketCreated via EventEmitter
        if (eventEmitter != address(0)) {
            bytes32 poolId = bytes32(uint256(uint160(poolAddr)));
            IEventEmitter(eventEmitter).emitTokenDeployed(tokenAddr, poolAddr, creator, salt, name, symbol, 6, virtualToken);
            IEventEmitter(eventEmitter).emitMarketCreated(poolId, tokenAddr, creator, poolAddr, optical);
        }

        emit MarketCreated(tokenAddr, poolAddr, creator, nftId, optical);
    }

    /// @notice Predict the actual pool deployment from exact initialization calldata.
    function predictPoolAddress(address token, address creator, address optical) external view returns (address) {
        bytes memory initData = abi.encodeCall(ISidioraPool.initialize, (
            token, usdlAddress, optical, feeAccumulator, eventEmitter, protocolConfig, creator,
            IProtocolConfig(protocolConfig).virtualUsdlDefault(), IProtocolConfig(protocolConfig).virtualTokenDefault()
        ));
        return PoolAddress.computeAddress(address(this), poolBeacon, token, type(BeaconProxyDeployer).creationCode, initData);
    }

    function _deployBeaconProxy(bytes32 salt, bytes memory initData) internal returns (address proxy) {
        bytes memory creationCode = abi.encodePacked(
            type(BeaconProxyDeployer).creationCode,
            abi.encode(poolBeacon, initData)
        );
        assembly {
            proxy := create2(0, add(creationCode, 0x20), mload(creationCode), salt)
            if iszero(proxy) { revert(0, 0) }
        }
    }

    function _authorizeUpgrade(address) internal override onlyRole(DEFAULT_ADMIN_ROLE) {}
}

/// @title BeaconProxyDeployer
/// @dev Minimal beacon proxy for CREATE2 deployment by factory.
///      Reads implementation from beacon, delegates all calls.
contract BeaconProxyDeployer {
    error BeaconCallFailed();

    constructor(address beacon, bytes memory data) {
        // Store beacon in ERC1967 slot
        bytes32 slot = 0xa3f0ad74e5423aebfd80d3ef4346578335a9a72aeaee59ff6cb3582b35133d50;
        assembly { sstore(slot, beacon) }

        // Initialize via delegatecall
        address impl = _getImpl(beacon);
        if (data.length > 0) {
            (bool ok, ) = impl.delegatecall(data);
            require(ok, "init failed");
        }
    }

    fallback() external payable {
        address impl = _getImpl(_beacon());
        assembly {
            calldatacopy(0, 0, calldatasize())
            let result := delegatecall(gas(), impl, 0, calldatasize(), 0, 0)
            returndatacopy(0, 0, returndatasize())
            switch result
            case 0 { revert(0, returndatasize()) }
            default { return(0, returndatasize()) }
        }
    }

    receive() external payable {
        address impl = _getImpl(_beacon());
        assembly {
            let result := delegatecall(gas(), impl, 0, 0, 0, 0)
            returndatacopy(0, 0, returndatasize())
            switch result
            case 0 { revert(0, returndatasize()) }
            default { return(0, returndatasize()) }
        }
    }

    function _beacon() internal view returns (address b) {
        bytes32 slot = 0xa3f0ad74e5423aebfd80d3ef4346578335a9a72aeaee59ff6cb3582b35133d50;
        assembly { b := sload(slot) }
    }

    function _getImpl(address beacon) internal view returns (address impl) {
        (bool ok, bytes memory data) = beacon.staticcall(abi.encodeWithSignature("implementation()"));
        if (!ok || data.length < 32) revert BeaconCallFailed();
        impl = abi.decode(data, (address));
        if (impl.code.length == 0) revert BeaconCallFailed();
    }
}
