'use client';
import type { ReactNode } from 'react';
import { BookOpen, Coins, PieChart, Send, Wallet } from 'lucide-react';
import Link from 'next/link';
import { useAccount, useBalance } from 'wagmi';
import { USDL_ADDRESS, useReadErc20Balance } from '@/core/network/contracts';
import { safeExternalDestination } from '@/core/app-foundation/navigation';
import { Skeleton, Tooltip } from '@/ui/primitives';
import { formatTokenAmount, formatUsdl } from '@/utils/format';
import { readPortfolio } from '@/widgets/profile/account-transport';
import { MaskedAmount } from '@/widgets/profile/balance-visibility';
import { usePrivateAccountQuery } from '@/widgets/profile/use-private-account-query';

function Amount({ icon, label, value, loading, title }: { icon: ReactNode; label: string; value: string | null; loading: boolean; title: string }) {
  return (
    <Tooltip content={title}>
      <span tabIndex={0} aria-label={`${label} ${value ?? 'not available yet'}`} className="inline-flex shrink-0 items-center gap-1 text-xs tabular-nums text-text" data-numeric>
        <span aria-hidden="true" className="inline-flex text-text-faint [&>svg]:size-icon-sm">{icon}</span>
        {loading ? <Skeleton variant="text" width={40} className="h-3" /> : value === null ? <span className="text-text-faint">-</span> : <MaskedAmount>{value}</MaskedAmount>}
      </span>
    </Tooltip>
  );
}

function ConnectedBalances({ account }: { account: `0x${string}` }) {
  const usdl = useReadErc20Balance({ token: USDL_ADDRESS, account });
  const pax = useBalance({ address: account, chainId: 125 });
  const portfolio = usePrivateAccountQuery('portfolio-summary', () => readPortfolio(), [], account);
  const summary = portfolio.data?.data.summary;
  const valueRaw = summary ? summary.completeValueUsdlRaw ?? summary.pricedSubtotalUsdlRaw : null;
  const usdlRaw = typeof usdl.data === 'bigint' ? usdl.data : null;
  return (
    <>
      <Amount icon={<Coins />} label="USDL balance" loading={usdl.isPending} value={usdlRaw === null ? null : formatUsdl(usdlRaw, { compact: true })}
        title={usdl.isError ? "Your USDL balance can't load right now" : 'Your USDL balance'} />
      <Amount icon={<Wallet />} label="PAX balance" loading={pax.isPending} value={pax.data ? `${formatTokenAmount(pax.data.value, 18, { compact: true })} PAX` : null}
        title={pax.isError ? "Your PAX balance can't load right now" : 'Your PAX balance, used for network fees'} />
      {portfolio.owner ? (
        <Amount icon={<PieChart />} label="Portfolio value" loading={portfolio.isPending} value={valueRaw === null ? null : formatUsdl(valueRaw, { compact: true })}
          title={summary ? (summary.completeValueUsdlRaw === null ? 'Value of your coins, some have no price yet' : 'Value of your coins') : "Portfolio value can't load right now"} />
      ) : null}
    </>
  );
}

/** USDL and PAX wallet balances plus the signed-in portfolio value; nothing while no wallet is connected. */
export function BalancesCluster() {
  const { address } = useAccount();
  if (!address) return <span className="shrink-0 text-xs text-text-faint">No wallet connected</span>;
  return <ConnectedBalances account={address} />;
}

const supportUrl = safeExternalDestination('https://t.me/Sidiorafunsupport');
const linkClass = 'inline-flex size-6 shrink-0 items-center justify-center rounded-sm text-text-muted transition-colors duration-fast hover:bg-surface-hover hover:text-text [&_svg]:size-icon-sm';

/** Community and docs links; only destinations accepted by safeExternalDestination are rendered. */
export function CommunityLinks() {
  return (
    <>
      {supportUrl ? (
        <a href={supportUrl} target="_blank" rel="noopener noreferrer" aria-label="Telegram support" title="Telegram support" className={linkClass}><Send aria-hidden="true" /></a>
      ) : null}
      <Link href="/docs" aria-label="Documentation" title="Documentation" className={linkClass}><BookOpen aria-hidden="true" /></Link>
    </>
  );
}
