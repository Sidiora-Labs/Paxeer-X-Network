// eslint-disable-next-line @typescript-eslint/no-namespace
export declare namespace RefferalAPI {
  export interface ReferralInfo {
    referrer_info: Referrer;
    referee_info: Referee;
  }

  export type ReferralCode = {
    code: string;
    max_rebate_rate: number;
    referee_rebate_rate: number;
    referrer_rebate_rate: number;
    total_invites: number;
    total_traded: number;
    total_volume: number;
    total_rebate: number;
  };

  export type AutoGenerateCode = {
    code: string;
    requireVolume: number;
    completedVolume: number;
  };

  export type Referrer = {
    total_invites: number;
    total_traded: number;
    total_referee_volume: number;
    total_referee_fee: number;
    referral_codes: ReferralCode[];
    total_referrer_rebate: number;
    "1d_invites": number;
    "7d_invites": number;
    "30d_invites": number;
    "1d_traded": number;
    "7d_traded": number;
    "30d_traded": number;
    "1d_referee_volume": number;
    "7d_referee_volume": number;
    "30d_referee_volume": number;
    "1d_referee_fee": number;
    "7d_referee_fee": number;
    "30d_referee_fee": number;
    "1d_referrer_rebate": number;
    "7d_referrer_rebate": number;
    "30d_referrer_rebate": number;
  };

  export type Referee = {
    referer_code?: string;
    referee_rebate_rate?: number;
    "1d_referee_rebate": number;
    "7d_referee_rebate": number;
    "30d_referee_rebate": number;
    total_referee_rebate: number;
  };

  export type RefereeInfoItem = {
    account_id: string;
    code_binding_time: number;
    fee: number;
    referral_code: string;
    referral_rebate: number;
    register_time: number;
    trade_status: string;
    user_address: string;
    volume: number;
  };

  //** use volume statistics */
  export type UserVolStats = {
    perp_volume_last_30_days: number;
    perp_volume_last_7_days: number;
    perp_volume_ltd: number;
    perp_volume_ytd: number;
  };

  export type Distribution = {
    amount: number;
    created_time: number;
    id: number;
    status: string;
    token: string;
    type: string;
    updated_time: number;
  };

  export type ReferralRebateSummary = {
    daily_traded_referral: number;
    direct_traded_referral: number;
    indirect_traded_referral: number;
    referral_rebate: number;
    direct_rebate: number;
    indirect_rebate: number;
    direct_bonus_rebate: number;
    volume: number;
    direct_volume: number;
    indirect_volume: number;
    fee: number;
    date: string;
  };

  export type RefereeRebateSummary = {
    referee_rebate: number;
    fee: number;
    date: string;
  };

  export type DayliVolume = {
    date: string;
    perp_volume: number;
  };
}
