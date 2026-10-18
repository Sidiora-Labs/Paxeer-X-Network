// SPDX-License-Identifier: LicenseRef-Paxlabs-HyperPax-OS-Protocol
pragma solidity ^0.8.27;

/**
 * @title MockFeeOnTransferERC20
 * @notice ERC20 variant that siphons a configurable basis-point fee on every
 *         transfer, used to prove {PECORVault.pullTokens} returns the real
 *         received amount (spec §7.5 fee-on-transfer safety, S5 regression).
 * @dev The fee is burned — the contract never needs a collector. A 0-bps
 *      setting reduces the mock to a normal ERC20.
 */
contract MockFeeOnTransferERC20 {
    uint256 public constant BPS_DENOMINATOR = 10_000;

    string public name;
    string public symbol;
    uint8 public decimals;
    uint256 public totalSupply;
    uint256 public feeBps;
    address public taxedRecipient;

    /// @dev Zero taxes every recipient; nonzero selects one custody boundary.
    function setTaxedRecipient(address recipient) external { taxedRecipient = recipient; }

    mapping(address => uint256) public balanceOf;
    mapping(address => mapping(address => uint256)) public allowance;

    event Transfer(address indexed from, address indexed to, uint256 value);
    event Approval(address indexed owner, address indexed spender, uint256 value);
    event FeeUpdated(uint256 bps);

    constructor(string memory name_, string memory symbol_, uint8 decimals_, uint256 feeBps_) {
        require(feeBps_ < BPS_DENOMINATOR, "MockFeeOnTransferERC20: fee too high");
        name = name_;
        symbol = symbol_;
        decimals = decimals_;
        feeBps = feeBps_;
    }

    function setFeeBps(uint256 bps) external {
        require(bps < BPS_DENOMINATOR, "MockFeeOnTransferERC20: fee too high");
        feeBps = bps;
        emit FeeUpdated(bps);
    }

    function mint(address to, uint256 amount) external {
        totalSupply += amount;
        balanceOf[to] += amount;
        emit Transfer(address(0), to, amount);
    }

    function approve(address spender, uint256 amount) external returns (bool) {
        allowance[msg.sender][spender] = amount;
        emit Approval(msg.sender, spender, amount);
        return true;
    }

    function transfer(address to, uint256 amount) external returns (bool) {
        _transferWithFee(msg.sender, to, amount);
        return true;
    }

    function transferFrom(address from, address to, uint256 amount) external returns (bool) {
        uint256 current = allowance[from][msg.sender];
        require(current >= amount, "MockFeeOnTransferERC20: insufficient allowance");
        if (current != type(uint256).max) {
            allowance[from][msg.sender] = current - amount;
        }
        _transferWithFee(from, to, amount);
        return true;
    }

    function _transferWithFee(address from, address to, uint256 amount) internal {
        require(balanceOf[from] >= amount, "MockFeeOnTransferERC20: insufficient balance");
        uint256 fee = taxedRecipient == address(0) || to == taxedRecipient ? (amount * feeBps) / BPS_DENOMINATOR : 0;
        uint256 netAmount = amount - fee;

        balanceOf[from] -= amount;
        if (fee > 0) {
            totalSupply -= fee;
            emit Transfer(from, address(0), fee);
        }
        balanceOf[to] += netAmount;
        emit Transfer(from, to, netAmount);
    }
}
