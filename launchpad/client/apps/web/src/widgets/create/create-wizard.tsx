'use client';

import { useCallback, useEffect, useMemo, useRef, useState, type ReactNode } from 'react';
import { useRouter } from 'next/navigation';
import { useAccount, usePublicClient, useSignMessage, useSwitchChain, useWalletClient } from 'wagmi';
import { formatUnits, parseUnits, type PublicClient, type WalletClient } from 'viem';
import { ExternalLink, Rocket } from 'lucide-react';
import { CORE_DEPLOYMENT } from '@/core/network/deployment';
import { useReadOpticalRegistryGetAllOpticals } from '@/core/network/contracts';
import { useRuntimeAuthority } from '@/core/runtime-authority';
import { poolPath, safeExternalDestination } from '@/core/app-foundation/navigation';
import { useAuthModal } from '@/shell/auth-modal-store';
import { Button, DisclosureRow, FormField, NoticeBar } from '@/ui/primitives';
import { formatTokenAmount, formatUsdl, shortAddress } from '@/utils/format';
import { plainLaunchError, plainLaunchStatus } from './launch-copy';
import { runtimeCreationAdmission, KnownCapabilityAdmissionError } from './cap-admission';
import { ChainCreationAdapter, creationFunding, type AccountScope, type FundingRead } from './creation-adapters';
import { CreationController, type LaunchState } from './creation-controller';
import { CreationStorage, validateCreationDraft, type CreationDraftV2, type LaunchAttempt } from './creation-storage';
import { CreateFormFields } from './create-form-fields';
import { CoinPreviewCard } from './coin-preview-card';
import { FeeSummary, fundingCopy } from './fee-summary';
import { LaunchProgress, type LaunchStep } from './launch-progress';
import { MediaDropzone } from './media-dropzone';
import { MediaRuleLegend } from './media-rule-legend';
import type { StagedMedia } from './media-validation';
import { STANDARD_OPTICAL, type OpticalRegistryState } from './optical-preset-picker';
import { feeStrategyLabel } from './strategy-picker';
import { useCreatePrefill } from './use-create-prefill';

const idle: LaunchState = { phase: 'draft', busy: false, message: '' };
const identityErrorKeys = new Set(['creator', 'chainId', 'draftId', 'updatedAt', 'draft']);
const launchStateLabel: Record<LaunchAttempt['state'], string> = { signing_locked: 'waiting for wallet', outcome_unknown: 'needs a check', submitted: 'confirming', resolved: 'live', reverted: 'failed' };
const rewardsLine: Record<number, string> = { 0: 'Creator rewards go to you', 1: 'Creator rewards are burned', 2: 'Creator rewards go to holders', 3: 'Creator rewards go to liquidity providers' };
const purchaseSettled = new Set<LaunchAttempt['purchaseState']>(['not_requested', 'skipped', 'confirmed']);

function blankDraft(creator: string): CreationDraftV2 {
  return { draftId: crypto.randomUUID(), version: 2, creator, chainId: 125, name: '', symbol: '', description: '', tags: [], socialLinks: [], uploadRefs: [], firstBuyRaw: '0', opticalSelection: STANDARD_OPTICAL, updatedAt: Date.now(), feeStrategy: 0 };
}

function hasContent(draft: CreationDraftV2): boolean {
  return Boolean(draft.name || draft.symbol || draft.description || draft.socialLinks.length || draft.stagedMedia || draft.firstBuyRaw !== '0' || draft.feeStrategy !== 0);
}

/**
 * A field that already holds a value (restored draft, carried-over typing, query
 * prefill) has its validation shown even before this session edits it; only the
 * empty fields of a pristine form stay quiet until the user touches them.
 */
function holdsValue(draft: CreationDraftV2 | null, key: string): boolean {
  if (!draft) return false;
  const value = (draft as unknown as Record<string, unknown>)[key];
  if (value === undefined || value === null) return false;
  if (key === 'opticalSelection') return value !== STANDARD_OPTICAL;
  if (key === 'firstBuyRaw') return value !== '0';
  if (typeof value === 'string') return value.length > 0;
  if (Array.isArray(value)) return value.length > 0;
  return true;
}

function carryOver(target: CreationDraftV2, source: CreationDraftV2): CreationDraftV2 {
  return { ...target, name: source.name, symbol: source.symbol, description: source.description, socialLinks: source.socialLinks, feeStrategy: source.feeStrategy, firstBuyRaw: source.firstBuyRaw, ...(source.stagedMedia ? { stagedMedia: source.stagedMedia } : {}), updatedAt: Date.now() };
}

