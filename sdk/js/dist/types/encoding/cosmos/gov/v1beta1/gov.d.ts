import type { GeneratedType } from "@cosmjs/proto-signing";
import type { DepositParams as DepositParams_type, Deposit as Deposit_type, Proposal as Proposal_type, TallyParams as TallyParams_type, TallyResult as TallyResult_type, TextProposal as TextProposal_type, Vote as Vote_type, VotingParams as VotingParams_type, WeightedVoteOption as WeightedVoteOption_type } from "../../../../types/cosmos/gov/v1beta1";
import { ProposalStatus, VoteOption } from "../../../../types/cosmos/gov/v1beta1";
import type { MessageFns } from "../../../common";
export interface WeightedVoteOption extends WeightedVoteOption_type {
}
export interface TextProposal extends TextProposal_type {
}
export interface Deposit extends Deposit_type {
}
export interface Proposal extends Proposal_type {
}
export interface TallyResult extends TallyResult_type {
}
export interface Vote extends Vote_type {
}
export interface DepositParams extends DepositParams_type {
}
export interface VotingParams extends VotingParams_type {
}
export interface TallyParams extends TallyParams_type {
}
export declare const WeightedVoteOption: MessageFns<WeightedVoteOption, "cosmos.gov.v1beta1.WeightedVoteOption">;
export declare const TextProposal: MessageFns<TextProposal, "cosmos.gov.v1beta1.TextProposal">;
export declare const Deposit: MessageFns<Deposit, "cosmos.gov.v1beta1.Deposit">;
export declare const Proposal: MessageFns<Proposal, "cosmos.gov.v1beta1.Proposal">;
export declare const TallyResult: MessageFns<TallyResult, "cosmos.gov.v1beta1.TallyResult">;
export declare const Vote: MessageFns<Vote, "cosmos.gov.v1beta1.Vote">;
export declare const DepositParams: MessageFns<DepositParams, "cosmos.gov.v1beta1.DepositParams">;
export declare const VotingParams: MessageFns<VotingParams, "cosmos.gov.v1beta1.VotingParams">;
export declare const TallyParams: MessageFns<TallyParams, "cosmos.gov.v1beta1.TallyParams">;
export declare function voteOptionFromJSON(object: any): VoteOption;
export declare function voteOptionToJSON(object: VoteOption): string;
export declare function proposalStatusFromJSON(object: any): ProposalStatus;
export declare function proposalStatusToJSON(object: ProposalStatus): string;
export declare const registry: Array<[string, GeneratedType]>;
export declare const aminoConverters: {
    "/cosmos.gov.v1beta1.WeightedVoteOption": {
        aminoType: string;
        toAmino: (message: WeightedVoteOption) => {
            option: VoteOption;
            weight: string;
        };
        fromAmino: (object: WeightedVoteOption) => {
            option: VoteOption;
            weight: string;
        };
    };
    "/cosmos.gov.v1beta1.TextProposal": {
        aminoType: string;
        toAmino: (message: TextProposal) => {
            title: string;
            description: string;
            is_expedited: boolean;
        };
        fromAmino: (object: TextProposal) => {
            title: string;
            description: string;
            is_expedited: boolean;
        };
    };
    "/cosmos.gov.v1beta1.Deposit": {
        aminoType: string;
        toAmino: (message: Deposit) => {
            proposal_id: number;
            depositor: string;
            amount: import("../../../../types/cosmos/base/v1beta1").Coin[];
        };
        fromAmino: (object: Deposit) => {
            proposal_id: number;
            depositor: string;
            amount: import("../../../../types/cosmos/base/v1beta1").Coin[];
        };
    };
    "/cosmos.gov.v1beta1.Proposal": {
        aminoType: string;
        toAmino: (message: Proposal) => {
            proposal_id: number;
            content?: import("../../../../types/google/protobuf").Any;
            status: ProposalStatus;
            final_tally_result?: TallyResult_type;
            submit_time?: Date;
            deposit_end_time?: Date;
            total_deposit: import("../../../../types/cosmos/base/v1beta1").Coin[];
            voting_start_time?: Date;
            voting_end_time?: Date;
            is_expedited: boolean;
        };
        fromAmino: (object: Proposal) => {
            proposal_id: number;
            content?: import("../../../../types/google/protobuf").Any;
            status: ProposalStatus;
            final_tally_result?: TallyResult_type;
            submit_time?: Date;
            deposit_end_time?: Date;
            total_deposit: import("../../../../types/cosmos/base/v1beta1").Coin[];
            voting_start_time?: Date;
            voting_end_time?: Date;
            is_expedited: boolean;
        };
    };
    "/cosmos.gov.v1beta1.TallyResult": {
        aminoType: string;
        toAmino: (message: TallyResult) => {
            yes: string;
            abstain: string;
            no: string;
            no_with_veto: string;
        };
        fromAmino: (object: TallyResult) => {
            yes: string;
            abstain: string;
            no: string;
            no_with_veto: string;
        };
    };
    "/cosmos.gov.v1beta1.Vote": {
        aminoType: string;
        toAmino: (message: Vote) => {
            proposal_id: number;
            voter: string;
            option: VoteOption;
            options: WeightedVoteOption_type[];
        };
        fromAmino: (object: Vote) => {
            proposal_id: number;
            voter: string;
            option: VoteOption;
            options: WeightedVoteOption_type[];
        };
    };
    "/cosmos.gov.v1beta1.DepositParams": {
        aminoType: string;
        toAmino: (message: DepositParams) => {
            min_deposit: import("../../../../types/cosmos/base/v1beta1").Coin[];
            max_deposit_period?: import("../../../../types/google/protobuf").Duration;
            min_expedited_deposit: import("../../../../types/cosmos/base/v1beta1").Coin[];
        };
        fromAmino: (object: DepositParams) => {
            min_deposit: import("../../../../types/cosmos/base/v1beta1").Coin[];
            max_deposit_period?: import("../../../../types/google/protobuf").Duration;
            min_expedited_deposit: import("../../../../types/cosmos/base/v1beta1").Coin[];
        };
    };
    "/cosmos.gov.v1beta1.VotingParams": {
        aminoType: string;
        toAmino: (message: VotingParams) => {
            voting_period?: import("../../../../types/google/protobuf").Duration;
            expedited_voting_period?: import("../../../../types/google/protobuf").Duration;
        };
        fromAmino: (object: VotingParams) => {
            voting_period?: import("../../../../types/google/protobuf").Duration;
            expedited_voting_period?: import("../../../../types/google/protobuf").Duration;
        };
    };
    "/cosmos.gov.v1beta1.TallyParams": {
        aminoType: string;
        toAmino: (message: TallyParams) => {
            quorum: Uint8Array;
            threshold: Uint8Array;
            veto_threshold: Uint8Array;
            expedited_quorum: Uint8Array;
            expedited_threshold: Uint8Array;
        };
        fromAmino: (object: TallyParams) => {
            quorum: Uint8Array;
            threshold: Uint8Array;
            veto_threshold: Uint8Array;
            expedited_quorum: Uint8Array;
            expedited_threshold: Uint8Array;
        };
    };
};
