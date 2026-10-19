import type { GeneratedType } from "@cosmjs/proto-signing";
import type { Duration as Duration_type } from "../../../types/google/protobuf";
import type { MessageFns } from "../../common";
export interface Duration extends Duration_type {
}
export declare const Duration: MessageFns<Duration, "google.protobuf.Duration">;
export declare const registry: Array<[string, GeneratedType]>;
export declare const aminoConverters: {
    "/google.protobuf.Duration": {
        aminoType: string;
        toAmino: (message: Duration) => {
            seconds: number;
            nanos: number;
        };
        fromAmino: (object: Duration) => {
            seconds: number;
            nanos: number;
        };
    };
};
