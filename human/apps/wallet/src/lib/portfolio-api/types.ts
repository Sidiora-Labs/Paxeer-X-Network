/**
 * Portfolio API v2 — Watch & Webhooks request/response types.
 *
 * These were previously sourced from `@paxeer/portfolio-sdk`. We replaced the
 * 4×@apimatic-runtime auto-generated SDK with a thin native fetch layer
 * because the wallet only consumes 9 endpoints out of the SDK's full surface.
 *
 * Wire format is snake_case; TypeScript shape is camelCase. The fetch helpers
 * map between them on send/receive.
 */

export interface ApiV2WatchRequest {
  address: string;
}

export interface ApiV2WatchResponse {
  status?: string;
  address?: string;
  watched_at?: string;
  [key: string]: unknown;
}

export interface ApiV2WatchStatusResponse {
  address?: string;
  watching?: boolean;
  webhook_count?: number;
  last_event_at?: string;
  [key: string]: unknown;
}

export interface CreateWebhookRequest {
  address: string;
  callbackUrl: string;
  /** HMAC-SHA256 signing secret */
  secret?: string;
  events?: string[];
  /** Minimum USD delta to trigger */
  minDeltaUsd?: string;
}

export interface UpdateWebhookRequest {
  callbackUrl?: string;
  events?: string[];
  minDeltaUsd?: string;
  isActive?: boolean;
}

export interface WebhookResponse {
  id?: string;
  address?: string;
  callback_url?: string;
  events?: string[];
  min_delta_usd?: string;
  is_active?: boolean;
  created_at?: string;
  [key: string]: unknown;
}

export interface WebhookDeliveryEntry {
  id?: string;
  webhook_id?: string;
  status?: string;
  attempted_at?: string;
  response_status?: number;
  [key: string]: unknown;
}
