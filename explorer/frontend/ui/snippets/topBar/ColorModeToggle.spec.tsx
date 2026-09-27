// @vitest-environment jsdom

import React from 'react';

import { render } from 'ui/shared/layout/testWrapper';
import { describe, it, expect, vi } from 'vitest';
import { fireEvent, screen } from 'vitest/lib';

vi.hoisted(() => {
  window.__envs = {
    ...window.__envs,
    NEXT_PUBLIC_NAVIGATION_LAYOUT: 'horizontal',
  };
});

import ColorModeToggle from './ColorModeToggle';

describe('ColorModeToggle', () => {
  it('offers the opposite color mode', () => {
    render(<ColorModeToggle/>);

    expect(screen.getByLabelText('Switch to dark theme')).toBeTruthy();
  });

  it('moves the color mode and the page background together', async() => {
    render(<ColorModeToggle/>);

    fireEvent.click(screen.getByLabelText('Switch to dark theme'));

    expect(await screen.findByLabelText('Switch to light theme')).toBeTruthy();
    expect(window.document.documentElement.style.getPropertyValue('--chakra-colors-black')).toBe('#101112');
    expect(window.document.documentElement.style.getPropertyValue('--chakra-colors-theme-bg-primary-_dark')).toBe('#101112');
    // lib/cookies and configs/app import each other, so the name is read once the app config is up
    const { NAMES } = await import('lib/cookies');

    expect(window.localStorage.getItem(NAMES.COLOR_MODE)).toBe('dark');
  });
});
