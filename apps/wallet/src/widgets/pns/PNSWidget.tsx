'use client';

import { useState, useEffect, useRef, useCallback } from 'react';
import { useWalletState, useWalletActions } from '@/providers/WalletProvider';
import {
  ArrowLeft, Search, Check, Loader2, Send,
  AlertTriangle, Tag, ChevronRight, Star,
  Copy, CheckCircle, RefreshCw,
} from 'lucide-react';
import {
  checkAvailability,
  getRentPrice,
  getMinCommitmentAge,
  commitName,
  registerName,
  renewName,
  setPrimaryName,
  transferName,
  fetchOwnedDomains,
  fetchDomainDetail,
  fetchAddressName,
  formatPaxPrice,
  formatExpiry,
  shortenAddr,
  isValidLabel,
  PNS_TLD,
  REGISTRATION_DURATION_1Y,
  type PriceResult,
  type PNSDomain,
  type PNSDetailedDomain,
} from '@/lib/pns';

type PNSView = 'main' | 'register' | 'success' | 'detail';
type MainTab = 'search' | 'mynames';
type DurationOption = { label: string; seconds: number };
type RegStep = 'idle' | 'committing' | 'waiting' | 'registering' | 'done' | 'error';

const DURATIONS: DurationOption[] = [
  { label: '1 yr', seconds: REGISTRATION_DURATION_1Y },
  { label: '2 yr', seconds: REGISTRATION_DURATION_1Y * 2 },
  { label: '3 yr', seconds: REGISTRATION_DURATION_1Y * 3 },
  { label: '5 yr', seconds: REGISTRATION_DURATION_1Y * 5 },
];

interface PNSWidgetProps {
  onBack: () => void;
  onPaxscan?: (path?: string) => void;
}

