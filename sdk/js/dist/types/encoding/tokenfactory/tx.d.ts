import type { GeneratedType } from "@cosmjs/proto-signing";
import type { MsgBurnResponse as MsgBurnResponse_type, MsgBurn as MsgBurn_type, MsgChangeAdminResponse as MsgChangeAdminResponse_type, MsgChangeAdmin as MsgChangeAdmin_type, MsgCreateDenomResponse as MsgCreateDenomResponse_type, MsgCreateDenom as MsgCreateDenom_type, MsgMintResponse as MsgMintResponse_type, MsgMint as MsgMint_type, MsgSetDenomMetadataResponse as MsgSetDenomMetadataResponse_type, MsgSetDenomMetadata as MsgSetDenomMetadata_type, MsgUpdateDenomResponse as MsgUpdateDenomResponse_type, MsgUpdateDenom as MsgUpdateDenom_type } from "../../types/tokenfactory";
import type { MessageFns } from "../common";
export interface MsgCreateDenom extends MsgCreateDenom_type {
}
export interface MsgCreateDenomResponse extends MsgCreateDenomResponse_type {
}
export interface MsgMint extends MsgMint_type {
}
export interface MsgMintResponse extends MsgMintResponse_type {
}
export interface MsgBurn extends MsgBurn_type {
}
export interface MsgBurnResponse extends MsgBurnResponse_type {
}
export interface MsgChangeAdmin extends MsgChangeAdmin_type {
}
export interface MsgChangeAdminResponse extends MsgChangeAdminResponse_type {
}
export interface MsgSetDenomMetadata extends MsgSetDenomMetadata_type {
}
export interface MsgSetDenomMetadataResponse extends MsgSetDenomMetadataResponse_type {
}
export interface MsgUpdateDenom extends MsgUpdateDenom_type {
}
export interface MsgUpdateDenomResponse extends MsgUpdateDenomResponse_type {
}
export declare const MsgCreateDenom: MessageFns<MsgCreateDenom, "paxprotocol.paxchain.tokenfactory.MsgCreateDenom">;
export declare const MsgCreateDenomResponse: MessageFns<MsgCreateDenomResponse, "paxprotocol.paxchain.tokenfactory.MsgCreateDenomResponse">;
export declare const MsgMint: MessageFns<MsgMint, "paxprotocol.paxchain.tokenfactory.MsgMint">;
export declare const MsgMintResponse: MessageFns<MsgMintResponse, "paxprotocol.paxchain.tokenfactory.MsgMintResponse">;
export declare const MsgBurn: MessageFns<MsgBurn, "paxprotocol.paxchain.tokenfactory.MsgBurn">;
export declare const MsgBurnResponse: MessageFns<MsgBurnResponse, "paxprotocol.paxchain.tokenfactory.MsgBurnResponse">;
export declare const MsgChangeAdmin: MessageFns<MsgChangeAdmin, "paxprotocol.paxchain.tokenfactory.MsgChangeAdmin">;
export declare const MsgChangeAdminResponse: MessageFns<MsgChangeAdminResponse, "paxprotocol.paxchain.tokenfactory.MsgChangeAdminResponse">;
export declare const MsgSetDenomMetadata: MessageFns<MsgSetDenomMetadata, "paxprotocol.paxchain.tokenfactory.MsgSetDenomMetadata">;
export declare const MsgSetDenomMetadataResponse: MessageFns<MsgSetDenomMetadataResponse, "paxprotocol.paxchain.tokenfactory.MsgSetDenomMetadataResponse">;
export declare const MsgUpdateDenom: MessageFns<MsgUpdateDenom, "paxprotocol.paxchain.tokenfactory.MsgUpdateDenom">;
export declare const MsgUpdateDenomResponse: MessageFns<MsgUpdateDenomResponse, "paxprotocol.paxchain.tokenfactory.MsgUpdateDenomResponse">;
export declare const registry: Array<[string, GeneratedType]>;
export declare const aminoConverters: {
    "/paxprotocol.paxchain.tokenfactory.MsgCreateDenom": {
        aminoType: string;
        toAmino: (message: MsgCreateDenom) => {
            sender: string;
            subdenom: string;
            allow_list?: import("../../types/cosmos/bank/v1beta1").AllowList;
        };
        fromAmino: (object: MsgCreateDenom) => {
            sender: string;
            subdenom: string;
            allow_list?: import("../../types/cosmos/bank/v1beta1").AllowList;
        };
    };
    "/paxprotocol.paxchain.tokenfactory.MsgCreateDenomResponse": {
        aminoType: string;
        toAmino: (message: MsgCreateDenomResponse) => {
            new_token_denom: string;
        };
        fromAmino: (object: MsgCreateDenomResponse) => {
            new_token_denom: string;
        };
    };
    "/paxprotocol.paxchain.tokenfactory.MsgMint": {
        aminoType: string;
        toAmino: (message: MsgMint) => {
            sender: string;
            amount?: import("../../types/cosmos/base/v1beta1").Coin;
        };
        fromAmino: (object: MsgMint) => {
            sender: string;
            amount?: import("../../types/cosmos/base/v1beta1").Coin;
        };
    };
    "/paxprotocol.paxchain.tokenfactory.MsgMintResponse": {
        aminoType: string;
        toAmino: (message: MsgMintResponse) => {};
        fromAmino: (object: MsgMintResponse) => {};
    };
    "/paxprotocol.paxchain.tokenfactory.MsgBurn": {
        aminoType: string;
        toAmino: (message: MsgBurn) => {
            sender: string;
            amount?: import("../../types/cosmos/base/v1beta1").Coin;
        };
        fromAmino: (object: MsgBurn) => {
            sender: string;
            amount?: import("../../types/cosmos/base/v1beta1").Coin;
        };
    };
    "/paxprotocol.paxchain.tokenfactory.MsgBurnResponse": {
        aminoType: string;
        toAmino: (message: MsgBurnResponse) => {};
        fromAmino: (object: MsgBurnResponse) => {};
    };
    "/paxprotocol.paxchain.tokenfactory.MsgChangeAdmin": {
        aminoType: string;
        toAmino: (message: MsgChangeAdmin) => {
            sender: string;
            denom: string;
            new_admin: string;
        };
        fromAmino: (object: MsgChangeAdmin) => {
            sender: string;
            denom: string;
            new_admin: string;
        };
    };
    "/paxprotocol.paxchain.tokenfactory.MsgChangeAdminResponse": {
        aminoType: string;
        toAmino: (message: MsgChangeAdminResponse) => {};
        fromAmino: (object: MsgChangeAdminResponse) => {};
    };
    "/paxprotocol.paxchain.tokenfactory.MsgSetDenomMetadata": {
        aminoType: string;
        toAmino: (message: MsgSetDenomMetadata) => {
            sender: string;
            metadata?: import("../../types/cosmos/bank/v1beta1").Metadata;
        };
        fromAmino: (object: MsgSetDenomMetadata) => {
            sender: string;
            metadata?: import("../../types/cosmos/bank/v1beta1").Metadata;
        };
    };
    "/paxprotocol.paxchain.tokenfactory.MsgSetDenomMetadataResponse": {
        aminoType: string;
        toAmino: (message: MsgSetDenomMetadataResponse) => {};
        fromAmino: (object: MsgSetDenomMetadataResponse) => {};
    };
    "/paxprotocol.paxchain.tokenfactory.MsgUpdateDenom": {
        aminoType: string;
        toAmino: (message: MsgUpdateDenom) => {
            sender: string;
            denom: string;
            allow_list?: import("../../types/cosmos/bank/v1beta1").AllowList;
        };
        fromAmino: (object: MsgUpdateDenom) => {
            sender: string;
            denom: string;
            allow_list?: import("../../types/cosmos/bank/v1beta1").AllowList;
        };
    };
    "/paxprotocol.paxchain.tokenfactory.MsgUpdateDenomResponse": {
        aminoType: string;
        toAmino: (message: MsgUpdateDenomResponse) => {};
        fromAmino: (object: MsgUpdateDenomResponse) => {};
    };
};
