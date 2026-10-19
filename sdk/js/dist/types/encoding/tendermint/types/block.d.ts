import type { GeneratedType } from "@cosmjs/proto-signing";
import type { Block as Block_type } from "../../../types/tendermint/types";
import type { MessageFns } from "../../common";
export interface Block extends Block_type {
}
export declare const Block: MessageFns<Block, "tendermint.types.Block">;
export declare const registry: Array<[string, GeneratedType]>;
export declare const aminoConverters: {
    "/tendermint.types.Block": {
        aminoType: string;
        toAmino: (message: Block) => {
            header?: import("../../../types/tendermint/types").Header;
            data?: import("../../../types/tendermint/types").Data;
            evidence?: import("../../../types/tendermint/types").EvidenceList;
            last_commit?: import("../../../types/tendermint/types").Commit;
        };
        fromAmino: (object: Block) => {
            header?: import("../../../types/tendermint/types").Header;
            data?: import("../../../types/tendermint/types").Data;
            evidence?: import("../../../types/tendermint/types").EvidenceList;
            last_commit?: import("../../../types/tendermint/types").Commit;
        };
    };
};
