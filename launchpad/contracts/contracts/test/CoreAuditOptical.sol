// SPDX-License-Identifier: LicenseRef-Paxlabs-HyperPax-OS-Protocol
pragma solidity ^0.8.24;

/// @dev Adversarial hook used only by audit remediation regressions.
contract CoreAuditOptical {
    int256 public delta;
    bool public reenterSync;
    bool public wrongSelector;
    address public lastTrader;
    function configure(int256 delta_, bool reenter_, bool wrong_) external {
        delta = delta_; reenterSync = reenter_; wrongSelector = wrong_;
    }
    function getFlags() external pure returns (uint8) { return 3; }
    function beforeSwap(address pool, address trader, bool, uint256) external returns (bool, int256) {
        lastTrader = trader;
        if (reenterSync) {
            (bool success, bytes memory result) = pool.call(abi.encodeWithSignature("syncReserves()"));
            if (!success) assembly { revert(add(result, 32), mload(result)) }
        }
        return (true, delta);
    }
    function afterSwap(address, address trader, bool, uint256, uint256) external returns (bytes4) {
        lastTrader = trader;
        return wrongSelector ? bytes4(0) : this.afterSwap.selector;
    }
}

/// @dev Deliberately malformed ERC20 balance response; installed only inside a local test.
contract CoreAuditMalformedBalance {
    fallback() external { assembly { return(0, 1) } }
}
