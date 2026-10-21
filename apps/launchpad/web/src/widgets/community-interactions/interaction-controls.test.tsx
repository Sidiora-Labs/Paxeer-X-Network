// @vitest-environment jsdom
import { act } from 'react';
import { createRoot } from 'react-dom/client';
import { afterEach, describe, expect, it } from 'vitest';
import { PlainInteractionContent, ExternalInteractionLink, validateInteractionText } from './plain-content';
import { CommentComposer, CommentActions, CalloutMentions, ReactionControl } from './interaction-controls';
import { InteractionClient } from './interaction-client';

const roots: ReturnType<typeof createRoot>[] = [];
function render(element: Parameters<ReturnType<typeof createRoot>['render']>[0]) {
  const container = document.createElement('div'); document.body.append(container);
  const root = createRoot(container); roots.push(root); act(() => root.render(element)); return container;
}
afterEach(() => { for (const root of roots.splice(0)) act(() => root.unmount()); document.body.replaceChildren(); });
describe('real React interaction controls', () => {
  it('renders hostile text inertly without interpreting HTML', () => {
    const text = '<script>globalThis.attack=1</script><img src=x onerror=alert(1)>';
    const element = render(<PlainInteractionContent text={text} />);
    expect(element.textContent).toBe(text); expect(element.querySelector('script')).toBeNull(); expect(element.querySelector('img')).toBeNull();
  });
  it('allows only http and https external links with visible warning', () => {
    const blocked = render(<ExternalInteractionLink href="javascript:alert(1)">unsafe</ExternalInteractionLink>); expect(blocked.querySelector('a')).toBeNull();
    const safe = render(<ExternalInteractionLink href="https://example.com">safe</ExternalInteractionLink>); const link = safe.querySelector('a'); expect(link?.getAttribute('rel')).toBe('noopener noreferrer'); expect(link?.textContent).toContain('(external)');
  });
  it('matches server scalar, UTF8 and hidden-control limits', () => {
    expect(validateInteractionText('界'.repeat(2000))).toBe(true); expect(validateInteractionText('界'.repeat(2001))).toBe(false); expect(validateInteractionText('😀'.repeat(2000))).toBe(true); expect(validateInteractionText('x\u202e')).toBe(false); expect(validateInteractionText('x\0')).toBe(false); expect(validateInteractionText('\ud800')).toBe(false); expect(validateInteractionText('a\n\tb')).toBe(true);
  });
  it('has labelled composer input and disabled invalid submission', () => {
    const container = render(<CommentComposer client={new InteractionClient()} pool={'0x' + '1'.repeat(40)} onCreated={() => {}} />);
    const label = container.querySelector('label'); const input = container.querySelector('textarea'); expect(label?.htmlFor).toBe(input?.id); expect(container.querySelector('button')?.disabled).toBe(true);
  });
  it('separates author editing from creator hiding with a required reason', () => {
    const comment = { commentId: '019a0000-0000-7000-8000-000000000001', poolAddress: '0x' + '1'.repeat(40), authorWallet: '0x' + '2'.repeat(40), parentCommentId: null, body: 'hello', revision: '1', likeCount: '0', edited: false };
    const container = render(<CommentActions client={new InteractionClient()} comment={comment} own={false} creator={true} onChanged={() => {}} />);
    expect(container.textContent).toContain('Hide as coin creator'); expect(container.textContent).not.toContain('Delete'); expect(container.querySelector('button')?.disabled).toBe(true);
  });
  it('shows reaction state and distinguishes user content from verified evidence', () => {
    const reaction = render(<ReactionControl client={new InteractionClient()} commentId="target" liked={true} count="1" onChanged={() => {}} />); expect(reaction.querySelector('button')?.getAttribute('aria-pressed')).toBe('true');
    const callout = render(<CalloutMentions client={new InteractionClient()} id="target" revision="1" onChanged={() => {}} />); expect(callout.textContent).toContain('Posted by users'); expect(callout.textContent).toContain('once we confirm the transaction');
  });
});

