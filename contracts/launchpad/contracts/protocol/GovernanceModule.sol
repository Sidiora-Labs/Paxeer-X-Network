// SPDX-License-Identifier: LicenseRef-Paxlabs-HyperPax-OS-Protocol
pragma solidity ^0.8.24;

import "../base/Initializable.sol";
import "../base/UUPSUpgradeable.sol";
import "../base/AccessControl.sol";

/// @title GovernanceModule
/// @notice Timelock governance with administrator or checkpoint-token decision modes.
/// @dev Token voting is opt-in; administrator mode never requires token checkpoints.
///      Proposals that pass are queued to the Timelock for delayed execution.
contract GovernanceModule is Initializable, UUPSUpgradeable, AccessControl {
    bytes32 public constant ADMIN_ROLE = keccak256("ADMIN_ROLE");

    enum ProposalState { Pending, Active, Defeated, Succeeded, Queued, Executed, Cancelled, Expired }

    struct Proposal {
        address proposer;
        address[] targets;
        uint256[] values;
        bytes[] calldatas;
        string description;
        uint256 startBlock;
        uint256 endBlock;
        uint256 forVotes;
        uint256 againstVotes;
        bool executed;
        bool cancelled;
    }

    address public votingToken; // SID token
    address public timelock;

    uint256 public proposalThreshold; // min SID balance to propose
    uint256 public votingPeriod; // blocks
    uint256 public quorumVotes; // min votes for quorum

    uint256 public proposalCount;
    mapping(uint256 => Proposal) public proposals;
    mapping(uint256 => mapping(address => bool)) public hasVoted;
    mapping(uint256 => mapping(address => uint256)) public voteSnapshot; // snapshot of voter balance

    bool public adminModeActive; // true = admin can bypass governance
    mapping(uint256 => bytes32) public proposalOperation;
    mapping(uint256 => uint256) public proposalEta;
    bool public adminOnlyMode;

    error BelowProposalThreshold();
    error AlreadyVoted();
    error VotingClosed();
    error QuorumNotMet();
    error ProposalNotPassed();
    error ProposalAlreadyExecuted();
    error ProposalIsCancelled();
    error InvalidProposal();
    error Unauthorized();
    error AdminModeNotActive();
    error ArrayLengthMismatch();
    error AdminOnlyGovernance();

    event ProposalCreated(uint256 indexed proposalId, address indexed proposer, string description);
    event VoteCast(uint256 indexed proposalId, address indexed voter, bool support, uint256 weight);
    event ProposalExecuted(uint256 indexed proposalId);
    event ProposalQueued(uint256 indexed proposalId, bytes32 indexed operationId, uint256 eta);
    event ProposalCancelled(uint256 indexed proposalId);
    event AdminModeDeactivated();

    /// @custom:oz-upgrades-unsafe-allow constructor
    constructor() {
        _disableInitializers();
    }

    function initialize(
        address _votingToken,
        address _timelock,
        address _admin,
        uint256 _proposalThreshold,
        uint256 _votingPeriod,
        uint256 _quorumVotes
    ) external initializer {
        if (_admin == address(0) || _votingToken.code.length == 0 || _timelock.code.length == 0) revert InvalidProposal();
        if (_proposalThreshold == 0 || _quorumVotes == 0 || _votingPeriod == 0 || _votingPeriod > 2_000_000) revert InvalidProposal();
        // Fail deployment early unless the token exposes historical checkpoint voting.
        IVotingCheckpoints(_votingToken).getPastVotes(address(this), block.number - 1);
        ITimelockQueue(_timelock).getMinDelay();
        votingToken = _votingToken;
        timelock = _timelock;
        proposalThreshold = _proposalThreshold;
        votingPeriod = _votingPeriod;
        quorumVotes = _quorumVotes;
        adminModeActive = true;

        _grantRole(DEFAULT_ADMIN_ROLE, _admin);
        _grantRole(ADMIN_ROLE, _admin);
    }

    /// @notice Initialize administrator-controlled governance without a voting token.
    /// @dev Administrator decisions still queue through Timelock and its enforced delay.
    function initializeAdmin(address _timelock, address _admin) external initializer {
        if (_admin == address(0) || _timelock.code.length == 0) revert InvalidProposal();
        ITimelockQueue(_timelock).getMinDelay();
        timelock = _timelock;
        adminOnlyMode = true;
        adminModeActive = true;
        _grantRole(DEFAULT_ADMIN_ROLE, _admin);
        _grantRole(ADMIN_ROLE, _admin);
    }

    /// @notice Create a proposal (requires SID balance >= proposalThreshold)
    function propose(
        address[] calldata targets,
        uint256[] calldata values,
        bytes[] calldata calldatas,
        string calldata description
    ) external returns (uint256 proposalId) {
        if (adminOnlyMode) revert AdminOnlyGovernance();
        if (targets.length == 0) revert InvalidProposal();
        if (targets.length != values.length || targets.length != calldatas.length) revert ArrayLengthMismatch();

        uint256 balance = IVotingCheckpoints(votingToken).getPastVotes(msg.sender, block.number - 1);
        if (balance < proposalThreshold) revert BelowProposalThreshold();

        proposalId = ++proposalCount;
        Proposal storage p = proposals[proposalId];
        p.proposer = msg.sender;
        p.targets = targets;
        p.values = values;
        p.calldatas = calldatas;
        p.description = description;
        p.startBlock = block.number + 1;
        p.endBlock = block.number + 1 + votingPeriod;

        emit ProposalCreated(proposalId, msg.sender, description);
    }

    /// @notice Vote on a proposal
    function castVote(uint256 proposalId, bool support) external {
        _requireProposal(proposalId);
        Proposal storage p = proposals[proposalId];
        if (block.number < p.startBlock || block.number > p.endBlock) revert VotingClosed();
        if (p.cancelled) revert ProposalIsCancelled();
        if (hasVoted[proposalId][msg.sender]) revert AlreadyVoted();

        uint256 weight = IVotingCheckpoints(votingToken).getPastVotes(msg.sender, p.startBlock - 1);
        hasVoted[proposalId][msg.sender] = true;
        voteSnapshot[proposalId][msg.sender] = weight;

        if (support) {
            p.forVotes += weight;
        } else {
            p.againstVotes += weight;
        }

        emit VoteCast(proposalId, msg.sender, support, weight);
    }

    /// @notice Queue a passed proposal as a single atomic Timelock operation.
    /// @dev The legacy ETA argument is ignored; the Timelock computes the delay.
    function execute(uint256 proposalId, uint256) external {
        _requireProposal(proposalId);
        Proposal storage p = proposals[proposalId];
        if (p.executed || proposalOperation[proposalId] != bytes32(0)) revert ProposalAlreadyExecuted();
        if (p.cancelled) revert ProposalIsCancelled();
        if (block.number <= p.endBlock) revert VotingClosed();
        if (p.forVotes < quorumVotes) revert QuorumNotMet();
        if (p.forVotes <= p.againstVotes) revert ProposalNotPassed();
        (bytes32 id, uint256 eta) = ITimelockQueue(timelock).queueBatch(p.targets, p.values, p.calldatas, bytes32(proposalId));
        proposalOperation[proposalId] = id;
        proposalEta[proposalId] = eta;
        emit ProposalQueued(proposalId, id, eta);
    }

    function executeQueued(uint256 proposalId) external {
        _requireProposal(proposalId);
        Proposal storage p = proposals[proposalId];
        ITimelockQueue(timelock).executeBatch(p.targets, p.values, p.calldatas, bytes32(proposalId));
        p.executed = true;
        emit ProposalExecuted(proposalId);
    }

    /// @notice Cancel a proposal (only proposer or admin)
    function cancel(uint256 proposalId) external {
        _requireProposal(proposalId);
        Proposal storage p = proposals[proposalId];
        if (msg.sender != p.proposer && !(adminModeActive && hasRole(ADMIN_ROLE, msg.sender))) revert Unauthorized();
        if (p.executed || (proposalOperation[proposalId] != bytes32(0) && ITimelockQueue(timelock).batchStatus(proposalOperation[proposalId]) == 2)) revert ProposalAlreadyExecuted();
        if (proposalOperation[proposalId] != bytes32(0) && ITimelockQueue(timelock).batchStatus(proposalOperation[proposalId]) == 1) ITimelockQueue(timelock).cancelBatch(proposalOperation[proposalId]);
        p.cancelled = true;
        emit ProposalCancelled(proposalId);
    }

    /// @notice Admin bypasses governance and queues directly to Timelock
    /// @dev Only works while adminModeActive is true
    function adminExecute(
        address target,
        uint256 value,
        bytes calldata data,
        uint256 eta
    ) external onlyRole(ADMIN_ROLE) {
        if (!adminModeActive) revert AdminModeNotActive();
        ITimelockQueue(timelock).queueTransaction(target, value, data, eta);
    }

    /// @notice Deactivate bypass and admin cancellation in this implementation.
    /// @dev Transfer upgrade/default-admin roles to Timelock to complete governance handoff.
    function deactivateAdminMode() external onlyRole(ADMIN_ROLE) {
        if (adminOnlyMode) revert AdminOnlyGovernance();
        adminModeActive = false;
        emit AdminModeDeactivated();
    }

    /// @notice Get the state of a proposal
    function getProposalState(uint256 proposalId) external view returns (ProposalState) {
        _requireProposal(proposalId);
        Proposal storage p = proposals[proposalId];
        bytes32 id = proposalOperation[proposalId];
        if (id != bytes32(0)) {
            uint8 status = ITimelockQueue(timelock).batchStatus(id);
            if (status == 2) return ProposalState.Executed;
            if (status == 3) return ProposalState.Cancelled;
            if (block.timestamp > proposalEta[proposalId] + ITimelockQueue(timelock).GRACE_PERIOD()) return ProposalState.Expired;
            return ProposalState.Queued;
        }
        if (p.cancelled) return ProposalState.Cancelled;
        if (p.executed) return ProposalState.Executed;
        if (block.number < p.startBlock) return ProposalState.Pending;
        if (block.number <= p.endBlock) return ProposalState.Active;
        if (p.forVotes < quorumVotes || p.forVotes <= p.againstVotes) return ProposalState.Defeated;
        return ProposalState.Succeeded;
    }

    function getProposalActions(uint256 proposalId) external view returns (address[] memory, uint256[] memory, bytes[] memory) {
        _requireProposal(proposalId);
        Proposal storage p = proposals[proposalId];
        return (p.targets, p.values, p.calldatas);
    }

    function _requireProposal(uint256 id) internal view {
        if (id == 0 || id > proposalCount) revert InvalidProposal();
    }

    function _authorizeUpgrade(address) internal override onlyRole(DEFAULT_ADMIN_ROLE) {}
}

interface IVotingCheckpoints {
    function getPastVotes(address account, uint256 blockNumber) external view returns (uint256);
}
interface ITimelockQueue {
    function queueBatch(address[] calldata, uint256[] calldata, bytes[] calldata, bytes32) external returns(bytes32,uint256);
    function executeBatch(address[] calldata, uint256[] calldata, bytes[] calldata, bytes32) external;
    function cancelBatch(bytes32) external;
    function batchStatus(bytes32) external view returns(uint8);
    function getMinDelay() external view returns(uint256);
    function GRACE_PERIOD() external view returns(uint256);
    function queueTransaction(address target, uint256 value, bytes calldata data, uint256 eta) external returns (bytes32);
}
