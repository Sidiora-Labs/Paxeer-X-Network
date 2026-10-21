// @vitest-environment jsdom

import React from 'react';

import * as paxeerXMock from 'mocks/paxeerX/unifiedAccount';
import { describe, expect, it } from 'vitest';
import { screen } from 'vitest/lib';

import IdentityList from './IdentityList';
import { render } from './testWrapper';

describe('IdentityList', () => {
  it('renders all four spellings of the account', () => {
    render(<IdentityList identities={ paxeerXMock.unifiedAccount.identities }/>);

    expect(screen.getByText('EVM address')).toBeTruthy();
    expect(screen.getByText('Paxeer address')).toBeTruthy();
    expect(screen.getByText('LayerX DID')).toBeTruthy();
    expect(screen.getByText('Kernel account')).toBeTruthy();

    expect(screen.getByText(paxeerXMock.evmAddress)).toBeTruthy();
    expect(screen.getByText(paxeerXMock.paxAddress)).toBeTruthy();
    expect(screen.getByText(paxeerXMock.did)).toBeTruthy();
    expect(screen.getByText(paxeerXMock.kernelAccount)).toBeTruthy();
  });

  it('gives every identity a copy button', () => {
    render(<IdentityList identities={ paxeerXMock.unifiedAccount.identities }/>);

    expect(screen.getAllByLabelText('copy')).toHaveLength(4);
  });

  it('hides identities the account does not have', () => {
    const { container } = render(<IdentityList identities={ paxeerXMock.unifiedAccountEvmOnly.identities }/>);

    expect(screen.getByText('EVM address')).toBeTruthy();
    expect(screen.queryByText('Paxeer address')).toBeNull();
    expect(screen.queryByText('LayerX DID')).toBeNull();
    expect(screen.queryByText('Kernel account')).toBeNull();
    expect(container.querySelectorAll('[data-identity]')).toHaveLength(1);
  });

  it('links the EVM address to its address page', () => {
    const { container } = render(<IdentityList identities={ paxeerXMock.unifiedAccount.identities }/>);

    const link = container.querySelector(`a[href="/address/${ paxeerXMock.evmAddress }"]`);

    expect(link).toBeTruthy();
  });
  it('heads the scan table card with the identity count and its note', () => {
    const { container } = render(<IdentityList identities={ paxeerXMock.unifiedAccount.identities }/>);

    const card = container.querySelector('[data-scan-table-card]');

    expect(card?.querySelector('[data-title]')?.textContent).toBe('A total of 4 identities found');
    expect(card?.querySelector('[data-note]')?.textContent).toBe('Every spelling of this account the node answers for');
  });

  it('renders the rows in the shared table and closes the card with a show-rows footer', () => {
    const { container } = render(<IdentityList identities={ paxeerXMock.unifiedAccount.identities }/>);

    expect(container.querySelector('[data-scan-table-card] [data-label="paxeer-x-identities"]')).toBeTruthy();
    expect(container.querySelector('[data-scan-table-card] [data-footer] [data-scan-show-rows]')).toBeTruthy();
  });

});
