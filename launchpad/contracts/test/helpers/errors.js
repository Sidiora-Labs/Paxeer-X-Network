// Custom error signatures used across the protocol
// These match the custom errors defined in contracts

// General
const ZERO_ADDRESS_ERROR = "ZeroAddress";
const UNAUTHORIZED_ERROR = "Unauthorized";
const ALREADY_INITIALIZED_ERROR = "AlreadyInitialized";
const NOT_INITIALIZED_ERROR = "NotInitialized";

// Math
const OVERFLOW_ERROR = "Overflow";
const DIVISION_BY_ZERO_ERROR = "DivisionByZero";

// Access Control
const MISSING_ROLE_ERROR = "MissingRole";

// Pausable
const PAUSED_ERROR = "Paused";
const NOT_PAUSED_ERROR = "NotPaused";

// Reentrancy
const REENTRANCY_ERROR = "ReentrancyGuardReentrantCall";

// ERC20
const INSUFFICIENT_BALANCE_ERROR = "InsufficientBalance";
const INSUFFICIENT_ALLOWANCE_ERROR = "InsufficientAllowance";
const INVALID_PERMIT_ERROR = "InvalidPermit";
const PERMIT_EXPIRED_ERROR = "PermitExpired";

// ERC721
const TOKEN_NOT_FOUND_ERROR = "TokenNotFound";
const NOT_TOKEN_OWNER_ERROR = "NotTokenOwner";
const NOT_APPROVED_ERROR = "NotApproved";
const ALREADY_MINTED_ERROR = "AlreadyMinted";

// Pool
const DEADLINE_EXPIRED_ERROR = "DeadlineExpired";
const SLIPPAGE_ERROR = "SlippageExceeded";
const INSUFFICIENT_LIQUIDITY_ERROR = "InsufficientLiquidity";
const INSUFFICIENT_INPUT_ERROR = "InsufficientInput";

// Fee
const FEE_OUT_OF_RANGE_ERROR = "FeeOutOfRange";
const INVALID_STRATEGY_ERROR = "InvalidStrategy";
const NOT_NFT_OWNER_ERROR = "NotNftOwner";
const NO_FEES_ERROR = "NoFeesAccumulated";
const WRONG_STRATEGY_ERROR = "WrongStrategy";

// Factory
const DUPLICATE_TOKEN_ERROR = "DuplicateToken";
const INSUFFICIENT_CREATION_FEE_ERROR = "InsufficientCreationFee";

// Timelock
const DELAY_NOT_MET_ERROR = "DelayNotMet";
const TX_NOT_QUEUED_ERROR = "TransactionNotQueued";
const TX_ALREADY_QUEUED_ERROR = "TransactionAlreadyQueued";

// Governance
const BELOW_THRESHOLD_ERROR = "BelowProposalThreshold";
const ALREADY_VOTED_ERROR = "AlreadyVoted";
const VOTING_CLOSED_ERROR = "VotingClosed";
const QUORUM_NOT_MET_ERROR = "QuorumNotMet";
const PROPOSAL_NOT_PASSED_ERROR = "ProposalNotPassed";

// TransferHelper
const TRANSFER_FAILED_ERROR = "TransferFailed";
const TRANSFER_FROM_FAILED_ERROR = "TransferFromFailed";
const APPROVE_FAILED_ERROR = "ApproveFailed";

module.exports = {
  ZERO_ADDRESS_ERROR,
  UNAUTHORIZED_ERROR,
  ALREADY_INITIALIZED_ERROR,
  NOT_INITIALIZED_ERROR,
  OVERFLOW_ERROR,
  DIVISION_BY_ZERO_ERROR,
  MISSING_ROLE_ERROR,
  PAUSED_ERROR,
  NOT_PAUSED_ERROR,
  REENTRANCY_ERROR,
  INSUFFICIENT_BALANCE_ERROR,
  INSUFFICIENT_ALLOWANCE_ERROR,
  INVALID_PERMIT_ERROR,
  PERMIT_EXPIRED_ERROR,
  TOKEN_NOT_FOUND_ERROR,
  NOT_TOKEN_OWNER_ERROR,
  NOT_APPROVED_ERROR,
  ALREADY_MINTED_ERROR,
  DEADLINE_EXPIRED_ERROR,
  SLIPPAGE_ERROR,
  INSUFFICIENT_LIQUIDITY_ERROR,
  INSUFFICIENT_INPUT_ERROR,
  FEE_OUT_OF_RANGE_ERROR,
  INVALID_STRATEGY_ERROR,
  NOT_NFT_OWNER_ERROR,
  NO_FEES_ERROR,
  WRONG_STRATEGY_ERROR,
  DUPLICATE_TOKEN_ERROR,
  INSUFFICIENT_CREATION_FEE_ERROR,
  DELAY_NOT_MET_ERROR,
  TX_NOT_QUEUED_ERROR,
  TX_ALREADY_QUEUED_ERROR,
  BELOW_THRESHOLD_ERROR,
  ALREADY_VOTED_ERROR,
  VOTING_CLOSED_ERROR,
  QUORUM_NOT_MET_ERROR,
  PROPOSAL_NOT_PASSED_ERROR,
  TRANSFER_FAILED_ERROR,
  TRANSFER_FROM_FAILED_ERROR,
  APPROVE_FAILED_ERROR,
};
