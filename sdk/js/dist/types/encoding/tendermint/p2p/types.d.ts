import type { GeneratedType } from "@cosmjs/proto-signing";
import type { NodeInfoOther as NodeInfoOther_type, NodeInfo as NodeInfo_type, PeerAddressInfo as PeerAddressInfo_type, PeerInfo as PeerInfo_type, ProtocolVersion as ProtocolVersion_type } from "../../../types/tendermint/p2p";
import type { MessageFns } from "../../common";
export interface ProtocolVersion extends ProtocolVersion_type {
}
export interface NodeInfo extends NodeInfo_type {
}
export interface NodeInfoOther extends NodeInfoOther_type {
}
export interface PeerInfo extends PeerInfo_type {
}
export interface PeerAddressInfo extends PeerAddressInfo_type {
}
export declare const ProtocolVersion: MessageFns<ProtocolVersion, "tendermint.p2p.ProtocolVersion">;
export declare const NodeInfo: MessageFns<NodeInfo, "tendermint.p2p.NodeInfo">;
export declare const NodeInfoOther: MessageFns<NodeInfoOther, "tendermint.p2p.NodeInfoOther">;
export declare const PeerInfo: MessageFns<PeerInfo, "tendermint.p2p.PeerInfo">;
export declare const PeerAddressInfo: MessageFns<PeerAddressInfo, "tendermint.p2p.PeerAddressInfo">;
export declare const registry: Array<[string, GeneratedType]>;
export declare const aminoConverters: {
    "/tendermint.p2p.ProtocolVersion": {
        aminoType: string;
        toAmino: (message: ProtocolVersion) => {
            p2p: number;
            block: number;
            app: number;
        };
        fromAmino: (object: ProtocolVersion) => {
            p2p: number;
            block: number;
            app: number;
        };
    };
    "/tendermint.p2p.NodeInfo": {
        aminoType: string;
        toAmino: (message: NodeInfo) => {
            protocol_version?: ProtocolVersion_type;
            node_id: string;
            listen_addr: string;
            network: string;
            version: string;
            channels: Uint8Array;
            moniker: string;
            other?: NodeInfoOther_type;
        };
        fromAmino: (object: NodeInfo) => {
            protocol_version?: ProtocolVersion_type;
            node_id: string;
            listen_addr: string;
            network: string;
            version: string;
            channels: Uint8Array;
            moniker: string;
            other?: NodeInfoOther_type;
        };
    };
    "/tendermint.p2p.NodeInfoOther": {
        aminoType: string;
        toAmino: (message: NodeInfoOther) => {
            tx_index: string;
            rpc_address: string;
        };
        fromAmino: (object: NodeInfoOther) => {
            tx_index: string;
            rpc_address: string;
        };
    };
    "/tendermint.p2p.PeerInfo": {
        aminoType: string;
        toAmino: (message: PeerInfo) => {
            id: string;
            address_info: PeerAddressInfo_type[];
            last_connected?: Date;
        };
        fromAmino: (object: PeerInfo) => {
            id: string;
            address_info: PeerAddressInfo_type[];
            last_connected?: Date;
        };
    };
    "/tendermint.p2p.PeerAddressInfo": {
        aminoType: string;
        toAmino: (message: PeerAddressInfo) => {
            address: string;
            last_dial_success?: Date;
            last_dial_failure?: Date;
            dial_failures: number;
        };
        fromAmino: (object: PeerAddressInfo) => {
            address: string;
            last_dial_success?: Date;
            last_dial_failure?: Date;
            dial_failures: number;
        };
    };
};
