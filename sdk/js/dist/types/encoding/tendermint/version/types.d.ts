import type { GeneratedType } from "@cosmjs/proto-signing";
import type { App as App_type, Consensus as Consensus_type } from "../../../types/tendermint/version";
import type { MessageFns } from "../../common";
export interface App extends App_type {
}
export interface Consensus extends Consensus_type {
}
export declare const App: MessageFns<App, "tendermint.version.App">;
export declare const Consensus: MessageFns<Consensus, "tendermint.version.Consensus">;
export declare const registry: Array<[string, GeneratedType]>;
export declare const aminoConverters: {
    "/tendermint.version.App": {
        aminoType: string;
        toAmino: (message: App) => {
            protocol: number;
            software: string;
        };
        fromAmino: (object: App) => {
            protocol: number;
            software: string;
        };
    };
    "/tendermint.version.Consensus": {
        aminoType: string;
        toAmino: (message: Consensus) => {
            block: number;
            app: number;
        };
        fromAmino: (object: Consensus) => {
            block: number;
            app: number;
        };
    };
};
