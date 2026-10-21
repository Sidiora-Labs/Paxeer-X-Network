type AnalyticsEvent =
  | 'wallet_connect_started'
  | 'wallet_connected'
  | 'trade_quote_requested'
  | 'trade_submitted'
  | 'trade_confirmed'
  | 'trade_failed'
  | 'chat_send_failed'
  | 'approval_submitted'
  | 'approval_confirmed'
  | 'swap_submitted'
  | 'swap_confirmed'
  | 'swap_failed';

type EventProperties = Record<string, string | number | boolean | null | undefined>;

export function trackEvent(event: AnalyticsEvent, properties?: EventProperties): void {
  // No-op when no analytics key is configured
  const key = process.env.NEXT_PUBLIC_ANALYTICS_KEY;
  if (!key) return;

  if (process.env.NODE_ENV !== 'production') {
    console.debug('[Sidiora Analytics]', event, properties);
  }

  // Future: PostHog / Mixpanel / Plausible integration
  // posthog.capture(event, properties);
}
