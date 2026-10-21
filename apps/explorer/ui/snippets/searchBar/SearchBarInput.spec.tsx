// @vitest-environment jsdom

import React from 'react';

import { render } from 'ui/shared/layout/testWrapper';
import { describe, it, expect, vi } from 'vitest';
import { fireEvent } from 'vitest/lib';

vi.hoisted(() => {
  window.__envs = {
    ...window.__envs,
    NEXT_PUBLIC_IS_ACCOUNT_SUPPORTED: 'false',
    NEXT_PUBLIC_MARKETPLACE_CONFIG_URL: '',
  };
});

vi.mock('next/router', async() => (await import('ui/shared/layout/testWrapper')).nextRouterModule());

import SearchBarInput from './SearchBarInput';

describe('SearchBarInput', () => {
  it('hands every typed term to its owner', () => {
    const onChange = vi.fn();
    const { container } = render(<SearchBarInput onChange={ onChange } value=""/>);

    fireEvent.change(container.querySelector('input') as HTMLInputElement, { target: { value: '0xb64a' } });

    expect(onChange).toHaveBeenCalledWith('0xb64a');
  });

  it('submits the form it sits in', () => {
    const onSubmit = vi.fn((event: React.FormEvent<HTMLFormElement>) => event.preventDefault());
    const { container } = render(<SearchBarInput onSubmit={ onSubmit } value="0xb64a"/>);

    fireEvent.submit(container.querySelector('form') as HTMLFormElement);

    expect(onSubmit).toHaveBeenCalledTimes(1);
  });

  it('draws the hero and the header field on the thin product border', () => {
    const { container: hero } = render(<SearchBarInput isHeroBanner value=""/>);
    const { container: header } = render(<SearchBarInput value=""/>);

    [ hero, header ].forEach((container) => {
      const input = container.querySelector('input') as HTMLInputElement;

      expect(input.placeholder).toContain('Search by address');
      expect(window.getComputedStyle(input).borderWidth).not.toBe('2px');
      expect(window.getComputedStyle(input).borderWidth).not.toBe('0px');
    });
  });
});
