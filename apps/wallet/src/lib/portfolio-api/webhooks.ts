/**
 * v2 Webhooks — balance change notifications.
 *
 * Endpoints:
 *   POST   /api/v2/webhooks                       — create
 *   GET    /api/v2/webhooks?address=…             — list by address
 *   GET    /api/v2/webhooks/{id}                  — get one
 *   PATCH  /api/v2/webhooks/{id}                  — update
 *   DELETE /api/v2/webhooks/{id}                  — delete
 *   GET    /api/v2/webhooks/{id}/deliveries       — recent deliveries
 *
 * Wire payloads are snake_case; this module maps from camelCase TS shapes.
 */

import { portfolioApi, toSnake } from './client';
import type {
  CreateWebhookRequest,
  UpdateWebhookRequest,
  WebhookResponse,
  WebhookDeliveryEntry,
} from './types';

export const createWebhook = (body: CreateWebhookRequest, signal?: AbortSignal) =>
  portfolioApi.post<WebhookResponse>('/api/v2/webhooks', toSnake(body as unknown as Record<string, unknown>), {
    signal,
  });

export const listWebhooks = (address: string, signal?: AbortSignal) =>
  portfolioApi.get<{ webhooks?: WebhookResponse[]; [key: string]: unknown }>(
    '/api/v2/webhooks',
    { address },
    { signal },
  );

export const getWebhook = (id: string, signal?: AbortSignal) =>
  portfolioApi.get<WebhookResponse>(
    `/api/v2/webhooks/${encodeURIComponent(id)}`,
    undefined,
    { signal },
  );

export const updateWebhook = (id: string, body: UpdateWebhookRequest, signal?: AbortSignal) =>
  portfolioApi.patch<WebhookResponse>(
    `/api/v2/webhooks/${encodeURIComponent(id)}`,
    toSnake(body as unknown as Record<string, unknown>),
    { signal },
  );

export const deleteWebhook = (id: string, signal?: AbortSignal) =>
  portfolioApi.delete<{ status?: string; [key: string]: unknown }>(
    `/api/v2/webhooks/${encodeURIComponent(id)}`,
    { signal },
  );

export const getWebhookDeliveries = (id: string, limit?: number, signal?: AbortSignal) =>
  portfolioApi.get<{ deliveries?: WebhookDeliveryEntry[]; [key: string]: unknown }>(
    `/api/v2/webhooks/${encodeURIComponent(id)}/deliveries`,
    { limit },
    { signal },
  );
