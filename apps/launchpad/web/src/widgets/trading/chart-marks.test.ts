import { describe, expect, it } from 'vitest';
import { createReplayUdfDatafeed, tradingViewChartMark, ValuationUnavailableError, VALUATION_HISTORY_OPERATION, type ConfirmedChartMarker } from './udf-datafeed';

const pool = `0x${'a'.repeat(40)}`;
const marker: ConfirmedChartMarker = { id: '1m:evt-1', time: 1_760_000_000, candleStart: '2026-10-12T12:00:00.000Z', interval: '1m', side: 'buy', tokenAmountRaw: '1000000', usdlAmountRaw: '2500000', executionPrice: '2.5', transactionHash: `0x${'b'.repeat(64)}`, actor: `0x${'c'.repeat(40)}`, actorBasis: 'event_sender' };

describe('chart mark presentation', () => {
  it('defaults to side initials with a flat fill and no image', () => {
    const mark = tradingViewChartMark(marker);
    expect(mark.label).toBe('B');
    expect(mark.color.border).toBe(mark.color.background);
    expect(mark.borderWidth).toBe(0);
    expect(mark.imageUrl).toBeUndefined();
    expect(mark.text).toContain(marker.transactionHash);
  });
  it('uses profile initials and the avatar image when the actor has one, and the palette the host passes', () => {
    const palette = { buy: 'rgb(68, 232, 200)', sell: 'rgb(255, 99, 103)', label: 'rgb(6, 32, 27)' };
    const decorated = tradingViewChartMark({ ...marker, side: 'sell' }, { enabled: true, palette, decorate: () => ({ name: 'Ada Lovelace', label: 'AL', imageUrl: 'https://kindleusercontent.kindlelaunch.com/users/0xcc/avatar' }) });
    expect(decorated.label).toBe('AL');
    expect(decorated.imageUrl).toBe('https://kindleusercontent.kindlelaunch.com/users/0xcc/avatar');
    expect(decorated.showLabelWhenImageLoaded).toBe(false);
    expect(decorated.color).toEqual({ border: palette.sell, background: palette.sell });
    expect(decorated.labelFontColor).toBe(palette.label);
    expect(decorated.text).toContain('Ada Lovelace');
    const initialsOnly = tradingViewChartMark(marker, { enabled: true, decorate: () => ({ name: '0x1234...5678', label: '0X', imageUrl: null }) });
    expect(initialsOnly.imageUrl).toBeUndefined();
    expect(initialsOnly.label).toBe('0X');
  });
  it('answers getMarks with nothing and no network while markers are switched off', () => {
    const feed = createReplayUdfDatafeed(pool);
    try {
      expect(feed.markPresentation()).toEqual({ enabled: true });
      feed.setMarkPresentation({ enabled: false });
      const results: unknown[][] = [];
      feed.getMarks({}, 0, 2_000_000_000, marks => results.push(marks));
      expect(results).toEqual([[]]);
      expect(feed.resourceCounts().requests).toBe(0);
    } finally { feed.dispose(); }
  });
});

describe('valuation history availability', () => {
  it('carries the real status and the contract operation', () => {
    const error = new ValuationUnavailableError(404, 'https://api.kindlelaunch.com');
    expect(error.status).toBe(404);
    expect(error.operation).toBe(VALUATION_HISTORY_OPERATION);
    expect(error.operation).toBe('GET /v2/udf/history?mode=total_supply_valuation');
    expect(error.message).toBe('Historical valuation: HTTP 404');
    expect(error).toBeInstanceOf(Error);
  });
});