export function PNSWidget({ onBack, onPaxscan }: PNSWidgetProps) {
  const { activeAccount } = useWalletState();
  const { getSigner } = useWalletActions();

  const [view, setView] = useState<PNSView>('main');
  const [tab, setTab] = useState<MainTab>('search');
  const [query, setQuery] = useState('');
  const [searching, setSearching] = useState(false);
  const [available, setAvailable] = useState<boolean | null>(null);
  const [price, setPrice] = useState<PriceResult | null>(null);
  const [searchedLabel, setSearchedLabel] = useState('');
  const searchTimer = useRef<ReturnType<typeof setTimeout>>();

  const [duration, setDuration] = useState<DurationOption>(DURATIONS[0]);
  const [regStep, setRegStep] = useState<RegStep>('idle');
  const [regError, setRegError] = useState('');
  const [countdown, setCountdown] = useState(0);
  const [successTxHash, setSuccessTxHash] = useState('');

  const [ownedDomains, setOwnedDomains] = useState<PNSDomain[]>([]);
  const [loadingOwned, setLoadingOwned] = useState(false);
  const [primaryDomain, setPrimaryDomainState] = useState<PNSDetailedDomain | null>(null);

  const [detailDomain, setDetailDomain] = useState<PNSDetailedDomain | null>(null);
  const [loadingDetail, setLoadingDetail] = useState(false);
  const [actionLoading, setActionLoading] = useState('');
  const [actionError, setActionError] = useState('');
  const [transferTo, setTransferTo] = useState('');
  const [showTransfer, setShowTransfer] = useState(false);
  const [copied, setCopied] = useState('');

  const loadMyNames = useCallback(async () => {
    if (!activeAccount?.address) return;
    setLoadingOwned(true);
    try {
      const [domains, primary] = await Promise.all([
        fetchOwnedDomains(activeAccount.address),
        fetchAddressName(activeAccount.address),
      ]);
      setOwnedDomains(domains);
      setPrimaryDomainState(primary);
    } catch { /* best-effort */ }
    finally { setLoadingOwned(false); }
  }, [activeAccount?.address]);

  useEffect(() => { loadMyNames(); }, [loadMyNames]);

  const handleSearch = useCallback(async (label: string) => {
    const clean = label.toLowerCase().replace(/\.pax$/, '').trim();
    if (!isValidLabel(clean)) { setAvailable(null); setPrice(null); setSearchedLabel(''); return; }
    setSearching(true); setSearchedLabel(clean); setAvailable(null); setPrice(null);
    try {
      const [isAvail, priceResult] = await Promise.all([checkAvailability(clean), getRentPrice(clean, duration.seconds)]);
      setAvailable(isAvail); setPrice(priceResult);
    } catch (err: unknown) {
      console.error('PNS search error:', err); setAvailable(null);
    } finally { setSearching(false); }
  }, [duration.seconds]);

  useEffect(() => {
    if (searchTimer.current) clearTimeout(searchTimer.current);
    if (!query.trim()) { setAvailable(null); setPrice(null); setSearchedLabel(''); return; }
    searchTimer.current = setTimeout(() => handleSearch(query), 500);
    return () => { if (searchTimer.current) clearTimeout(searchTimer.current); };
  }, [query, handleSearch]);

  useEffect(() => {
    if (searchedLabel && available) { getRentPrice(searchedLabel, duration.seconds).then(setPrice).catch(() => {}); }
  }, [duration.seconds, searchedLabel, available]);

  const startRegistration = async () => {
    if (!activeAccount?.address || !searchedLabel) return;
    setRegStep('committing'); setRegError(''); setView('register');
    try {
      const signer = await getSigner();
      const result = await commitName(signer, { label: searchedLabel, owner: activeAccount.address, durationSeconds: duration.seconds });
      setRegStep('waiting');
      const minAge = await getMinCommitmentAge();
      const waitSec = minAge + 5;
      setCountdown(waitSec);
      await new Promise<void>((resolve) => {
        let remaining = waitSec;
        const interval = setInterval(() => {
          remaining -= 1; setCountdown(remaining);
          if (remaining <= 0) { clearInterval(interval); resolve(); }
        }, 1000);
      });
      setRegStep('registering');
      const currentPrice = await getRentPrice(searchedLabel, duration.seconds);
      const txHash = await registerName(signer, result.registration, currentPrice);
      setSuccessTxHash(txHash); setRegStep('done'); setView('success');
      loadMyNames();
    } catch (err: unknown) {
      console.error('PNS registration error:', err);
      setRegError((err as Error)?.message || 'Registration failed'); setRegStep('error');
    }
  };

  const openDetail = async (domainName: string) => {
    setView('detail'); setLoadingDetail(true); setShowTransfer(false); setTransferTo(''); setActionError('');
    try { const detail = await fetchDomainDetail(domainName); setDetailDomain(detail); }
    catch { setDetailDomain(null); }
    finally { setLoadingDetail(false); }
  };

  const handleSetPrimary = async (fullName: string) => {
    setActionLoading('primary'); setActionError('');
    try { const signer = await getSigner(); await setPrimaryName(signer, fullName); await loadMyNames(); }
    catch (err: unknown) { setActionError((err as Error)?.message || 'Failed'); }
    finally { setActionLoading(''); }
  };

  const handleRenew = async (label: string) => {
    setActionLoading('renew'); setActionError('');
    try { const signer = await getSigner(); await renewName(signer, label, REGISTRATION_DURATION_1Y); await openDetail(`${label}.${PNS_TLD}`); await loadMyNames(); }
    catch (err: unknown) { setActionError((err as Error)?.message || 'Renewal failed'); }
    finally { setActionLoading(''); }
  };

  const handleTransfer = async () => {
    if (!detailDomain || !transferTo) return;
    setActionLoading('transfer'); setActionError('');
    try { const signer = await getSigner(); await transferName(signer, detailDomain, transferTo); setShowTransfer(false); setTransferTo(''); await loadMyNames(); setView('main'); setTab('mynames'); }
    catch (err: unknown) { setActionError((err as Error)?.message || 'Transfer failed'); }
    finally { setActionLoading(''); }
  };

  const copyText = (text: string, key: string) => { navigator.clipboard.writeText(text); setCopied(key); setTimeout(() => setCopied(''), 2000); };

  if (view === 'success') {
    return (
      <div className="min-h-screen flex flex-col px-4 safe-area-pt">
        <div className="flex-1 flex flex-col items-center justify-center gap-4 -mt-16">
          <div className="w-20 h-20 rounded-full bg-green-500/10 flex items-center justify-center">
            <CheckCircle className="w-10 h-10 text-green-400" />
          </div>
          <h2 className="text-xl font-bold">Name Registered</h2>
          <p className="text-sm text-pax-muted text-center">
            <span className="text-white font-semibold">{searchedLabel}.{PNS_TLD}</span> is now yours
          </p>
          {successTxHash && (
            <button onClick={() => onPaxscan?.(`/tx/${successTxHash}`)} className="text-xs text-pax-accent underline">View transaction</button>
          )}
          <div className="flex gap-3 mt-4">
            <button onClick={() => handleSetPrimary(`${searchedLabel}.${PNS_TLD}`)} disabled={actionLoading === 'primary'}
              className="px-4 py-2.5 rounded-xl bg-pax-accent/10 text-pax-accent text-xs font-medium press-scale disabled:opacity-40">
              {actionLoading === 'primary' ? <Loader2 className="w-3.5 h-3.5 animate-spin inline mr-1" /> : <Star className="w-3.5 h-3.5 inline mr-1" />}
              Set as primary
            </button>
            <button onClick={() => { setView('main'); setTab('mynames'); setQuery(''); setAvailable(null); setPrice(null); setSearchedLabel(''); setRegStep('idle'); }}
              className="px-4 py-2.5 rounded-xl bg-white/5 text-xs font-medium press-scale">
              My names
            </button>
          </div>
          <button onClick={onBack} className="mt-6 px-6 py-3 rounded-2xl bg-pax-accent text-black text-sm font-semibold press-scale">Done</button>
        </div>
      </div>
    );
  }

  if (view === 'register') {
    return (
      <div className="min-h-screen flex flex-col px-4 safe-area-pt">
        <div className="flex items-center gap-3 py-4">
          <button onClick={() => { if (regStep === 'error') { setView('main'); setRegStep('idle'); } }} disabled={regStep !== 'error' && regStep !== 'idle'} className="p-2 -ml-2 press-scale disabled:opacity-30">
            <ArrowLeft className="w-5 h-5 text-pax-muted" />
          </button>
          <h1 className="text-base font-bold">Registering {searchedLabel}.{PNS_TLD}</h1>
        </div>
        <div className="flex-1 flex flex-col items-center justify-center -mt-16 gap-8">
          <div className="w-full max-w-xs space-y-4">
            <StepRow step={1} label="Requesting name" sublabel="Sending commitment to the network" status={regStep === 'committing' ? 'active' : ['waiting','registering','done'].includes(regStep) ? 'done' : regStep === 'error' ? 'error' : 'pending'} />
            <StepRow step={2} label={countdown > 0 ? `Waiting ${countdown}s` : 'Commitment matured'} sublabel="Prevents front-running" status={regStep === 'waiting' ? 'active' : ['registering','done'].includes(regStep) ? 'done' : 'pending'} />
            <StepRow step={3} label="Completing registration" sublabel="Securing your name on-chain" status={regStep === 'registering' ? 'active' : regStep === 'done' ? 'done' : 'pending'} />
          </div>
          {regStep === 'error' && (
            <div className="glass-card p-4 w-full max-w-xs">
              <div className="flex items-start gap-2">
                <AlertTriangle className="w-4 h-4 text-red-400 shrink-0 mt-0.5" />
                <div>
                  <p className="text-xs font-semibold text-red-400">Registration failed</p>
                  <p className="text-[11px] text-red-300/70 mt-1 break-all">{regError}</p>
                </div>
              </div>
              <button onClick={() => { setView('main'); setRegStep('idle'); }} className="w-full mt-3 py-2.5 rounded-xl bg-white/5 text-xs font-medium press-scale">Back to search</button>
            </div>
          )}
        </div>
      </div>
    );
  }

  if (view === 'detail') {
    const d = detailDomain;
    const label = d?.name?.replace(`.${PNS_TLD}`, '') || '';
    const isPrimary = primaryDomain?.name === d?.name;
    const isOwner = d?.owner?.hash?.toLowerCase() === activeAccount?.address?.toLowerCase() || d?.registrant?.hash?.toLowerCase() === activeAccount?.address?.toLowerCase();
    return (
      <div className="min-h-screen flex flex-col safe-area-pt">
        <header className="shrink-0 px-4 py-4">
          <div className="flex items-center gap-3">
            <button onClick={() => { setView('main'); setTab('mynames'); }} className="p-2 -ml-2 press-scale"><ArrowLeft className="w-5 h-5 text-pax-muted" /></button>
            <h1 className="text-base font-bold truncate">{d?.name || 'Loading...'}</h1>
            {isPrimary && <span className="text-[9px] bg-pax-accent/20 text-pax-accent px-1.5 py-0.5 rounded font-bold">PRIMARY</span>}
          </div>
        </header>
        <div className="flex-1 px-4 pb-24">
          {loadingDetail ? (
            <div className="flex items-center justify-center py-16"><Loader2 className="w-6 h-6 text-pax-muted animate-spin" /></div>
          ) : !d ? (
            <div className="glass-card p-6 text-center"><p className="text-xs text-pax-muted">Domain not found</p></div>
          ) : (
            <div className="space-y-3">
              <div className="glass-card  divide-white/5">
                <PNSDetailRow label="Owner" value={d.owner?.hash ? shortenAddr(d.owner.hash) : '—'} onCopy={d.owner?.hash ? () => copyText(d.owner!.hash, 'owner') : undefined} copied={copied === 'owner'} />
                <PNSDetailRow label="Resolved to" value={d.resolved_address?.hash ? shortenAddr(d.resolved_address.hash) : '—'} onCopy={d.resolved_address?.hash ? () => copyText(d.resolved_address!.hash, 'resolved') : undefined} copied={copied === 'resolved'} />
                <PNSDetailRow label="Registered" value={d.registration_date ? new Date(d.registration_date).toLocaleDateString() : '—'} />
                <PNSDetailRow label="Expires" value={formatExpiry(d.expiry_date)} valueClass={d.expiry_date && new Date(d.expiry_date).getTime() - Date.now() < 30 * 86_400_000 ? 'text-yellow-400' : undefined} />
                {d.resolver_address?.hash && <PNSDetailRow label="Resolver" value={shortenAddr(d.resolver_address.hash)} />}
              </div>
              {isOwner && (
                <div className="space-y-2 mt-4">
                  {!isPrimary && (
                    <button onClick={() => handleSetPrimary(d.name)} disabled={!!actionLoading} className="w-full flex items-center justify-center gap-2 py-3 rounded-xl bg-pax-accent/10 text-pax-accent text-sm font-medium press-scale disabled:opacity-40">
                      {actionLoading === 'primary' ? <Loader2 className="w-4 h-4 animate-spin" /> : <Star className="w-4 h-4" />} Set as primary name
                    </button>
                  )}
                  <button onClick={() => handleRenew(label)} disabled={!!actionLoading} className="w-full flex items-center justify-center gap-2 py-3 rounded-xl bg-white/5 text-sm font-medium press-scale disabled:opacity-40">
                    {actionLoading === 'renew' ? <Loader2 className="w-4 h-4 animate-spin" /> : <RefreshCw className="w-4 h-4" />} Renew (+1 year)
                  </button>
                  {!showTransfer ? (
                    <button onClick={() => setShowTransfer(true)} disabled={!!actionLoading} className="w-full flex items-center justify-center gap-2 py-3 rounded-xl bg-white/5 text-sm font-medium press-scale disabled:opacity-40">
                      <Send className="w-4 h-4" /> Transfer
                    </button>
                  ) : (
                    <div className="glass-card p-3 space-y-2">
                      <p className="text-xs font-medium">Transfer {d.name}</p>
                      <input type="text" value={transferTo} onChange={(e) => setTransferTo(e.target.value)} placeholder="Recipient address (0x...)" className="w-full px-3 py-2.5 rounded-xl bg-white/5 text-sm outline-none placeholder:text-white/20" />
                      <div className="flex gap-2">
                        <button onClick={() => { setShowTransfer(false); setTransferTo(''); setActionError(''); }} className="flex-1 py-2.5 rounded-xl bg-white/5 text-xs font-medium press-scale">Cancel</button>
                        <button onClick={handleTransfer} disabled={!transferTo || !!actionLoading} className="flex-1 py-2.5 rounded-xl bg-red-500/20 text-red-400 text-xs font-semibold press-scale disabled:opacity-40">
                          {actionLoading === 'transfer' ? <Loader2 className="w-3.5 h-3.5 animate-spin inline" /> : 'Confirm transfer'}
                        </button>
                      </div>
                    </div>
                  )}
                  {actionError && (
                    <div className="flex items-start gap-2 p-3 rounded-xl bg-red-500/5  ">
                      <AlertTriangle className="w-3.5 h-3.5 text-red-400 shrink-0 mt-0.5" />
                      <p className="text-[11px] text-red-300/80 break-all">{actionError}</p>
                    </div>
                  )}
                </div>
              )}
            </div>
          )}
        </div>
      </div>
    );
  }

  return (
    <div className="min-h-screen flex flex-col safe-area-pt">
      <header className="shrink-0 px-4 pt-4 pb-2">
        <div className="flex items-center gap-3 mb-3">
          <button onClick={onBack} className="p-2 -ml-2 press-scale"><ArrowLeft className="w-5 h-5 text-pax-muted" /></button>
          <div>
            <h1 className="text-base font-bold">Paxeer Name Service</h1>
            <p className="text-[11px] text-pax-muted">.{PNS_TLD} names on Paxeer Network</p>
          </div>
        </div>
        <div className="flex gap-1 bg-white/[0.03] p-1 rounded-xl">
          {(['search', 'mynames'] as MainTab[]).map((t) => (
            <button key={t} onClick={() => setTab(t)} className={`flex-1 py-2 rounded-lg text-xs font-medium transition-colors ${tab === t ? 'bg-white/10 text-white' : 'text-pax-muted'}`}>
              {t === 'search' ? 'Register' : `My Names${ownedDomains.length ? ` (${ownedDomains.length})` : ''}`}
            </button>
          ))}
        </div>
      </header>

      <div className="flex-1 px-4 pb-24 pt-3">
        {tab === 'search' && (
          <>
            <div className="flex items-center gap-2 px-3.5 py-3 rounded-2xl bg-white/5    transition-colors mb-1">
              <Search className="w-4 h-4 text-pax-muted shrink-0" />
              <input type="text" value={query} onChange={(e) => setQuery(e.target.value.toLowerCase().replace(/[^a-z0-9-]/g, ''))} placeholder="Search for a name..." className="flex-1 bg-transparent text-sm outline-none placeholder:text-white/20 min-w-0" autoFocus />
              <span className="text-sm text-pax-muted">.{PNS_TLD}</span>
            </div>
            {query && !isValidLabel(query) && <p className="text-[10px] text-red-400/70 mt-1 ml-1">3-64 chars, lowercase alphanumeric and hyphens only</p>}
            {searching && <div className="flex items-center justify-center py-12"><Loader2 className="w-5 h-5 text-pax-muted animate-spin" /></div>}
            {!searching && searchedLabel && available !== null && (
              <div className="mt-4">
                {available ? (
                  <div className="glass-card overflow-hidden">
                    <div className="px-4 py-3 flex items-center gap-2  ">
                      <div className="w-5 h-5 rounded-full bg-green-500/20 flex items-center justify-center"><Check className="w-3 h-3 text-green-400" /></div>
                      <p className="text-sm font-semibold">{searchedLabel}.{PNS_TLD} <span className="text-green-400 font-normal text-xs ml-1">Available</span></p>
                    </div>
                    <div className="px-4 py-3  ">
                      <p className="text-[10px] text-pax-muted uppercase tracking-wider mb-2">Duration</p>
                      <div className="flex gap-2 flex-wrap">
                        {DURATIONS.map((d) => (
                          <button key={d.seconds} onClick={() => setDuration(d)} className={`px-3 py-1.5 rounded-lg text-xs font-medium press-scale transition-colors ${duration.seconds === d.seconds ? 'bg-pax-accent text-black' : 'bg-white/5 text-pax-muted'}`}>{d.label}</button>
                        ))}
                      </div>
                    </div>
                    <div className="px-4 py-3  ">
                      <div className="flex items-center justify-between">
                        <p className="text-xs text-pax-muted">Cost</p>
                        {price ? <p className="text-sm font-bold">{formatPaxPrice(price.total)} PAX</p> : <Loader2 className="w-3.5 h-3.5 text-pax-muted animate-spin" />}
                      </div>
                      {price && price.premium > BigInt(0) && <p className="text-[10px] text-yellow-400/70 mt-1">Includes {formatPaxPrice(price.premium)} PAX premium</p>}
                    </div>
                    <div className="px-4 py-3">
                      <button onClick={startRegistration} disabled={!price || !activeAccount?.address} className="w-full py-3 rounded-xl bg-pax-accent text-black font-semibold text-sm press-scale disabled:opacity-40">Register {searchedLabel}.{PNS_TLD}</button>
                      <p className="text-[10px] text-pax-muted text-center mt-2">2-step commit-reveal (~60s)</p>
                    </div>
                  </div>
                ) : (
                  <div className="glass-card p-4">
                    <div className="flex items-center gap-2">
                      <div className="w-5 h-5 rounded-full bg-red-500/20 flex items-center justify-center"><AlertTriangle className="w-3 h-3 text-red-400" /></div>
                      <p className="text-sm font-semibold">{searchedLabel}.{PNS_TLD} <span className="text-red-400 font-normal text-xs ml-1">Taken</span></p>
                    </div>
                    <button onClick={() => openDetail(`${searchedLabel}.${PNS_TLD}`)} className="mt-3 text-xs text-pax-accent press-scale">View domain details</button>
                  </div>
                )}
              </div>
            )}
          </>
        )}

        {tab === 'mynames' && (
          <>
            {primaryDomain && (
              <div className="glass-card p-3.5 mb-4 flex items-center gap-3">
                <div className="w-10 h-10 rounded-full bg-pax-accent/10 flex items-center justify-center shrink-0"><Tag className="w-5 h-5 text-pax-accent" /></div>
                <div className="flex-1 min-w-0">
                  <p className="text-[10px] text-pax-muted uppercase tracking-wider">Primary name</p>
                  <p className="text-sm font-semibold truncate">{primaryDomain.name}</p>
                </div>
                <button onClick={() => openDetail(primaryDomain.name)} className="p-2 press-scale"><ChevronRight className="w-4 h-4 text-pax-muted" /></button>
              </div>
            )}
            {loadingOwned ? (
              <div className="flex items-center justify-center py-12"><Loader2 className="w-5 h-5 text-pax-muted animate-spin" /></div>
            ) : ownedDomains.length === 0 ? (
              <div className="text-center py-12">
                <Tag className="w-8 h-8 text-pax-muted/30 mx-auto mb-3" />
                <p className="text-sm text-pax-muted">No names yet</p>
                <button onClick={() => setTab('search')} className="mt-3 px-4 py-2 rounded-xl bg-pax-accent/10 text-pax-accent text-xs font-medium press-scale">Register a .{PNS_TLD} name</button>
              </div>
            ) : (
              <div className="glass-card  divide-white/5">
                {ownedDomains.map((d) => {
                  const isPrimary = primaryDomain?.name === d.name;
                  return (
                    <button key={d.id} onClick={() => openDetail(d.name)} className="w-full flex items-center gap-3 px-4 py-3.5 press-scale text-left transition-colors hover:bg-white/[0.02]">
                      <div className="w-9 h-9 rounded-full bg-pax-accent/10 flex items-center justify-center shrink-0">
                        <span className="text-xs font-bold text-pax-accent">{(d.name || '?')[0].toUpperCase()}</span>
                      </div>
                      <div className="flex-1 min-w-0">
                        <div className="flex items-center gap-1.5">
                          <p className="text-sm font-medium truncate">{d.name}</p>
                          {isPrimary && <span className="text-[8px] bg-pax-accent/20 text-pax-accent px-1 py-0.5 rounded font-bold shrink-0">PRIMARY</span>}
                        </div>
                        <p className="text-[10px] text-pax-muted">{d.expiry_date ? `Expires: ${formatExpiry(d.expiry_date)}` : 'No expiry'}</p>
                      </div>
                      <ChevronRight className="w-4 h-4 text-pax-muted/50 shrink-0" />
                    </button>
                  );
                })}
              </div>
            )}
            {ownedDomains.length > 0 && (
              <button onClick={loadMyNames} disabled={loadingOwned} className="mt-3 mx-auto flex items-center gap-1.5 text-xs text-pax-muted press-scale disabled:opacity-30">
                <RefreshCw className={`w-3 h-3 ${loadingOwned ? 'animate-spin' : ''}`} /> Refresh
              </button>
            )}
          </>
        )}
      </div>
    </div>
  );
}

