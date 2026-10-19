import type { Any } from "../../../google/protobuf/any";
import type { Coin } from "../../base/v1beta1/coin";
import type { VoteOption, WeightedVoteOption } from "./gov";
export interface MsgSubmitProposal {
    content?: Any;
    initial_deposit: Coin[];
    proposer: string;
    is_expedited: boolean;
}
export interface MsgSubmitProposalResponse {
    proposal_id: number;
}
export interface MsgVote {
    proposal_id: number;
    voter: string;
    option: VoteOption;
}
export type MsgVoteResponse = {};
export interface MsgVoteWeighted {
    proposal_id: number;
    voter: string;
    options: WeightedVoteOption[];
}
export type MsgVoteWeightedResponse = {};
export interface MsgDeposit {
    proposal_id: number;
    depositor: string;
    amount: Coin[];
}
export type MsgDepositResponse = {};
