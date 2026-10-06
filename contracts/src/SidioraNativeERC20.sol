// SPDX-License-Identifier: MIT
pragma solidity ^0.8.27;

import {ERC20} from "@openzeppelin/contracts/token/ERC20/ERC20.sol";
import {UUPSUpgradeable} from "@openzeppelin/contracts/proxy/utils/UUPSUpgradeable.sol";
import {BANK_CONTRACT} from "./precompiles/IBank.sol";

/// @dev Proxy state lives only in the ERC-7201 namespace paxeer.storage.SidioraNativeERC20.
/// Offsets: 0 initialized, 1 denom, 2 name, 3 symbol, 4 decimals, 5 allowances.
/// Inherited ERC20 storage is never read or written through the proxy. Legacy slots,
/// including balances and allowances, remain untouched; allowances here start empty.
/// The upgrade must initialize atomically and confirm this namespace is unused in
/// the deployed implementation's layout, which is not supplied with this contract.
/// The deployed proxy is UUPS over OpenZeppelin v5 upgradeable storage; its owner stays in
/// the ERC-7201 slot openzeppelin.storage.Ownable and is the only account that can upgrade.
contract SidioraNativeERC20 is ERC20, UUPSUpgradeable {
    error AlreadyInitialized();
    error NotInitialized();
    error BankTransferFailed();
    error UnauthorizedUpgrade(address caller);
    error NothingToMigrate(address holder);
    error MigrationNotRecorded();

    event LegacyMigrated(address indexed holder, uint256 amount, bool minted);

    /// @dev keccak256(abi.encode(uint256(keccak256("openzeppelin.storage.Ownable")) - 1)) & ~bytes32(uint256(0xff))
    bytes32 private constant OWNABLE_STORAGE = 0x9016d09d72d40fdae2fd8ceac6b6234c7706214fd39c1cd1e609a0528c199300;
    /// @dev keccak256(abi.encode(uint256(keccak256("openzeppelin.storage.ERC20")) - 1)) & ~bytes32(uint256(0xff))
    bytes32 private constant LEGACY_ERC20_STORAGE = 0x52c63247e1f47db19d5ce0460030c497f067ca4cebf71ba98eeadabe20bace00;
    /// @dev keccak256(abi.encode(uint256(keccak256("paxeer.storage.SidioraLegacyMigration")) - 1)) & ~bytes32(uint256(0xff));
    /// offset 0 holds the v6.11 migration height, offset 1 the unminted shortfall. Written by the chain only.
    bytes32 private constant LEGACY_MIGRATION_STORAGE = keccak256(
        abi.encode(uint256(keccak256("paxeer.storage.SidioraLegacyMigration")) - 1)
    ) & ~bytes32(uint256(0xff));

    struct NativeStorage {
        bool initialized;
        string denom;
        string name;
        string symbol;
        uint8 decimals;
        mapping(address => mapping(address => uint256)) allowances;
    }

    constructor() ERC20("", "") {
        _nativeStorage().initialized = true;
    }

    function initialize() external {
        NativeStorage storage state = _nativeStorage();
        if (state.initialized) revert AlreadyInitialized();
        state.initialized = true;
        state.denom = _sidioraDenom();
        state.name = "Sidiora";
        state.symbol = "SID";
        state.decimals = 6;
    }

    function owner() public view returns (address account) {
        bytes32 slot = OWNABLE_STORAGE;
        assembly {
            account := sload(slot)
        }
    }

    function _authorizeUpgrade(address) internal view override {
        if (msg.sender != owner()) revert UnauthorizedUpgrade(msg.sender);
    }

    /// @notice Clears a legacy ERC-20 balance slot the v6.11 upgrade left behind. usid is minted only by
    /// chain modules; minted reports whether the holder's bank balance already covers the legacy amount.
    function migrateLegacy(address holder) external {
        bytes32 heightSlot = LEGACY_MIGRATION_STORAGE;
        uint256 height;
        assembly {
            height := sload(heightSlot)
        }
        if (height == 0) revert MigrationNotRecorded();
        bytes32 slot = keccak256(abi.encode(holder, LEGACY_ERC20_STORAGE));
        uint256 amount;
        assembly {
            amount := sload(slot)
        }
        if (amount == 0) revert NothingToMigrate(holder);
        assembly {
            sstore(slot, 0)
        }
        emit LegacyMigrated(holder, amount, balanceOf(holder) >= amount);
    }

    function denom() public view returns (string memory) {
        return _initializedStorage().denom;
    }

    function name() public view override returns (string memory) {
        return _initializedStorage().name;
    }

    function symbol() public view override returns (string memory) {
        return _initializedStorage().symbol;
    }

    function decimals() public view override returns (uint8) {
        return _initializedStorage().decimals;
    }

    function balanceOf(address account) public view override returns (uint256) {
        return BANK_CONTRACT.balance(account, denom());
    }

    function totalSupply() public view override returns (uint256) {
        return BANK_CONTRACT.supply(denom());
    }

    function allowance(address owner, address spender) public view override returns (uint256) {
        return _initializedStorage().allowances[owner][spender];
    }

    function _approve(address owner, address spender, uint256 value, bool emitEvent) internal override {
        if (owner == address(0)) revert ERC20InvalidApprover(address(0));
        if (spender == address(0)) revert ERC20InvalidSpender(address(0));
        _initializedStorage().allowances[owner][spender] = value;
        if (emitEvent) emit Approval(owner, spender, value);
    }

    function _update(address from, address to, uint256 value) internal override {
        string memory nativeDenom = denom();
        try BANK_CONTRACT.send(from, to, nativeDenom, value) returns (bool success) {
            if (!success) revert BankTransferFailed();
        } catch {
            revert BankTransferFailed();
        }
        emit Transfer(from, to, value);
    }

    function _initializedStorage() private view returns (NativeStorage storage state) {
        state = _nativeStorage();
        if (!state.initialized) revert NotInitialized();
    }

    function _nativeStorage() private pure returns (NativeStorage storage state) {
        bytes32 slot = keccak256(abi.encode(uint256(keccak256("paxeer.storage.SidioraNativeERC20")) - 1))
            & ~bytes32(uint256(0xff));
        assembly {
            state.slot := slot
        }
    }

    /// @dev Matches layerxbridge/types.SidioraDenom: factory/{ModuleAddress()}/usid.
    /// ModuleAddress is the first 20 bytes of SHA-256("layerxbridge"), Bech32 encoded
    /// with the SDK account prefix pax. No EVM address association is involved.
    function _sidioraDenom() private pure returns (string memory) {
        bytes memory alphabet = "qpzry9x8gf2tvdw0s3jn54khce6mua7l";
        bytes memory creator = new bytes(42);
        creator[0] = "p";
        creator[1] = "a";
        creator[2] = "x";
        creator[3] = "1";
        uint256 checksum = 1;
        bytes memory expandedPrefix = hex"03030300100118";
        for (uint256 i; i < expandedPrefix.length; ++i) {
            checksum = _polymod(checksum, uint8(expandedPrefix[i]));
        }
        uint160 moduleAddress = uint160(bytes20(sha256("layerxbridge")));
        for (uint256 i; i < 32; ++i) {
            uint256 digit = (uint256(moduleAddress) >> (155 - 5 * i)) & 31;
            creator[4 + i] = alphabet[digit];
            checksum = _polymod(checksum, digit);
        }
        for (uint256 i; i < 6; ++i) {
            checksum = _polymod(checksum, 0);
        }
        checksum ^= 1;
        for (uint256 i; i < 6; ++i) {
            creator[36 + i] = alphabet[(checksum >> (5 * (5 - i))) & 31];
        }
        return string.concat("factory/", string(creator), "/usid");
    }

    function _polymod(uint256 checksum, uint256 value) private pure returns (uint256) {
        uint256 top = checksum >> 25;
        checksum = ((checksum & 0x1ffffff) << 5) ^ value;
        uint256[5] memory generators = [uint256(0x3b6a57b2), 0x26508e6d, 0x1ea119fa, 0x3d4233dd, 0x2a1462b3];
        for (uint256 i; i < 5; ++i) {
            if ((top >> i) & 1 != 0) checksum ^= generators[i];
        }
        return checksum;
    }
}
