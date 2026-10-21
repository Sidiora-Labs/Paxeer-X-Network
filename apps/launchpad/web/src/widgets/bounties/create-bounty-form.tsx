'use client';
import { useEffect, useId, useState, type FormEvent } from 'react';
import { useQuery } from '@tanstack/react-query';
import { Plus, Search, X } from 'lucide-react';
import { Button, FormField, IconButton, NoticeBar, SegmentedControl, UnavailableState } from '@/ui/primitives';
import { cn } from '@/lib/utils';
import { shortAddress } from '@/utils/format';
import { queryKeys } from '@/core/query-keys';
import { useOperations } from '@/core/market/operations';
import { toUnsupported } from '@/core/market/unsupported';
import { userFacingMessage } from '@/core/user-facing-error';
import { usePoolSnapshot } from '@/core/market/use-pool-snapshot';
import { openAuthModal } from '@/shell/auth-modal-store';
import { bountiesContract, type BountyFailure } from './bounties-contract';
import { BountyTokenChip } from './bounty-token-chip';
import { BountyCard } from './bounty-card';
import { FundingStep } from './funding-step';
import { useBountiesAvailability } from './use-bounties';
import { DESCRIPTION_MAX, TITLE_MAX, parseDecimalAmount, useCreateBounty, type BountyCreateBody } from './use-bounty-mutations';

const SEARCH_LIMIT = 8;
const SEARCH_MIN = 2;
const ADDRESS = /^0x[0-9a-fA-F]{40}$/;

type AssetChoice = 'usdl' | 'token';

function useDebounced(value: string, ms: number): string {
  const [debounced, setDebounced] = useState(value);
  useEffect(() => {
    const timer = setTimeout(() => setDebounced(value), ms);
    return () => clearTimeout(timer);
  }, [value, ms]);
  return debounced;
}

function failureText(failure: BountyFailure): string {
  return failure.state === 'unsupported' ? 'Try again in a moment' : userFacingMessage(failure.error, 'Something went wrong, try again');
}

interface PoolPickerProps { value: string | null; onChange: (pool: string | null) => void }

