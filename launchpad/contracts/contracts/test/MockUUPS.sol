// SPDX-License-Identifier: LicenseRef-Paxlabs-HyperPax-OS-Protocol
pragma solidity ^0.8.24;

import "../base/UUPSUpgradeable.sol";
import "../base/Proxy.sol";
import "../base/ERC1967Utils.sol";

contract MockUUPS is UUPSUpgradeable {
    uint256 public value;
    address public upgrader;

    function initialize(address _upgrader) external initializer {
        upgrader = _upgrader;
    }

    function setValue(uint256 _val) external {
        value = _val;
    }

    function version() external pure returns (uint256) {
        return 1;
    }

    function _authorizeUpgrade(address) internal view override {
        if (msg.sender != upgrader) revert UnauthorizedUpgrade();
    }
}

contract MockUUPSV2 is UUPSUpgradeable {
    uint256 public value;
    address public upgrader;
    uint256 public extra;

    function setValue(uint256 _val) external {
        value = _val;
    }

    function setExtra(uint256 _val) external {
        extra = _val;
    }

    function version() external pure returns (uint256) {
        return 2;
    }

    function _authorizeUpgrade(address) internal view override {
        if (msg.sender != upgrader) revert UnauthorizedUpgrade();
    }
}

/// @dev Simple ERC1967 proxy for UUPS testing
contract UUPSProxy is Proxy {
    constructor(address implementation, bytes memory data) {
        ERC1967Utils.setImplementation(implementation);
        if (data.length > 0) {
            (bool success, ) = implementation.delegatecall(data);
            require(success, "init failed");
        }
    }

    function _implementation() internal view override returns (address) {
        return ERC1967Utils.getImplementation();
    }
}
