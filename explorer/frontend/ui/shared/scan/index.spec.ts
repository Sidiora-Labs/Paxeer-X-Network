// @vitest-environment jsdom

import { describe, expect, it } from 'vitest';

import * as scan from './index';

const COMPONENTS = [
  'ScanDirectionBadge',
  'ScanExpander',
  'ScanKeyValue',
  'ScanMethodChip',
  'ScanPagination',
  'ScanPreviewButton',
  'ScanSectionTabs',
  'ScanShowRows',
  'ScanStatCard',
  'ScanTableCard',
];

describe('the scan primitives barrel', () => {
  it('publishes every primitive the pages build on', () => {
    for (const name of COMPONENTS) {
      expect(scan).toHaveProperty(name);
    }
  });

  it('publishes the two helpers the primitives share', () => {
    expect(scan.SCAN_ROWS_PER_PAGE).toEqual([ 25, 50, 100 ]);
    expect(typeof scan.formatScanTableCount).toBe('function');
  });

  it('publishes nothing else, so a page cannot reach past the primitives', () => {
    expect(Object.keys(scan).sort()).toEqual([ ...COMPONENTS, 'SCAN_ROWS_PER_PAGE', 'formatScanTableCount' ].sort());
  });
});
