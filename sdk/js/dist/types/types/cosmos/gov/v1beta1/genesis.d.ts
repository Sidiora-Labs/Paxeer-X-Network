import type { Deposit, DepositParams, Proposal, TallyParams, Vote, VotingParams } from "./gov";
export interface GenesisState {
    /** starting_proposal_id is the ID of the starting proposal. */
    starting_proposal_id: number;
    /** deposits defines all the deposits present at genesis. */
    deposits: Deposit[];
    /** votes defines all the votes present at genesis. */
    votes: Vote[];
    /** proposals defines all the proposals present at genesis. */
    proposals: Proposal[];
    /** params defines all the paramaters of related to deposit. */
    deposit_params?: DepositParams;
    /** params defines all the paramaters of related to voting. */
    voting_params?: VotingParams;
    /** params defines all the paramaters of related to tally. */
    tally_params?: TallyParams;
}
