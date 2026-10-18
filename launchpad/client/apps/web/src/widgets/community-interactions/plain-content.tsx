import type { ReactNode } from 'react';

// Match server Unicode scalar and UTF-8 bounds; controls and format characters
// are rejected rather than silently changing the signed/submitted content.
export function validateInteractionText(text: string): boolean {
  const scalars = Array.from(text);
  return text.trim().length > 0 && scalars.length <= 2000 &&
    new TextEncoder().encode(text).length <= 8192 &&
    !scalars.some((char) => /[\p{Cf}\p{Cs}]/u.test(char) ||
      (/\p{Cc}/u.test(char) && char !== '\n' && char !== '\t'));
}

export function PlainInteractionContent({ text }: { text: string }): ReactNode {
  return <span style={{ whiteSpace: 'pre-wrap', overflowWrap: 'anywhere' }}>{text}</span>;
}

export function ExternalInteractionLink({ href, children }: { href: string; children: ReactNode }): ReactNode {
  let url: URL;
  try { url = new URL(href); } catch { return <span>{children}</span>; }
  if (!['http:', 'https:'].includes(url.protocol) || url.username !== '' || url.password !== '' || /[\p{Cc}\p{Cf}\p{Cs}]/u.test(href)) return <span>{children}</span>;
  return <a href={url.href} target="_blank" rel="noopener noreferrer" aria-label={`Opens ${url.hostname} in a new tab`}>{children} <small>(external)</small></a>;
}
