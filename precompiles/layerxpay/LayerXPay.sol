// SPDX-License-Identifier: MIT
pragma solidity ^0.8.0;

address constant LAYERX_PAY_PRECOMPILE_ADDRESS = 0x000000000000000000000000000000000000101B;

ILayerXPay constant LAYERX_PAY_CONTRACT = ILayerXPay(LAYERX_PAY_PRECOMPILE_ADDRESS);

/// LayerX kernel escrow, budget, stream and service modules. Every call runs
/// the kernel activity under the authority bound to msg.sender at 0x1004 and
/// reverts with Error(string) "LXP_<code>" on refusal. Amounts are raw asset
/// base units, times are milliseconds. The per-request draw lives at 0x101A.
interface ILayerXPay {
    struct Deliverable {
        bytes32 hash;
        uint64 artifactSizeBytes;
        bytes32 availabilityReference;
    }

    struct Escrow {
        bytes32 owner;
        bytes32 escrowAccount;
        bytes32 beneficiary;
        bytes32 arbiter;
        bytes32 assetId;
        uint128 lockedAmount;
        uint128 capturedAmount;
        uint8 state;
        uint64 expiryMs;
        uint64 disputeWindowMs;
        bytes32 termsHash;
        bytes32 agreementReference;
    }

    struct Budget {
        bytes32 owner;
        bytes32 budgetAccount;
        bytes32 assetId;
        bytes32 purposeHash;
        uint128 perPeriodLimit;
        uint128 configuredPeriodLimit;
        uint128 carryCap;
        uint128 spentThisPeriod;
        uint128 carried;
        uint64 periodLengthMs;
        uint64 periodStartMs;
        uint64 expiryMs;
        uint64 revocationSequence;
        uint8 rolloverPolicy;
        bool closed;
        bool revoked;
        bytes32[] delegates;
        bytes32 sourceAccount;
    }

    struct Stream {
        bytes32 payer;
        bytes32 streamAccount;
        bytes32 recipient;
        bytes32 assetId;
        uint8 mode;
        uint128 rate;
        uint64 rateUnit;
        uint64 startMs;
        uint64 lastAccrualMs;
        uint64 endMs;
        uint128 totalCap;
        uint128 accruedTotal;
        uint128 settledTotal;
        uint128 remainderCarry;
        uint64 cumulativeMeter;
        bytes32[] meterAuthorities;
        bool underfunded;
        bool paused;
        bool closed;
    }

    struct StreamResult {
        bytes32 streamId;
        bytes32 transferSetRoot;
        uint128 paid;
        uint128 refunded;
        uint16 ordinal;
        uint8 legCount;
    }

    struct Offer {
        bytes32 offerId;
        bytes32 activityId;
        bytes32 offeringAgent;
        bytes32 assetId;
        uint128 price;
        bytes32 termsHash;
        bytes32 deliverableSpecHash;
        uint64 deliveryDeadlineMs;
        uint64 acceptanceWindowMs;
        uint64 disputeWindowMs;
        uint8 defaultOutcome;
        uint64 offerExpiryMs;
        uint64 globalSequence;
        bool withdrawn;
        bool accepted;
    }

    struct Agreement {
        bytes32 agreementId;
        bytes32 offerId;
        bytes32 provider;
        bytes32 buyer;
        bytes32 termsHash;
        bytes32 escrowId;
        uint64 deliveryDeadlineMs;
        uint64 acceptanceWindowEndMs;
        uint64 disputeWindowEndMs;
        uint8 defaultOutcome;
        uint8 state;
        uint64 acceptedSequence;
        uint16 rejectionReason;
        bytes32[] contestedHashes;
        bool defaultApplied;
        uint64 outcomeSequence;
        uint64 outcomeTimestampMs;
    }

    struct Commitment {
        bytes32 commitmentId;
        bytes32 activityId;
        bytes32 provider;
        bytes32 agreementId;
        bytes32 taskHash;
        uint64 deadlineMs;
        uint64 resourceBound;
        bytes32 escrowId;
        uint64 globalSequence;
        bool abandoned;
        uint16 abandonReason;
    }

