// SPDX-License-Identifier: LicenseRef-Paxlabs-HyperPax-OS-Protocol
pragma solidity ^0.8.24;

/// @title Timelock
/// @notice Trust anchor. Enforces delay on all governance-approved transactions.
/// @dev IMMUTABLE — not upgradeable. This is the security root.
contract Timelock {
    uint256 public immutable minDelay;
    address public proposer;
    address public guardian;

    mapping(bytes32 => bool) public queuedTransactions;
    mapping(bytes32 => uint8) public batchStatus; // 1 queued, 2 executed, 3 cancelled
    mapping(bytes32 => uint256) public batchEta;
    uint256 public constant GRACE_PERIOD = 14 days;
    address public pendingGuardian;
    bool public proposerHandedOff;
    error OperationExpired();
    error InvalidBatch();
    event BatchQueued(bytes32 indexed operationId, uint256 eta);
    event BatchExecuted(bytes32 indexed operationId);
    event BatchCancelled(bytes32 indexed operationId);
    event GuardianNominated(address indexed nominee);

    error DelayNotMet();
    error TransactionNotQueued();
    error TransactionAlreadyQueued();
    error Unauthorized();
    error ExecutionFailed();
    error ZeroAddress();
    error InvalidDelay();

    event TransactionQueued(
        bytes32 indexed txHash,
        address indexed target,
        uint256 value,
        bytes data,
        uint256 eta
    );
    event TransactionExecuted(
        bytes32 indexed txHash,
        address indexed target,
        uint256 value,
        bytes data,
        uint256 eta
    );
    event TransactionCancelled(bytes32 indexed txHash);
    event ProposerChanged(address indexed oldProposer, address indexed newProposer);
    event GuardianChanged(address indexed oldGuardian, address indexed newGuardian);

    modifier onlyProposer() {
        if (msg.sender != proposer) revert Unauthorized();
        _;
    }

    modifier onlyGuardian() {
        if (msg.sender != guardian) revert Unauthorized();
        _;
    }

    constructor(uint256 _minDelay, address _proposer, address _guardian) {
        if (_minDelay == 0 || _minDelay > 30 days) revert InvalidDelay();
        if (_proposer == address(0)) revert ZeroAddress();
        if (_guardian == address(0)) revert ZeroAddress();

        minDelay = _minDelay;
        proposer = _proposer;
        guardian = _guardian;
    }

    /// @notice Queue a transaction for future execution
    function queueTransaction(
        address target,
        uint256 value,
        bytes calldata data,
        uint256 eta
    ) external onlyProposer returns (bytes32 txHash) {
        if (target == address(0)) revert ZeroAddress();
        if (eta < block.timestamp + minDelay || eta > block.timestamp + minDelay + 1 days) revert DelayNotMet();

        txHash = _getTxHash(target, value, data, eta);
        if (queuedTransactions[txHash]) revert TransactionAlreadyQueued();

        queuedTransactions[txHash] = true;
        emit TransactionQueued(txHash, target, value, data, eta);
    }

    /// @notice Execute a queued transaction after its delay has passed
    function executeTransaction(
        address target,
        uint256 value,
        bytes calldata data,
        uint256 eta
    ) external returns (bytes memory result) {
        bytes32 txHash = _getTxHash(target, value, data, eta);
        if (!queuedTransactions[txHash]) revert TransactionNotQueued();
        if (block.timestamp < eta) revert DelayNotMet();
        if (block.timestamp - eta > GRACE_PERIOD) revert OperationExpired();

        queuedTransactions[txHash] = false;

        (bool success, bytes memory returnData) = target.call{value: value}(data);
        if (!success) revert ExecutionFailed();

        emit TransactionExecuted(txHash, target, value, data, eta);
        return returnData;
    }

    /// @notice Cancel a queued transaction (guardian only)
    function cancelTransaction(
        address target,
        uint256 value,
        bytes calldata data,
        uint256 eta
    ) external onlyGuardian {
        bytes32 txHash = _getTxHash(target, value, data, eta);
        if (!queuedTransactions[txHash]) revert TransactionNotQueued();

        queuedTransactions[txHash] = false;
        emit TransactionCancelled(txHash);
    }

    /// @notice Bootstrap proposer once; subsequent rotations require a delayed self-call.
    function setProposer(address newProposer) external {
        if (msg.sender != address(this) && (msg.sender != proposer || proposerHandedOff)) revert Unauthorized();
        proposerHandedOff = true;
        if (newProposer == address(0)) revert ZeroAddress();
        address old = proposer;
        proposer = newProposer;
        emit ProposerChanged(old, newProposer);
    }

    /// @notice Nominate a guardian; the nominee must accept before authority changes.
    function setGuardian(address newGuardian) external {
        if (msg.sender != guardian && msg.sender != address(this)) revert Unauthorized();
        if (newGuardian == address(0)) revert ZeroAddress();
        pendingGuardian = newGuardian;
        emit GuardianNominated(newGuardian);
    }

    function acceptGuardian() external {
        if (msg.sender != pendingGuardian) revert Unauthorized();
        address old = guardian;
        guardian = msg.sender;
        pendingGuardian = address(0);
        emit GuardianChanged(old, msg.sender);
    }

    function hashBatch(address[] calldata targets, uint256[] calldata values, bytes[] calldata data, bytes32 salt)
        public pure returns (bytes32) {
        return keccak256(abi.encode(targets, values, data, salt));
    }

    function queueBatch(address[] calldata targets, uint256[] calldata values, bytes[] calldata data, bytes32 salt)
        external onlyProposer returns (bytes32 id, uint256 eta) {
        if (targets.length == 0 || targets.length != values.length || targets.length != data.length) revert InvalidBatch();
        for (uint256 i; i < targets.length; ++i) if (targets[i] == address(0)) revert ZeroAddress();
        id = hashBatch(targets, values, data, salt);
        if (batchStatus[id] != 0) revert TransactionAlreadyQueued();
        eta = block.timestamp + minDelay;
        batchStatus[id] = 1;
        batchEta[id] = eta;
        emit BatchQueued(id, eta);
    }

    function executeBatch(address[] calldata targets, uint256[] calldata values, bytes[] calldata data, bytes32 salt) external {
        bytes32 id = hashBatch(targets, values, data, salt);
        if (batchStatus[id] != 1) revert TransactionNotQueued();
        uint256 eta = batchEta[id];
        if (block.timestamp < eta) revert DelayNotMet();
        if (block.timestamp - eta > GRACE_PERIOD) revert OperationExpired();
        batchStatus[id] = 2;
        for (uint256 i; i < targets.length; ++i) {
            (bool ok,) = targets[i].call{value: values[i]}(data[i]);
            if (!ok) revert ExecutionFailed();
        }
        emit BatchExecuted(id);
    }

    function cancelBatch(bytes32 id) external {
        if (msg.sender != guardian && msg.sender != proposer) revert Unauthorized();
        if (batchStatus[id] != 1) revert TransactionNotQueued();
        batchStatus[id] = 3;
        emit BatchCancelled(id);
    }

    function getMinDelay() external view returns (uint256) {
        return minDelay;
    }

    function _getTxHash(
        address target,
        uint256 value,
        bytes calldata data,
        uint256 eta
    ) internal pure returns (bytes32) {
        return keccak256(abi.encode(target, value, data, eta));
    }

    receive() external payable {}
}
