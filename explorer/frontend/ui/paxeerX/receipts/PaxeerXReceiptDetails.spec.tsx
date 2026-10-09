// @vitest-environment jsdom

import React from 'react';

import type { PaxeerXReceipt } from 'types/api/paxeerXLists';

import dayjs from 'lib/date/dayjs';
import { describe, expect, it } from 'vitest';
import { screen } from 'vitest/lib';

import { render } from '../account/testWrapper';
import PaxeerXReceiptDetails from './PaxeerXReceiptDetails';

// The payload the single-receipt endpoint renders, field for field: the list item fields plus the
// rung of the kernel verification lattice, the payload hash, the emitting transaction and the
// timestamp of the block that holds it.
const receipt: PaxeerXReceipt = {
  id: '0x0000000000000000000000000000000000000000000000000000000000000007',
  account: '0x0000000000000000000000000000000000000000000000000000000000000009',
  status: 'instant',
  block_number: 70,
  verification_status: 'checkpoint_finalised',
  payload_hash: '0x3ed9d81e7c1001bdda1caa1dc62c0acbbe3d2c671cdc20dc1e65efdaa4186967',
  transaction_hash: '0x8f9e7d6c5b4a39281706f5e4d3c2b1a0998877665544332211ffeeddccbbaa99',
  timestamp: '2023-05-22T18:00:36.000000Z',
};

const rowValues = (container: HTMLElement) => {
  const card = container.querySelector('[data-receipt-details-card]') as HTMLElement;
  const values = Array.from(card.querySelectorAll('[data-scan-value]'));

  return Object.fromEntries(Array.from(card.querySelectorAll('[data-scan-key]')).map((key, index) => [ key.textContent ?? '', values[index] ]));
};