    struct Delivery {
        bytes32 agreementId;
        bytes32 provider;
        Deliverable[] deliverables;
        uint64 globalSequence;
    }

    struct Progress {
        bytes32 reportId;
        bytes32 activityId;
        bytes32 commitmentId;
        bytes32 agreementId;
        bytes32 provider;
        bytes32 noteHash;
        bytes32 availabilityReference;
        uint32 progressBps;
        uint64 reportedAtMs;
        uint64 globalSequence;
    }

    struct Dispute {
        bytes32 disputeId;
        bytes32 activityId;
        bytes32 agreementId;
        bytes32 raiser;
        bytes32[] evidenceHashes;
        uint64 globalSequence;
        bool resolved;
        uint16 ruling;
        uint32 providerBps;
        bytes32 escrowResolutionId;
        uint64 resolutionSequence;
    }

    event EscrowOpened(bytes32 indexed escrowId, uint16 ordinal, uint8 state, uint128 capturedAmount, uint128 lockedAmount);
    event EscrowCaptured(bytes32 indexed escrowId, uint16 ordinal, uint8 state, uint128 capturedAmount, uint128 lockedAmount);
    event EscrowPartiallyCaptured(bytes32 indexed escrowId, uint16 ordinal, uint8 state, uint128 capturedAmount, uint128 lockedAmount);
    event EscrowReleased(bytes32 indexed escrowId, uint16 ordinal, uint8 state, uint128 capturedAmount, uint128 lockedAmount);
    event EscrowTimedOut(bytes32 indexed escrowId, uint16 ordinal, uint8 state, uint128 capturedAmount, uint128 lockedAmount);
    event EscrowDisputed(bytes32 indexed escrowId, uint16 ordinal, uint8 state, uint128 capturedAmount, uint128 lockedAmount);
    event EscrowResolved(bytes32 indexed escrowId, uint16 ordinal, uint8 state, uint128 capturedAmount, uint128 lockedAmount);

    event BudgetCreated(bytes32 indexed budgetId, bytes32 assetId, uint128 amount);
    event BudgetFunded(bytes32 indexed budgetId, uint128 amount);
    event BudgetAmended(bytes32 indexed budgetId, uint128 perPeriodLimit, uint64 expiryMs);
    event BudgetDelegateAdded(bytes32 indexed budgetId, bytes32 delegateActor);
    event BudgetDelegateRemoved(bytes32 indexed budgetId, bytes32 delegateActor);
    event BudgetSpent(bytes32 indexed budgetId, bytes32 recipient, uint128 amount);
    event BudgetClosed(bytes32 indexed budgetId, uint128 drained, uint64 revocationSequence);
    event BudgetDefunded(bytes32 indexed budgetId, uint128 amount, uint128 perPeriodLimit);
    event BudgetRevoked(bytes32 indexed budgetId, uint128 drained, uint64 revocationSequence);

    event StreamOpened(bytes32 indexed streamId, bytes32 payer, bytes32 streamAccount, bytes32 recipient, bytes32 assetId, uint8 mode, uint128 funding);
    event StreamToppedUp(bytes32 indexed streamId, uint128 amount, bool wasUnderfunded);
    event StreamMetered(bytes32 indexed streamId, uint64 cumulativeMeter, uint128 accrued, uint128 accruedTotal);
    event StreamSettled(bytes32 indexed streamId, uint128 paid, uint128 settledTotal, bool underfunded);
    event StreamPaused(bytes32 indexed streamId, uint64 lastAccrualMs, uint128 accruedTotal);
    event StreamResumed(bytes32 indexed streamId, uint64 lastAccrualMs, uint128 accruedTotal);
    event StreamClosed(bytes32 indexed streamId, uint128 paid, uint128 refunded);
    event StreamRateUpdated(bytes32 indexed streamId, uint128 oldRate, uint128 newRate, uint128 paid);

