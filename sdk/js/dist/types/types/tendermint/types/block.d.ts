import type { EvidenceList } from "./evidence";
import type { Commit, Data, Header } from "./types";
export interface Block {
    header?: Header;
    data?: Data;
    evidence?: EvidenceList;
    last_commit?: Commit;
}
