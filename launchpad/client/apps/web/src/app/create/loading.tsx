import { CreatePageFallback } from '@/page-modules/create';

/**
 * Route-level loading UI for /create. The app shell (rail, top bar, ticker,
 * footer) comes from the root layout, so this only fills the content area with
 * the create wizard's own skeleton frame instead of the global centred spinner.
 */
export default function Loading() {
  return <CreatePageFallback />;
}