    event OfferPublished(bytes32 indexed primary, bytes32 indexed secondary, uint8 code, uint64 sequence);
    event OfferWithdrawn(bytes32 indexed primary, bytes32 indexed secondary, uint8 code, uint64 sequence);
    event AgreementProposed(bytes32 indexed primary, bytes32 indexed secondary, uint8 code, uint64 sequence);
    event AgreementAccepted(bytes32 indexed primary, bytes32 indexed secondary, uint8 code, uint64 sequence);
    event TaskCommitted(bytes32 indexed primary, bytes32 indexed secondary, uint8 code, uint64 sequence);
    event CommitAbandoned(bytes32 indexed primary, bytes32 indexed secondary, uint8 code, uint64 sequence);
    event ExecutionAttested(bytes32 indexed primary, bytes32 indexed secondary, uint8 code, uint64 sequence);
    event ProgressReported(bytes32 indexed primary, bytes32 indexed secondary, uint8 code, uint64 sequence);
    event Delivered(bytes32 indexed primary, bytes32 indexed secondary, uint8 code, uint64 sequence);
    event OutcomeAccepted(bytes32 indexed primary, bytes32 indexed secondary, uint8 code, uint64 sequence);
    event OutcomeRejected(bytes32 indexed primary, bytes32 indexed secondary, uint8 code, uint64 sequence);
    event DisputeOpened(bytes32 indexed primary, bytes32 indexed secondary, uint8 code, uint64 sequence);
    event DisputeResolved(bytes32 indexed primary, bytes32 indexed secondary, uint8 code, uint64 sequence);
    event DefaultOutcomeApplied(bytes32 indexed agreementId, uint8 state, bool defaultApplied, uint64 outcomeSequence, uint64 outcomeTimestampMs);

    function escrowOpen(bytes32 escrowId, bytes32 owner, bytes32 escrowAccount, bytes32 beneficiary, bytes32 arbiter, bytes32 assetId, uint128 amount, uint64 expiryMs, uint64 disputeWindowMs, bytes32 termsHash, bytes32 agreementReference) external;
    function escrowCapture(bytes32 escrowId, uint128 amount, bytes32 idempotencyKey) external;
    function escrowPartialCapture(bytes32 escrowId, uint128 amount, bytes32 idempotencyKey) external;
    function escrowRelease(bytes32 escrowId, bytes32 idempotencyKey) external;
    function escrowTimeout(bytes32 escrowId, bytes32 idempotencyKey) external;
    function escrowDisputeOpen(bytes32 escrowId) external;
    function escrowDisputeResolve(bytes32 escrowId, uint32 beneficiaryBps, bytes32 idempotencyKey) external;

    function budgetCreate(bytes32 budgetId, bytes32 budgetAccount, bytes32 assetId, bytes32 purposeHash, uint128 perPeriodLimit, uint128 carryCap, uint128 amount, uint64 periodLengthMs, uint64 periodStartMs, uint64 expiryMs, uint64 revocationSequence, uint8 rolloverPolicy) external;
    function budgetCreateNative(bytes32 budgetId, bytes32 budgetAccount, bytes32 assetId, bytes32 purposeHash, uint128 perPeriodLimit, uint128 carryCap, uint128 amount, uint64 periodLengthMs, uint64 periodStartMs, uint64 expiryMs, uint64 revocationSequence, uint8 rolloverPolicy, bytes32 sourceAccount, uint64 sourceSequence) external;
    function budgetFund(bytes32 budgetId, uint128 amount) external;
    function budgetFundNative(bytes32 budgetId, uint128 amount, uint64 sourceSequence) external;
    function budgetAmend(bytes32 budgetId, uint128 perPeriodLimit, uint128 carryCap, uint64 expiryMs, uint8 rolloverPolicy) external;
    function budgetDelegateAdd(bytes32 budgetId, bytes32 delegateActor) external;
    function budgetDelegateRemove(bytes32 budgetId, bytes32 delegateActor) external;
    function budgetSpend(bytes32 budgetId, bytes32 recipient, uint128 amount) external;
    function budgetClose(bytes32 budgetId, uint64 revocationSequence) external;
    function budgetDefund(bytes32 budgetId, uint128 amount) external;
    function budgetRevoke(bytes32 budgetId, uint64 revocationSequence) external;
    function budgetPull(bytes32 budgetId, bytes32 recipient, uint128 amount, bytes calldata payerGrant) external;
    function budgetDelegateSpend(bytes32 budgetId, bytes32 recipient, uint128 amount) external;

