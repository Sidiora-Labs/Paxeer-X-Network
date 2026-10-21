// SPDX-License-Identifier: LicenseRef-Paxlabs-HyperPax-OS-Protocol
pragma solidity ^0.8.24;

import "../base/Proxy.sol";
import "../base/ERC1967Utils.sol";

contract MockERC1967Proxy is Proxy {
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

contract MockImplementation {
    uint256 public value;

    function setValue(uint256 _value) external {
        value = _value;
    }

    function getValue() external view returns (uint256) {
        return value;
    }
}

contract MockImplementationV2 {
    uint256 public value;
    uint256 public extra;

    function setValue(uint256 _value) external {
        value = _value;
    }

    function getValue() external view returns (uint256) {
        return value;
    }

    function setExtra(uint256 _extra) external {
        extra = _extra;
    }

    function version() external pure returns (uint256) {
        return 2;
    }
}

contract ERC1967UtilsWrapper {
    function getImplementation() external view returns (address) {
        return ERC1967Utils.getImplementation();
    }

    function setImplementation(address impl) external {
        ERC1967Utils.setImplementation(impl);
    }

    function getBeacon() external view returns (address) {
        return ERC1967Utils.getBeacon();
    }

    function setBeacon(address beacon) external {
        ERC1967Utils.setBeacon(beacon);
    }

    function getAdmin() external view returns (address) {
        return ERC1967Utils.getAdmin();
    }

    function setAdmin(address admin) external {
        ERC1967Utils.setAdmin(admin);
    }
}
