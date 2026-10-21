# Solidity API

## GovernanceModule

Decentralized decision-making for protocol upgrades and parameter changes

_Dual mode: Admin bypass (initial) + full SID governance (after transition).
     Proposals that pass are queued to the Timelock for delayed execution._

### ADMIN_ROLE

```solidity
bytes32 ADMIN_ROLE
```

### ProposalState

```solidity
enum ProposalState {
  Pending,
  Active,
  Defeated,
  Succeeded,
  Queued,
  Executed,
  Cancelled
}
```

### Proposal

```solidity
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
```

### votingToken

```solidity
address votingToken
```

### timelock

```solidity
address timelock
```

### proposalThreshold

```solidity
uint256 proposalThreshold
```

### votingPeriod

```solidity
uint256 votingPeriod
```

### quorumVotes

```solidity
uint256 quorumVotes
```

### proposalCount

```solidity
uint256 proposalCount
```

### proposals

```solidity
mapping(uint256 => struct GovernanceModule.Proposal) proposals
```

### hasVoted

```solidity
mapping(uint256 => mapping(address => bool)) hasVoted
```

### voteSnapshot

```solidity
mapping(uint256 => mapping(address => uint256)) voteSnapshot
```

### adminModeActive

```solidity
bool adminModeActive
```

### BelowProposalThreshold

```solidity
error BelowProposalThreshold()
```

### AlreadyVoted

```solidity
error AlreadyVoted()
```

### VotingClosed

```solidity
error VotingClosed()
```

### QuorumNotMet

```solidity
error QuorumNotMet()
```

### ProposalNotPassed

```solidity
error ProposalNotPassed()
```

### ProposalAlreadyExecuted

```solidity
error ProposalAlreadyExecuted()
```

### ProposalIsCancelled

```solidity
error ProposalIsCancelled()
```

### InvalidProposal

```solidity
error InvalidProposal()
```

### Unauthorized

```solidity
error Unauthorized()
```

### AdminModeNotActive

```solidity
error AdminModeNotActive()
```

### ArrayLengthMismatch

```solidity
error ArrayLengthMismatch()
```

### ProposalCreated

```solidity
event ProposalCreated(uint256 proposalId, address proposer, string description)
```

### VoteCast

```solidity
event VoteCast(uint256 proposalId, address voter, bool support, uint256 weight)
```

### ProposalExecuted

```solidity
event ProposalExecuted(uint256 proposalId)
```

### ProposalCancelled

```solidity
event ProposalCancelled(uint256 proposalId)
```

### AdminModeDeactivated

```solidity
event AdminModeDeactivated()
```

### constructor

```solidity
constructor() public
```

### initialize

```solidity
function initialize(address _votingToken, address _timelock, address _admin, uint256 _proposalThreshold, uint256 _votingPeriod, uint256 _quorumVotes) external
```

### propose

```solidity
function propose(address[] targets, uint256[] values, bytes[] calldatas, string description) external returns (uint256 proposalId)
```

Create a proposal (requires SID balance >= proposalThreshold)

### castVote

```solidity
function castVote(uint256 proposalId, bool support) external
```

Vote on a proposal

### execute

```solidity
function execute(uint256 proposalId, uint256 eta) external
```

Execute a passed proposal (queues it through Timelock)

### cancel

```solidity
function cancel(uint256 proposalId) external
```

Cancel a proposal (only proposer or admin)

### adminExecute

```solidity
function adminExecute(address target, uint256 value, bytes data, uint256 eta) external
```

Admin bypasses governance and queues directly to Timelock

_Only works while adminModeActive is true_

### deactivateAdminMode

```solidity
function deactivateAdminMode() external
```

Permanently deactivate admin mode (irreversible)

### getProposalState

```solidity
function getProposalState(uint256 proposalId) external view returns (enum GovernanceModule.ProposalState)
```

Get the state of a proposal

### _getVotingPower

```solidity
function _getVotingPower(address account) internal view returns (uint256)
```

### _authorizeUpgrade

```solidity
function _authorizeUpgrade(address) internal
```

## ITimelockQueue

### queueTransaction

```solidity
function queueTransaction(address target, uint256 value, bytes data, uint256 eta) external returns (bytes32)
```

