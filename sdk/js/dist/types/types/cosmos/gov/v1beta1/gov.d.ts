import type { Any } from "../../../google/protobuf/any";
import type { Duration } from "../../../google/protobuf/duration";
import type { Coin } from "../../base/v1beta1/coin";
export declare enum VoteOption {
    /** VOTE_OPTION_UNSPECIFIED - VOTE_OPTION_UNSPECIFIED defines a no-op vote option. */
    VOTE_OPTION_UNSPECIFIED = 0,
    /** VOTE_OPTION_YES - VOTE_OPTION_YES defines a yes vote option. */
    VOTE_OPTION_YES = 1,
    /** VOTE_OPTION_ABSTAIN - VOTE_OPTION_ABSTAIN defines an abstain vote option. */
    VOTE_OPTION_ABSTAIN = 2,
    /** VOTE_OPTION_NO - VOTE_OPTION_NO defines a no vote option. */
    VOTE_OPTION_NO = 3,
    /** VOTE_OPTION_NO_WITH_VETO - VOTE_OPTION_NO_WITH_VETO defines a no with veto vote option. */
    VOTE_OPTION_NO_WITH_VETO = 4,
    UNRECOGNIZED = -1
}
export declare enum ProposalStatus {
    /** PROPOSAL_STATUS_UNSPECIFIED - PROPOSAL_STATUS_UNSPECIFIED defines the default propopsal status. */
    PROPOSAL_STATUS_UNSPECIFIED = 0,
    /**
     * PROPOSAL_STATUS_DEPOSIT_PERIOD - PROPOSAL_STATUS_DEPOSIT_PERIOD defines a proposal status during the deposit
     * period.
     */
    PROPOSAL_STATUS_DEPOSIT_PERIOD = 1,
    /**
     * PROPOSAL_STATUS_VOTING_PERIOD - PROPOSAL_STATUS_VOTING_PERIOD defines a proposal status during the voting
     * period.
     */
    PROPOSAL_STATUS_VOTING_PERIOD = 2,
    /**
     * PROPOSAL_STATUS_PASSED - PROPOSAL_STATUS_PASSED defines a proposal status of a proposal that has
     * passed.
     */
    PROPOSAL_STATUS_PASSED = 3,
    /**
     * PROPOSAL_STATUS_REJECTED - PROPOSAL_STATUS_REJECTED defines a proposal status of a proposal that has
     * been rejected.
     */
    PROPOSAL_STATUS_REJECTED = 4,
    /**
     * PROPOSAL_STATUS_FAILED - PROPOSAL_STATUS_FAILED defines a proposal status of a proposal that has
     * failed.
     */
    PROPOSAL_STATUS_FAILED = 5,
    UNRECOGNIZED = -1
}
export interface WeightedVoteOption {
    option: VoteOption;
    weight: string;
}
export interface TextProposal {
    title: string;
    description: string;
    is_expedited: boolean;
}
export interface Deposit {
    proposal_id: number;
    depositor: string;
    amount: Coin[];
}
export interface Proposal {
    proposal_id: number;
    content?: Any;
    status: ProposalStatus;
    final_tally_result?: TallyResult;
    submit_time?: Date;
    deposit_end_time?: Date;
    total_deposit: Coin[];
    voting_start_time?: Date;
    voting_end_time?: Date;
    is_expedited: boolean;
}
export interface TallyResult {
    yes: string;
    abstain: string;
    no: string;
    no_with_veto: string;
}
export interface Vote {
    proposal_id: number;
    voter: string;
    /**
     * Deprecated: Prefer to use `options` instead. This field is set in queries
     * if and only if `len(options) == 1` and that option has weight 1. In all
     * other cases, this field will default to VOTE_OPTION_UNSPECIFIED.
     *
     * @deprecated
     */
    option: VoteOption;
    /** Since: cosmos-sdk 0.43 */
    options: WeightedVoteOption[];
}
export interface DepositParams {
    /** Minimum deposit for a proposal to enter voting period. */
    min_deposit: Coin[];
    /**
     * Maximum period for Atom holders to deposit on a proposal. Initial value: 2
     *  months.
     */
    max_deposit_period?: Duration;
    /** Minimum deposit for a expedited proposal to enter voting period. */
    min_expedited_deposit: Coin[];
}
export interface VotingParams {
    /** Length of the voting period. */
    voting_period?: Duration;
    /** Length of the expedited voting period. */
    expedited_voting_period?: Duration;
}
export interface TallyParams {
    /**
     * Minimum percentage of total stake needed to vote for a result to be
     *  considered valid.
     */
    quorum: Uint8Array;
    /** Minimum proportion of Yes votes for proposal to pass. Default value: 0.5. */
    threshold: Uint8Array;
    /**
     * Minimum value of Veto votes to Total votes ratio for proposal to be
     *  vetoed. Default value: 1/3.
     */
    veto_threshold: Uint8Array;
    /** Minimum percentage of total stake needed to vote for expedited proposal to be valid */
    expedited_quorum: Uint8Array;
    /** Minimum proportion of Yes votes for an expedited proposal to pass. Default value: 0.67. */
    expedited_threshold: Uint8Array;
}
