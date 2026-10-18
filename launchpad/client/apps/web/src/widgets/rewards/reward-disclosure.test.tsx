import React from 'react';
import { renderToString } from 'react-dom/server';
import { describe, expect, it } from 'vitest';
import { isRawErrorText, plainCopy } from '@/core/user-facing-error';
import { rewardProgramView, rewardPrograms } from './reward-disclosure';
import { RewardProgramList } from './rewards-view';

/** Backend jargon the rewards disclosure used to print as body text. */
const JARGON = /FeesRouter|IF_[A-Z_]+|\/v2\/|authority|settlement|ledger|nftId|USDL6|chain125|qualified|Blocker|Entitlement API|wallet lane/;
const BORDER = /\b(border(?!-0\b|-none\b)(-[a-z0-9/\[\]-]+)?|divide-[a-z]|ring-(?!offset)[a-z0-9]|outline-(?!none|focus|2|offset)[a-z]+)\b/;

function bodyText(html: string): string {
  return html.replace(/<[^>]*>/g, ' ').replace(/&#x27;/g, "'").replace(/&amp;/g, '&');
}

describe('reward program disclosure', () => {
  it('routes the status through userFacingMessage and keeps backend detail out of production', () => {
    for (const program of rewardPrograms) {
      const view = rewardProgramView(program, 'production');
      expect(view.status).toBe(plainCopy.notAvailable);
      expect(view.technical).toBeNull();
      for (const text of [view.title, view.summary, view.status]) {
        expect(isRawErrorText(text)).toBe(false);
        expect(text).not.toMatch(JARGON);
      }
    }
  });

  it('never exposes the technical detail, whatever the environment name', () => {
    const cup = rewardPrograms.find(program => program.type === 'cup')!;
    expect(rewardProgramView(cup, 'development').technical).toBeNull();
    expect(rewardProgramView(cup, 'production').technical).toBeNull();
  });

  it('renders the real list with plain body text, no technical attribute and no border strokes', () => {
    const production = renderToString(<RewardProgramList env="production" />);
    expect(bodyText(production)).not.toMatch(JARGON);
    expect(production).not.toContain('data-technical');
    expect(bodyText(production)).toContain('Creator fees');
    expect(bodyText(production)).toContain(plainCopy.notAvailable);
    const classes = Array.from(production.matchAll(/class="([^"]*)"/g)).flatMap(match => match[1].split(/\s+/));
    expect(classes.filter(name => BORDER.test(name))).toEqual([]);

    const development = renderToString(<RewardProgramList env="development" />);
    expect(bodyText(development)).not.toMatch(JARGON);
    expect(development).not.toContain('data-technical');
  });
});
