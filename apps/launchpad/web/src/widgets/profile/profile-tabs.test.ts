import { describe, it, expect } from 'vitest';
import { MORE_TABS, PRIMARY_TABS, parseProfileTab, profileTabHref } from './profile-tabs';

describe('profile tab query parameter', () => {
  it('maps ?tab= values onto tab keys and defaults to open', () => {
    expect(parseProfileTab(null, true)).toBe('open');
    expect(parseProfileTab(undefined, false)).toBe('open');
    expect(parseProfileTab('', true)).toBe('open');
    expect(parseProfileTab('activity', false)).toBe('activity');
    expect(parseProfileTab('closed', true)).toBe('closed');
    expect(parseProfileTab('positions', true)).toBe('open');
    expect(parseProfileTab('OPEN', true)).toBe('open');
  });
  it('keeps owner-only sections behind the owner flag', () => {
    for (const tab of MORE_TABS) {
      expect(parseProfileTab(tab, true)).toBe(tab);
      expect(parseProfileTab(tab, false)).toBe('open');
    }
    for (const tab of PRIMARY_TABS) expect(parseProfileTab(tab, false)).toBe(tab);
  });
  it('rewrites only the tab parameter and drops it for the default tab', () => {
    const path = '/profile/0x1234567890abcdef1234567890abcdef12345678';
    expect(profileTabHref(path, '', 'activity')).toBe(`${path}?tab=activity`);
    expect(profileTabHref(path, 'ref=abc&tab=closed', 'activity')).toBe(`${path}?ref=abc&tab=activity`);
    expect(profileTabHref(path, 'tab=activity', 'open')).toBe(path);
    expect(profileTabHref(path, 'ref=abc&tab=activity', 'open')).toBe(`${path}?ref=abc`);
  });
});
