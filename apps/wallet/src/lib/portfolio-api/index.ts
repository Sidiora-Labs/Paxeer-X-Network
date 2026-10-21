/**
 * Portfolio API — v2 watch + webhooks.
 *
 * Replaces the 59-file auto-generated `@paxeer/portfolio-sdk` (and its 4
 * `@apimatic/*` runtime deps) with ~80 lines of native fetch against
 * `PAXEER_CONFIG.portfolioApiBase`.
 */

export type {
  ApiV2WatchRequest,
  ApiV2WatchResponse,
  ApiV2WatchStatusResponse,
  CreateWebhookRequest,
  UpdateWebhookRequest,
  WebhookResponse,
  WebhookDeliveryEntry,
} from './types';

export { portfolioApi, PortfolioApiError } from './client';

export {
  watchAddress,
  getWatchStatus,
  unwatchAddress,
} from './watch';

export {
  createWebhook,
  listWebhooks,
  getWebhook,
  updateWebhook,
  deleteWebhook,
  getWebhookDeliveries,
} from './webhooks';
