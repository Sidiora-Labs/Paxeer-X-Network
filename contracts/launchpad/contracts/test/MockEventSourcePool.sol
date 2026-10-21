// SPDX-License-Identifier: LicenseRef-Paxlabs-HyperPax-OS-Protocol
pragma solidity ^0.8.24;
import "../interfaces/IEventEmitter.sol";
/// @dev Auth regression fixture: a registered contract, not an EOA stand-in.
contract MockEventSourcePool {
    function mirrorSwap(address hub, bytes32 id) external {
        IEventEmitter(hub).emitSwap(id, msg.sender, true, 1, 2, 0, 2);
    }
}
