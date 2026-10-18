'use client';
import { useState } from 'react';
import { Info } from 'lucide-react';
import { DisclosureRow, KeyValueRow } from '@/ui/primitives';
import { formatUsdl } from '@/utils/format';
import { cupModeLabel, cupScoringLabel, ladderTotalRaw, type Cup } from './cups-contract';

export interface CupRulesDisclosureProps {
  cup: Cup;
  className?: string;
}

const dateTime = (iso: string) => new Date(iso).toLocaleString(undefined, { dateStyle: 'medium', timeStyle: 'short' });

/** Markdown from the cup record rendered as plain paragraphs; no markup is interpreted. */
export function rulesParagraphs(markdown: string): string[] {
  return markdown.split(/\n{2,}/).map(block => block.replace(/^#+\s*/gm, '').replace(/\s*\n\s*/g, ' ').trim()).filter(Boolean);
}

/**
 * Rules row (components.md 68, rules variant) expanding into the cup's
 * schedule, scoring, eligibility and prize ladder, all read from the record.
 */
export function CupRulesDisclosure({ cup, className }: CupRulesDisclosureProps) {
  const [open, setOpen] = useState(false);
  const paragraphs = rulesParagraphs(cup.rulesMarkdown);
  return (
    <DisclosureRow id={`cup-rules-${cup.cupId}`} icon={Info} label="Schedule, scoring, eligibility and rules" open={open} onToggle={setOpen} className={className}>
      <dl className="flex flex-col">
        <KeyValueRow label="Starts" value={dateTime(cup.startsAt)} />
        <KeyValueRow label="Ends" value={dateTime(cup.endsAt)} />
        <KeyValueRow label="Format" value={cupModeLabel[cup.mode]} />
        <KeyValueRow label="Scoring" value={cupScoringLabel[cup.scoring]} />
        <KeyValueRow label="Prize pool" value={formatUsdl(cup.prizePoolRaw, { fractionDigits: 0 })} />
      </dl>
      {cup.prizeLadder.length > 0 ? (
        <table className="mt-3 w-full text-sm">
          <caption className="mb-1 text-left text-2xs font-bold uppercase tracking-[0.04em] text-text-faint">Prize ladder</caption>
          <thead>
            <tr className="text-left text-2xs uppercase tracking-[0.04em] text-text-faint">
              <th scope="col" className="h-9 font-bold">Rank</th>
              <th scope="col" className="h-9 text-right font-bold">Prize</th>
            </tr>
          </thead>
          <tbody>
            {[...cup.prizeLadder].sort((a, b) => a.rank - b.rank).map(tier => (
              <tr key={tier.rank} className="odd:bg-surface-3">
                <th scope="row" className="h-9 px-2 text-left font-semibold text-text-muted">#{tier.rank}</th>
                <td className="h-9 px-2 text-right font-bold tabular-nums text-text" data-numeric>{formatUsdl(tier.amountRaw, { fractionDigits: 0 })}</td>
              </tr>
            ))}
            <tr>
              <th scope="row" className="h-9 px-2 text-left font-semibold text-text-muted">Total</th>
              <td className="h-9 px-2 text-right font-bold tabular-nums text-text" data-numeric>{formatUsdl(ladderTotalRaw(cup).toString(), { fractionDigits: 0 })}</td>
            </tr>
          </tbody>
        </table>
      ) : null}
      {paragraphs.length > 0 ? (
        <div className="mt-3 flex flex-col gap-2 text-sm text-text-muted">
          {paragraphs.map((text, index) => <p key={index}>{text}</p>)}
        </div>
      ) : (
        <p className="mt-3 text-sm text-text-faint">No rules posted for this cup yet</p>
      )}
    </DisclosureRow>
  );
}
