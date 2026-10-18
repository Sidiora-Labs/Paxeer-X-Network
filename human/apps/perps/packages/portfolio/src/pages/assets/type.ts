// Asset conversion types
export interface ConvertedAssets {
  [asset: string]: number;
}

export interface ConvertTransaction {
  transaction_id: number;
  venue: "on_chain" | "internal_fund";
  converted_asset: string;
  received_asset: string;
  converted_qty: number;
  received_qty: number;
  haircut: number;
  chain_id?: number; // Optional, only for on_chain venue
  tx_id?: string; // Optional, only for on_chain venue
  result?: string;
}

export interface ConvertRecord {
  convert_id: number;
  converted_asset: ConvertedAssets;
  received_asset: string;
  received_qty: number;
  type: "auto" | "manual";
  status: "completed" | "pending" | "failed" | "cancelled";
  created_time: number;
  updated_time: number;
  details: ConvertTransaction[];
}

// Additional utility types
export type ConvertType = "auto" | "manual";

export type ConvertStatus = "completed" | "pending" | "failed" | "cancelled";

export type VenueType = "on_chain" | "internal_fund";

export const PAXEER_NETWORK_ASSETS_VISIBLE_KEY = "paxeerNetwork_assets_visible";
