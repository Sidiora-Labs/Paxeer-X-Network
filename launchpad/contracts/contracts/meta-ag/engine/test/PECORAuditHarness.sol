// SPDX-License-Identifier: LicenseRef-Paxlabs-HyperPax-OS-Protocol
pragma solidity ^0.8.27;
import {PECORMath} from "../PECORMath.sol";

contract PECORAuditOracle {
    mapping(address => uint256) public prices;
    mapping(address => bool) public unavailable;
    function setPrice(address token, uint256 price) external { prices[token] = price; }
    function setUnavailable(address token, bool value) external { unavailable[token] = value; }
    function getPrice(address token) external view returns (uint256) {
        require(!unavailable[token] && prices[token] != 0, "PRICE_UNAVAILABLE");
        return prices[token];
    }
}

contract PECORAuditTracker {
    fallback() external { revert("ANALYTICS_UNAVAILABLE"); }
}

contract PECORMathHarness {
    function convert(uint256 amount, uint256 inputPrice, uint256 outputPrice, uint8 inputDecimals, uint8 outputDecimals, bool roundUp)
        external pure returns (uint256)
    { return PECORMath.convert(amount, inputPrice, outputPrice, inputDecimals, outputDecimals, roundUp); }
}

/// @dev Raw uint256 metadata deliberately exercises ABI truncation cases.
contract PECORAuditMetadataToken {
    uint256 public decimals;
    uint256 public senderSurchargeBps;
    mapping(address => uint256) public balanceOf;
    mapping(address => mapping(address => uint256)) public allowance;
    constructor(uint256 decimals_) { decimals = decimals_; }
    function mint(address user, uint256 amount) external { balanceOf[user] += amount; }
    function burn(address user, uint256 amount) external { balanceOf[user] -= amount; }
    function setSenderSurcharge(uint256 bps) external { senderSurchargeBps = bps; }
    function approve(address spender, uint256 amount) external returns (bool) { allowance[msg.sender][spender] = amount; return true; }
    function transfer(address to, uint256 amount) external returns (bool) { _transfer(msg.sender, to, amount); return true; }
    function transferFrom(address from, address to, uint256 amount) external returns (bool) {
        allowance[from][msg.sender] -= amount; _transfer(from, to, amount); return true;
    }
    function _transfer(address from, address to, uint256 amount) internal {
        balanceOf[from] -= amount + amount * senderSurchargeBps / 10000;
        balanceOf[to] += amount;
    }
}