describe('PaxeerXReceiptDetails', () => {
  it('renders every field the endpoint returns', () => {
    const { container } = render(<PaxeerXReceiptDetails data={ receipt }/>);

    expect(screen.getByText('Receipt ID')).toBeTruthy();
    expect(container.querySelector('[data-field="id"]')?.textContent).toBe(receipt.id);

    expect(screen.getByText('Kernel account')).toBeTruthy();
    expect(container.querySelector('[data-field="account"]')?.textContent).toBe(receipt.account);

    expect(screen.getByText('Settlement status')).toBeTruthy();

    expect(screen.getByText('Verification status')).toBeTruthy();
    expect(container.querySelector('[data-field="verification_status"]')?.textContent).toBe('Checkpoint finalised');

    expect(screen.getByText('Payload hash')).toBeTruthy();
    expect(container.querySelector('[data-field="payload_hash"]')?.textContent).toBe(receipt.payload_hash);

    expect(screen.getByText('Transaction')).toBeTruthy();
    expect(screen.getByText(receipt.transaction_hash)).toBeTruthy();

    expect(screen.getByText('Block')).toBeTruthy();
    expect(screen.getByText(String(receipt.block_number))).toBeTruthy();

    expect(screen.getByText('Timestamp')).toBeTruthy();
    expect(screen.getByText(dayjs(receipt.timestamp).utc().format('lll'))).toBeTruthy();
  });

  it('puts the receipt on the settlement ladder', () => {
    const { container } = render(<PaxeerXReceiptDetails data={ receipt }/>);

    expect(container.querySelector('[data-rung="instant"]')).toBeTruthy();
  });

  it('links the transaction hash to the transaction page and the block number to the block page', () => {
    const { container } = render(<PaxeerXReceiptDetails data={ receipt }/>);

    const transactionLink = container.querySelector(`a[href="/tx/${ receipt.transaction_hash }"]`);
    const blockLink = container.querySelector(`a[href="/block/${ receipt.block_number }"]`);

    expect(transactionLink).toBeTruthy();
    expect(transactionLink?.textContent).toBe(receipt.transaction_hash);
    expect(blockLink).toBeTruthy();
    expect(blockLink?.textContent).toBe(String(receipt.block_number));
  });

  it('renders every rung of the verification lattice by its own label', () => {
    const labels = {
      unverified: 'Unverified',
      sequencer_signed: 'Sequencer signed',
      batch_included: 'Batch included',
      state_proven: 'State proven',
      checkpoint_finalised: 'Checkpoint finalised',
      settlement_anchored: 'Settlement anchored',
    } as const;

    for (const [ status, label ] of Object.entries(labels)) {
      const { unmount } = render(
        <PaxeerXReceiptDetails data={{ ...receipt, verification_status: status as PaxeerXReceipt['verification_status'] }}/>,
      );

      expect(screen.getByText(label)).toBeTruthy();

      unmount();
    }
  });

  it('marks the account and the payload hash the endpoint returns as null', () => {
    const { container } = render(<PaxeerXReceiptDetails data={{ ...receipt, account: null, payload_hash: null }}/>);

    expect(container.querySelector('[data-field="account"]')?.textContent).toBe('—');
    expect(container.querySelector('[data-field="payload_hash"]')?.textContent).toBe('—');
    expect(screen.queryByText(String(receipt.account))).toBeNull();
    expect(screen.queryByText(String(receipt.payload_hash))).toBeNull();
  });
  it('renders the fields as key-value rows of one surface card', () => {
    const { container } = render(<PaxeerXReceiptDetails data={ receipt }/>);

    const card = container.querySelector('[data-receipt-details] [data-receipt-details-card]') as HTMLElement;

    expect(card).toBeTruthy();

    const keys = Array.from(card.querySelectorAll('[data-scan-key]')).map((key) => key.textContent);

    expect(keys).toEqual([
      'Receipt ID',
      'Kernel account',
      'Settlement status',
      'Verification status',
      'Payload hash',
      'Transaction',
      'Block',
      'Timestamp',
    ]);
    expect(card.querySelectorAll('[data-scan-value]')).toHaveLength(keys.length);
  });

  it('puts a dash in place of the transaction, block and timestamp the endpoint returns as null', () => {
    const data = { ...receipt, transaction_hash: null, block_number: null, timestamp: null } as unknown as PaxeerXReceipt;
    const { container } = render(<PaxeerXReceiptDetails data={ data }/>);

    const values = rowValues(container);

    expect(values.Transaction?.textContent).toBe('—');
    expect(values.Block?.textContent).toBe('—');
    expect(values.Timestamp?.textContent).toBe('—');
    expect(container.querySelector('a[href^="/tx/"]')).toBeNull();
    expect(container.querySelector('a[href^="/block/"]')).toBeNull();
  });

  it('keeps the transaction, block and timestamp entities in their own rows when the endpoint reports them', () => {
    const { container } = render(<PaxeerXReceiptDetails data={ receipt }/>);

    const values = rowValues(container);

    expect(values.Transaction?.querySelector(`a[href="/tx/${ receipt.transaction_hash }"]`)).toBeTruthy();
    expect(values.Block?.querySelector(`a[href="/block/${ receipt.block_number }"]`)).toBeTruthy();
    expect(values.Timestamp?.textContent).toContain(dayjs(receipt.timestamp).utc().format('lll'));
  });

  it('reports a block alone as missing while the transaction and timestamp stay linked', () => {
    const { container } = render(<PaxeerXReceiptDetails data={{ ...receipt, block_number: null }}/>);

    const values = rowValues(container);

    expect(values.Block?.textContent).toBe('—');
    expect(values.Transaction?.querySelector(`a[href="/tx/${ receipt.transaction_hash }"]`)).toBeTruthy();
    expect(values.Timestamp?.textContent).toContain(dayjs(receipt.timestamp).utc().format('lll'));
  });

  it('groups the rows with dividers after the account and the payload hash', () => {
    const { container } = render(<PaxeerXReceiptDetails data={ receipt }/>);

    expect(container.querySelectorAll('[data-receipt-details-card] [data-scan-divider]')).toHaveLength(2);
  });

});
