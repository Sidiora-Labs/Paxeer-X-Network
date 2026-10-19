import type { GeneratedType } from "@cosmjs/proto-signing";
import type { Timestamp as Timestamp_type } from "../../../types/google/protobuf";
import type { MessageFns } from "../../common";
export interface Timestamp extends Timestamp_type {
}
export declare const Timestamp: MessageFns<Timestamp, "google.protobuf.Timestamp">;
export declare const registry: Array<[string, GeneratedType]>;
export declare const aminoConverters: {
    "/google.protobuf.Timestamp": {
        aminoType: string;
        toAmino: (message: Timestamp) => {
            seconds: number;
            nanos: number;
        };
        fromAmino: (object: Timestamp) => {
            seconds: number;
            nanos: number;
        };
    };
};
