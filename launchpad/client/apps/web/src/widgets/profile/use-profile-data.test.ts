import { describe, it, expect } from 'vitest';
import type { ExtendedProfile } from './account-transport';
import { profileCreatedAt, toProfileIdentity } from './use-profile-data';
import { formatJoinedDate } from './profile-stats';
import { localeOptions } from './account-preferences';

const wallet = '0xABCDEFabcdefABCDEFabcdefABCDEFabcdefABCD';
const presentation: ExtendedProfile = { wallet, displayName: 'Ada', bio: 'Builds things', avatarAssetId: null, bannerAssetId: 'b1', website: 'https://example.com', revision: '7', socials: { twitter: 'https://x.com/ada' }, creatorPools: null };

describe('profile identity from GET /v3/profiles/{wallet}', () => {
  it('normalises the presentation and treats a blank display name as absent', () => {
    const identity = toProfileIdentity(presentation);
    expect(identity.wallet).toBe(wallet.toLowerCase());
    expect(identity.displayName).toBe('Ada');
    expect(identity.socials).toEqual({ website: 'https://example.com', twitter: 'https://x.com/ada', telegram: null, discord: null });
    expect(identity.createdAt).toBeNull();
    expect(toProfileIdentity({ ...presentation, displayName: '   ' }).displayName).toBeNull();
  });
  it('exposes createdAt only when the backend publishes a parseable timestamp', () => {
    expect(profileCreatedAt(presentation)).toBeNull();
    expect(profileCreatedAt({ ...presentation, createdAt: 'yesterday' })).toBeNull();
    expect(profileCreatedAt({ ...presentation, createdAt: 42 })).toBeNull();
    expect(profileCreatedAt({ ...presentation, createdAt: '2026-10-12T09:30:00.000Z' })).toBe('2026-10-12T09:30:00.000Z');
    expect(toProfileIdentity({ ...presentation, createdAt: '2026-10-12T09:30:00.000Z' } as ExtendedProfile).createdAt).toBe('2026-10-12T09:30:00.000Z');
    expect(formatJoinedDate('2026-10-12T09:30:00.000Z')).toMatch(/^Oct 1[12], 2026$/);
  });
  it('keeps an unknown saved locale selectable', () => {
    expect(localeOptions('en-US')[0].value).toBe('en-US');
    expect(localeOptions('')[0].value).toBe('en-US');
    expect(localeOptions('xx-YY')[0]).toEqual({ value: 'xx-YY', label: 'xx-YY' });
  });
});
