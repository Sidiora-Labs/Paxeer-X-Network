'use client';

import { useEffect, useId, useMemo, useState } from 'react';
import { ArrowDownToLine, ChevronDown, CircleDollarSign } from 'lucide-react';
import { cn } from '@/lib/utils';
import { Button, Chip, IconButton, SegmentedControl, Skeleton, UnavailableState } from '@/ui/primitives';
import { formatPercent, formatUsdl } from '@/utils/format';
import { openAccountMenu } from '@/shell/account-menu-store';
import { isSignedOutError } from '@/core/market/unsupported';
import { SignedOutState } from '@/ui/shared/signed-out-state';
import { MaskedAmount } from './balance-visibility';
import { PortfolioChart } from './portfolio-chart';
import { portfolioIssue, portfolioWindows, seriesPoints, usePortfolio, useValueHistory, windowDelta, type PortfolioWindow } from './portfolio-transport';
import { PortfolioIssueState } from './portfolio-issue-state';
import { Dash, useProfileIssue } from './profile-issues';

export interface PortfolioBlockProps {
  wallet: string;
  isOwner: boolean;
  className?: string;
}

const COLLAPSE_KEY = 'sidiora_portfolio_chart_collapsed';

function readCollapsed(): boolean {
  try { return window.localStorage.getItem(COLLAPSE_KEY) === '1'; } catch { return false; }
}

function persistCollapsed(collapsed: boolean) {
  try { if (collapsed) window.localStorage.setItem(COLLAPSE_KEY, '1'); else window.localStorage.removeItem(COLLAPSE_KEY); } catch { /* preference lives for this page only */ }
}

/** "1,843.41" -> large integer part and smaller fraction part, one text node for assistive tech. */
export function splitAmount(text: string): { integer: string; fraction: string | null } {
  const dot = text.indexOf('.');
  if (dot < 0) return { integer: text, fraction: null };
  return { integer: text.slice(0, dot), fraction: text.slice(dot) };
}

const deltaClass = { gain: 'text-success', loss: 'text-danger', flat: 'text-text-muted' } as const;

/**
 * S16 portfolio value block: value with split typography, window delta, rank
 * line, 1D/1W/1M segmented control, USDL chip, Deposit (own profile) and the
 * collapsible value chart. Every amount honours the balance visibility toggle.
 */
