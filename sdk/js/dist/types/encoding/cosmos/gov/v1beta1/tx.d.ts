import type { GeneratedType } from "@cosmjs/proto-signing";
import type { MsgDepositResponse as MsgDepositResponse_type, MsgDeposit as MsgDeposit_type, MsgSubmitProposalResponse as MsgSubmitProposalResponse_type, MsgSubmitProposal as MsgSubmitProposal_type, MsgVoteResponse as MsgVoteResponse_type, MsgVoteWeightedResponse as MsgVoteWeightedResponse_type, MsgVoteWeighted as MsgVoteWeighted_type, MsgVote as MsgVote_type } from "../../../../types/cosmos/gov/v1beta1";
import type { MessageFns } from "../../../common";
export interface MsgSubmitProposal extends MsgSubmitProposal_type {
}
export interface MsgSubmitProposalResponse extends MsgSubmitProposalResponse_type {
}
export interface MsgVote extends MsgVote_type {
}
export interface MsgVoteResponse extends MsgVoteResponse_type {
}
export interface MsgVoteWeighted extends MsgVoteWeighted_type {
}
export interface MsgVoteWeightedResponse extends MsgVoteWeightedResponse_type {
}
export interface MsgDeposit extends MsgDeposit_type {
}
export interface MsgDepositResponse extends MsgDepositResponse_type {
}
export declare const MsgSubmitProposal: MessageFns<MsgSubmitProposal, "cosmos.gov.v1beta1.MsgSubmitProposal">;
export declare const MsgSubmitProposalResponse: MessageFns<MsgSubmitProposalResponse, "cosmos.gov.v1beta1.MsgSubmitProposalResponse">;
export declare const MsgVote: MessageFns<MsgVote, "cosmos.gov.v1beta1.MsgVote">;
export declare const MsgVoteResponse: MessageFns<MsgVoteResponse, "cosmos.gov.v1beta1.MsgVoteResponse">;
export declare const MsgVoteWeighted: MessageFns<MsgVoteWeighted, "cosmos.gov.v1beta1.MsgVoteWeighted">;
export declare const MsgVoteWeightedResponse: MessageFns<MsgVoteWeightedResponse, "cosmos.gov.v1beta1.MsgVoteWeightedResponse">;
export declare const MsgDeposit: MessageFns<MsgDeposit, "cosmos.gov.v1beta1.MsgDeposit">;
export declare const MsgDepositResponse: MessageFns<MsgDepositResponse, "cosmos.gov.v1beta1.MsgDepositResponse">;
export declare const registry: Array<[string, GeneratedType]>;
export declare const aminoConverters: {
    "/cosmos.gov.v1beta1.MsgSubmitProposal": {
        aminoType: string;
        toAmino: (message: MsgSubmitProposal) => {
            content?: import("../../../../types/google/protobuf").Any;
            initial_deposit: import("../../../../types/cosmos/base/v1beta1").Coin[];
            proposer: string;
            is_expedited: boolean;
        };
        fromAmino: (object: MsgSubmitProposal) => {
            content?: import("../../../../types/google/protobuf").Any;
            initial_deposit: import("../../../../types/cosmos/base/v1beta1").Coin[];
            proposer: string;
            is_expedited: boolean;
        };
    };
    "/cosmos.gov.v1beta1.MsgSubmitProposalResponse": {
        aminoType: string;
        toAmino: (message: MsgSubmitProposalResponse) => {
            proposal_id: number;
        };
        fromAmino: (object: MsgSubmitProposalResponse) => {
            proposal_id: number;
        };
    };
    "/cosmos.gov.v1beta1.MsgVote": {
        aminoType: string;
        toAmino: (message: MsgVote) => {
            proposal_id: number;
            voter: string;
            option: import("../../../../types/cosmos/gov/v1beta1").VoteOption;
        };
        fromAmino: (object: MsgVote) => {
            proposal_id: number;
            voter: string;
            option: import("../../../../types/cosmos/gov/v1beta1").VoteOption;
        };
    };
    "/cosmos.gov.v1beta1.MsgVoteResponse": {
        aminoType: string;
        toAmino: (message: MsgVoteResponse) => {};
        fromAmino: (object: MsgVoteResponse) => {};
    };
    "/cosmos.gov.v1beta1.MsgVoteWeighted": {
        aminoType: string;
        toAmino: (message: MsgVoteWeighted) => {
            proposal_id: number;
            voter: string;
            options: import("../../../../types/cosmos/gov/v1beta1").WeightedVoteOption[];
        };
        fromAmino: (object: MsgVoteWeighted) => {
            proposal_id: number;
            voter: string;
            options: import("../../../../types/cosmos/gov/v1beta1").WeightedVoteOption[];
        };
    };
    "/cosmos.gov.v1beta1.MsgVoteWeightedResponse": {
        aminoType: string;
        toAmino: (message: MsgVoteWeightedResponse) => {};
        fromAmino: (object: MsgVoteWeightedResponse) => {};
    };
    "/cosmos.gov.v1beta1.MsgDeposit": {
        aminoType: string;
        toAmino: (message: MsgDeposit) => {
            proposal_id: number;
            depositor: string;
            amount: import("../../../../types/cosmos/base/v1beta1").Coin[];
        };
        fromAmino: (object: MsgDeposit) => {
            proposal_id: number;
            depositor: string;
            amount: import("../../../../types/cosmos/base/v1beta1").Coin[];
        };
    };
    "/cosmos.gov.v1beta1.MsgDepositResponse": {
        aminoType: string;
        toAmino: (message: MsgDepositResponse) => {};
        fromAmino: (object: MsgDepositResponse) => {};
    };
};
