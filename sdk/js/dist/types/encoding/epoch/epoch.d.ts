import type { GeneratedType } from "@cosmjs/proto-signing";
import type { Epoch as Epoch_type } from "../../types/epoch";
import type { MessageFns } from "../common";
export interface Epoch extends Epoch_type {
}
export declare const Epoch: MessageFns<Epoch, "paxprotocol.paxchain.epoch.Epoch">;
export declare const registry: Array<[string, GeneratedType]>;
export declare const aminoConverters: {
    "/paxprotocol.paxchain.epoch.Epoch": {
        aminoType: string;
        toAmino: (message: Epoch) => {
            genesis_time?: Date;
            epoch_duration?: import("../../types/google/protobuf").Duration;
            current_epoch: number;
            current_epoch_start_time?: Date;
            current_epoch_height: number;
        };
        fromAmino: (object: Epoch) => {
            genesis_time?: Date;
            epoch_duration?: import("../../types/google/protobuf").Duration;
            current_epoch: number;
            current_epoch_start_time?: Date;
            current_epoch_height: number;
        };
    };
};