/** Linked-pool search through IF_SEARCH type=pool; the chosen pool renders as the live token chip. */
function PoolPicker({ value, onChange }: PoolPickerProps) {
  const id = useId();
  const operations = useOperations();
  const [text, setText] = useState('');
  const q = useDebounced(text.trim(), 250);
  const enabled = value === null && q.length >= SEARCH_MIN;
  const search = useQuery({
    queryKey: queryKeys.search(q, 'pool', SEARCH_LIMIT),
    queryFn: async ({ signal }) => (await operations.call('IF_SEARCH', { query: { q, type: 'pool', limit: SEARCH_LIMIT } }, { signal })).data.items,
    enabled,
    retry: false,
    staleTime: 30_000,
  });
  const results = (search.data ?? []).filter(item => item.poolAddress !== null && ADDRESS.test(item.poolAddress));
  const unsupported = search.status === 'error' ? toUnsupported('IF_SEARCH', search.error) : null;

  if (value) {
    return (
      <div className="flex flex-col gap-1.5">
        <span className="text-sm font-bold text-text">Linked coin</span>
        <div className="flex items-center gap-2 rounded-sm bg-surface-2 px-3 py-2">
          <BountyTokenChip poolAddress={value} />
          <span className="truncate font-mono text-xs text-text-faint">{shortAddress(value, 6, 4)}</span>
          <IconButton icon={<X />} label="Remove linked coin" size="sm" onClick={() => onChange(null)} className="ml-auto" />
        </div>
      </div>
    );
  }

  return (
    <div className="flex flex-col gap-2">
      <FormField
        id={`${id}-search`}
        label="Linked coin"
        optional
        leading={<Search />}
        loading={enabled && search.isFetching}
        helper="Search by name, ticker or address to pay the reward in that coin"
        inputProps={{ value: text, onChange: event => setText(event.target.value), placeholder: 'Search coins', autoComplete: 'off', type: 'search', 'aria-controls': `${id}-results` }}
      />
      <div id={`${id}-results`} aria-live="polite">
        {unsupported ? <UnavailableState variant="inline" title="Coin search" operation={unsupported.operation} status={unsupported.status} /> : null}
        {search.status === 'error' && !unsupported ? <p role="alert" className="text-xs text-danger">Search can't load right now, try again</p> : null}
        {enabled && search.status === 'success' && results.length === 0 ? <p className="text-xs text-text-faint">No coins match “{q}”</p> : null}
        {enabled && results.length > 0 ? (
          <ul aria-label="Matching coins" className="flex flex-col gap-1 rounded-sm bg-surface-2 p-1">
            {results.map(item => (
              <li key={item.id}>
                <button
                  type="button"
                  onClick={() => { onChange((item.poolAddress as string).toLowerCase()); setText(''); }}
                  className="flex w-full items-center justify-between gap-3 rounded-sm px-3 py-2 text-left text-sm text-text hover:bg-surface-hover focus-visible:outline-2 focus-visible:outline-focus"
                >
                  <span className="truncate">{item.label}</span>
                  <span className="shrink-0 font-mono text-xs text-text-faint">{shortAddress(item.poolAddress, 4, 4)}</span>
                </button>
              </li>
            ))}
          </ul>
        ) : null}
      </div>
    </div>
  );
}

/**
 * Create-bounty form (title, description, linked coin, reward asset,
 * budget). The fields always work; submitting checks sign-in first, then the
 * bounties service probe, and only then calls IF_BOUNTY_CREATE. A created
 * bounty moves straight to the funding step.
 */
export function CreateBountyForm({ className }: { className?: string }) {
  const id = useId();
  const availability = useBountiesAvailability();
  const { signedIn, mutation } = useCreateBounty();
  const [title, setTitle] = useState('');
  const [description, setDescription] = useState('');
  const [pool, setPool] = useState<string | null>(null);
  const [asset, setAsset] = useState<AssetChoice>('usdl');
  const [budget, setBudget] = useState('');
  const [attempted, setAttempted] = useState(false);
  const [blocked, setBlocked] = useState(false);
  const snapshot = usePoolSnapshot(pool);
  const token = pool && snapshot.data?.token && snapshot.data.pool.poolAddress.toLowerCase() === pool ? snapshot.data.token : null;
  const tokenUsable = token !== null && token.decimalsQuality === 'verified' && token.decimals !== null;
  const effectiveAsset: AssetChoice = asset === 'token' && tokenUsable ? 'token' : 'usdl';
  const decimals = effectiveAsset === 'usdl' ? bountiesContract.usdlDecimals : (token?.decimals as number);
  const symbol = effectiveAsset === 'usdl' ? 'USDL' : token?.symbol ?? 'token';
  const budgetRaw = parseDecimalAmount(budget, decimals);

  const errors = {
    title: !title.trim() ? 'Give your bounty a title' : Array.from(title).length > TITLE_MAX ? `Keep the title under ${TITLE_MAX} characters` : null,
    description: !description.trim() ? 'Say what a winning submission looks like' : Array.from(description).length > DESCRIPTION_MAX ? `Keep the description under ${DESCRIPTION_MAX} characters` : null,
    budget: budgetRaw === null ? `Enter an amount with up to ${decimals} decimals` : null,
  };
  const valid = !errors.title && !errors.description && !errors.budget;

  useEffect(() => { if (availability.loadable.state === 'ready') setBlocked(false); }, [availability.loadable.state]);

  function submit(event: FormEvent) {
    event.preventDefault();
    setAttempted(true);
    if (!signedIn) { openAuthModal('Sign in with your wallet to create a bounty'); return; }
    if (availability.loadable.state !== 'ready') { setBlocked(true); return; }
    if (!valid || budgetRaw === null || mutation.isPending) return;
    const body: BountyCreateBody = {
      title: title.trim(),
      description: description.trim(),
      assetAddress: effectiveAsset === 'usdl' ? bountiesContract.usdlAddress : (token?.address as string).toLowerCase(),
      budgetRaw: budgetRaw.toString(),
      ...(pool ? { poolAddress: pool } : {}),
    };
    mutation.mutate(body);
  }

  if (mutation.isSuccess) {
    const created = mutation.data.data;
    return (
      <div className={cn('flex flex-col gap-4', className)}>
        <NoticeBar tone="success" announce title="Bounty created!">
          Fund it to start taking submissions
        </NoticeBar>
        <BountyCard bounty={created.bounty} />
        <FundingStep bounty={created.bounty} request={created.fundingTransactionRequest} />
      </div>
    );
  }

  const probe = availability.loadable;
  const failure = mutation.error;

  return (
    <form noValidate onSubmit={submit} aria-labelledby={`${id}-heading`} className={cn('flex flex-col gap-5 rounded-lg bg-surface-1 p-4 md:p-6', className)}>
      <h2 id={`${id}-heading`} className="text-lg font-bold text-text">Bounty details</h2>
      <FormField
        id={`${id}-title`}
        label="Title"
        error={attempted ? errors.title : null}
        counter={{ count: Array.from(title).length, max: TITLE_MAX }}
        inputProps={{ value: title, onChange: event => setTitle(event.target.value), maxLength: TITLE_MAX * 2, required: true, placeholder: 'What should people do?' }}
      />
      <FormField
        id={`${id}-description`}
        label="Description"
        multiline
        error={attempted ? errors.description : null}
        helper="Tasks, rules and how you'll pick winners"
        counter={{ count: Array.from(description).length, max: DESCRIPTION_MAX }}
        textareaProps={{ value: description, onChange: event => setDescription(event.target.value), rows: 6, required: true }}
      />
      <PoolPicker value={pool} onChange={next => { setPool(next); if (!next) setAsset('usdl'); }} />
      <div className="flex flex-col gap-1.5">
        <span id={`${id}-asset-label`} className="text-sm font-bold text-text">Reward asset</span>
        <SegmentedControl<AssetChoice>
          label="Reward asset"
          value={effectiveAsset}
          onChange={setAsset}
          options={[
            { value: 'usdl', label: 'USDL' },
            { value: 'token', label: token ? `$${token.symbol}` : 'Linked coin', disabled: !tokenUsable },
          ]}
        />
        <p className="text-xs text-text-faint">
          {pool === null ? 'Link a coin to pay the reward in it' : snapshot.isPending ? 'Loading coin...' : !tokenUsable ? "This coin can't be used as a reward yet, use USDL" : "Coin rewards are valued at the coin's current price"}
        </p>
      </div>
      <FormField
        id={`${id}-budget`}
        label="Reward"
        mono
        error={attempted ? errors.budget : null}
        helper="Locked when you fund the bounty. Network fee is paid separately"
        trailing={<span className="text-sm font-bold text-text-muted">{symbol}</span>}
        inputProps={{ value: budget, onChange: event => setBudget(event.target.value.replace(',', '.')), inputMode: 'decimal', placeholder: '0.00', autoComplete: 'off' }}
      />

      {blocked && probe.state === 'unsupported' ? (
        <UnavailableState title="Creating bounties" operation={probe.operation} status={probe.status} message="Your draft is saved here. Try again in a moment" onRetry={availability.retry} />
      ) : null}
      {blocked && probe.state === 'error' ? <NoticeBar tone="danger" announce title="Can't create bounties right now">Your draft is saved here. Try again in a moment</NoticeBar> : null}
      {blocked && probe.state === 'loading' ? <p role="status" className="text-sm text-text-muted">Still checking, try again in a moment</p> : null}
      {failure && failure.state === 'unsupported' ? (
        <UnavailableState title="Creating bounties" operation={failure.operation} status={failure.status} message="Try again in a moment" />
      ) : null}
      {failure && failure.state === 'error' ? <NoticeBar tone="danger" announce title="Couldn't create bounty">{failureText(failure)}</NoticeBar> : null}

      <div className="flex flex-col gap-2 md:flex-row md:items-center md:justify-end">
        <Button type="submit" variant="primary" size="lg" icon={<Plus />} loading={mutation.isPending} className="w-full md:w-auto">
          {signedIn ? 'Create bounty' : 'Sign in to create'}
        </Button>
      </div>
    </form>
  );
}
