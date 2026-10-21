'use client';

import { useEffect, useState } from 'react';
import { Link2 } from 'lucide-react';
import { DisclosureRow, FormField } from '@/ui/primitives';

export interface SocialSlots {
  x: string;
  telegram: string;
  website: string;
  others: string[];
}

const X_HOSTS = new Set(['x.com', 'www.x.com', 'twitter.com', 'www.twitter.com', 'mobile.twitter.com']);
const TELEGRAM_HOSTS = new Set(['t.me', 'www.t.me', 'telegram.me', 'telegram.org', 'www.telegram.org']);

function hostOf(value: string): string | null {
  try {
    return new URL(value).hostname.toLowerCase();
  } catch {
    return null;
  }
}

export function slotsFromLinks(links: readonly string[]): SocialSlots {
  const slots: SocialSlots = { x: '', telegram: '', website: '', others: [] };
  for (const link of links) {
    const host = hostOf(link);
    if (host && X_HOSTS.has(host) && !slots.x) slots.x = link;
    else if (host && TELEGRAM_HOSTS.has(host) && !slots.telegram) slots.telegram = link;
    else if (!slots.website) slots.website = link;
    else slots.others.push(link);
  }
  return slots;
}

export function linksFromSlots(slots: SocialSlots): string[] {
  return [slots.x, slots.telegram, slots.website, ...slots.others].map(value => value.trim()).filter(Boolean);
}

/** Mirrors the draft validator's link rule so each field can explain its own problem. */
export function linkProblem(value: string): string | null {
  const trimmed = value.trim();
  if (!trimmed) return null;
  if (/[\p{Cc}\p{Cf}]/u.test(trimmed)) return "This link has characters that aren't allowed";
  if (trimmed.length > 2048) return 'This link is too long';
  try {
    const url = new URL(trimmed);
    if (url.protocol !== 'https:') return 'Link must start with https://';
    if (url.username || url.password) return 'Remove the username and password from this link';
    return null;
  } catch {
    return 'Enter a full link starting with https://';
  }
}

export interface SocialLinksDisclosureProps {
  links: string[];
  onChange: (links: string[]) => void;
  disabled?: boolean;
  error?: string | null;
  defaultOpen?: boolean;
}

export function SocialLinksDisclosure({ links, onChange, disabled = false, error = null, defaultOpen = false }: SocialLinksDisclosureProps) {
  const [open, setOpen] = useState(defaultOpen || links.length > 0);
  const [slots, setSlots] = useState<SocialSlots>(() => slotsFromLinks(links));

  useEffect(() => {
    setSlots(current => (JSON.stringify(linksFromSlots(current)) === JSON.stringify(links) ? current : slotsFromLinks(links)));
  }, [links]);

  function update(patch: Partial<SocialSlots>) {
    const next = { ...slots, ...patch };
    setSlots(next);
    onChange(linksFromSlots(next));
  }

  const count = linksFromSlots(slots).length;
  return (
    <DisclosureRow
      id="create-social-links"
      label="Add social links"
      icon={Link2}
      optional
      open={open}
      onToggle={setOpen}
      trailing={count > 0 ? <span className="text-xs text-text-faint">{count} added</span> : null}
    >
      <div className="flex flex-col gap-4">
        <FormField
          id="create-link-x"
          label="X"
          optional
          surface="surface-2"
          error={linkProblem(slots.x)}
          helper="https://x.com/yourcoin"
          inputProps={{ value: slots.x, disabled, inputMode: 'url', autoComplete: 'off', spellCheck: false, placeholder: 'https://x.com/', onChange: event => update({ x: event.target.value }) }}
        />
        <FormField
          id="create-link-telegram"
          label="Telegram"
          optional
          surface="surface-2"
          error={linkProblem(slots.telegram)}
          helper="https://t.me/yourgroup"
          inputProps={{ value: slots.telegram, disabled, inputMode: 'url', autoComplete: 'off', spellCheck: false, placeholder: 'https://t.me/', onChange: event => update({ telegram: event.target.value }) }}
        />
        <FormField
          id="create-link-website"
          label="Website"
          optional
          surface="surface-2"
          error={linkProblem(slots.website)}
          helper="https://yourcoin.com"
          inputProps={{ value: slots.website, disabled, inputMode: 'url', autoComplete: 'off', spellCheck: false, placeholder: 'https://', onChange: event => update({ website: event.target.value }) }}
        />
        {slots.others.map((link, index) => (
          <FormField
            key={`other-${index}`}
            id={`create-link-other-${index}`}
            label={`Link ${index + 4}`}
            optional
            surface="surface-2"
            error={linkProblem(link)}
            inputProps={{ value: link, disabled, inputMode: 'url', autoComplete: 'off', spellCheck: false, onChange: event => update({ others: slots.others.map((value, i) => (i === index ? event.target.value : value)) }) }}
          />
        ))}
        <p className="text-xs text-text-faint">Add up to 5 links. You can edit them later.</p>
        {error ? <p role="alert" className="text-xs text-danger">{error}</p> : null}
      </div>
    </DisclosureRow>
  );
}