function StepRow({ step, label, sublabel, status }: { step: number; label: string; sublabel: string; status: 'pending' | 'active' | 'done' | 'error' }) {
  return (
    <div className={`flex items-center gap-3 p-3 rounded-xl transition-colors ${status === 'active' ? 'bg-pax-accent/5  ' : status === 'done' ? 'bg-green-500/5  ' : status === 'error' ? 'bg-red-500/5  ' : 'bg-white/[0.02]  '}`}>
      <div className={`w-8 h-8 rounded-full flex items-center justify-center shrink-0 ${status === 'active' ? 'bg-pax-accent/20' : status === 'done' ? 'bg-green-500/20' : status === 'error' ? 'bg-red-500/20' : 'bg-white/5'}`}>
        {status === 'active' ? <Loader2 className="w-4 h-4 text-pax-accent animate-spin" /> : status === 'done' ? <Check className="w-4 h-4 text-green-400" /> : status === 'error' ? <AlertTriangle className="w-4 h-4 text-red-400" /> : <span className="text-xs font-bold text-pax-muted">{step}</span>}
      </div>
      <div className="min-w-0">
        <p className={`text-sm font-medium ${status === 'active' ? 'text-white' : status === 'done' ? 'text-green-400' : status === 'error' ? 'text-red-400' : 'text-pax-muted'}`}>{label}</p>
        <p className="text-[10px] text-pax-muted">{sublabel}</p>
      </div>
    </div>
  );
}

function PNSDetailRow({ label, value, valueClass, onCopy, copied }: { label: string; value: string; valueClass?: string; onCopy?: () => void; copied?: boolean }) {
  return (
    <div className="flex items-center justify-between px-4 py-3">
      <p className="text-xs text-pax-muted">{label}</p>
      <div className="flex items-center gap-1.5">
        <p className={`text-xs font-medium ${valueClass || ''}`}>{value}</p>
        {onCopy && (
          <button onClick={onCopy} className="p-1 press-scale">
            {copied ? <Check className="w-3 h-3 text-green-400" /> : <Copy className="w-3 h-3 text-pax-muted" />}
          </button>
        )}
      </div>
    </div>
  );
}
