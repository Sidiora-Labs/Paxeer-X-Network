import type { GeneratedType } from "@cosmjs/proto-signing";
import type { GenesisState as GenesisState_type } from "../../../../types/cosmos/gov/v1beta1";
import type { MessageFns } from "../../../common";
export interface GenesisState extends GenesisState_type {
}
export declare const GenesisState: MessageFns<GenesisState, "cosmos.gov.v1beta1.GenesisState">;
export declare const registry: Array<[string, GeneratedType]>;
export declare const aminoConverters: {
    "/cosmos.gov.v1beta1.GenesisState": {
        aminoType: string;
        toAmino: (message: GenesisState) => {
            starting_proposal_id: number;
            deposits: import("../../../../types/cosmos/gov/v1beta1").Deposit[];
            votes: import("../../../../types/cosmos/gov/v1beta1").Vote[];
            proposals: import("../../../../types/cosmos/gov/v1beta1").Proposal[];
            deposit_params?: import("../../../../types/cosmos/gov/v1beta1").DepositParams;
            voting_params?: import("../../../../types/cosmos/gov/v1beta1").VotingParams;
            tally_params?: import("../../../../types/cosmos/gov/v1beta1").TallyParams;
        };
        fromAmino: (object: GenesisState) => {
            starting_proposal_id: number;
            deposits: import("../../../../types/cosmos/gov/v1beta1").Deposit[];
            votes: import("../../../../types/cosmos/gov/v1beta1").Vote[];
            proposals: import("../../../../types/cosmos/gov/v1beta1").Proposal[];
            deposit_params?: import("../../../../types/cosmos/gov/v1beta1").DepositParams;
            voting_params?: import("../../../../types/cosmos/gov/v1beta1").VotingParams;
            tally_params?: import("../../../../types/cosmos/gov/v1beta1").TallyParams;
        };
    };
};
