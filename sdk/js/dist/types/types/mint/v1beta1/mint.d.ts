export interface Minter {
    /** yyyy-mm-dd */
    start_date: string;
    /** yyyy-mm-dd */
    end_date: string;
    denom: string;
    total_mint_amount: number;
    remaining_mint_amount: number;
    last_mint_amount: number;
    last_mint_date: string;
    /** yyyy-mm-dd */
    last_mint_height: number;
}
export interface ScheduledTokenRelease {
    /** yyyy-mm-dd */
    start_date: string;
    /** yyyy-mm-dd */
    end_date: string;
    token_release_amount: number;
}
export interface Params {
    /** type of coin to mint */
    mint_denom: string;
    /** List of token release schedules */
    token_release_schedule: ScheduledTokenRelease[];
}
export interface Version2Minter {
    last_mint_amount: string;
    last_mint_date: string;
    last_mint_height: number;
    denom: string;
}
export interface Version2ScheduledTokenRelease {
    /** yyyy-mm-dd */
    date: string;
    token_release_amount: number;
}
export interface Version2Params {
    /** type of coin to mint */
    mint_denom: string;
    /** List of token release schedules */
    token_release_schedule: Version2ScheduledTokenRelease[];
}