function TxLink({ hash, label = 'View transaction' }: { hash: string; label?: string }) {
  const href = safeExternalDestination(`https://paxscan.paxeer.app/tx/${hash}`);
  if (!href) return null;
  return (
    <a href={href} target="_blank" rel="noreferrer noopener" className="inline-flex items-center gap-1 font-mono text-xs text-accent hover:underline">
      {label}
      <ExternalLink aria-hidden="true" className="size-icon-sm" />
      <span className="sr-only">, opens in a new tab</span>
    </a>
  );
}

function Consent({ id, checked, onChange, children, disabled }: { id: string; checked: boolean; onChange: (next: boolean) => void; children: ReactNode; disabled?: boolean }) {
  return (
    <label htmlFor={id} className="flex items-start gap-2 text-xs text-text-muted">
      <input id={id} type="checkbox" checked={checked} disabled={disabled} onChange={event => onChange(event.target.checked)} className="mt-0.5 size-4 shrink-0 accent-[var(--sl-accent)]" />
      <span>{children}</span>
    </label>
  );
}

export default function CreateWizard() {
  const router = useRouter();
  const { openModal } = useAuthModal();
  const prefill = useCreatePrefill();
  const { authority } = useRuntimeAuthority();
  const authorityRef = useRef(authority);
  authorityRef.current = authority;
  const { address, chainId } = useAccount();
  const publicClient = usePublicClient();
  const { data: wallet } = useWalletClient();
  const { signMessageAsync } = useSignMessage();
  const { switchChainAsync } = useSwitchChain();
  const scope = useRef<AccountScope>({ creator: address ?? '', chainId: chainId ?? 0, epoch: 0 });
  if (scope.current.creator !== (address?.toLowerCase() ?? '') || scope.current.chainId !== (chainId ?? 0)) {
    scope.current = { creator: address?.toLowerCase() ?? '', chainId: chainId ?? 0, epoch: scope.current.epoch + 1 };
  }

  const [draft, setDraft] = useState<CreationDraftV2 | null>(() => blankDraft(''));
  const draftRef = useRef<CreationDraftV2 | null>(null);
  draftRef.current = draft;
  const [state, setState] = useState<LaunchState>(idle);
  const [history, setHistory] = useState<LaunchAttempt[]>([]);
  const [historyLoaded, setHistoryLoaded] = useState(false);
  const [error, setError] = useState('');
  const [fundingError, setFundingError] = useState<string | null>(null);
  const [lastRead, setLastRead] = useState<FundingRead | null>(null);
  const [firstBuy, setFirstBuy] = useState('');
  const [amountError, setAmountError] = useState('');
  const [staged, setStaged] = useState<StagedMedia | null>(null);
  const stagedRef = useRef<StagedMedia | null>(null);
  stagedRef.current = staged;
  const [imageSkipped, setImageSkipped] = useState(false);
  const [metadataSkipped, setMetadataSkipped] = useState(false);
  const [recoveryHash, setRecoveryHash] = useState('');
  const [slippage, setSlippage] = useState(100);
  const [impactConsent, setImpactConsent] = useState(false);
  const [slippageConsent, setSlippageConsent] = useState(false);
  const [recoveryChecked, setRecoveryChecked] = useState(false);
  const [newConsent, setNewConsent] = useState(false);
  const [verifiedPools, setVerifiedPools] = useState<string[]>([]);
  const [historyOffset, setHistoryOffset] = useState(0n);
  const launchedHere = useRef(false);
  const navigated = useRef(false);
  const autoUploaded = useRef<string | null>(null);
  const autoReviewed = useRef(false);
  const prefillApplied = useRef(false);

  const { data: opticalsData, isPending: registryPending, error: registryError } = useReadOpticalRegistryGetAllOpticals({ offset: 0n, limit: 50n });
  const registeredOpticals = useMemo(() => (Array.isArray(opticalsData) ? opticalsData.filter((value): value is string => typeof value === 'string') : []), [opticalsData]);
  const registry: OpticalRegistryState = registryError ? { state: 'error', message: registryError.message } : registryPending ? { state: 'loading' } : { state: 'ready' };

  const signRef = useRef(signMessageAsync);
  signRef.current = signMessageAsync;
  const controller = useMemo(() => {
    const currentAuthority = authorityRef.current;
    if (typeof window === 'undefined' || !address || !publicClient || !wallet || !currentAuthority) return null;
    const deployment = { ...CORE_DEPLOYMENT, revision: currentAuthority.revision.revisionSha256, tradeDeployment: currentAuthority.tradeDeployment };
    const chain = new ChainCreationAdapter(publicClient as PublicClient, wallet as WalletClient, deployment, () => scope.current, async (candidate, account) => {
      const current = authorityRef.current;
      if (!current) throw new KnownCapabilityAdmissionError('Committed creation authority is unavailable.');
      await runtimeCreationAdmission(current, deployment)(candidate, account);
    });
    return new CreationController(chain, new CreationStorage(address), message => signRef.current({ message, account: scope.current.creator as `0x${string}` }));
  }, [address, publicClient, wallet, authority?.revision.revisionSha256, authority?.tradeDeployment]);

  useEffect(() => {
    setState(idle); setError(''); setAmountError(''); setFundingError(null); setLastRead(null);
    setImpactConsent(false); setSlippageConsent(false); setRecoveryChecked(false); setNewConsent(false);
    setHistory([]); setHistoryLoaded(false); setVerifiedPools([]); setHistoryOffset(0n);
    setImageSkipped(false); setMetadataSkipped(false);
    autoReviewed.current = false;
    if (!controller || !address) {
      const current = draftRef.current;
      setDraft(current ? { ...current, creator: '' } : blankDraft(''));
      return;
    }
    const unsubscribe = controller.subscribe(() => setState({ ...controller.state }));
    let live = true;
    const creator = address.toLowerCase();
    const typed = draftRef.current;
    let value: CreationDraftV2;
    try {
      const restored = controller.storage.restoreDraft();
      value = typed && hasContent(typed) ? carryOver(restored ?? blankDraft(creator), typed) : restored ?? blankDraft(creator);
    } catch (cause) {
      setError(plainLaunchError(cause, "Your saved draft couldn't be loaded. Start fresh"));
      value = typed && hasContent(typed) ? carryOver(blankDraft(creator), typed) : blankDraft(creator);
    }
    setDraft(value);
    setFirstBuy(value.firstBuyRaw === '0' ? '' : formatUnits(BigInt(value.firstBuyRaw), 6));
    controller.storage.history().then(async rows => {
      if (!live) return;
      setHistory(rows);
      const pending = rows.find(row => !row.deliberateNew && row.state !== 'reverted');
      if (pending) {
        setDraft(pending.draft);
        setFirstBuy(pending.draft.firstBuyRaw === '0' ? '' : formatUnits(BigInt(pending.draft.firstBuyRaw), 6));
        try { await controller.resume(pending.intentId); } catch (cause) { if (live) setError(plainLaunchError(cause, 'Your last launch is still confirming. Check it below')); }
      }
      if (live) setHistoryLoaded(true);
    }).catch(cause => {
      if (!live) return;
      setError(plainLaunchError(cause, "Your past launches can't load right now"));
      setHistoryLoaded(true);
    });
    return () => { live = false; unsubscribe(); controller.invalidate('Wallet connection changed.'); controller.dispose(); };
  }, [controller, address]);

  useEffect(() => {
    if (!controller || !draft || !address || draft.creator !== address.toLowerCase()) return;
    const timer = setTimeout(() => {
      try { controller.storage.saveDraft(draft); } catch (cause) { setError(plainLaunchError(cause, "Your draft couldn't be saved. Try again")); }
    }, 1000);
    return () => clearTimeout(timer);
  }, [controller, draft, address]);

  useEffect(() => { controller?.invalidate('Wallet account or chain changed. The launch is reviewed again with the creator.'); }, [controller, chainId]);

  useEffect(() => {
    if (!controller || !state.reviewed) return;
    const timer = setTimeout(() => controller.invalidate('Creation review expired. Fees, balances and gas are refreshed before signing.'), Math.max(0, state.reviewed.reviewedAt + 30000 - Date.now()));
    return () => clearTimeout(timer);
  }, [controller, state.reviewed]);

  useEffect(() => { if (state.reviewed) setLastRead(state.reviewed.read); }, [state.reviewed]);

  useEffect(() => () => { if (stagedRef.current) URL.revokeObjectURL(stagedRef.current.previewUrl); }, []);

  const attempt = state.attempt;
  const locked = state.busy || (!!attempt && !attempt.deliberateNew);

  // Validation copy is shown only for fields the user has edited (or the service
  // rejected); a pristine form must not open covered in error states.
  const [touched, setTouched] = useState<ReadonlySet<string>>(() => new Set<string>());
  const edit = useCallback(<K extends keyof CreationDraftV2>(key: K, value: CreationDraftV2[K]) => {
    controller?.invalidate();
    setImpactConsent(false); setSlippageConsent(false); setFundingError(null);
    setTouched(current => (current.has(key) ? current : new Set(current).add(key)));
    setDraft(current => (current ? { ...current, [key]: value, updatedAt: Date.now() } : current));
  }, [controller]);

  useEffect(() => {
    if (prefill.feeStrategy === null || prefillApplied.current || !draft || locked) return;
    prefillApplied.current = true;
    if (draft.feeStrategy !== prefill.feeStrategy) edit('feeStrategy', prefill.feeStrategy);
  }, [prefill.feeStrategy, draft, locked, edit]);

  const perform = useCallback(async (action: () => Promise<unknown>) => {
    setError('');
    try {
      await action();
      if (controller) setHistory(await controller.storage.history());
    } catch (cause) {
      setError(plainLaunchError(cause));
    }
  }, [controller]);

  function stage(next: StagedMedia | null) {
    setStaged(current => { if (current && current !== next) URL.revokeObjectURL(current.previewUrl); return next; });
    setImageSkipped(false);
    edit('stagedMedia', next?.descriptor);
  }

  const rawErrors: Record<string, string> = { ...(draft ? validateCreationDraft(draft) : {}), ...(state.fieldErrors ?? {}), ...(amountError ? { firstBuyRaw: amountError } : {}) };
  const fieldErrors = Object.fromEntries(Object.entries(rawErrors).filter(([key]) => !identityErrorKeys.has(key)));
  const visibleErrors = Object.fromEntries(Object.entries(fieldErrors).filter(([key]) => touched.has(key) || holdsValue(draft, key) || Boolean(state.fieldErrors?.[key]) || Boolean(amountError && key === 'firstBuyRaw')));
  const connected = Boolean(address && chainId === 125 && controller);
  const missing: string[] = [];
  if (draft && !draft.name.trim()) missing.push('a name');
  if (draft && !draft.symbol.trim()) missing.push('a ticker');
  if (!staged) missing.push('an image');
  const formComplete = Object.keys(fieldErrors).length === 0 && missing.length === 0;
  const reviewed = state.reviewed;
  const read = reviewed?.read ?? lastRead;
  const plan = read ? creationFunding(read) : null;

  const opticalReason = useCallback((presetId: string) => {
    if (presetId === STANDARD_OPTICAL) return null;
    return validateCreationDraft({ ...(draftRef.current ?? blankDraft('')), opticalSelection: presetId }).opticalSelection ?? null;
  }, []);

  useEffect(() => {
    if (!controller || !draft || !connected || !formComplete || !historyLoaded || attempt || state.busy || state.reviewed || autoReviewed.current) return;
    autoReviewed.current = true;
    controller.reviewDraft(draft).catch(cause => setFundingError(plainLaunchError(cause, "Fees can't load right now. Try again")));
  }, [controller, draft, connected, formComplete, historyLoaded, attempt, state.busy, state.reviewed]);

  useEffect(() => {
    if (!controller || !attempt || attempt.state !== 'resolved' || !state.resolutionVerified || state.busy || attempt.metadataAssetId || attempt.metadataPatch || attempt.metadataStatus === 'published' || !staged || imageSkipped) return;
    if (autoUploaded.current === attempt.intentId) return;
    autoUploaded.current = attempt.intentId;
    void perform(() => controller.uploadLogo(attempt.intentId, staged.file));
  }, [controller, attempt, state.resolutionVerified, state.busy, staged, imageSkipped, perform]);

  const metadataSettled = Boolean(attempt && ((attempt.metadataStatus === 'published' && state.publicationVerified) || metadataSkipped));
  const launchSettled = Boolean(attempt && attempt.state === 'resolved' && state.resolutionVerified && attempt.poolAddress && metadataSettled && purchaseSettled.has(attempt.purchaseState));

  useEffect(() => {
    if (!launchedHere.current || navigated.current || !launchSettled || !attempt?.poolAddress) return;
    navigated.current = true;
    router.push(poolPath(125, attempt.poolAddress));
  }, [launchSettled, attempt?.poolAddress, router]);

  async function launch() {
    if (!controller || !draft) return;
    let current = await controller.reviewDraft(draft);
    let funding = creationFunding(current.read);
    if (funding.state === 'approval_required') {
      await controller.approve();
      current = await controller.reviewDraft(draft);
      funding = creationFunding(current.read);
    }
    if (funding.state !== 'signable') throw new Error(fundingCopy[funding.state]);
    const submitted = await controller.create();
    launchedHere.current = true;
    await controller.resume(submitted.intentId);
  }

  function primaryAction(): { label: string; onClick?: () => void; disabled?: boolean; reason?: string } | null {
    if (attempt && !attempt.deliberateNew) return null;
    if (!address) return { label: 'Connect wallet to create', onClick: () => openModal('Connect a wallet to create your coin.') };
    if (chainId !== 125) return { label: 'Switch to Paxeer', onClick: () => void switchChainAsync({ chainId: 125 }).catch(cause => setError(plainLaunchError(cause, "Your wallet didn't switch networks. Try again"))) };
    if (!controller) return { label: 'Create coin', disabled: true, reason: 'Getting ready, try again in a moment' };
    if (!draft) return null;
    if (Object.keys(visibleErrors).length > 0) return { label: 'Create coin', disabled: true, reason: 'Fix the highlighted fields first' };
    if (missing.length > 0) return { label: 'Create coin', disabled: true, reason: `Add ${missing.length > 1 ? `${missing.slice(0, -1).join(', ')} and ${missing[missing.length - 1]}` : missing[0]} to continue` };
    if (Object.keys(fieldErrors).length > 0) return { label: 'Create coin', disabled: true, reason: 'Fix the highlighted fields first' };
    if (plan && (plan.state === 'insufficient_usdl' || plan.state === 'insufficient_gas')) return { label: 'Create coin', disabled: true, reason: fundingCopy[plan.state] };
    return { label: 'Create coin', onClick: () => void perform(launch), disabled: state.busy };
  }

  function buildSteps(): LaunchStep[] {
    const steps: LaunchStep[] = [];
    const approveStatus = attempt ? 'done' : state.phase === 'approving' || plan?.state === 'approval_required' ? 'active' : plan?.state === 'signable' ? 'done' : 'pending';
    steps.push({ id: 'approve', label: 'Approve the 1 USDL fee', status: approveStatus, detail: approveStatus === 'done' ? 'Approved' : approveStatus === 'active' ? 'Confirm in your wallet' : undefined });

    if (!attempt) {
      steps.push({ id: 'create', label: 'Create coin', status: state.phase === 'signing' ? 'active' : 'pending', detail: state.phase === 'signing' ? 'Confirm in your wallet' : undefined });
      steps.push({ id: 'image', label: 'Upload image', status: 'pending' });
      steps.push({ id: 'metadata', label: 'Save coin details', status: 'pending' });
      steps.push({ id: 'first-buy', label: 'First buy', status: draft?.firstBuyRaw === '0' ? 'skipped' : 'pending', detail: draft?.firstBuyRaw === '0' ? 'None' : undefined });
      return steps;
    }

    const a = attempt;
    const resolved = a.state === 'resolved' && state.resolutionVerified;
    const createStatus = a.state === 'reverted' ? 'error' : resolved ? 'done' : 'active';
    steps.push({
      id: 'create',
      label: 'Create coin',
      status: createStatus,
      detail: a.state === 'reverted' ? "Didn't go through" : resolved ? 'Created' : a.createTxHash ? (a.state === 'resolved' ? 'Checking...' : 'Confirming...') : 'Not confirmed yet',
      body: !a.createTxHash ? (
        <>
          <p className="text-xs text-text-muted">We couldn&apos;t tell if your wallet sent this. Check your wallet&apos;s activity. If it was sent, paste the transaction ID here so you don&apos;t create the same coin twice.</p>
          <FormField id="create-recovery-hash" label="Transaction ID" mono surface="surface-2" inputProps={{ value: recoveryHash, spellCheck: false, autoComplete: 'off', placeholder: '0x', onChange: event => setRecoveryHash(event.target.value.trim()) }} />
          <div className="flex flex-wrap gap-2">
            <Button variant="primary" size="sm" disabled={state.busy || !/^0x[0-9a-fA-F]{64}$/.test(recoveryHash)} onClick={() => void perform(() => controller!.attachRecoveredHash(a.intentId, recoveryHash))}>Continue</Button>
            <Button variant="secondary" size="sm" disabled={state.busy} onClick={() => void perform(() => controller!.resume(a.intentId))}>Check again</Button>
          </div>
        </>
      ) : (
        <div className="flex flex-wrap items-center gap-3">
          <TxLink hash={a.createTxHash} />
          {a.state !== 'reverted' ? <Button variant="secondary" size="sm" disabled={state.busy} loading={state.busy} onClick={() => void perform(() => controller!.resume(a.intentId))}>Check again</Button> : null}
        </div>
      ),
    });

    const imageDone = Boolean(a.metadataAssetId);
    const imageClosed = Boolean(a.metadataPatch) || a.metadataStatus === 'published';
    const imageStatus = imageDone ? 'done' : imageClosed || imageSkipped ? 'skipped' : resolved ? 'active' : 'pending';
    const differentImage = staged && a.draft.stagedMedia && staged.descriptor.sha256 !== a.draft.stagedMedia.sha256;
    steps.push({
      id: 'image',
      label: 'Upload image',
      status: imageStatus,
      detail: imageDone ? 'Uploaded' : imageStatus === 'skipped' ? 'No image' : undefined,
      body: imageStatus === 'active' ? (
        <>
          {staged ? (
            <div className="flex flex-wrap items-center gap-3">
              <img src={staged.previewUrl} alt="" width={48} height={48} className="size-12 rounded-sm object-cover" />
              <div className="min-w-0 flex-1">
                <p className="truncate text-sm text-text">{staged.descriptor.name}</p>
                {differentImage ? <p className="text-xs text-warning">This isn&apos;t the image you picked before. This one will be used.</p> : null}
              </div>
              <Button variant="primary" size="sm" disabled={state.busy} loading={state.busy} onClick={() => void perform(() => controller!.uploadLogo(a.intentId, staged.file))}>Upload image</Button>
            </div>
          ) : (
            <MediaDropzone id="create-media-resume" staged={null} onStaged={stage} onClear={() => stage(null)} remembered={a.draft.stagedMedia ?? null} disabled={state.busy} headline="Select your coin image" />
          )}
          <p className="text-xs text-text-faint">Your wallet may ask you to sign in first. This is free.</p>
          <Button variant="ghost" size="sm" disabled={state.busy} onClick={() => setImageSkipped(true)}>Continue without an image</Button>
        </>
      ) : undefined,
    });

    const metadataStatus = a.metadataStatus === 'published' ? (state.publicationVerified ? 'done' : 'active') : metadataSkipped ? 'skipped' : resolved && (imageDone || imageSkipped || imageClosed) ? 'active' : 'pending';
    steps.push({
      id: 'metadata',
      label: 'Save coin details',
      status: metadataStatus,
      detail: a.metadataStatus === 'published' ? 'Saved' : metadataSkipped ? 'Add them later from the coin page' : a.metadataLastError ? "Didn't save, try again" : undefined,
      body: metadataStatus === 'active' ? (
        <>
          <p className="text-xs text-text-muted">Adds your description, links and image to the coin page. Your wallet asks you to sign a message. This is free.</p>
          <div className="flex flex-wrap gap-2">
            <Button variant="primary" size="sm" disabled={state.busy} loading={state.busy} onClick={() => void perform(() => controller!.publish(a.intentId))}>{a.metadataStatus === 'published' ? 'Check again' : a.metadataOperationId ? 'Try again' : 'Save details'}</Button>
            <Button variant="ghost" size="sm" disabled={state.busy} onClick={() => setMetadataSkipped(true)}>Skip for now</Button>
            {a.tokenAddress ? <Button variant="link" size="sm" href={`/tokens/${a.tokenAddress}/metadata`}>Edit later</Button> : null}
          </div>
        </>
      ) : undefined,
    });

    const buyRequested = a.draft.firstBuyRaw !== '0';
    const buyStatus = !buyRequested ? 'skipped' : a.purchaseState === 'confirmed' ? 'done' : a.purchaseState === 'skipped' ? 'skipped' : a.purchaseState === 'failed' ? 'error' : resolved && metadataSettled ? 'active' : 'pending';
    const quote = state.quote;
    steps.push({
      id: 'first-buy',
      label: 'First buy',
      status: buyStatus,
      detail: !buyRequested ? 'None' : a.purchaseState === 'confirmed' ? `${formatUsdl(a.draft.firstBuyRaw)} bought` : a.purchaseState === 'skipped' ? 'Skipped' : a.purchaseState === 'failed' ? "Didn't go through" : buyRequested ? formatUsdl(a.draft.firstBuyRaw) : undefined,
      body: buyStatus === 'active' || buyStatus === 'error' ? (
        <>
          <p className="text-xs text-text-muted">Be the first to buy your coin. It&apos;s already live, so you can skip this.</p>
          <FormField
            id="create-slippage"
            label="Slippage"
            surface="surface-2"
            helper="100 = 1%. Between 1 and 5000"
            inputProps={{ type: 'number', min: 1, max: 5000, value: slippage, disabled: state.busy || !!a.purchaseTxHash, onChange: event => { setSlippage(Number(event.target.value)); setImpactConsent(false); setSlippageConsent(false); controller?.invalidate('Purchase slippage changed. Request a new quote.'); } }}
          />
          {!a.purchaseTxHash ? (
            <div className="flex flex-wrap gap-2">
              <Button variant="secondary" size="sm" disabled={state.busy} onClick={() => void perform(() => controller!.approvePurchase(a.intentId))}>Approve {formatUsdl(a.draft.firstBuyRaw)}</Button>
              <Button variant="secondary" size="sm" disabled={state.busy} onClick={() => void perform(() => controller!.reviewPurchase(a.intentId, slippage))}>Get quote</Button>
              <Button variant="ghost" size="sm" disabled={state.busy} onClick={() => void perform(() => controller!.skipPurchase(a.intentId))}>Skip</Button>
            </div>
          ) : (
            <div className="flex flex-wrap items-center gap-3">
              <TxLink hash={a.purchaseTxHash} label="View buy" />
              <Button variant="secondary" size="sm" disabled={state.busy} onClick={() => void perform(() => controller!.resumePurchase(a.intentId))}>Check again</Button>
            </div>
          )}
          {quote ? (
            <div className="flex flex-col gap-2 rounded-md bg-surface-3 p-3">
              <dl className="grid grid-cols-2 gap-x-3 gap-y-1 text-xs">
                <dt className="text-text-faint">You receive</dt><dd data-numeric className="text-right tabular-nums text-text">{formatTokenAmount(quote.amountOutRaw)} {a.draft.symbol}</dd>
                <dt className="text-text-faint">Minimum received</dt><dd data-numeric className="text-right tabular-nums text-text">{formatTokenAmount(quote.minOutRaw)} {a.draft.symbol}</dd>
                <dt className="text-text-faint">Price impact</dt><dd data-numeric className="text-right tabular-nums text-text">{(quote.priceImpactBps / 100).toFixed(2)}%</dd>
                <dt className="text-text-faint">Quote expires</dt><dd className="text-right text-text">{new Date(quote.expiresAt).toLocaleTimeString()}</dd>
              </dl>
              {quote.priceImpactBps >= 500 ? <Consent id="create-impact-consent" checked={impactConsent} onChange={setImpactConsent}>I&apos;m okay with a {(quote.priceImpactBps / 100).toFixed(2)}% price impact</Consent> : null}
              {quote.slippageBps >= 500 ? <Consent id="create-slippage-consent" checked={slippageConsent} onChange={setSlippageConsent}>I&apos;m okay with {(quote.slippageBps / 100).toFixed(2)}% slippage</Consent> : null}
              <Button variant="primary" size="sm" disabled={state.busy} loading={state.busy} onClick={() => void perform(() => controller!.buy(a.intentId, impactConsent, slippageConsent))}>Buy</Button>
            </div>
          ) : null}
        </>
      ) : undefined,
    });
    return steps;
  }

  const action = primaryAction();
  const showProgress = Boolean(attempt || reviewed || lastRead || state.busy);

  return (
    <div className="mx-auto w-full max-w-content-narrow-max-width px-4 py-4 lg:px-6">
      <div className="flex flex-col gap-4 lg:flex-row lg:items-start lg:gap-3">
        <div className="flex min-w-0 flex-1 flex-col gap-4 lg:max-w-form-max-width">
          <h1 className="text-md font-semibold text-text">Create new coin</h1>

          {draft ? (
            <CreateFormFields
              draft={draft}
              errors={visibleErrors}
              locked={locked}
              onEdit={edit}
              registeredOpticals={registeredOpticals}
              registry={registry}
              opticalReason={opticalReason}
            />
          ) : null}

          <section aria-labelledby="create-media-title" className="flex flex-col gap-3 rounded-lg bg-surface-1 p-4">
            <div className="flex flex-col gap-0.5">
              <h2 id="create-media-title" className="text-base font-semibold text-text">Image</h2>
              <p className="text-xs text-text-faint">Uploaded right after your coin is created</p>
            </div>
            <MediaDropzone
              id="create-media"
              staged={staged}
              onStaged={stage}
              onClear={() => stage(null)}
              remembered={draft?.stagedMedia ?? null}
              disabled={locked}
              footer={<MediaRuleLegend />}
            />
            {visibleErrors.stagedMedia ? <p role="alert" className="text-xs text-danger">{visibleErrors.stagedMedia}</p> : null}
          </section>

          <section aria-labelledby="create-costs-title" className="flex flex-col gap-4 rounded-lg bg-surface-1 p-4">
            <h2 id="create-costs-title" className="text-base font-semibold text-text">Cost to create</h2>
            {draft ? (
              <FormField
                id="create-first-buy"
                label="Buy your coin first (USDL)"
                optional
                error={visibleErrors.firstBuyRaw ?? null}
                helper="Be the first buyer. Happens right after your coin is created"
                inputProps={{
                  value: firstBuy,
                  inputMode: 'decimal',
                  disabled: locked,
                  placeholder: '0.00',
                  onChange: event => {
                    const value = event.target.value;
                    setFirstBuy(value);
                    try {
                      if (!/^(?:[0-9]+(?:\.[0-9]{0,6})?)?$/.test(value)) throw new Error('Use up to 6 decimal places');
                      edit('firstBuyRaw', value ? parseUnits(value, 6).toString() : '0');
                      setAmountError('');
                    } catch (cause) {
                      controller?.invalidate('Invalid first purchase amount.');
                      setAmountError(cause instanceof Error && cause.message === 'Use up to 6 decimal places' ? cause.message : 'Enter a valid amount');
                    }
                  },
                }}
              />
            ) : null}
            <FeeSummary
              read={read}
              plan={plan}
              firstBuyRaw={draft?.firstBuyRaw ?? '0'}
              stale={!reviewed && !!lastRead}
              error={fundingError}
              action={connected && formComplete && !attempt ? (
                <Button variant="ghost" size="sm" disabled={state.busy} onClick={() => void perform(async () => { setFundingError(null); await controller!.reviewDraft(draft!); })}>Refresh</Button>
              ) : null}
            />
            {!address ? (
              <NoticeBar tone="info" announce wrap>You can fill in everything now. A wallet is only needed when you create.</NoticeBar>
            ) : chainId !== 125 ? (
              <NoticeBar tone="warning" announce wrap>Your wallet is on the wrong network. Switch to Paxeer to create.</NoticeBar>
            ) : !controller ? (
              <NoticeBar tone="warning" announce wrap>Getting ready. You can create in a moment.</NoticeBar>
            ) : null}
            {action ? (
              <div className="flex flex-col gap-2">
                <Button variant="primary" size="lg" icon={<Rocket />} disabled={action.disabled} loading={state.busy && !action.disabled} onClick={action.onClick} aria-describedby={action.reason ? 'create-primary-reason' : undefined}>
                  {action.label}
                </Button>
                {action.reason ? <p id="create-primary-reason" className="text-xs text-text-faint">{action.reason}</p> : null}
                {draft ? <p className="text-xs text-text-faint">{rewardsLine[draft.feeStrategy] ?? `Creator rewards go to ${feeStrategyLabel(draft.feeStrategy).toLowerCase()}`}. Creating costs 1 USDL, and your wallet may ask you to confirm twice.</p> : null}
              </div>
            ) : null}
            {showProgress ? <LaunchProgress steps={buildSteps()} message={plainLaunchStatus(state.message)} busy={state.busy} /> : null}
            {launchSettled && attempt?.poolAddress ? (
              <NoticeBar tone="success" variant="card" title="Your coin is live" announce action={<Button variant="primary" size="sm" href={poolPath(125, attempt.poolAddress)}>View coin</Button>}>
                Anyone can buy it now. Share it to get your first buyers!
              </NoticeBar>
            ) : null}
            {error ? <p role="alert" className="text-sm text-danger">{error}</p> : null}
            {attempt && !attempt.deliberateNew && controller ? (
              <DisclosureRow id="create-separate-launch" label="Create a different coin instead">
                <div className="flex flex-col gap-3">
                  <p className="text-xs text-text-muted">You have a launch in progress above. To make sure you don&apos;t create the same coin twice, check it first.</p>
                  <Consent id="create-recovery-checked" checked={recoveryChecked} onChange={setRecoveryChecked}>I checked the launch above and my wallet&apos;s activity</Consent>
                  <Consent id="create-new-consent" checked={newConsent} onChange={setNewConsent}>I want to create a new, separate coin</Consent>
                  <Button variant="secondary" size="sm" disabled={state.busy || !recoveryChecked || !newConsent} onClick={() => void perform(() => controller.deliberateNew(attempt.intentId, recoveryChecked, newConsent))}>Start a new coin</Button>
                </div>
              </DisclosureRow>
            ) : null}
            {controller && address ? (
              <DisclosureRow id="create-history" label="Your past launches" trailing={history.length > 0 ? <span className="text-xs text-text-faint">{history.length}</span> : null}>
                <div className="flex flex-col gap-3">
                  {history.length === 0 ? <p className="text-xs text-text-faint">No coins created from this browser yet</p> : (
                    <ul className="flex flex-col gap-1">
                      {history.map(row => (
                        <li key={row.intentId}>
                          <button type="button" disabled={state.busy} onClick={() => void perform(() => controller.resume(row.intentId))} className="flex w-full items-center justify-between gap-3 rounded-sm px-2 py-2 text-left text-sm text-text transition-colors duration-fast hover:bg-surface-hover disabled:text-text-disabled">
                            <span className="truncate">{row.draft.name || 'Untitled'} <span className="text-text-faint">${row.draft.symbol}</span></span>
                            <span className="shrink-0 text-xs text-text-faint">{launchStateLabel[row.state]}</span>
                          </button>
                        </li>
                      ))}
                    </ul>
                  )}
                  <div className="flex flex-wrap items-center gap-2">
                    <Button variant="secondary" size="sm" disabled={state.busy} onClick={() => void perform(async () => { const page = await controller.chain.createdPools(address, historyOffset); setVerifiedPools(previous => [...previous, ...page]); setHistoryOffset(previous => previous + BigInt(page.length)); })}>Show coins I created</Button>
                    {verifiedPools.map(pool => <Button key={pool} variant="link" size="sm" href={poolPath(125, pool)}>{shortAddress(pool)}</Button>)}
                  </div>
                </div>
              </DisclosureRow>
            ) : null}
          </section>
        </div>

        <aside className="shrink-0 lg:sticky lg:top-4 lg:w-56 2xl:w-60">
          <CoinPreviewCard
            name={draft?.name ?? ''}
            symbol={draft?.symbol ?? ''}
            description={draft?.description ?? ''}
            image={staged?.previewUrl ?? null}
            strategy={draft?.feeStrategy ?? 0}
            socialLinks={draft?.socialLinks ?? []}
          />
        </aside>
      </div>
    </div>
  );
}
