// SPDX-License-Identifier: LicenseRef-Paxlabs-HyperPax-OS-Protocol
pragma solidity ^0.8.24;

import "../base/Initializable.sol";

contract MockInitializable is Initializable {
    uint256 public value;
    bool public initialized;

    function initialize(uint256 _value) external initializer {
        value = _value;
        initialized = true;
    }

    function reinitialize2(uint256 _value) external reinitializer(2) {
        value = _value;
    }

    function reinitialize3(uint256 _value) external reinitializer(3) {
        value = _value;
    }

    function getInitializedVersion() external view returns (uint64) {
        return _getInitializedVersion();
    }

    function isInitializing() external view returns (bool) {
        return _isInitializing();
    }
}

contract MockInitializableDisabled is Initializable {
    uint256 public value;

    constructor() {
        _disableInitializers();
    }

    function initialize(uint256 _value) external initializer {
        value = _value;
    }
}

contract MockInitializableChild is Initializable {
    uint256 public childValue;
    uint256 public parentValue;

    function initialize(uint256 _parent, uint256 _child) external initializer {
        _initParent(_parent);
        childValue = _child;
    }

    function _initParent(uint256 _val) internal onlyInitializing {
        parentValue = _val;
    }
}