describe('adversarial content and interrupted scope changes', () => {
  it.each([
    '<script>globalThis.attack=1</script>',
    '<svg onload=alert(1)>',
    '<img src=x onerror=alert(1)>',
    '<a href="javascript:alert(1)">click</a>',
    'javascript:alert(1)',
    'x\u202e',
    'x\u2066',
    '界'.repeat(2000),
    '😀'.repeat(2000),
  ])('keeps corpus content inert as plain text', (text) => {
    const element = render(<PlainInteractionContent text={text} />);
    expect(element.textContent).toBe(text);
    expect(element.querySelector('script, svg, img, a, iframe')).toBeNull();
  });
  it.each(['javascript:alert(1)', 'JaVaScRiPt:alert(1)', 'data:text/html,<script>alert(1)</script>', 'file:///etc/passwd', '//example.com', 'https://user:secret@example.com', 'https://exa\nmple.com', 'https://example.com/\u202e'])('rejects unsafe link corpus', (href) => {
    expect(render(<ExternalInteractionLink href={href}>destination</ExternalInteractionLink>).querySelector('a')).toBeNull();
  });
  it.each([
    ['界'.repeat(2000), true], ['界'.repeat(2001), false],
    ['😀'.repeat(2000), true], ['😀'.repeat(2001), false],
    ['a\n\tb', true], [' \n\t', false], ['x\0', false],
    ['x\u202e', false], ['x\u2066', false], ['x\u200b', false], ['\ud800', false],
  ] as const)('enforces deterministic scalar validation for corpus', (text, expected) => {
    expect(validateInteractionText(text)).toBe(expected);
  });
  it('refreshes author editor from the current server revision after removal races', () => {
    const client = new InteractionClient();
    const comment = { commentId: '019a0000-0000-7000-8000-000000000001', poolAddress: '0x' + '1'.repeat(40), authorWallet: '0x' + '2'.repeat(40), parentCommentId: null, body: 'old revision', revision: '1', likeCount: '0', edited: false };
    const container = render(<CommentActions client={client} comment={comment} own={true} creator={false} onChanged={() => {}} />);
    expect(container.querySelector('textarea')?.value).toBe('old revision');
    act(() => roots[roots.length - 1].render(<CommentActions client={client} comment={{ ...comment, body: 'current revision', revision: '2', edited: true }} own={true} creator={false} onChanged={() => {}} />));
    expect(container.querySelector('textarea')?.value).toBe('current revision');
    expect(container.textContent).toContain('Edited');
  });
});

describe('Phase4 interaction accessibility', () => {
  it('gives every mutation input a unique visible label and explicit button semantics', () => {
    const client = new InteractionClient();
    const comment = { commentId: '019a0000-0000-7000-8000-000000000001', poolAddress: '0x' + '1'.repeat(40), authorWallet: '0x' + '2'.repeat(40), parentCommentId: null, body: 'Accessible comment', revision: '1', likeCount: '0', edited: false };
    const container = render(<><CommentComposer client={client} pool={comment.poolAddress} onCreated={() => {}} /><CommentActions client={client} comment={comment} own={true} creator={false} onChanged={() => {}} /><CommentActions client={client} comment={comment} own={false} creator={true} onChanged={() => {}} /><CalloutMentions client={client} id={comment.commentId} revision={comment.revision} onChanged={() => {}} /></>);
    const inputs = Array.from(container.querySelectorAll<HTMLInputElement | HTMLTextAreaElement>('input, textarea'));
    const ids = inputs.map(input => input.id);
    expect(new Set(ids).size).toBe(inputs.length);
    for (const input of inputs) {
      expect(input.id).not.toBe('');
      const label = Array.from(container.querySelectorAll('label')).find(value => value.htmlFor === input.id);
      expect(label?.textContent?.trim()).not.toBe('');
      expect(label).toBeDefined();
    }
    for (const button of container.querySelectorAll('button')) {
      expect(button.hasAttribute('type')).toBe(true);
      expect(button.textContent?.trim()).not.toBe('');
      expect(button.getAttribute('tabindex')).not.toBe('-1');
      if (!button.disabled) { button.focus(); expect(document.activeElement).toBe(button); }
    }
  });
  it('exposes reaction state and count through accessible names', () => {
    const element = render(<ReactionControl client={new InteractionClient()} commentId="target" liked={true} count="125" onChanged={() => {}} />);
    expect(element.querySelector('button')?.getAttribute('aria-pressed')).toBe('true');
    expect(element.querySelector('[aria-label="125 likes"]')).not.toBeNull();
    expect(element.querySelector('button')?.textContent).toContain('Unlike');
  });
  it('announces the external destination and preserves keyboard link semantics', () => {
    const element = render(<ExternalInteractionLink href="https://example.com/community">Community reference</ExternalInteractionLink>);
    const link = element.querySelector('a');
    expect(link?.getAttribute('aria-label')).toBe('Opens example.com in a new tab');
    expect(link?.textContent).toContain('(external)');
    expect(link?.getAttribute('target')).toBe('_blank');
    link?.focus(); expect(document.activeElement).toBe(link);
  });
});
