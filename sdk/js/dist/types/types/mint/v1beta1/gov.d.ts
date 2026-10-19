import type { Minter } from "./mint";
export interface UpdateMinterProposal {
    title: string;
    description: string;
    minter?: Minter;
}
