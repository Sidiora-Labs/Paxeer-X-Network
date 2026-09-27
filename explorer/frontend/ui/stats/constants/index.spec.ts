import { Resolution } from '@blockscout/stats-types';

import { describe, expect, it } from 'vitest';

import {
  getStatsSectionAnchorId,
  getStatsSectionKeys,
  STATS_INTERVALS,
  STATS_OVERVIEW_SECTION,
  STATS_RESOLUTIONS,
} from './index';

describe('STATS_RESOLUTIONS', () => {
  it('keeps the four resolutions the chart queries accept', () => {
    expect(STATS_RESOLUTIONS).toEqual([
      { id: Resolution.DAY, title: 'Day' },
      { id: Resolution.WEEK, title: 'Week' },
      { id: Resolution.MONTH, title: 'Month' },
      { id: Resolution.YEAR, title: 'Year' },
    ]);
  });
});

describe('STATS_INTERVALS', () => {
  it('keeps the interval ids and the labels the selector shows', () => {
    expect(Object.keys(STATS_INTERVALS)).toEqual([ 'all', 'oneMonth', 'threeMonths', 'sixMonths', 'oneYear' ]);
    expect(Object.values(STATS_INTERVALS).map((entry) => entry.title))
      .toEqual([ 'All time', '1 month', '3 months', '6 months', '1 year' ]);
    expect(Object.values(STATS_INTERVALS).map((entry) => entry.shortTitle))
      .toEqual([ 'All time', '1M', '3M', '6M', '1Y' ]);
  });

  it('leaves the whole history open and dates every bounded interval back from today', () => {
    expect(STATS_INTERVALS.all.start).toBeUndefined();
    [ 'oneMonth', 'threeMonths', 'sixMonths', 'oneYear' ].forEach((id) => {
      expect(STATS_INTERVALS[id as keyof typeof STATS_INTERVALS].start).toBeInstanceOf(Date);
    });
  });
});

describe('the section keys', () => {
  it('names the overview section the counters grid sits in', () => {
    expect(STATS_OVERVIEW_SECTION).toEqual({ id: 'overview', title: 'Overview stats' });
  });

  it('anchors a section on its own id so the links into the page keep landing', () => {
    expect(getStatsSectionAnchorId('gas')).toBe('gas');
    expect(getStatsSectionAnchorId(STATS_OVERVIEW_SECTION.id)).toBe('overview');
  });

  it('puts the overview first and the chart sections after it', () => {
    const keys = getStatsSectionKeys([
      { id: 'transactions', title: 'Transactions' },
      { id: 'gas', title: 'Gas' },
    ]);

    expect(keys).toEqual([
      STATS_OVERVIEW_SECTION,
      { id: 'transactions', title: 'Transactions' },
      { id: 'gas', title: 'Gas' },
    ]);
  });

  it('still names the overview when no section has loaded', () => {
    expect(getStatsSectionKeys()).toEqual([ STATS_OVERVIEW_SECTION ]);
  });
});