    function streamOpen(bytes32 streamId, bytes32 streamAccount, bytes32 recipient, bytes32 assetId, uint8 mode, uint128 rate, uint64 rateUnit, uint64 startMs, uint64 endMs, uint128 totalCap, uint128 initialFunding, bytes32[] calldata meterAuthorities) external;
    function streamTopUp(bytes32 streamId, uint128 amount) external;
    function streamMeter(bytes32 streamId, uint64 cumulativeReading, bytes32 authorityKey, bytes calldata signature) external;
    function streamSettle(bytes32 streamId, bytes32 idempotencyKey) external;
    function streamPause(bytes32 streamId) external;
    function streamResume(bytes32 streamId) external;
    function streamClose(bytes32 streamId, bytes32 idempotencyKey) external;

    function serviceOfferPublish(bytes32 offerId, bytes32 assetId, uint128 price, bytes32 termsHash, bytes32 deliverableSpecHash, uint64 deliveryDeadlineMs, uint64 acceptanceWindowMs, uint64 disputeWindowMs, uint8 defaultOutcome, uint64 offerExpiryMs) external;
    function serviceOfferWithdraw(bytes32 offerId) external;
    function serviceAgreementPropose(bytes32 agreementId, bytes32 offerId, bytes32 termsHash, bytes32 escrowId) external;
    function serviceAgreementAccept(bytes32 agreementId) external;
    function serviceCommitTask(bytes32 commitmentId, bytes32 agreementId, bytes32 taskHash, bytes32 escrowId, uint64 deadlineMs, uint64 resourceBound) external;
    function serviceCommitAbandon(bytes32 commitmentId, uint16 reason) external;
    function serviceToolExecAttest(bytes calldata execution) external;
    function serviceProgressReport(bytes32 reportId, bytes32 commitmentId, bytes32 noteHash, bytes32 availabilityReference, uint32 progressBps) external;
    function serviceDeliver(bytes32 deliveryId, bytes32 agreementId, Deliverable[] calldata deliverables) external;
    function serviceAccept(bytes32 agreementId) external;
    function serviceReject(bytes32 agreementId, uint16 reason, bytes32[] calldata contestedHashes) external;
    function serviceDisputeOpen(bytes32 disputeId, bytes32 agreementId, bytes32[] calldata evidenceHashes) external;
    function serviceDisputeResolve(bytes32 disputeId, uint16 ruling, uint32 providerBps, bytes32 escrowResolutionId) external;

    function getEscrow(bytes32 escrowId) external view returns (Escrow memory escrow);
    function escrowRemaining(bytes32 escrowId) external view returns (uint128 remaining);
    function getEscrowResult(bytes32 idempotencyKey) external view returns (bytes memory result);
    function getBudget(bytes32 budgetId) external view returns (Budget memory budget);
    function budgetRemaining(bytes32 budgetId) external view returns (uint128 remaining);
    function getStream(bytes32 streamId) external view returns (Stream memory stream);
    function streamAccrued(bytes32 streamId) external view returns (uint128 accruedTotal, uint128 unsettled);
    function getStreamResult(bytes32 idempotencyKey) external view returns (StreamResult memory result);
    function getOffer(bytes32 offerId) external view returns (Offer memory offer);
    function getAgreement(bytes32 agreementId) external view returns (Agreement memory agreement);
    function getCommitment(bytes32 commitmentId) external view returns (Commitment memory commitment);
    function getExecution(bytes32 attestationId) external view returns (bytes memory execution);
    function getDelivery(bytes32 deliveryId) external view returns (Delivery memory delivery);
    function latestDelivery(bytes32 agreementId) external view returns (Delivery memory delivery);
    function getProgress(bytes32 reportId) external view returns (Progress memory progress);
    function progressHighWater(bytes32 commitmentId) external view returns (uint32 progressBps);
    function getDispute(bytes32 disputeId) external view returns (Dispute memory dispute);
}
