import type { GeneratedType } from "@cosmjs/proto-signing";
import type { Minter as Minter_type, Params as Params_type, ScheduledTokenRelease as ScheduledTokenRelease_type, Version2Minter as Version2Minter_type, Version2Params as Version2Params_type, Version2ScheduledTokenRelease as Version2ScheduledTokenRelease_type } from "../../../types/mint/v1beta1";
import type { MessageFns } from "../../common";
export interface Minter extends Minter_type {
}
export interface ScheduledTokenRelease extends ScheduledTokenRelease_type {
}
export interface Params extends Params_type {
}
export interface Version2Minter extends Version2Minter_type {
}
export interface Version2ScheduledTokenRelease extends Version2ScheduledTokenRelease_type {
}
export interface Version2Params extends Version2Params_type {
}
export declare const Minter: MessageFns<Minter, "paxprotocol.paxchain.mint.Minter">;
export declare const ScheduledTokenRelease: MessageFns<ScheduledTokenRelease, "paxprotocol.paxchain.mint.ScheduledTokenRelease">;
export declare const Params: MessageFns<Params, "paxprotocol.paxchain.mint.Params">;
export declare const Version2Minter: MessageFns<Version2Minter, "paxprotocol.paxchain.mint.Version2Minter">;
export declare const Version2ScheduledTokenRelease: MessageFns<Version2ScheduledTokenRelease, "paxprotocol.paxchain.mint.Version2ScheduledTokenRelease">;
export declare const Version2Params: MessageFns<Version2Params, "paxprotocol.paxchain.mint.Version2Params">;
export declare const registry: Array<[string, GeneratedType]>;
export declare const aminoConverters: {
    "/paxprotocol.paxchain.mint.Minter": {
        aminoType: string;
        toAmino: (message: Minter) => {
            start_date: string;
            end_date: string;
            denom: string;
            total_mint_amount: number;
            remaining_mint_amount: number;
            last_mint_amount: number;
            last_mint_date: string;
            last_mint_height: number;
        };
        fromAmino: (object: Minter) => {
            start_date: string;
            end_date: string;
            denom: string;
            total_mint_amount: number;
            remaining_mint_amount: number;
            last_mint_amount: number;
            last_mint_date: string;
            last_mint_height: number;
        };
    };
    "/paxprotocol.paxchain.mint.ScheduledTokenRelease": {
        aminoType: string;
        toAmino: (message: ScheduledTokenRelease) => {
            start_date: string;
            end_date: string;
            token_release_amount: number;
        };
        fromAmino: (object: ScheduledTokenRelease) => {
            start_date: string;
            end_date: string;
            token_release_amount: number;
        };
    };
    "/paxprotocol.paxchain.mint.Params": {
        aminoType: string;
        toAmino: (message: Params) => {
            mint_denom: string;
            token_release_schedule: ScheduledTokenRelease_type[];
        };
        fromAmino: (object: Params) => {
            mint_denom: string;
            token_release_schedule: ScheduledTokenRelease_type[];
        };
    };
    "/paxprotocol.paxchain.mint.Version2Minter": {
        aminoType: string;
        toAmino: (message: Version2Minter) => {
            last_mint_amount: string;
            last_mint_date: string;
            last_mint_height: number;
            denom: string;
        };
        fromAmino: (object: Version2Minter) => {
            last_mint_amount: string;
            last_mint_date: string;
            last_mint_height: number;
            denom: string;
        };
    };
    "/paxprotocol.paxchain.mint.Version2ScheduledTokenRelease": {
        aminoType: string;
        toAmino: (message: Version2ScheduledTokenRelease) => {
            date: string;
            token_release_amount: number;
        };
        fromAmino: (object: Version2ScheduledTokenRelease) => {
            date: string;
            token_release_amount: number;
        };
    };
    "/paxprotocol.paxchain.mint.Version2Params": {
        aminoType: string;
        toAmino: (message: Version2Params) => {
            mint_denom: string;
            token_release_schedule: Version2ScheduledTokenRelease_type[];
        };
        fromAmino: (object: Version2Params) => {
            mint_denom: string;
            token_release_schedule: Version2ScheduledTokenRelease_type[];
        };
    };
};
