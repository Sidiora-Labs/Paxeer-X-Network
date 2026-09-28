'use client';

import { motion } from 'framer-motion';
import { Clock, AlertTriangle, ExternalLink, Fuel } from 'lucide-react';
import { useWalletState } from '@/providers/WalletProvider';
import { shortenAddress, formatBalance } from '@/lib/format';
import { PAX_ICON_URL } from '@/lib/constants';
import { SvgIcon } from '@/components/ui/SvgIcon';
import { useTxDetailQuery } from '@/lib/queries/txDetail';
import { ethers } from 'ethers';
import Image from "next/image";

interface TxDetailWidgetProps {
  txHash: string;
  onBack?: () => void;
  onPaxscan?: (path?: string) => void;
}

function fmtUnits(value: string, decimals = 18) {
  try { return ethers.formatUnits(value || '0', decimals); } catch { return value; }
}

function fmtDate(ts: string) {
  try {
    const d = new Date(ts);
    return d.toLocaleDateString('en-US', { month: 'short', day: 'numeric', year: 'numeric' })
      + ' at ' + d.toLocaleTimeString('en-US', { hour: 'numeric', minute: '2-digit', hour12: true });
  } catch { return ts; }
}

export function TxDetailWidget({ txHash, onBack, onPaxscan }: TxDetailWidgetProps) {
  const { activeAccount } = useWalletState();
  const { data, isLoading, error } = useTxDetailQuery(txHash);

  const tx = data?.tx as any;
  const tokenTransfers = (data?.tokenTransfers ?? []) as any[];

  const addr = activeAccount?.address?.toLowerCase() || '';
  const isSent = tx?.from?.hash?.toLowerCase() === addr;

  const primaryTransfer = tokenTransfers[0];
  const isTokenTx = !!primaryTransfer;
  const symbol = isTokenTx ? (primaryTransfer.token?.symbol || '???') : 'PAX';
  const decimals = isTokenTx ? Number(primaryTransfer.token?.decimals || 18) : 18;
  const rawAmount = isTokenTx ? (primaryTransfer.total?.value || '0') : (tx?.value || '0');
  const iconUrl = isTokenTx ? (primaryTransfer.token?.icon_url || '/default_icon.webp') : PAX_ICON_URL;

  const fmtAmount = () => {
    const n = Number(fmtUnits(rawAmount, decimals));
    if (n === 0) return '0';
    if (n < 0.0001) return '< 0.0001';
    if (n >= 1000000) return n.toLocaleString('en-US', { maximumFractionDigits: 2 });
    if (n >= 1) return n.toLocaleString('en-US', { maximumFractionDigits: 4 });
    return n.toFixed(5);
  };

  const fmtFee = () => {
    if (!tx?.fee?.value) return '0 PAX';
    const n = Number(fmtUnits(tx.fee.value, 18));
    if (n === 0) return '0 PAX';
    if (n < 0.00001) return '< 0.00001 PAX';
    return `-${n.toFixed(6)} PAX`;
  };

  const statusLabel = tx?.status === 'ok' ? 'Succeeded' : tx?.status === 'error' ? 'Failed' : 'Pending';
  const statusColor = tx?.status === 'ok' ? 'text-green-400' : tx?.status === 'error' ? 'text-red-400' : 'text-amber-400';
  const directionLabel = isSent ? 'Sent' : 'Received';
  const directionColor = isSent ? 'text-red-400' : 'text-green-400';
  const counterparty = isSent ? shortenAddress(tx?.to?.hash || '') : shortenAddress(tx?.from?.hash || '');
  const counterpartyLabel = isSent ? 'To' : 'From';

  const isPending = tx?.status === 'pending';
  const isFailed = tx?.status === 'error';
  const revertReason = tx?.revert_reason
    ?? tx?.decode?.parameters?.find((p: any) => p.name === 'revert_reason')?.value;

  const fmtGasUsed = () => {
    if (!tx?.gas_used) return null;
    const used = Number(tx.gas_used);
    const limit = Number(tx.gas_limit || tx.gas || 0);
    const utilPct = limit > 0 ? Math.round((used / limit) * 100) : null;
    return { used: used.toLocaleString(), limit: limit > 0 ? limit.toLocaleString() : null, utilPct };
  };

  const fmtGasPrice = () => {
    if (!tx?.gas_price) return null;
    try {
      const gwei = Number(ethers.formatUnits(tx.gas_price, 'gwei'));
      return gwei < 1 ? `${(gwei * 1000).toFixed(2)} mGwei` : `${gwei.toFixed(2)} Gwei`;
    } catch { return null; }
  };

  const gasInfo = fmtGasUsed();
  const gasPriceLabel = fmtGasPrice();

  return (
    <div className="flex flex-col min-h-[100dvh]">
      <div className="flex-1 px-4 pt-6 pb-4">
        {isLoading ? (
          <div className="space-y-4 pt-12">
            <div className="h-8 w-24 mx-auto shimmer rounded-lg" />
            <div className="h-16 w-16 mx-auto shimmer rounded-full" />
            <div className="h-12 w-48 mx-auto shimmer rounded-lg" />
            <div className="h-48 shimmer rounded-2xl mt-8" />
          </div>
        ) : error ? (
          <motion.div
            initial={{ opacity: 0, y: 8 }}
            animate={{ opacity: 1, y: 0 }}
            className="flex flex-col items-center justify-center text-center py-16 px-4 gap-4 mt-4"
          >
            <div className="w-16 h-16 rounded-2xl bg-red-500/10 flex items-center justify-center">
              <AlertTriangle className="w-7 h-7 text-red-400" />
            </div>
            <div className="space-y-1">
              <p className="text-base font-semibold text-white/80">Could not load transaction</p>
              <p className="text-sm text-pax-muted max-w-[260px] leading-relaxed">{(error as Error).message}</p>
            </div>
            {onPaxscan && (
              <button
                onClick={() => onPaxscan(`/tx/${txHash}`)}
                className="flex items-center gap-2 px-5 py-2.5 rounded-xl bg-white/[0.08] text-sm font-medium press-scale"
              >
                <ExternalLink className="w-4 h-4" />
                Try PaxScan
              </button>
            )}
          </motion.div>
        ) : tx ? (
          <>
            <div className="text-center pt-4 pb-6">
              <p className={`text-lg font-semibold mb-4 ${directionColor}`}>{directionLabel}</p>
              <div className="relative w-16 h-16 mx-auto mb-5">
                <Image src={iconUrl} alt={symbol} className="w-16 h-16 rounded-full bg-white/10 object-cover" onError={(e) => { (e.target as HTMLImageElement).src = '/default_icon.webp'; }} width={64} height={64} />
                <div className={`absolute -bottom-1 -right-1 w-7 h-7 rounded-full flex items-center justify-center ${isSent ? 'bg-red-500' : 'bg-green-500'}`}>
                  <SvgIcon name={isSent ? 'arrow-up-right' : 'arrow-down-left'} className="w-3.5 h-3.5" style={{ filter: 'brightness(0) invert(1)' }} />
                </div>
              </div>
              <p className={`text-3xl font-bold tracking-tight ${directionColor}`}>
                {isSent ? '-' : '+'}{fmtAmount()} {symbol}
              </p>
            </div>

            {/* Pending banner */}
            {isPending && (
              <motion.div
                initial={{ opacity: 0, y: -4 }}
                animate={{ opacity: 1, y: 0 }}
                className="flex items-center gap-3 mb-4 px-4 py-3 rounded-xl bg-amber-500/10  "
              >
                <Clock className="w-4 h-4 text-amber-400 shrink-0" />
                <div className="flex-1">
                  <p className="text-xs font-semibold text-amber-400">Transaction Pending</p>
                  <p className="text-[11px] text-amber-300/70 mt-0.5">Waiting for confirmation on the network</p>
                </div>
              </motion.div>
            )}

            {/* Failed revert reason */}
            {isFailed && revertReason && (
              <motion.div
                initial={{ opacity: 0, y: -4 }}
                animate={{ opacity: 1, y: 0 }}
                className="mb-4 px-4 py-3 rounded-xl bg-red-500/8  "
              >
                <p className="text-xs font-semibold text-red-400 mb-1">Revert reason</p>
                <p className="text-xs text-red-300/80 font-mono break-all leading-relaxed">{revertReason}</p>
              </motion.div>
            )}

            <div className="glass-card  divide-white/[0.06] overflow-hidden">
              <DetailRow label="Date" value={tx.timestamp ? fmtDate(tx.timestamp) : '—'} />
              <DetailRow label="Status" value={statusLabel} valueClass={statusColor} />
              <DetailRow label={counterpartyLabel} value={counterparty} mono />
              <DetailRow label="Network" value="Paxeer" />
              <DetailRow label="Network Fee" value={fmtFee()} />
              <button onClick={() => onPaxscan?.(`/tx/${tx.hash}`)} className="w-full py-3.5 text-center text-sm font-medium text-pax-accent hover:bg-white/[0.02] transition-colors">
                View on PaxScan
              </button>
            </div>

            {/* Gas breakdown */}
            {(gasInfo || gasPriceLabel) && (
              <div className="glass-card mt-4 overflow-hidden">
                <div className="flex items-center gap-2 px-4 py-3  ">
                  <Fuel className="w-3.5 h-3.5 text-pax-muted" />
                  <p className="text-xs font-semibold text-pax-muted">Gas Breakdown</p>
                </div>
                {gasInfo && (
                  <div className="px-4 py-3 space-y-2">
                    <div className="flex justify-between">
                      <span className="text-xs text-pax-muted">Gas used</span>
                      <span className="text-xs font-medium">
                        {gasInfo.used}{gasInfo.limit ? ` / ${gasInfo.limit}` : ''}
                        {gasInfo.utilPct !== null && (
                          <span className="ml-1 text-pax-muted">({gasInfo.utilPct}%)</span>
                        )}
                      </span>
                    </div>
                    {gasInfo.utilPct !== null && (
                      <div className="h-1.5 rounded-full bg-white/[0.06] overflow-hidden">
                        <div
                          className={`h-full rounded-full transition-all ${
                            gasInfo.utilPct > 90 ? 'bg-red-400' : gasInfo.utilPct > 70 ? 'bg-amber-400' : 'bg-pax-accent'
                          }`}
                          style={{ width: `${Math.min(gasInfo.utilPct, 100)}%` }}
                        />
                      </div>
                    )}
                  </div>
                )}
                {gasPriceLabel && (
                  <div className="flex justify-between px-4 py-3  ">
                    <span className="text-xs text-pax-muted">Gas price</span>
                    <span className="text-xs font-medium">{gasPriceLabel}</span>
                  </div>
                )}
              </div>
            )}

            {tokenTransfers.length > 1 && (
              <div className="glass-card mt-4  divide-white/[0.06] overflow-hidden">
                <div className="px-4 py-3">
                  <p className="text-xs font-semibold text-pax-muted">Token Transfers ({tokenTransfers.length})</p>
                </div>
                {tokenTransfers.map((tt: any, i: number) => {
                  const ttSent = tt.from?.hash?.toLowerCase() === addr;
                  const ttSym = tt.token?.symbol || '???';
                  const ttDec = Number(tt.token?.decimals || 18);
                  const ttAmt = formatBalance(tt.total?.value || '0', ttDec, 4);
                  return (
                    <div key={i} className="flex items-center gap-3 px-4 py-3">
                      <div className={`w-8 h-8 rounded-full flex items-center justify-center shrink-0 ${ttSent ? 'bg-red-500/10' : 'bg-green-500/10'}`}>
                        <SvgIcon name={ttSent ? 'arrow-up-right' : 'arrow-down-left'} className="w-3.5 h-3.5" style={{ filter: ttSent ? 'invert(48%) sepia(79%) saturate(2476%) hue-rotate(338deg) brightness(118%) contrast(119%)' : 'invert(69%) sepia(61%) saturate(588%) hue-rotate(88deg) brightness(93%) contrast(93%)' }} />
                      </div>
                      <div className="flex-1 min-w-0">
                        <p className="text-sm font-medium">{ttSent ? 'Sent' : 'Received'} {ttSym}</p>
                        <p className="text-xs text-pax-muted truncate">
                          {ttSent ? `To ${shortenAddress(tt.to?.hash || '')}` : `From ${shortenAddress(tt.from?.hash || '')}`}
                        </p>
                      </div>
                      <p className={`text-sm font-medium shrink-0 ${ttSent ? 'text-red-400' : 'text-green-400'}`}>
                        {ttSent ? '-' : '+'}{ttAmt} {ttSym}
                      </p>
                    </div>
                  );
                })}
              </div>
            )}
          </>
        ) : null}
      </div>

      <div className="px-4 pb-6 pt-2">
        <button onClick={onBack} className="w-full py-3.5 rounded-2xl bg-white/[0.08] text-white font-semibold text-sm press-scale hover:bg-white/[0.12] transition-colors">
          Close
        </button>
      </div>
    </div>
  );
}

function DetailRow({ label, value, valueClass = '', mono = false }: { label: string; value: string; valueClass?: string; mono?: boolean }) {
  return (
    <div className="flex items-center justify-between px-4 py-3.5">
      <span className="text-sm text-pax-muted">{label}</span>
      <span className={`text-sm font-medium text-right ${mono ? 'font-mono' : ''} ${valueClass}`}>{value}</span>
    </div>
  );
}
