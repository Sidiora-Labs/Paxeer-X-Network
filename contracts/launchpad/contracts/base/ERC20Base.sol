// SPDX-License-Identifier: LicenseRef-Paxlabs-HyperPax-OS-Protocol
pragma solidity ^0.8.24;

/// @title ERC20Base
/// @notice Minimal ERC20 implementation with EIP-2612 permit support
/// @dev Includes transfer, approve, transferFrom, permit, DOMAIN_SEPARATOR
abstract contract ERC20Base {
    string public name;
    string public symbol;
    uint8 public immutable decimals;
    uint256 public totalSupply;

    mapping(address => uint256) public balanceOf;
    mapping(address => mapping(address => uint256)) public allowance;
    mapping(address => uint256) public nonces;

    // EIP-2612
    bytes32 private immutable _cachedDomainSeparator;
    uint256 private immutable _cachedChainId;
    bytes32 private immutable _nameHash;

    struct Checkpoint { uint256 blockNumber; uint256 value; }
    mapping(address => Checkpoint[]) private _balanceCheckpoints;
    Checkpoint[] private _supplyCheckpoints;
    error FutureLookup();
    bytes32 public constant PERMIT_TYPEHASH =
        keccak256("Permit(address owner,address spender,uint256 value,uint256 nonce,uint256 deadline)");

    error InsufficientBalance();
    error InsufficientAllowance();
    error InvalidPermit();
    error PermitExpired();
    error ZeroAddress();

    event Transfer(address indexed from, address indexed to, uint256 value);
    event Approval(address indexed owner, address indexed spender, uint256 value);

    constructor(string memory _name, string memory _symbol, uint8 _decimals) {
        name = _name;
        symbol = _symbol;
        decimals = _decimals;

        _nameHash = keccak256(bytes(_name));
        _cachedChainId = block.chainid;
        _cachedDomainSeparator = _domainSeparator();
    }

    function DOMAIN_SEPARATOR() public view returns (bytes32) {
        return block.chainid == _cachedChainId ? _cachedDomainSeparator : _domainSeparator();
    }

    function _domainSeparator() private view returns (bytes32) {
        return keccak256(abi.encode(
            keccak256("EIP712Domain(string name,string version,uint256 chainId,address verifyingContract)"),
            _nameHash, keccak256("1"), block.chainid, address(this)
        ));
    }

    /// @notice Historical direct balances; delegation is not supported.
    function getPastBalance(address account, uint256 blockNumber) public view returns (uint256) {
        return _lookup(_balanceCheckpoints[account], blockNumber);
    }

    function getPastVotes(address account, uint256 blockNumber) external view returns (uint256) {
        return getPastBalance(account, blockNumber);
    }

    function getPastTotalSupply(uint256 blockNumber) external view returns (uint256) {
        return _lookup(_supplyCheckpoints, blockNumber);
    }

    function _lookup(Checkpoint[] storage checkpoints, uint256 blockNumber) private view returns (uint256) {
        if (blockNumber >= block.number) revert FutureLookup();
        uint256 low;
        uint256 high = checkpoints.length;
        while (low < high) {
            uint256 mid = low + (high - low) / 2;
            if (checkpoints[mid].blockNumber > blockNumber) high = mid;
            else low = mid + 1;
        }
        return low == 0 ? 0 : checkpoints[low - 1].value;
    }

    function _checkpoint(Checkpoint[] storage checkpoints, uint256 value) private {
        uint256 length = checkpoints.length;
        if (length > 0 && checkpoints[length - 1].blockNumber == block.number) {
            checkpoints[length - 1].value = value;
        } else {
            checkpoints.push(Checkpoint(block.number, value));
        }
    }

    function approve(address spender, uint256 amount) external returns (bool) {
        _approve(msg.sender, spender, amount);
        return true;
    }

    function transfer(address to, uint256 amount) external returns (bool) {
        _transfer(msg.sender, to, amount);
        return true;
    }

    function transferFrom(address from, address to, uint256 amount) external returns (bool) {
        uint256 currentAllowance = allowance[from][msg.sender];
        if (currentAllowance != type(uint256).max) {
            if (currentAllowance < amount) revert InsufficientAllowance();
            unchecked {
                allowance[from][msg.sender] = currentAllowance - amount;
            }
        }
        _transfer(from, to, amount);
        return true;
    }

    /// @notice EIP-2612 permit: approve via signature
    function permit(
        address owner_,
        address spender,
        uint256 value,
        uint256 deadline,
        uint8 v,
        bytes32 r,
        bytes32 s
    ) external {
        if (block.timestamp > deadline) revert PermitExpired();

        bytes32 structHash = keccak256(
            abi.encode(PERMIT_TYPEHASH, owner_, spender, value, nonces[owner_]++, deadline)
        );
        bytes32 digest = keccak256(
            abi.encodePacked("\x19\x01", DOMAIN_SEPARATOR(), structHash)
        );

        address recovered = ecrecover(digest, v, r, s);
        if (recovered == address(0) || recovered != owner_) revert InvalidPermit();

        _approve(owner_, spender, value);
    }

    function _transfer(address from, address to, uint256 amount) internal virtual {
        if (from == address(0)) revert ZeroAddress();
        if (to == address(0)) revert ZeroAddress();
        if (balanceOf[from] < amount) revert InsufficientBalance();

        unchecked {
            balanceOf[from] -= amount;
        }
        balanceOf[to] += amount;
        _checkpoint(_balanceCheckpoints[from], balanceOf[from]);
        if (from != to) _checkpoint(_balanceCheckpoints[to], balanceOf[to]);
        emit Transfer(from, to, amount);
    }

    function _approve(address owner_, address spender, uint256 amount) internal {
        allowance[owner_][spender] = amount;
        emit Approval(owner_, spender, amount);
    }

    function _mint(address to, uint256 amount) internal {
        if (to == address(0)) revert ZeroAddress();
        totalSupply += amount;
        balanceOf[to] += amount;
        _checkpoint(_balanceCheckpoints[to], balanceOf[to]);
        _checkpoint(_supplyCheckpoints, totalSupply);
        emit Transfer(address(0), to, amount);
    }

    function _burn(address from, uint256 amount) internal {
        if (balanceOf[from] < amount) revert InsufficientBalance();
        unchecked {
            balanceOf[from] -= amount;
        }
        totalSupply -= amount;
        _checkpoint(_balanceCheckpoints[from], balanceOf[from]);
        _checkpoint(_supplyCheckpoints, totalSupply);
        emit Transfer(from, address(0), amount);
    }
}