export function PortfolioBlock({ wallet, isOwner, className }: PortfolioBlockProps) {
  const portfolio = usePortfolio(wallet, isOwner);
  const owner = portfolio.owner;
  const [range, setRange] = useState<PortfolioWindow>('1D');
  const [collapsed, setCollapsed] = useState(false);
  useEffect(() => { setCollapsed(readCollapsed()); }, []);
  const history = useValueHistory(wallet, range, owner);
  const points = useMemo(() => (history.state === 'ready' ? seriesPoints(history.data) : []), [history]);
  const delta = useMemo(() => windowDelta(points), [points]);

  const issue = portfolioIssue(portfolio.loadable, owner);
  const historyIssue = owner ? portfolioIssue(history, true) : null;
  const summary = portfolio.loadable.state === 'ready' ? portfolio.loadable.data.data.summary : null;
  const valueRaw = summary ? summary.completeValueUsdlRaw ?? summary.pricedSubtotalUsdlRaw : null;
  const partialValue = !!summary && summary.completeValueUsdlRaw === null;
  const decimals = portfolio.loadable.state === 'ready' ? portfolio.loadable.data.decimals : 6;
  const quote = portfolio.loadable.state === 'ready' ? portfolio.loadable.data.quoteAsset : 'USDL';
  const amount = valueRaw !== null ? splitAmount(formatUsdl(valueRaw, { decimals, unit: false })) : null;
  const chartId = `${useId()}-portfolio-chart`;
  const valueFailed = portfolio.loadable.state === 'error' && !issue && !(owner && isSignedOutError(portfolio.loadable.error));
  const historyFailed = history.state === 'error' && !historyIssue && !(owner && isSignedOutError(history.error));
  const valueCollected = useProfileIssue('portfolio-value', valueFailed && portfolio.loadable.state === 'error' ? { label: 'Portfolio value', error: portfolio.loadable.error, retry: portfolio.loadable.retry } : null);
  const historyCollected = useProfileIssue('portfolio-history', historyFailed && history.state === 'error' ? { label: 'Portfolio chart', error: history.error, retry: history.retry } : null);

  const toggleCollapsed = () => setCollapsed(value => { persistCollapsed(!value); return !value; });

  return (
    <section aria-labelledby="portfolio-value-title" className={cn('flex flex-col gap-3 rounded-lg bg-surface-1 p-4', className)} data-profile-section="portfolio">
      <div className="flex flex-wrap items-start justify-between gap-3">
        <div className="min-w-0">
          <h2 id="portfolio-value-title" className="text-xs font-normal text-text-faint">Portfolio value</h2>
          {portfolio.loadable.state === 'loading' ? (
            <Skeleton width={180} height={40} label="Loading portfolio value" />
          ) : amount ? (
            <p data-numeric className="flex items-baseline tabular-nums text-text">
              <MaskedAmount className="inline-flex items-baseline">
                <span className="text-2xl font-bold">{amount.integer}</span>
                {amount.fraction ? <span className="text-lg font-semibold text-text-muted">{amount.fraction}</span> : null}
              </MaskedAmount>
              <span className="ml-1.5 text-sm font-semibold text-text-faint">{quote}</span>
            </p>
          ) : issue ? (
            <PortfolioIssueState issue={issue} variant="inline" className="max-w-md" />
          ) : portfolio.loadable.state === 'unsupported' ? (
            <UnavailableState variant="inline" title="Portfolio value" operation={portfolio.loadable.operation} status={portfolio.loadable.status} onRetry={portfolio.refetch} />
          ) : valueFailed && valueCollected ? (
            <p data-numeric className="text-2xl font-bold"><Dash label="Portfolio value" /></p>
          ) : portfolio.loadable.state === 'error' ? (
            owner && isSignedOutError(portfolio.loadable.error)
              ? <SignedOutState variant="inline" title="Sign in to see your portfolio value" />
              : <p role="alert" className="text-sm text-danger">Can't load portfolio value right now</p>
          ) : null}
          {partialValue && summary ? <p className="text-xs text-warning">{summary.unpricedAssetCount} {summary.unpricedAssetCount === 1 ? 'coin has' : 'coins have'} no price yet, so this total leaves them out.</p> : null}
          <div className="mt-1 flex flex-wrap items-baseline gap-x-2 text-sm font-semibold">
            {owner && history.state === 'loading' ? (
              <Skeleton width={140} height={16} label="Loading change" />
            ) : delta ? (
              <>
                <MaskedAmount className={cn('tabular-nums', deltaClass[delta.tone])}>
                  {formatUsdl(delta.absRaw, { decimals, signed: true })}{delta.pct !== null ? ` (${formatPercent(delta.pct)})` : ''}
                </MaskedAmount>
                <span className="text-xs text-text-faint">{range}</span>
              </>
            ) : history.state === 'ready' ? (
              <span className="text-xs text-text-faint">No change yet</span>
            ) : null}
          </div>
        </div>
        <div className="flex flex-wrap items-center gap-2">
          <Chip variant="token" size="md" icon={<CircleDollarSign />} className="hover:bg-surface-2 hover:text-text-muted">
            <span className="sr-only">Quoted in </span>{quote}
          </Chip>
          {owner ? <Button variant="secondary" size="sm" icon={<ArrowDownToLine />} onClick={() => openAccountMenu('receive')}>Deposit</Button> : null}
        </div>
      </div>

      <div className="flex items-center justify-between gap-2">
        <SegmentedControl<PortfolioWindow>
          label="Time range"
          size="sm"
          options={portfolioWindows.map(value => ({ value, label: value }))}
          value={range}
          onChange={setRange}
        />
        <IconButton
          label={collapsed ? 'Expand portfolio chart' : 'Collapse portfolio chart'}
          icon={<ChevronDown className={cn('transition-transform duration-fast', !collapsed && 'rotate-180')} />}
          size="sm"
          aria-expanded={!collapsed}
          aria-controls={chartId}
          onClick={toggleCollapsed}
        />
      </div>

      <div id={chartId} hidden={collapsed}>
        {collapsed ? null : history.state === 'loading' ? (
          <Skeleton variant="block" height={200} label="Loading portfolio chart" className="w-full" />
        ) : history.state === 'ready' ? (
          points.length === 0 ? (
            <p className="flex h-40 items-center justify-center rounded-md bg-surface-2 px-4 text-center text-sm text-text-faint">No history yet. The chart fills in once this wallet holds coins.</p>
          ) : (
            <PortfolioChart points={points} tone={delta?.tone ?? 'flat'} height={180} />
          )
        ) : historyIssue ? (
          <PortfolioIssueState issue={historyIssue} variant="inline" />
        ) : history.state === 'unsupported' ? (
          <div className="flex h-40 items-center justify-center rounded-md bg-surface-1 px-4 text-center">
            <UnavailableState variant="inline" title="Portfolio chart" operation={history.operation} status={history.status} message={history.message} />
          </div>
        ) : owner && isSignedOutError(history.error) ? (
          <SignedOutState variant="inline" title="Sign in to see your value history" />
        ) : historyCollected ? (
          <div aria-hidden="true" data-chart-placeholder className="h-40 rounded-md bg-surface-1" />
        ) : (
          <p role="alert" className="text-sm text-danger">Can't load the chart right now</p>
        )}
      </div>

    </section>
  );
}
